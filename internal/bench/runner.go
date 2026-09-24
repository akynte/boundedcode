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

	"github.com/akynte/boundedcode/internal/sandbox"
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
	Manifest     *FrozenManifest
	// Rerun allows a caller to execute a run whose result already exists.
	Rerun bool
	// ForceSmoke is set by the CLI only for an explicitly requested
	// non-official deterministic diagnostic. It never changes an official
	// suite into smoke provenance.
	ForceSmoke bool
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
	if err := r.validateOutputRoot(); err != nil {
		return Result{}, err
	}
	release, err := acquireOutputLock(ctx, r.OutputRoot)
	if err != nil {
		return Result{}, err
	}
	result, runErr := r.run(ctx, spec, r.Rerun)
	return result, errors.Join(runErr, release())
}

func (r *Runner) validateOutputRoot() error {
	return ValidateOutputLocation(r.OutputRoot, r.Suite)
}

func (r *Runner) validateWorkerCandidatePath(runDir, initial, returned string, mode Mode) error {
	if strings.TrimSpace(returned) == "" {
		return errors.New("bench: worker returned an empty candidate path")
	}
	info, err := os.Lstat(returned)
	if err != nil {
		return fmt.Errorf("bench: worker candidate is unavailable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("bench: worker candidate path is a symlink")
	}
	if !info.IsDir() {
		return errors.New("bench: worker candidate path is not a directory")
	}
	returnedResolved := comparablePath(returned)
	initialResolved := comparablePath(initial)
	runResolved := comparablePath(runDir)
	if mode == Raw && returnedResolved != initialResolved {
		return fmt.Errorf("bench: RAW worker returned a candidate outside its materialized workspace: %s", returned)
	}
	if returnedResolved == runResolved {
		return errors.New("bench: worker returned the run directory as its candidate")
	}
	if inside, pathErr := pathWithin(r.OutputRoot, returnedResolved); pathErr != nil {
		return fmt.Errorf("bench: validate worker candidate path: %w", pathErr)
	} else if inside && !strings.HasPrefix(returnedResolved, runResolved+string(filepath.Separator)) {
		return errors.New("bench: worker candidate belongs to another benchmark run")
	}
	if mode == Bounded {
		if insideRun, pathErr := pathWithin(runResolved, returnedResolved); pathErr != nil {
			return fmt.Errorf("bench: validate bounded candidate path: %w", pathErr)
		} else if !insideRun && !strings.Contains(filepath.ToSlash(returnedResolved), "/workspaces/") {
			return errors.New("bench: bounded worker candidate is outside the harness-owned workspace root")
		}
	}
	for _, task := range r.Suite.Tasks {
		if task.OracleRoot() != "" {
			if inside, pathErr := pathWithin(comparablePath(task.OracleRoot()), returnedResolved); pathErr != nil {
				return fmt.Errorf("bench: validate worker candidate against oracle: %w", pathErr)
			} else if inside {
				return errors.New("bench: worker candidate points into evaluator oracle material")
			}
		}
		source := task.ResolvedRepository()
		if source == "" || strings.Contains(source, "://") {
			continue
		}
		if sourceInfo, statErr := os.Stat(source); statErr != nil || !sourceInfo.IsDir() {
			continue
		}
		if inside, pathErr := pathWithin(comparablePath(source), returnedResolved); pathErr != nil {
			return fmt.Errorf("bench: validate worker candidate against source: %w", pathErr)
		} else if inside {
			return fmt.Errorf("bench: worker candidate points into source repository %s", source)
		}
	}
	return nil
}

func (r *Runner) run(ctx context.Context, spec RunSpec, force bool) (Result, error) {
	if err := spec.Validate(); err != nil {
		return Result{}, err
	}
	if err := r.Suite.Validate(); err != nil {
		return Result{}, err
	}
	if r.Suite.Official && r.Manifest == nil {
		return Result{}, errors.New("bench: official suite runs require a verified frozen manifest")
	}
	t, ok := r.Suite.TaskByID(spec.TaskID)
	if !ok {
		return Result{}, fmt.Errorf("bench: task %q is not in suite", spec.TaskID)
	}
	if !spec.Mode.Valid() || strings.TrimSpace(spec.RunID) == "" || strings.TrimSpace(spec.PairID) == "" {
		return Result{}, fmt.Errorf("bench: invalid run specification for %s", spec.RunID)
	}
	if !r.Suite.Smoke && t.WorkerDriver == "" && len(t.MutableScope) == 0 {
		return Result{}, fmt.Errorf("bench: task %q has no mutable_scope; both arms require an explicit write boundary", t.ID)
	}
	if r.Suite.Official {
		r.Bounded.RequireProduction = true
	}
	if r.Suite.Official && r.ForceSmoke {
		return Result{}, errors.New("bench: official suite cannot force the smoke worker")
	}
	if r.Suite.Official && (r.Evaluator.Runner == nil || r.Evaluator.AllowUnconfined) {
		return Result{}, errors.New("bench: official runs require a confined independent evaluator")
	}
	if r.Suite.Official && r.Materializer.AllowUnconfinedSetup {
		return Result{}, errors.New("bench: official runs cannot opt into unconfined setup")
	}
	if !t.Setup.Empty() && r.Materializer.Sandbox != nil && !r.Materializer.AllowUnconfinedSetup {
		if err := RequireConfinedRunner(r.Materializer.Sandbox, sandbox.NetworkNone); err != nil {
			return Result{}, fmt.Errorf("bench: setup sandbox: %w", err)
		}
	}
	if r.Suite.Official && spec.Mode == Raw {
		attestor, ok := r.Raw.Worker.(ModelIdentityAttestor)
		if !ok || !attestor.ModelIdentityAttestable() {
			return Result{}, errors.New("bench: official RAW runs require a worker with served-model identity attestation")
		}
		limits, ok := r.Raw.Worker.(LimitAttestor)
		if !ok || !limits.LimitsAttestable() {
			return Result{}, errors.New("bench: official RAW runs require hard generation/verification limit attestation")
		}
	}
	if r.Manifest != nil {
		if r.Manifest.Official && (r.Suite.Smoke || t.WorkerDriver != "") {
			return Result{}, errors.New("bench: an official manifest cannot run a smoke fixture or worker driver")
		}
		if err := VerifyFrozenAt(ctx, r.Suite, *r.Manifest); err != nil {
			return Result{}, err
		}
		if r.Manifest.ExecutionSeed != spec.Seed {
			return Result{}, fmt.Errorf("bench: run seed %d does not match frozen execution seed %d", spec.Seed, r.Manifest.ExecutionSeed)
		}
	}
	if spec.SuiteHash != "" && spec.SuiteHash != r.Suite.Hash() {
		return Result{}, fmt.Errorf("bench: run %s belongs to suite hash %s, current suite is %s", spec.RunID, spec.SuiteHash, r.Suite.Hash())
	}
	if err := r.validateOutputRoot(); err != nil {
		return Result{}, err
	}
	runDir := RunDirectory(r.OutputRoot, spec)
	if source := t.ResolvedRepository(); source != "" {
		if info, statErr := os.Stat(source); statErr == nil && info.IsDir() {
			if inside, pathErr := pathWithin(comparablePath(source), comparablePath(r.OutputRoot)); pathErr != nil {
				return Result{}, fmt.Errorf("bench: validate output root: %w", pathErr)
			} else if inside {
				return Result{}, fmt.Errorf("bench: output root %s is inside source repository %s", r.OutputRoot, source)
			}
		}
	}
	if existing := ResultPath(r.OutputRoot, spec); !force {
		if old, loadErr := LoadResult(existing); loadErr == nil {
			if !r.resultMatchesSpec(old, spec) {
				return Result{}, fmt.Errorf("bench: existing result %s does not belong to run %s", existing, spec.RunID)
			}
			if old.Status.Evidence() || !old.Status.Retryable() {
				return old, nil
			}
		}
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
	if r.ForceSmoke || t.WorkerDriver == "smoke" {
		result.Smoke = true
		result.Official = false
	}
	result.Evaluator.Independent = r.Evaluator.Runner != nil && !r.Evaluator.AllowUnconfined
	executionID := fmt.Sprintf("%s-%d", spec.RunID, time.Now().UnixNano())
	result.ExecutionID = executionID
	start := time.Now()
	result.Execution.StartTime = start.UTC()
	result.Execution.TimeoutSeconds = t.Limits.WallClock().Seconds()
	result.Artifacts.ResultPath = ResultPath(r.OutputRoot, spec)
	result.Artifacts.SchedulePath = filepath.Join(r.OutputRoot, "schedule.json")
	if r.Manifest != nil {
		result.Official = result.Official || r.Manifest.Official
		result.Smoke = result.Smoke && !r.Manifest.Official
		result.ManifestHash = ManifestHash(*r.Manifest)
		manifestPath := filepath.Join(r.OutputRoot, "manifest.json")
		if err := writeManifestUnlocked(manifestPath, *r.Manifest); err != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "write frozen manifest: " + err.Error()
			result.System.ReportedStatus = "artifact_error"
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, err
		}
		result.Artifacts.ManifestPath = manifestPath
	}
	// Persist the task/config snapshot before any worker runs. It contains the
	// task identity and limits, never evaluator bytes.
	result.Artifacts.TaskSnapshotPath = filepath.Join(runDir, "task.json")
	result.Artifacts.ConfigurationPath = filepath.Join(runDir, "configuration.json")
	taskSnapshot := WorkerTask(t)
	taskSnapshot.Limits = EffectiveLimits(t.Limits)
	taskSnapshot.Environment = mergeStrings(r.Suite.Environment, t.Environment)
	if err := writeJSONArtifact(result.Artifacts.TaskSnapshotPath, taskSnapshot); err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "write task snapshot: " + err.Error()
		result.System.ReportedStatus = "artifact_error"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	if err := writeJSONArtifact(result.Artifacts.ConfigurationPath, result.Configuration); err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "write configuration snapshot: " + err.Error()
		result.System.ReportedStatus = "artifact_error"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}

	materializer := r.Materializer
	materializer.SetupEnvironment = mergeStrings(r.Suite.Environment, r.Materializer.SetupEnvironment)
	candidate, err := materializer.Materialize(ctx, t, filepath.Join(runDir, "workspace"))
	if err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = err.Error()
		result.System.ReportedStatus = "not_started"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "materialization_error"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	result.Repository = RepositoryResult{Source: candidate.Repository, ConfiguredBase: candidate.ConfiguredBase,
		BaseCommit: candidate.BaseCommit, BaseTree: candidate.BaseTree, FixtureHash: candidate.FixtureHash,
		StartingCandidate: candidate.StartingManifest, SetupCandidate: candidate.SetupManifest,
		OracleOutside: candidate.OracleOutside, StartingStateChecked: true}
	result.Repository.FinalCandidate = candidate.StartingManifest
	rawGitFingerprint := ""
	if spec.Mode == Raw {
		rawGitFingerprint, err = GitInspectionFingerprint(candidate.Path)
		if err != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "inspect candidate git metadata: " + err.Error()
			result.System.ReportedStatus = "candidate_unreadable"
			result.Execution.EndTime = time.Now().UTC()
			result.Execution.TerminationReason = "candidate_git_metadata_failed"
			result.Execution.WallClockSeconds = time.Since(start).Seconds()
			result.Derive()
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, err
		}
	}

	workerTask := WorkerTask(t)
	workerTask.Limits = effectiveLimits(t.Limits, spec)
	workerTask.Environment = mergeStrings(r.Suite.Environment, t.Environment)
	workerReq := WorkerRequest{RunID: spec.RunID, ExecutionID: executionID, PairID: spec.PairID, Task: workerTask, Smoke: r.ForceSmoke || r.Suite.Smoke || t.WorkerDriver == "smoke",
		Workspace: filepath.Join(runDir, "workspace"), Model: r.Suite.Model, Limits: workerTask.Limits,
		Network: t.NetworkPolicy, Environment: mergeStrings(r.Suite.Environment, t.Environment), Seed: spec.Seed,
		WorkerName: string(spec.Mode) + ":" + r.Suite.Runner.Name}
	workerReq.Workspace = candidate.Path
	workerReq.RunID = spec.RunID
	workerReq.PairID = spec.PairID
	workerReq.Seed = spec.Seed

	baselineLimit := time.Duration(t.Evaluator.TimeoutSeconds) * time.Second
	if baselineLimit <= 0 {
		baselineLimit = 5 * time.Minute
	}
	if wall := t.Limits.WallClock(); wall > 0 && wall < baselineLimit {
		baselineLimit = wall
	}
	baselineCtx, baselineCancel := context.WithTimeout(ctx, baselineLimit)
	baseline := r.Evaluator.Evaluate(baselineCtx, t, candidate.Path, filepath.Join(runDir, "baseline-evaluator"))
	baselineCancel()
	result.Evaluator.BaselineStatus = baseline.Status
	result.Evaluator.BaselineError = baseline.Error
	result.Evaluator.BaselineMS = baseline.DurationMS
	baselineDisqualified := baseline.Status != EvaluatorFail
	if baselineDisqualified {
		result.InfrastructureError = "evaluator baseline did not fail on the pristine candidate: " + string(baseline.Status)
		if r.Suite.Official {
			result.Status = RunInvalid
			result.System.ReportedStatus = "evaluator_baseline_qualified"
			result.Execution.EndTime = time.Now().UTC()
			result.Execution.TerminationReason = "evaluator_baseline_qualified"
			result.Execution.WallClockSeconds = time.Since(start).Seconds()
			result.Derive()
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, errors.New(result.InfrastructureError)
		}
	}

	workerStart := time.Now()
	workerCtx, cancel := context.WithTimeout(ctx, workerReq.Limits.WallClock())
	workerOut, workerErr := r.runAdapter(workerCtx, spec.Mode, workerReq)
	if metricErr := validateWorkerMetrics(workerOut, workerReq.Limits); metricErr != nil {
		workerErr = errors.Join(workerErr, metricErr)
	}
	if r.Suite.Official && !workerOut.ModelVerified {
		workerErr = errors.Join(workerErr, errors.New("bench: official worker did not return an independently observed served model identity"))
	}
	leaked := leakedCanary(t, string(mustJSON(workerReq))+"\n"+workerOut.Stdout+"\n"+workerOut.Stderr+"\n"+workerOut.FailureReason+"\n"+string(mustJSON(workerOut.Events)))
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(workerErr, context.DeadlineExceeded) || errors.Is(workerCtx.Err(), context.DeadlineExceeded)
	operatorCancelled := errors.Is(ctx.Err(), context.Canceled)
	cancel()
	if workerOut.ModelVerified && workerReq.Model.Model != "" {
		if err := observedModelMismatch(workerReq.Model, workerOut.Model); err != nil {
			workerErr = errors.Join(workerErr, err)
			workerOut.ModelVerified = false
		}
	}
	workerExecution := executionFromWorker(workerStart, workerReq.Limits, workerOut, workerErr, timedOut)
	if operatorCancelled && !timedOut {
		result.Status = RunCancelled
	}

	if !workerOut.WorkerStarted {
		if workerErr == nil {
			workerErr = errors.New("bench: worker returned without starting")
		}
		result.Status = RunInfrastructure
		result.InfrastructureError = workerErr.Error()
		result.System = SystemResult{ReportedStatus: "worker_not_started", ReportedFailureReason: RedactText(workerErr.Error())}
		result.Execution = workerExecution
		result.Execution.TerminationReason = "worker_not_started"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, workerErr
	}
	candidatePath := workerOut.CandidateDir
	if candidatePath == "" {
		candidatePath = candidate.Path
	}
	if pathErr := r.validateWorkerCandidatePath(runDir, candidate.Path, candidatePath, spec.Mode); pathErr != nil {
		result.Status = RunInvalid
		result.InfrastructureError = pathErr.Error()
		result.System.ReportedStatus = "candidate_untrusted"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "candidate_untrusted"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, pathErr
	}
	if spec.Mode == Raw {
		current, fingerprintErr := GitInspectionFingerprint(candidatePath)
		if fingerprintErr != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "inspect candidate git metadata: " + fingerprintErr.Error()
			result.System.ReportedStatus = "candidate_unreadable"
			result.Execution.EndTime = time.Now().UTC()
			result.Execution.TerminationReason = "candidate_git_metadata_failed"
			result.Execution.WallClockSeconds = time.Since(start).Seconds()
			result.Derive()
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, fingerprintErr
		}
		if current != rawGitFingerprint {
			err := errors.New("bench: RAW worker changed trusted Git metadata")
			result.Status = RunInvalid
			result.InfrastructureError = err.Error()
			result.System.ReportedStatus = "git_metadata_changed"
			result.Execution.EndTime = time.Now().UTC()
			result.Execution.TerminationReason = "git_metadata_changed"
			result.Execution.WallClockSeconds = time.Since(start).Seconds()
			result.Derive()
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, err
		}
	}
	if err := rejectOracleInHistory(ctx, candidatePath, t); err != nil {
		result.Status = RunInvalid
		result.InfrastructureError = err.Error()
		result.System.ReportedStatus = "oracle_history_leak"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "oracle_history_leak"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	if err := AssertNoOracle(t, candidatePath); err != nil {
		result.Status = RunInvalid
		result.InfrastructureError = err.Error()
		result.System.ReportedStatus = "oracle_leak"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "oracle_leak"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	if baseErr := VerifyBaseRevision(ctx, candidatePath, candidate.BaseCommit, candidate.BaseTree, spec.Mode == Raw); baseErr != nil {
		result.Status = RunInvalid
		result.InfrastructureError = baseErr.Error()
		result.System.ReportedStatus = "base_revision_changed"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "base_revision_changed"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, baseErr
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
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, manifestErr
	}
	result.Repository.FinalCandidate = finalManifest
	diff, diffErr := GitDiff(ctx, candidatePath, candidate.BaseCommit)
	if diffErr != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "inspect candidate diff: " + diffErr.Error()
		result.System.ReportedStatus = "candidate_unreadable"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "candidate_diff_failed"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, diffErr
	}
	changedFiles, changedErr := GitChangedFiles(ctx, candidatePath, candidate.BaseCommit)
	if changedErr != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "list candidate changes: " + changedErr.Error()
		result.System.ReportedStatus = "candidate_unreadable"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "candidate_diff_failed"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, changedErr
	}
	effectiveChangedFiles := changedFiles
	if spec.Mode == Bounded {
		effectiveChangedFiles = filterBenchmarkControlFiles(changedFiles)
	}
	result.Repository.ChangedFiles = effectiveChangedFiles
	result.Repository.DiffStats = diffStats(diff, effectiveChangedFiles)
	if err := writeTextArtifact(filepath.Join(runDir, "candidate.patch"), RedactText(diff)); err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "write candidate patch: " + err.Error()
		result.System.ReportedStatus = "artifact_error"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	result.Artifacts.DiffPath = filepath.Join(runDir, "candidate.patch")
	// A production task runner may keep its authoritative checkout in the
	// store-owned worktree area. Copy the final candidate into this run's
	// durable artifact tree before closing the per-run store, so later
	// evaluation and forensics do not depend on a live control-plane root.
	// Never remove the adapter-supplied path: it may belong to a production
	// store or another owner. Only this run's copied artifact is harness-owned.
	evaluationCandidate := candidatePath
	if inside, pathErr := pathWithin(comparablePath(runDir), comparablePath(candidatePath)); pathErr != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "validate candidate path: " + pathErr.Error()
		result.System.ReportedStatus = "candidate_unreadable"
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, pathErr
	} else if spec.Mode == Bounded || !inside {
		copied := filepath.Join(runDir, "final-candidate")
		if copyErr := CopyDirectory(candidatePath, copied); copyErr != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "preserve bounded candidate: " + copyErr.Error()
			result.System.ReportedStatus = "candidate_unreadable"
			result.Derive()
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, copyErr
		}
		if spec.Mode == Bounded {
			if stripErr := stripBenchmarkControlArtifacts(copied); stripErr != nil {
				result.Status = RunInfrastructure
				result.InfrastructureError = "remove bounded control artifacts: " + stripErr.Error()
				result.System.ReportedStatus = "candidate_unreadable"
				result.Derive()
				_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
				return result, stripErr
			}
		}
		evaluationCandidate = copied
	}
	if evaluationCandidate != candidatePath {
		if evaluatedManifest, manifestErr := ContentManifest(evaluationCandidate); manifestErr == nil {
			result.Repository.FinalCandidate = evaluatedManifest
		}
	}
	if leaked != "" {
		result.Status = RunInvalid
		result.InfrastructureError = "hidden evaluator canary reached worker-visible material"
		result.System.ReportedStatus = "oracle_leak"
		result.Execution.EndTime = time.Now().UTC()
		result.Execution.TerminationReason = "oracle_leak"
		result.Execution.WallClockSeconds = time.Since(start).Seconds()
		result.Derive()
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, errors.New(result.InfrastructureError)
	}
	if workerOut.Stdout != "" {
		if err := writeTextArtifact(filepath.Join(runDir, "stdout.log"), RedactText(workerOut.Stdout)); err != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "write stdout artifact: " + err.Error()
			result.System.ReportedStatus = "artifact_error"
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, err
		}
		result.Artifacts.StdoutPath = filepath.Join(runDir, "stdout.log")
	}
	if workerOut.Stderr != "" {
		if err := writeTextArtifact(filepath.Join(runDir, "stderr.log"), RedactText(workerOut.Stderr)); err != nil {
			result.Status = RunInfrastructure
			result.InfrastructureError = "write stderr artifact: " + err.Error()
			result.System.ReportedStatus = "artifact_error"
			_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
			return result, err
		}
		result.Artifacts.StderrPath = filepath.Join(runDir, "stderr.log")
	}

	// Evaluation gets its own bounded context. A timed-out agent does not erase
	// the candidate or prevent a meaningful independent check.
	evalLimit := time.Duration(t.Evaluator.TimeoutSeconds) * time.Second
	if evalLimit <= 0 {
		evalLimit = 5 * time.Minute
	}
	evalBase := context.WithoutCancel(ctx)
	if ctx.Err() != nil {
		evalBase = ctx
	}
	evalCtx, evalCancel := context.WithTimeout(evalBase, evalLimit)
	evaluation := r.Evaluator.Evaluate(evalCtx, t, evaluationCandidate, runDir)
	evalCancel()
	if violations := CheckConstraints(t, result.Repository.ChangedFiles); len(violations) > 0 {
		evaluation.ConstraintFail = append(evaluation.ConstraintFail, violations...)
		if evaluation.Status == EvaluatorPass {
			evaluation.Status = EvaluatorFail
		}
	}
	if violations := CheckMutableScope(t.MutableScope, result.Repository.ChangedFiles); len(violations) > 0 {
		evaluation.ConstraintFail = append(evaluation.ConstraintFail, violations...)
		if evaluation.Status == EvaluatorPass {
			evaluation.Status = EvaluatorFail
		}
	}
	baselineStatus, baselineError, baselineMS := result.Evaluator.BaselineStatus, result.Evaluator.BaselineError, result.Evaluator.BaselineMS
	result.Evaluator = evaluation
	result.Evaluator.BaselineStatus, result.Evaluator.BaselineError, result.Evaluator.BaselineMS = baselineStatus, baselineError, baselineMS
	result.Artifacts.EvaluatorOutputPath = filepath.Join(runDir, "evaluator.json")
	if err := writeJSONArtifact(result.Artifacts.EvaluatorOutputPath, evaluation); err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "write evaluator artifact: " + err.Error()
		result.System.ReportedStatus = "artifact_error"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	if workerOut.ModelVerified {
		// A served response is a new observation, not an overlay on the
		// configured route. Clear fields the adapter could not observe instead
		// of carrying a configured model-file hash or sampling value forward as
		// if the provider had attested it.
		observed := workerOut.Model
		result.Model = ModelResult{
			Provider: observed.Provider, Model: observed.Model, Runtime: observed.Runtime,
			ModelFile: observed.ModelFile, ModelFileSHA256: observed.ModelFileSHA256,
			Quantization: observed.Quantization, ContextTokens: intPtr(observed.ContextTokens),
			Temperature: cloneFloat(observed.Temperature), TopP: cloneFloat(observed.TopP),
			TopK: cloneInt(observed.TopK), Seed: cloneInt64(observed.Seed),
			IdentityVerified: observed.Provider != "" && observed.Model != "" && observed.ContextTokens > 0,
			Limitations:      []string{},
		}
		if result.Model.ModelFileSHA256 == "" {
			result.Model.Limitations = append(result.Model.Limitations, "model file hash unavailable")
		}
		if result.Model.Runtime == "" {
			result.Model.Limitations = append(result.Model.Limitations, "served runtime identity unavailable")
		}
		if result.Model.ContextTokens == nil {
			result.Model.Limitations = append(result.Model.Limitations, "served context window unavailable")
		}
		if result.Model.Temperature == nil {
			result.Model.Limitations = append(result.Model.Limitations, "served sampling temperature unavailable")
		}
		if result.Model.TopP == nil {
			result.Model.Limitations = append(result.Model.Limitations, "served sampling top_p unavailable")
		}
		if result.Model.TopK == nil {
			result.Model.Limitations = append(result.Model.Limitations, "served sampling top_k unavailable")
		}
		if result.Model.Seed == nil {
			result.Model.Limitations = append(result.Model.Limitations, "served sampling seed unavailable")
		}
	} else {
		result.Model.IdentityVerified = false
		result.Model.Limitations = append(result.Model.Limitations, "served model identity was not independently observed")
	}
	if leaked != "" {
		result.InfrastructureError = "hidden evaluator canary reached worker-visible material"
	}
	result.System = SystemResult{ReportedSuccess: workerOut.ReportedSuccess,
		ReportedStatus: workerOut.ReportedStatus, ReportedFailureReason: RedactText(workerOut.FailureReason),
		BoundedTaskID: workerOut.TaskID, ReportedCandidate: finalManifest}
	result.Execution = workerExecution
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
	if leaked != "" {
		result.Status = RunInvalid
		result.Execution.TerminationReason = "oracle_leak"
	}
	result.Artifacts.CandidatePath = evaluationCandidate
	result.Artifacts.TranscriptPath = filepath.Join(runDir, "events.json")
	if err := writeJSONArtifact(result.Artifacts.TranscriptPath, result.Events); err != nil {
		result.Status = RunInfrastructure
		result.InfrastructureError = "write event transcript: " + err.Error()
		result.System.ReportedStatus = "artifact_error"
		_ = writeResultUnlocked(result.Artifacts.ResultPath, result)
		return result, err
	}
	if err := writeResultUnlocked(result.Artifacts.ResultPath, result); err != nil {
		return result, err
	}
	r.logf("bench %s/%s %s: %s", spec.TaskID, spec.Mode, spec.RunID, result.Status)
	return result, nil
}

