package bench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EvaluatorStatus is intentionally independent of task.Runner's recipe status.
type EvaluatorStatus string

const (
	EvaluatorPass    EvaluatorStatus = "PASS"
	EvaluatorFail    EvaluatorStatus = "FAIL"
	EvaluatorError   EvaluatorStatus = "ERROR"
	EvaluatorTimeout EvaluatorStatus = "TIMEOUT"
)

// EvaluatorStep is one independently executed check against the final
// candidate. ExitCode is a pointer because a command that could not start has
// no exit code; writing zero there would look like a passing test.
type EvaluatorStep struct {
	Name       string          `json:"name"`
	Command    []string        `json:"command"`
	Status     EvaluatorStatus `json:"status"`
	ExitCode   *int            `json:"exit_code"`
	DurationMS int64           `json:"duration_ms"`
	Stdout     string          `json:"stdout,omitempty"`
	Stderr     string          `json:"stderr,omitempty"`
	Error      string          `json:"error,omitempty"`
	OracleUsed bool            `json:"oracle_used"`
}

// EvaluatorResult is the benchmark's correctness decision. The system's
// reported status is deliberately not accepted here as an input.
type EvaluatorResult struct {
	Status         EvaluatorStatus `json:"status"`
	ExitCode       *int            `json:"exit_code"`
	DurationMS     int64           `json:"duration_ms"`
	Steps          []EvaluatorStep `json:"steps"`
	OracleHash     string          `json:"oracle_hash"`
	ConstraintFail []string        `json:"constraint_failures,omitempty"`
	Error          string          `json:"error,omitempty"`
	Independent    bool            `json:"independent"`
}

// IndependentEvaluator runs task-declared checks in a separate copy of the
// candidate. It is a process boundary from the worker's writable directory;
// the oracle is copied only into this evaluator workspace.
type IndependentEvaluator struct {
	// Timeout bounds setup and all steps combined when the task does not name a
	// step timeout. Zero uses the task value or a conservative default.
	Timeout time.Duration
	// MaxOutput bounds each captured stream.
	MaxOutput int
}

const defaultEvaluatorTimeout = 5 * time.Minute

