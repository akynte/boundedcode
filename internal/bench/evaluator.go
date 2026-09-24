package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/sandbox"
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
	BaselineStatus EvaluatorStatus `json:"baseline_status,omitempty"`
	BaselineError  string          `json:"baseline_error,omitempty"`
	BaselineMS     int64           `json:"baseline_duration_ms,omitempty"`
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
	// Runner confines evaluator commands to the copied candidate, its private
	// oracle copy, and private temporary state. A nil Runner is rejected by
	// default; callers that consciously accept an unconfined evaluator for a
	// trusted, non-benchmark diagnostic must opt in explicitly.
	Runner sandbox.Runner
	// AllowUnconfined is intentionally explicit and is never set by the CLI.
	// It exists for small programmatic diagnostics that need the historical
	// direct-execution behavior without making an official benchmark silently
	// run its oracle outside a process boundary.
	AllowUnconfined bool
}

const defaultEvaluatorTimeout = 5 * time.Minute

func (e IndependentEvaluator) Evaluate(ctx context.Context, t Task, candidateDir, artifactDir string) EvaluatorResult {
	start := time.Now()
	independent := e.Runner != nil && !e.AllowUnconfined
	if err := validateEvaluatorMaterial(t); err != nil {
		return EvaluatorResult{Status: EvaluatorError, Error: err.Error(), Independent: independent, DurationMS: time.Since(start).Milliseconds()}
	}
	if e.Runner == nil && !e.AllowUnconfined {
		return EvaluatorResult{Status: EvaluatorError, Error: "independent evaluator requires a process sandbox", Independent: false, DurationMS: time.Since(start).Milliseconds()}
	}
	if e.Runner != nil && !e.AllowUnconfined {
		network := sandbox.NetworkNone
		if strings.EqualFold(strings.TrimSpace(t.NetworkPolicy), "host") {
			network = sandbox.NetworkHost
		}
		if err := RequireConfinedRunner(e.Runner, network); err != nil {
			return EvaluatorResult{Status: EvaluatorError, Error: "independent evaluator sandbox: " + err.Error(), Independent: false, DurationMS: time.Since(start).Milliseconds()}
		}
	}
	result := EvaluatorResult{Independent: independent, Steps: []EvaluatorStep{}, OracleHash: t.hiddenHash}
	if candidateDir == "" {
		result.Status, result.Error = EvaluatorError, "candidate directory is empty"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if strings.TrimSpace(artifactDir) == "" {
		result.Status, result.Error = EvaluatorError, "evaluator artifact directory is empty"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if err := validateEnvironment("evaluator", t.Evaluator.Environment); err != nil {
		result.Status, result.Error = EvaluatorError, err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if inside, pathErr := pathWithin(comparablePath(candidateDir), comparablePath(artifactDir)); pathErr != nil {
		result.Status, result.Error = EvaluatorError, "validate evaluator artifact boundary: "+pathErr.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	} else if inside {
		result.Status, result.Error = EvaluatorError, "evaluator artifact directory is inside the worker candidate"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if t.OracleRoot() != "" {
		oraclePath := comparablePath(t.OracleRoot())
		if inside, pathErr := pathWithin(oraclePath, comparablePath(artifactDir)); pathErr != nil {
			result.Status, result.Error = EvaluatorError, "validate oracle/artifact boundary: "+pathErr.Error()
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		} else if inside {
			result.Status, result.Error = EvaluatorError, "evaluator artifact directory is inside the oracle root"
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
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
	evalDir := filepath.Join(artifactDir, "evaluator-workspace")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		result.Status, result.Error = EvaluatorError, err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if err := CopyDirectoryContext(evalCtx, candidateDir, evalDir); err != nil {
		if evalCtx.Err() != nil {
			result.Status, result.Error = EvaluatorTimeout, evalCtx.Err().Error()
		} else {
			result.Status, result.Error = EvaluatorError, "copy candidate for evaluator: "+err.Error()
		}
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if err := evalCtx.Err(); err != nil {
		result.Status, result.Error = EvaluatorTimeout, err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	oracleDir := filepath.Join(artifactDir, "oracle")
	for name := range t.hiddenFiles {
		if _, err := safeRelative(name); err != nil {
			result.Status, result.Error = EvaluatorError, "invalid evaluator file "+name+": "+err.Error()
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
	}
	if err := materializeEvaluatorOracle(t, oracleDir); err != nil {
		result.Status, result.Error = EvaluatorError, "materialize oracle: "+err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	if err := evalCtx.Err(); err != nil {
		result.Status, result.Error = EvaluatorTimeout, err.Error()
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
	candidateBefore, err := ContentManifestContext(evalCtx, evalDir)
	if err != nil {
		result.Status, result.Error = EvaluatorError, "fingerprint evaluator candidate: "+err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	oracleBefore, err := ContentManifestContext(evalCtx, oracleDir)
	if err != nil {
		result.Status, result.Error = EvaluatorError, "fingerprint evaluator oracle: "+err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	steps := evaluatorCommands(t)
	if len(steps) == 0 {
		result.Status, result.Error = EvaluatorError, "evaluator declares no command"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	for _, spec := range steps {
		step := EvaluatorStep{Name: spec.name, Command: append([]string(nil), spec.command.Argv...), OracleUsed: usesOracle(spec.command.Argv)}
		stepStart := time.Now()
		stepCtx := evalCtx
		cancelStep := func() {}
		if spec.timeout > 0 {
			stepCtx, cancelStep = context.WithTimeout(evalCtx, spec.timeout)
		}
		status, code, stdout, stderr, err := e.runStep(stepCtx, spec.command, evalDir, oracleDir, t.Evaluator.Environment, t.NetworkPolicy)
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
	candidateAfter, candidateErr := ContentManifestContext(evalCtx, evalDir)
	oracleAfter, oracleErr := ContentManifestContext(evalCtx, oracleDir)
	if evalCtx.Err() != nil {
		result.Status = EvaluatorTimeout
		if result.Error == "" {
			result.Error = evalCtx.Err().Error()
		}
	} else if candidateErr != nil || oracleErr != nil || candidateAfter != candidateBefore || oracleAfter != oracleBefore {
		if candidateErr != nil {
			result.Error = "evaluator modified or invalidated candidate: " + candidateErr.Error()
		} else if oracleErr != nil {
			result.Error = "evaluator modified or invalidated oracle: " + oracleErr.Error()
		} else {
			result.Error = "evaluator modified candidate or oracle material"
		}
		result.Status = EvaluatorError
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

func validateEvaluatorMaterial(t Task) error {
	for label, files := range map[string]map[string][]byte{"hidden": t.hiddenFiles, "oracle": t.oracleFiles} {
		seen := make(map[string]string, len(files))
		for name := range files {
			clean, err := safeRelative(name)
			if err != nil {
				return fmt.Errorf("%s evaluator file %q: %w", label, name, err)
			}
			if previous, exists := seen[clean]; exists {
				return fmt.Errorf("%s evaluator material paths %q and %q normalize to the same file", label, previous, name)
			}
			seen[clean] = name
		}
	}
	return nil
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
			if t.Evaluator.Required {
				required = true
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

func (e IndependentEvaluator) runStep(ctx context.Context, command Command, dir, oracleDir string, extra map[string]string, networkPolicy string) (EvaluatorStatus, *int, string, string, error) {
	argv := append([]string(nil), command.Argv...)
	if command.Shell {
		if len(argv) == 1 {
			if strings.Contains(argv[0], "oracle/../") || strings.Contains(argv[0], "./oracle/../") {
				return EvaluatorError, nil, "", "", errors.New("evaluator shell oracle path escapes the oracle directory")
			}
			argv[0] = replaceOracleReferences(argv[0], oracleDir)
		}
		argv = append([]string{"/bin/sh", "-c"}, argv...)
	} else {
		for i := range argv {
			resolved, resolveErr := resolveEvaluatorArgv0(argv[i], oracleDir)
			if resolveErr != nil {
				return EvaluatorError, nil, "", "", resolveErr
			}
			argv[i] = resolved
		}
	}
	var cmd *exec.Cmd
	if e.Runner != nil {
		network, err := evaluatorNetwork(networkPolicy)
		if err != nil {
			return EvaluatorError, nil, "", "", err
		}
		tmp := filepath.Join(filepath.Dir(dir), ".bench-evaluator-tmp")
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			return EvaluatorError, nil, "", "", err
		}
		spec := sandbox.Spec{
			Network:   network,
			ReadOnly:  uniqueSorted(append(benchmarkReadOnlyPaths(), dir, oracleDir)),
			ReadWrite: []string{tmp},
			Dir:       dir, TmpDir: tmp,
			Env: evaluatorEnv(extra, oracleDir, dir),
		}
		if err := spec.Validate(); err != nil {
			return EvaluatorError, nil, "", "", err
		}
		cmd, err = e.Runner.Command(ctx, spec, argv...)
		if err != nil {
			return EvaluatorError, nil, "", "", err
		}
	} else {
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv comes from the frozen evaluator definition
		cmd.Dir = dir
		cmd.Env = evaluatorEnv(extra, oracleDir, dir)
	}
	stdout := newCappedBuffer(e.MaxOutput)
	stderr := newCappedBuffer(e.MaxOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := runProcess(ctx, cmd)
	if ctx.Err() != nil {
		return EvaluatorTimeout, nil, stdout.String(), stderr.String(), ctx.Err()
	}
	if stdout.Overflowed() || stderr.Overflowed() {
		return EvaluatorError, nil, stdout.String(), stderr.String(), errors.New("evaluator output exceeded the configured limit")
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

func evaluatorNetwork(policy string) (sandbox.Network, error) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "none":
		return sandbox.NetworkNone, nil
	case "host":
		return sandbox.NetworkHost, nil
	case "allowlist":
		return "", errors.New("bench: evaluator network_policy=allowlist is not implemented")
	default:
		return "", fmt.Errorf("bench: unsupported evaluator network policy %q", policy)
	}
}

func benchmarkReadOnlyPaths() []string {
	paths := []string{
		"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl", "/etc/ca-certificates",
		"/usr/local/bin", "/usr/local/go", "/opt/bcode", "/proc", "/sys", "/dev",
		"/etc/hosts", "/etc/resolv.conf",
	}
	// The Landlock runner re-executes the current bcode binary as its helper.
	// Keep that helper visible read-only; without this mount a bwrap evaluator
	// reports the misleading "helper: no such file or directory" before the
	// declared command ever starts.
	if self, err := os.Executable(); err == nil {
		paths = append(paths, self, filepath.Dir(self))
	}
	return uniqueSorted(paths)
}

func evaluatorEnv(extra map[string]string, oracleDir, candidate string) []string {
	// Go deliberately refuses to treat a module below its configured system
	// temp root as a module. Keep HOME/TMPDIR in a sibling scratch directory,
	// not in the candidate root, or `go test` would reject an otherwise valid
	// oracle for an environmental reason.
	tmp := filepath.Join(filepath.Dir(candidate), ".bench-evaluator-tmp")
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
		if validEnvName(k) && !reservedEnvName(k) {
			env = append(env, k+"="+extra[k])
		}
	}
	return env
}

func resolveEvaluatorArgv0(arg, oracleDir string) (string, error) {
	rel := ""
	switch {
	case arg == "./oracle" || arg == "oracle":
		return oracleDir, nil
	case strings.HasPrefix(arg, "./oracle/"):
		rel = strings.TrimPrefix(arg, "./oracle/")
	case strings.HasPrefix(arg, "oracle/"):
		rel = strings.TrimPrefix(arg, "oracle/")
	default:
		return arg, nil
	}
	clean, err := safeRelative(rel)
	if err != nil {
		return "", fmt.Errorf("evaluator oracle argument %q escapes the oracle directory: %w", arg, err)
	}
	return filepath.Join(oracleDir, filepath.FromSlash(clean)), nil
}

func replaceOracleReferences(command, oracleDir string) string {
	command = strings.ReplaceAll(command, "./oracle/", oracleDir+string(filepath.Separator))
	command = strings.ReplaceAll(command, "oracle/", oracleDir+string(filepath.Separator))
	return command
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
	if err := validateEvaluatorMaterial(t); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	if len(t.oracleFiles) > 0 {
		keys := make([]string, 0, len(t.oracleFiles))
		for name := range t.oracleFiles {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			if err := copyFile(filepath.Join(dst, filepath.FromSlash(name)), t.oracleFiles[name]); err != nil {
				return err
			}
		}
		return nil
	}
	for name, body := range t.hiddenFiles {
		if err := copyFile(filepath.Join(dst, filepath.FromSlash(name)), body); err != nil {
			return err
		}
	}
	if t.OracleRoot() == "" {
		return nil
	}
	// Programmatic tasks may provide an oracle root without going through the
	// loader. Keep a safe fallback for them; loaded tasks use the immutable
	// snapshot above and never re-read a changing oracle directory.
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
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evaluator oracle contains symlink %s", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("evaluator oracle contains non-regular file %s", rel)
		}
		body, err := os.ReadFile(p) //nolint:gosec // path is walked beneath the evaluator oracle root
		if err != nil {
			return err
		}
		mode := d.Type().Perm()
		if info, infoErr := d.Info(); infoErr == nil {
			mode = info.Mode().Perm()
		}
		return copyFileMode(filepath.Join(dst, rel), body, mode)
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
	s = RedactText(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}

// CheckConstraints evaluates declarative paths that must not change. It is
// kept separate from command status so a candidate cannot pass by deleting a
// protected test.
func isBenchmarkControlPath(path string) bool {
	path = filepath.ToSlash(path)
	return path == ".agent/tasks" || strings.HasPrefix(path, ".agent/tasks/")
}

func filterBenchmarkControlFiles(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isBenchmarkControlPath(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func stripBenchmarkControlArtifacts(root string) error {
	if err := os.RemoveAll(filepath.Join(root, ".agent", "tasks")); err != nil {
		return err
	}
	agent := filepath.Join(root, ".agent")
	if entries, err := os.ReadDir(agent); err == nil && len(entries) == 0 {
		return os.Remove(agent)
	}
	return nil
}

func CheckConstraints(t Task, changed []string) []string {
	var failures []string
	for _, protected := range t.Evaluator.MustNotChange {
		clean, err := safeRelative(protected)
		if err != nil {
			continue
		}
		protected = strings.TrimSuffix(filepath.ToSlash(clean), "/")
		for _, path := range changed {
			path = filepath.ToSlash(path)
			if path == protected || strings.HasPrefix(path, protected+"/") {
				failures = append(failures, path)
				break
			}
		}
	}
	return sortStrings(failures)
}

// CheckMutableScope applies the task's declared write boundary to every arm.
// BOUNDED's native firewall enforces it during editing; this post-condition is
// what keeps a raw command from receiving a broader filesystem authority than
// the production arm.
func CheckMutableScope(scope, changed []string) []string {
	var failures []string
	for _, path := range changed {
		if !policy.Covers(scope, filepath.ToSlash(path)) {
			failures = append(failures, path)
		}
	}
	return sortStrings(failures)
}
