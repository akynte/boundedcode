package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Runner executes a schedule. It owns no Supervisor semantics: Raw and
// Bounded are adapters, and the independent evaluator is the only source of
// benchmark correctness.
type Runner struct {
	Suite        Suite
	OutputRoot   string
	WorkRoot     string
	Raw          RawAdapter
	Bounded      BoundedAdapter
	Materializer Materializer
	Evaluator    IndependentEvaluator
	Environment  EnvironmentSnapshot
	// Rerun allows a caller to execute a run whose result already exists.
	Rerun bool
	// Logf receives human-readable progress. Nil is quiet.
	Logf func(string, ...any)
}

func (r *Runner) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// Run executes one scheduled run. A returned error is a harness/control error;
// the durable Result is still written whenever a run directory can be made.
func (r *Runner) Run(ctx context.Context, spec RunSpec) (Result, error) {
	if err := r.Suite.Validate(); err != nil {
		return Result{}, err
	}
	t, ok := r.Suite.TaskByID(spec.TaskID)
	if !ok {
		return Result{}, fmt.Errorf("bench: task %q is not in suite", spec.TaskID)
	}
	if spec.SuiteHash != "" && spec.SuiteHash != r.Suite.Hash() {
		return Result{}, fmt.Errorf("bench: run %s belongs to suite hash %s, current suite is %s", spec.RunID, spec.SuiteHash, r.Suite.Hash())
	}
	if r.OutputRoot == "" {
		return Result{}, errors.New("bench: output root is empty")
	}
	runDir := RunDirectory(r.OutputRoot, spec)
	if existing := ResultPath(r.OutputRoot, spec); ResultExists(existing) && !r.Rerun {
		return LoadResult(existing)
	}
	if err := prepareRunDir(runDir); err != nil {
		return Result{}, err
	}
	env := r.Environment
	if env.OS == "" {
		cwd, _ := os.Getwd()
		env = CaptureEnvironment(ctx, cwd)
	}
	result := NewResult(r.Suite, t, spec, env)
	start := time.Now()
	result.Execution.StartTime = start.UTC()
	result.Execution.TimeoutSeconds = t.Limits.WallClock().Seconds()
	result.Artifacts.ResultPath = ResultPath(r.OutputRoot, spec)
	result.Artifacts.SchedulePath = filepath.Join(r.OutputRoot, "schedule.json")
	// Persist the task/config snapshot before any worker runs. It contains the
	// task identity and limits, never evaluator bytes.
	_ = writeJSONArtifact(filepath.Join(runDir, "task.json"), WorkerTask(t))
	_ = writeJSONArtifact(filepath.Join(runDir, "configuration.json"), result.Configuration)

	candidate, err := r.Materializer.Materialize(ctx, t, filepath.Join(runDir, "workspace"))
	if err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = err.Error()
		result.System.ReportedStatus = "not_started"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "materialization_error"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = WriteResult(result.Artifacts.ResultPath, result)
		return result, err
	}
	result.Repository = RepositoryResult{Source: candidate.Repository, ConfiguredBase: candidate.ConfiguredBase,
		BaseCommit: candidate.BaseCommit, BaseTree: candidate.BaseTree,
		StartingCandidate: candidate.StartingManifest, OracleOutside: candidate.OracleOutside,
		StartingStateChecked: true}
	result.Repository.FinalCandidate = candidate.StartingManifest

	workerReq := WorkerRequest{RunID: spec.RunID, PairID: spec.PairID, Task: WorkerTask(t),
		Workspace: filepath.Join(runDir, "workspace"), Model: r.Suite.Model, Limits: t.Limits,
		Network: t.NetworkPolicy, Environment: cloneStrings(t.Environment), Seed: spec.Seed,
		WorkerName: string(spec.Mode) + ":" + r.Suite.Runner.Name}
	workerReq.Workspace = candidate.Path
	workerReq.Limits = effectiveLimits(t.Limits, spec)
	workerReq.RunID = spec.RunID
	workerReq.PairID = spec.PairID
	workerReq.Seed = spec.Seed

	workerCtx, cancel := context.WithTimeout(ctx, workerReq.Limits.WallClock())
	workerOut, workerErr := r.runAdapter(workerCtx, spec.Mode, workerReq)
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(workerErr, context.DeadlineExceeded) || errors.Is(workerCtx.Err(), context.DeadlineExceeded)
	operatorCancelled := errors.Is(ctx.Err(), context.Canceled)
	cancel()
	if operatorCancelled && !timedOut {
		result.Status = RunCancelled
	}

	candidatePath := workerOut.CandidateDir
	if candidatePath == "" {
		candidatePath = candidate.Path
	}
	if _, statErr := os.Stat(candidatePath); statErr != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "worker returned no inspectable candidate: " + statErr.Error()
		result.System.ReportedStatus = "candidate_unavailable"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "candidate_missing"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = WriteResult(result.Artifacts.ResultPath, result)
		return result, errors.New(result.InfrastructureError)
	}
	if err := AssertNoOracle(t, candidatePath); err != nil {
		result.Status = RunInvalid
		result.InfrastructureError = err.Error()
		result.System.ReportedStatus = "oracle_leak"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "oracle_leak"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = WriteResult(result.Artifacts.ResultPath, result)
		return result, err
	}
	finalManifest, manifestErr := ContentManifest(candidatePath)
	if manifestErr != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = manifestErr.Error()
		result.System.ReportedStatus = "candidate_unreadable"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "candidate_unreadable"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = WriteResult(result.Artifacts.ResultPath, result)
		return result, manifestErr
	}
	result.Repository.FinalCandidate = finalManifest
	if diff, diffErr := GitDiff(ctx, candidatePath, candidate.BaseCommit); diffErr == nil {
		result.Repository.ChangedFiles, _ = GitChangedFiles(ctx, candidatePath, candidate.BaseCommit)
		result.Repository.DiffStats = diffStats(diff, result.Repository.ChangedFiles)
		_ = writeTextArtifact(filepath.Join(runDir, "candidate.patch"), diff)
		result.Artifacts.DiffPath = filepath.Join(runDir, "candidate.patch")
	}
	if workerOut.Stdout != "" {
		_ = writeTextArtifact(filepath.Join(runDir, "stdout.log"), workerOut.Stdout)
		result.Artifacts.StdoutPath = filepath.Join(runDir, "stdout.log")
	}
	if workerOut.Stderr != "" {
		_ = writeTextArtifact(filepath.Join(runDir, "stderr.log"), workerOut.Stderr)
		result.Artifacts.StderrPath = filepath.Join(runDir, "stderr.log")
	}

	// Evaluation gets its own bounded context. A timed-out agent does not erase
	// the candidate or prevent a meaningful independent check.
	evalLimit := time.Duration(t.Evaluator.TimeoutSeconds) * time.Second
	if evalLimit <= 0 {
		evalLimit = 5 * time.Minute
	}
	evalBase := context.Background()
	if !operatorCancelled {
		evalBase = context.WithoutCancel(ctx)
	}
	evalCtx, evalCancel := context.WithTimeout(evalBase, evalLimit)
	evaluation := r.Evaluator.Evaluate(evalCtx, t, candidatePath, runDir)
	evalCancel()
	if violations := CheckConstraints(t, result.Repository.ChangedFiles); len(violations) > 0 {
		evaluation.ConstraintFail = violations
		if evaluation.Status == EvaluatorPass {
			evaluation.Status = EvaluatorFail
		}
	}
	result.Evaluator = evaluation
	result.Artifacts.EvaluatorOutputPath = filepath.Join(runDir, "evaluator.json")
	_ = writeJSONArtifact(result.Artifacts.EvaluatorOutputPath, evaluation)
	result.System = SystemResult{ReportedSuccess: workerOut.ReportedSuccess,
		ReportedStatus: workerOut.ReportedStatus, ReportedFailureReason: workerOut.FailureReason,
		BoundedTaskID: workerOut.TaskID, ReportedCandidate: finalManifest}
	result.Execution = executionFromWorker(start, workerReq.Limits, workerOut, workerErr, timedOut)
	result.Events = append(result.Events, workerOut.Events...)
	if timedOut {
		result.Execution.TerminationReason = "timeout"
	} else if workerErr != nil {
		result.Execution.TerminationReason = "worker_error"
	} else {
		result.Execution.TerminationReason = "worker_finished"
	}
	if workerErr != nil && !timedOut && !operatorCancelled {
		result.InfrastructureError = workerErr.Error()
	}
	result.Fingerprints = ComputeFingerprints(r.Suite, t, spec.Mode, env)
	result.Derive()
	result.Classify(workerErr, timedOut)
	if result.InfrastructureError != "" && !timedOut && !operatorCancelled {
		// A worker/control fault is not converted into task failure merely
		// because a partial candidate happened to pass the oracle.
		result.Status = RunInfrastructure
	}
	if operatorCancelled && !timedOut {
		result.Status = RunCancelled
	}
	result.Artifacts.CandidatePath = candidatePath
	result.Artifacts.TranscriptPath = filepath.Join(runDir, "events.json")
	_ = writeJSONArtifact(result.Artifacts.TranscriptPath, result.Events)
	_ = WriteResult(result.Artifacts.ResultPath, result)
	r.logf("bench %s/%s %s: %s", spec.TaskID, spec.Mode, spec.RunID, result.Status)
	return result, nil
}