func (r *Runner) resultMatchesSpec(result Result, spec RunSpec) bool {
	if !resultMatchesSpec(result, spec) {
		return false
	}
	return r.Manifest == nil || result.ManifestHash == ManifestHash(*r.Manifest)
}

func observedModelMismatch(expected, observed ModelConfig) error {
	if expected.Provider != "" && observed.Provider != expected.Provider {
		return fmt.Errorf("bench: observed provider %q does not match frozen provider %q", observed.Provider, expected.Provider)
	}
	if observed.Model != expected.Model {
		return fmt.Errorf("bench: observed model %q does not match frozen model %q", observed.Model, expected.Model)
	}
	if expected.ContextTokens > 0 && observed.ContextTokens > 0 && observed.ContextTokens != expected.ContextTokens {
		return fmt.Errorf("bench: observed context window %d does not match frozen context window %d", observed.ContextTokens, expected.ContextTokens)
	}
	if expected.Runtime != "" && observed.Runtime != "" && observed.Runtime != expected.Runtime {
		return fmt.Errorf("bench: observed runtime %q does not match frozen runtime %q", observed.Runtime, expected.Runtime)
	}
	return nil
}

func effectiveLimits(l Limits, spec RunSpec) Limits {
	_ = spec
	return EffectiveLimits(l)
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

func validateWorkerMetrics(out WorkerResult, limits Limits) error {
	if out.Metrics.GenerationRequests != nil && *out.Metrics.GenerationRequests < 0 {
		return errors.New("worker reported a negative generation count")
	}
	if limits.MaxGenerationRequests > 0 && out.Metrics.GenerationRequests != nil && *out.Metrics.GenerationRequests > limits.MaxGenerationRequests {
		return fmt.Errorf("worker exceeded generation budget: %d > %d", *out.Metrics.GenerationRequests, limits.MaxGenerationRequests)
	}
	if out.Metrics.VerificationAttempts != nil && *out.Metrics.VerificationAttempts < 0 {
		return errors.New("worker reported a negative verification count")
	}
	if limits.MaxVerificationAttempts > 0 && out.Metrics.VerificationAttempts != nil && *out.Metrics.VerificationAttempts > limits.MaxVerificationAttempts {
		return fmt.Errorf("worker exceeded verification budget: %d > %d", *out.Metrics.VerificationAttempts, limits.MaxVerificationAttempts)
	}
	if limits.MaxTokens > 0 && out.Metrics.TotalTokens != nil && *out.Metrics.TotalTokens > int64(limits.MaxTokens) {
		return fmt.Errorf("worker exceeded token budget: %d > %d", *out.Metrics.TotalTokens, limits.MaxTokens)
	}
	return nil
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
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	clean, err := json.MarshalIndent(redactJSON(decoded), "", "  ")
	if err != nil {
		return err
	}
	return writeTextArtifact(path, string(clean)+"\n")
}

func writeTextArtifact(path, body string) error {
	return writeAtomicFile(path, []byte(body), 0o600)
}

// RunSchedule runs pending entries in their stored order and returns every
// result, including resumed ones. Sequential execution is intentional.
func (r *Runner) RunSchedule(ctx context.Context, schedule Schedule, rerun map[string]bool) (out []Result, retErr error) {
	if schedule.SuiteHash != r.Suite.Hash() || schedule.SuiteID != r.Suite.ID || schedule.SuiteVersion != r.Suite.Version {
		return nil, fmt.Errorf("bench: schedule belongs to %s/%s, current suite is %s/%s", schedule.SuiteID, schedule.SuiteHash, r.Suite.ID, r.Suite.Hash())
	}
	if schedule.Concurrency > 1 {
		return nil, errors.New("bench: concurrent execution is not enabled; use concurrency=1 for comparable runs")
	}
	if r.Manifest != nil && schedule.Seed != r.Manifest.ExecutionSeed {
		return nil, fmt.Errorf("bench: schedule seed %d does not match frozen execution seed %d", schedule.Seed, r.Manifest.ExecutionSeed)
	}
	if r.Manifest != nil {
		if err := VerifyFrozenAt(ctx, r.Suite, *r.Manifest); err != nil {
			return nil, err
		}
	}
	if err := r.validateOutputRoot(); err != nil {
		return nil, err
	}
	release, err := acquireOutputLock(ctx, r.OutputRoot)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, release()) }()
	if err := writeScheduleUnlocked(filepath.Join(r.OutputRoot, "schedule.json"), schedule); err != nil {
		return nil, err
	}
	if r.Rerun && rerun == nil {
		rerun = map[string]bool{}
		for _, spec := range schedule.Entries {
			rerun[spec.RunID] = true
		}
	}
	for _, spec := range schedule.Entries {
		path := ResultPath(r.OutputRoot, spec)
		if old, err := LoadResult(path); err == nil && !r.resultMatchesSpec(old, spec) {
			return nil, fmt.Errorf("bench: existing result %s does not belong to the frozen run", path)
		}
	}
	pending, err := PendingRuns(schedule, r.OutputRoot, rerun)
	if err != nil {
		return nil, err
	}
	byID := map[string]Result{}
	var runErrs []error
	for _, spec := range schedule.Entries {
		path := ResultPath(r.OutputRoot, spec)
		if old, err := LoadResult(path); err == nil && r.resultMatchesSpec(old, spec) &&
			(old.Status.Evidence() || !old.Status.Retryable()) {
			byID[spec.RunID] = old
		}
	}
	for _, spec := range pending {
		if err := ctx.Err(); err != nil {
			runErrs = append(runErrs, err)
			break
		}
		result, runErr := r.run(ctx, spec, r.Rerun || rerun[spec.RunID])
		byID[spec.RunID] = result
		if runErr != nil {
			// Continue the schedule after an infrastructure fault; dropping the
			// remaining cells would make a resume/report silently incomplete.
			runErrs = append(runErrs, fmt.Errorf("bench run %s: %w", spec.RunID, runErr))
			r.logf("bench run %s: %v", spec.RunID, runErr)
		}
	}
	out = make([]Result, 0, len(schedule.Entries))
	for _, spec := range schedule.Entries {
		if result, ok := byID[spec.RunID]; ok {
			out = append(out, result)
		}
	}
	return out, errors.Join(runErrs...)
}