func (e IndependentEvaluator) Evaluate(ctx context.Context, t Task, candidateDir, artifactDir string) EvaluatorResult {
	start := time.Now()
	result := EvaluatorResult{Independent: true, Steps: []EvaluatorStep{}, OracleHash: t.hiddenHash}
	if candidateDir == "" {
		result.Status, result.Error = EvaluatorError, "candidate directory is empty"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	evalDir := filepath.Join(artifactDir, "evaluator-workspace")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		result.Status, result.Error = EvaluatorError, err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if err := CopyDirectory(candidateDir, evalDir); err != nil {
		result.Status, result.Error = EvaluatorError, "copy candidate for evaluator: "+err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	oracleDir := filepath.Join(artifactDir, "oracle")
	if err := materializeEvaluatorOracle(t, oracleDir); err != nil {
		result.Status, result.Error = EvaluatorError, "materialize oracle: "+err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	for name, body := range t.hiddenFiles {
		if err := copyFile(filepath.Join(evalDir, filepath.FromSlash(name)), body); err != nil {
			result.Status, result.Error = EvaluatorError, "write evaluator file "+name+": "+err.Error()
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
	}

	steps := evaluatorCommands(t)
	if len(steps) == 0 {
		result.Status, result.Error = EvaluatorError, "evaluator declares no command"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	limit := e.Timeout
	if limit <= 0 {
		limit = time.Duration(t.Evaluator.TimeoutSeconds) * time.Second
	}
	if limit <= 0 {
		limit = defaultEvaluatorTimeout
	}
	evalCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	for _, spec := range steps {
		step := EvaluatorStep{Name: spec.name, Command: append([]string(nil), spec.command.Argv...), OracleUsed: usesOracle(spec.command.Argv)}
		stepStart := time.Now()
		stepCtx := evalCtx
		cancelStep := func() {}
		if spec.timeout > 0 {
			stepCtx, cancelStep = context.WithTimeout(evalCtx, spec.timeout)
		}
		status, code, stdout, stderr, err := e.runStep(stepCtx, spec.command, evalDir, oracleDir, t.Evaluator.Environment)
		cancelStep()
		step.DurationMS = time.Since(stepStart).Milliseconds()
		step.Status, step.ExitCode, step.Stdout, step.Stderr = status, code, truncateOutput(stdout, e.maxOutput()), truncateOutput(stderr, e.maxOutput())
		if err != nil {
			step.Error = err.Error()
		}
		result.Steps = append(result.Steps, step)
		if step.Status == EvaluatorError || step.Status == EvaluatorTimeout {
			result.Status = step.Status
			result.ExitCode = code
			result.Error = step.Error
			break
		}
		if step.Status == EvaluatorFail && spec.required {
			result.Status = EvaluatorFail
			result.ExitCode = code
			break
		}
	}
	if result.Status == "" {
		result.Status = EvaluatorPass
	}
	if evalCtx.Err() != nil && result.Status != EvaluatorError {
		result.Status = EvaluatorTimeout
		if result.Error == "" {
			result.Error = evalCtx.Err().Error()
		}
	}
	result.DurationMS = time.Since(start).Milliseconds()
	if result.ExitCode == nil && len(result.Steps) > 0 {
		result.ExitCode = result.Steps[len(result.Steps)-1].ExitCode
	}
	return result
}

type evaluatorCommand struct {
	name     string
	command  Command
	timeout  time.Duration
	required bool
}

func evaluatorCommands(t Task) []evaluatorCommand {
	if len(t.Evaluator.Steps) > 0 {
		out := make([]evaluatorCommand, 0, len(t.Evaluator.Steps))
		for _, step := range t.Evaluator.Steps {
			required := true
			if step.Required != nil {
				required = *step.Required
			}
			out = append(out, evaluatorCommand{name: step.Name, command: step.Command,
				timeout: time.Duration(step.TimeoutSeconds) * time.Second, required: required})
		}
		return out
	}
	if t.Evaluator.Command.Empty() {
		return nil
	}
	return []evaluatorCommand{{name: "evaluator", command: t.Evaluator.Command, required: true}}
}

func (e IndependentEvaluator) maxOutput() int {
	if e.MaxOutput > 0 {
		return e.MaxOutput
	}
	return 1 << 20
}

func (e IndependentEvaluator) runStep(ctx context.Context, command Command, dir, oracleDir string, extra map[string]string) (EvaluatorStatus, *int, string, string, error) {
	argv := append([]string(nil), command.Argv...)
	if command.Shell {
		argv = append([]string{"/bin/sh", "-c"}, argv...)
	} else {
		argv[0] = resolveEvaluatorArgv0(argv[0], oracleDir)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv comes from the frozen evaluator definition
	cmd.Dir = dir
	cmd.Env = evaluatorEnv(extra, oracleDir, dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return EvaluatorTimeout, nil, stdout.String(), stderr.String(), ctx.Err()
	}
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return EvaluatorError, nil, stdout.String(), stderr.String(), err
		}
		code := exitCode(err)
		return EvaluatorFail, &code, stdout.String(), stderr.String(), err
	}
	code := 0
	return EvaluatorPass, &code, stdout.String(), stderr.String(), nil
}

func evaluatorEnv(extra map[string]string, oracleDir, candidate string) []string {
	// Go deliberately refuses to treat a module below its configured system
	// temp root as a module. Keep HOME/TMPDIR in a sibling scratch directory,
	// not in the candidate root, or `go test` would reject an otherwise valid
	// oracle for an environmental reason.
	tmp := filepath.Join(candidate, ".bench-evaluator-tmp")
	_ = os.MkdirAll(tmp, 0o700)
	env := []string{
		"HOME=" + tmp, "TMPDIR=" + tmp, "GOTMPDIR=" + tmp, "PATH=" + os.Getenv("PATH"),
		"LANG=C", "LC_ALL=C", "NO_COLOR=1", "TERM=dumb",
		"BENCH_ORACLE_DIR=" + oracleDir, "BENCH_CANDIDATE_DIR=" + candidate,
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		if validEnvName(k) {
			env = append(env, k+"="+extra[k])
		}
	}
	return env
}

func resolveEvaluatorArgv0(arg, oracleDir string) string {
	if arg == "./oracle" || strings.HasPrefix(arg, "./oracle/") {
		return filepath.Join(oracleDir, strings.TrimPrefix(strings.TrimPrefix(arg, "./"), "oracle/"))
	}
	if arg == "oracle" || strings.HasPrefix(arg, "oracle/") {
		return filepath.Join(oracleDir, strings.TrimPrefix(arg, "oracle/"))
	}
	return arg
}

func usesOracle(argv []string) bool {
	for _, arg := range argv {
		if strings.HasPrefix(arg, "oracle/") || strings.HasPrefix(arg, "./oracle/") || strings.Contains(arg, "BENCH_ORACLE_DIR") {
			return true
		}
	}
	return false
}

func materializeEvaluatorOracle(t Task, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for name, body := range t.hiddenFiles {
		if err := copyFile(filepath.Join(dst, filepath.FromSlash(name)), body); err != nil {
			return err
		}
	}
	if t.OracleRoot() == "" {
		return nil
	}
	// Copy only evaluator material, never the candidate. Symlinks are skipped
	// so an oracle cannot point at the worker's workspace.
	return filepath.WalkDir(t.OracleRoot(), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(t.OracleRoot(), p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(p) //nolint:gosec // path is walked beneath the evaluator oracle root
		if err != nil {
			return err
		}
		return copyFile(filepath.Join(dst, rel), body)
	})
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func truncateOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}

// CheckConstraints evaluates declarative paths that must not change. It is
// kept separate from command status so a candidate cannot pass by deleting a
// protected test.
func CheckConstraints(t Task, changed []string) []string {
	var failures []string
	for _, protected := range t.Evaluator.MustNotChange {
		for _, path := range changed {
			if path == protected || strings.HasPrefix(path, strings.TrimSuffix(protected, "/")+"/") || path == strings.TrimSuffix(protected, "/") {
				failures = append(failures, path)
				break
			}
		}
	}
	return sortStrings(failures)
}