func effectiveLimits(l Limits, spec RunSpec) Limits {
	if l.WallClockSeconds <= 0 {
		l.WallClockSeconds = int((10 * time.Minute).Seconds())
	}
	if l.Concurrency <= 0 {
		l.Concurrency = 1
	}
	_ = spec
	return l
}

func (r *Runner) runAdapter(ctx context.Context, mode Mode, req WorkerRequest) (WorkerResult, error) {
	switch mode {
	case Raw:
		return r.Raw.Run(ctx, req)
	case Bounded:
		return r.Bounded.Run(ctx, req)
	default:
		return WorkerResult{}, fmt.Errorf("bench: unknown mode %q", mode)
	}
}

func executionFromWorker(start time.Time, limits Limits, out WorkerResult, err error, timedOut bool) ExecutionResult {
	e := ExecutionResult{StartTime: start.UTC(), EndTime: time.Now().UTC(),
		WallClockSeconds: time.Since(start).Seconds(), TimeoutSeconds: limits.WallClock().Seconds(), Concurrency: 1}
	if out.Metrics.GenerationRequests != nil {
		e.GenerationRequests = out.Metrics.GenerationRequests
	}
	e.AssistantTurns, e.ToolCalls, e.Reads, e.Edits = out.Metrics.AssistantTurns, out.Metrics.ToolCalls, out.Metrics.Reads, out.Metrics.Edits
	e.VerificationAttempts, e.Retries = out.Metrics.VerificationAttempts, out.Metrics.Retries
	e.RepeatedFailureCount, e.SessionRestarts, e.Compactions = out.Metrics.RepeatedFailures, out.Metrics.SessionRestarts, out.Metrics.Compactions
	e.InputTokens, e.OutputTokens, e.CachedInputTokens, e.TotalTokens = out.Metrics.InputTokens, out.Metrics.OutputTokens, out.Metrics.CachedInputTokens, out.Metrics.TotalTokens
	if timedOut || errors.Is(err, context.DeadlineExceeded) {
		e.TerminationReason = "timeout"
	} else if err != nil {
		e.TerminationReason = "worker_error"
	} else {
		e.TerminationReason = "worker_finished"
	}
	return e
}

func diffStats(diff string, files []string) DiffStats {
	s := DiffStats{Files: len(files), Bytes: len(diff)}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			s.Added++
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			s.Removed++
		}
	}
	return s
}

func prepareRunDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o750)
	} else if err != nil {
		return err
	}
	// An incomplete run is evidence, not a workspace to reuse. Keep it under a
	// timestamped sibling and start from a new directory.
	old := dir + ".interrupted-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.Rename(dir, old); err != nil {
		return fmt.Errorf("bench: preserve incomplete run %s: %w", dir, err)
	}
	return os.MkdirAll(dir, 0o750)
}

func writeJSONArtifact(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeTextArtifact(path, string(body)+"\n")
}

func writeTextArtifact(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600) //nolint:gosec // benchmark artifact
}

// RunSchedule runs pending entries in their stored order and returns every
// result, including resumed ones. Sequential execution is intentional.
func (r *Runner) RunSchedule(ctx context.Context, schedule Schedule, rerun map[string]bool) ([]Result, error) {
	if schedule.Concurrency > 1 {
		return nil, errors.New("bench: concurrent execution is not enabled; use concurrency=1 for comparable runs")
	}
	pending, err := PendingRuns(schedule, r.OutputRoot, rerun)
	if err != nil {
		return nil, err
	}
	byID := map[string]Result{}
	for _, spec := range schedule.Entries {
		path := ResultPath(r.OutputRoot, spec)
		if ResultExists(path) {
			if old, err := LoadResult(path); err == nil {
				byID[spec.RunID] = old
			}
		}
	}
	for _, spec := range pending {
		result, runErr := r.Run(ctx, spec)
		byID[spec.RunID] = result
		if runErr != nil {
			// Continue the schedule after an infrastructure fault; dropping the
			// remaining cells would make a resume/report silently incomplete.
			r.logf("bench run %s: %v", spec.RunID, runErr)
		}
	}
	out := make([]Result, 0, len(schedule.Entries))
	for _, spec := range schedule.Entries {
		if result, ok := byID[spec.RunID]; ok {
			out = append(out, result)
		}
	}
	return out, nil
}
