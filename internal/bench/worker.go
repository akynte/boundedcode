package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// WorkerRequest is the complete task-facing surface of a benchmark worker.
// Evaluator, oracle, hidden files, expected labels, and benchmark metadata are
// not fields and therefore cannot be passed by a careless adapter.
type WorkerRequest struct {
	RunID string `json:"run_id"`
	// ExecutionID identifies this physical attempt. RunID remains the stable
	// schedule identity used for pairing; a rerun gets a fresh value so its
	// production store cannot reopen the previous attempt's ledger/cache.
	ExecutionID string            `json:"execution_id"`
	PairID      string            `json:"pair_id"`
	Task        TaskConfig        `json:"task"`
	Workspace   string            `json:"workspace"`
	Model       ModelConfig       `json:"model"`
	Limits      Limits            `json:"limits"`
	Network     string            `json:"network_policy"`
	Environment map[string]string `json:"environment,omitempty"`
	Seed        int64             `json:"seed"`
	// WorkerName identifies the executable/runtime for audit. It contains no
	// provider credential.
	WorkerName string `json:"worker_name"`
	// Smoke is harness-only routing metadata. It is deliberately omitted from
	// the serialized worker request so a worker cannot infer evaluator mode.
	Smoke bool `json:"-"`
}

// ValidateRequest is a final boundary check used before every adapter call.
func (r WorkerRequest) ValidateRequest() error {
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.ExecutionID) == "" || strings.TrimSpace(r.PairID) == "" {
		return errors.New("bench: worker request identity is incomplete")
	}
	if strings.TrimSpace(r.Workspace) == "" {
		return errors.New("bench: worker workspace is empty")
	}
	if strings.TrimSpace(r.Task.ID) == "" {
		return errors.New("bench: worker task id is empty")
	}
	if err := r.Limits.Validate(); err != nil {
		return fmt.Errorf("bench: worker limits: %w", err)
	}
	if err := validateEnvironment("worker", r.Environment); err != nil {
		return err
	}
	if err := validateModel(r.Model); err != nil {
		return fmt.Errorf("bench: worker model: %w", err)
	}
	if r.Network != "" && r.Network != "none" && r.Network != "allowlist" && r.Network != "host" {
		return fmt.Errorf("bench: worker network policy %q is invalid", r.Network)
	}
	if requestHasForbiddenKey(r) {
		// This is intentionally a structural check. A path or task description
		// may legitimately contain the word "oracle"; a forbidden JSON key is
		// the actual metadata leak.
		return errors.New("bench: worker request contains evaluator/oracle metadata")
	}
	return nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func requestHasForbiddenKey(value any) bool {
	body, err := json.Marshal(value)
	if err != nil {
		return true
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return true
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				lk := strings.ToLower(key)
				if lk == "oracle" || lk == "evaluator" || strings.Contains(lk, "hidden") || strings.Contains(lk, "expected") {
					return true
				}
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(decoded)
}

// WorkerResult is the system's self-report and measured execution metadata. It
// is not the benchmark correctness decision.
type WorkerResult struct {
	Model           ModelConfig `json:"model,omitempty"`
	WorkerStarted   bool        `json:"worker_started"`
	ModelVerified   bool        `json:"model_identity_verified,omitempty"`
	ReportedSuccess bool        `json:"reported_success"`
	ReportedStatus  string      `json:"reported_status"`
	FailureReason   string      `json:"failure_reason,omitempty"`
	TaskID          string      `json:"bounded_task_id,omitempty"`
	CandidateDir    string      `json:"candidate_dir,omitempty"`
	Stdout          string      `json:"-"`
	Stderr          string      `json:"-"`
	Events          []Event     `json:"events,omitempty"`
	Metrics         Metrics     `json:"metrics"`
	Termination     string      `json:"termination_reason,omitempty"`
}

// Event is an adapter-neutral recovery/interruption event. It is data, not a
// claim: workers emit it only when their runtime actually observes it.
type Event struct {
	Kind    string         `json:"kind"`
	At      time.Time      `json:"at"`
	Attempt int            `json:"attempt,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type Metrics struct {
	GenerationRequests   *int   `json:"generation_requests"`
	AssistantTurns       *int   `json:"assistant_turns"`
	ToolCalls            *int   `json:"tool_calls"`
	Reads                *int   `json:"reads"`
	Edits                *int   `json:"edits"`
	VerificationAttempts *int   `json:"verification_attempts"`
	Retries              *int   `json:"retries"`
	RepeatedFailures     *int   `json:"repeated_failure_count"`
	SessionRestarts      *int   `json:"session_restarts"`
	Compactions          *int   `json:"compactions"`
	InputTokens          *int64 `json:"input_tokens"`
	OutputTokens         *int64 `json:"output_tokens"`
	CachedInputTokens    *int64 `json:"cached_input_tokens"`
	TotalTokens          *int64 `json:"total_tokens"`
}

// Worker executes one mode against a fresh candidate. Implementations must
// honor ctx; the runner supplies the external wall-clock context.
type Worker interface {
	Run(context.Context, WorkerRequest) (WorkerResult, error)
}

// ModelIdentityAttestor is an optional worker capability for official runs.
// A worker that cannot attest the provider-reported model identity must not be
// used to manufacture an apparently fair official pair.
type ModelIdentityAttestor interface {
	ModelIdentityAttestable() bool
}

// LimitAttestor is an explicit claim that an adapter enforces the frozen
// generation/token/verification admission limits, rather than merely passing
// them as environment hints. Official paired runs require this capability.
type LimitAttestor interface {
	LimitsAttestable() bool
}

// RawAdapter is the baseline arm. It intentionally has no Supervisor in its
// call path; it only receives a WorkerRequest and the isolated workspace.
type RawAdapter struct {
	Worker Worker
}

func (a RawAdapter) Run(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if a.Worker == nil {
		return WorkerResult{}, errors.New("bench: RAW worker is nil")
	}
	if err := req.ValidateRequest(); err != nil {
		return WorkerResult{}, err
	}
	return a.Worker.Run(ctx, req)
}

// BoundedExecutor is the production-control boundary. An implementation must
// enter the real BoundedCode Supervisor/task path; the benchmark package does
// not provide a fake one.
type BoundedExecutor interface {
	Execute(context.Context, WorkerRequest) (WorkerResult, error)
}

// BoundedExecutorFactory creates a fresh control-plane state for every run.
// A shared ledger or task namespace is rejected by the production adapter;
// making the factory per-run is what prevents cross-repetition memory.
type BoundedExecutorFactory func(context.Context, WorkerRequest) (BoundedExecutor, error)

type BoundedAdapter struct {
	Factory BoundedExecutorFactory
	// RequireProduction rejects fake/programmatic executors in an official
	// paired run. The production adapter is the only implementation that can
	// satisfy this boundary.
	RequireProduction bool
}

func (a BoundedAdapter) Run(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if a.Factory == nil {
		return WorkerResult{}, errors.New("bench: BOUNDED executor factory is nil")
	}
	if err := req.ValidateRequest(); err != nil {
		return WorkerResult{}, err
	}
	executor, err := a.Factory(ctx, req)
	if err != nil {
		return WorkerResult{}, err
	}
	if executor == nil {
		return WorkerResult{}, errors.New("bench: BOUNDED executor factory returned nil")
	}
	if a.RequireProduction {
		if _, ok := executor.(*ProductionBounded); !ok {
			return WorkerResult{}, errors.New("bench: official BOUNDED run requires the production executor")
		}
	}
	var out WorkerResult
	var executeErr error
	if closer, ok := executor.(interface{ Close() error }); ok {
		out, executeErr = executor.Execute(ctx, req)
		if closeErr := closer.Close(); executeErr == nil && closeErr != nil {
			executeErr = fmt.Errorf("bench: close bounded executor: %w", closeErr)
		}
		return out, executeErr
	}
	return executor.Execute(ctx, req)
}

// CommandWorker runs a normal OpenCode/standalone command in the same
// candidate-only sandbox shape. It is suitable for RAW and for a bounded
// adapter only when the bounded adapter has separately entered production
// control; the benchmark does not infer control from a command name.
type CommandWorker struct {
	Runner       sandbox.Runner
	Command      []string
	Shell        bool
	Network      sandbox.Network
	networkSet   bool
	ReadOnly     []string
	ExtraEnv     map[string]string
	ClaimPattern *regexp.Regexp
	SuccessExit  int
	Name         string
	MaxOutput    int
}

func NewCommandWorker(command []string, opts ...CommandWorkerOption) (*CommandWorker, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, errors.New("bench: worker command is empty")
	}
	for i, arg := range command {
		if strings.TrimSpace(arg) == "" {
			return nil, fmt.Errorf("bench: worker command argument %d is empty", i)
		}
		if strings.ContainsRune(arg, '\x00') {
			return nil, fmt.Errorf("bench: worker command argument %d contains NUL", i)
		}
	}
	w := &CommandWorker{Command: append([]string(nil), command...), SuccessExit: 0, Name: "command"}
	for _, opt := range opts {
		opt(w)
	}
	if w.Runner == nil {
		w.Runner = SelectCommandSandbox(context.Background())
	}
	network := sandbox.NetworkNone
	if w.networkSet {
		network = w.Network
	}
	if err := RequireConfinedRunner(w.Runner, network); err != nil {
		return nil, err
	}
	return w, nil
}

type cappedBuffer struct {
	bytes.Buffer
	mu       sync.Mutex
	limit    int
	overflow bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	if limit <= 0 {
		limit = 1 << 20
	}
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	if remaining := b.limit - b.Buffer.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
			b.overflow = true
		}
		if len(p) > 0 {
			_, _ = b.Buffer.Write(p)
		}
	} else if original > 0 {
		b.overflow = true
	}
	return original, nil
}

func (b *cappedBuffer) Overflowed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Buffer.Len() < b.limit {
		return b.Buffer.String()
	}
	return b.Buffer.String() + "\n… (truncated)"
}

type CommandWorkerOption func(*CommandWorker)

func WithCommandSandbox(r sandbox.Runner) CommandWorkerOption {
	return func(w *CommandWorker) { w.Runner = r }
}
func WithCommandNetwork(n sandbox.Network) CommandWorkerOption {
	return func(w *CommandWorker) { w.Network, w.networkSet = n, true }
}
func WithCommandReadOnly(paths ...string) CommandWorkerOption {
	return func(w *CommandWorker) { w.ReadOnly = append(w.ReadOnly, paths...) }
}
func WithCommandEnv(env map[string]string) CommandWorkerOption {
	return func(w *CommandWorker) { w.ExtraEnv = cloneStrings(env) }
}
func WithCommandName(name string) CommandWorkerOption {
	return func(w *CommandWorker) { w.Name = name }
}

func WithCommandShell(shell bool) CommandWorkerOption {
	return func(w *CommandWorker) { w.Shell = shell }
}
func WithClaimPattern(pattern string) CommandWorkerOption {
	return func(w *CommandWorker) {
		if pattern != "" {
			w.ClaimPattern, _ = regexp.Compile(pattern)
		}
	}
}

func (w *CommandWorker) ModelIdentityAttestable() bool { return false }
func (w *CommandWorker) LimitsAttestable() bool        { return false }

var errWorkerOutputLimit = errors.New("bench: worker output exceeded the configured limit")

func (w *CommandWorker) Run(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if err := req.ValidateRequest(); err != nil {
		return WorkerResult{}, err
	}
	if w.Runner == nil {
		return WorkerResult{}, errors.New("bench: command worker has no sandbox")
	}
	tmp, err := os.MkdirTemp(filepath.Join(req.Workspace, ".bench-tmp"), "worker-")
	if err != nil {
		// Some repositories do not permit a dot-directory creation policy. A
		// run-local temp beside the candidate is still isolated and is removed
		// with the candidate; use the parent only as a fallback.
		tmp, err = os.MkdirTemp(filepath.Dir(req.Workspace), "bench-worker-")
		if err != nil {
			return WorkerResult{}, err
		}
	}
	defer os.RemoveAll(tmp)
	network, err := w.networkFor(req.Network)
	if err != nil {
		return WorkerResult{}, err
	}
	if req.Network == "none" && network == sandbox.NetworkHost {
		return WorkerResult{}, errors.New("bench: worker network override cannot widen network_policy=none")
	}
	readOnly := append([]string(nil), w.ReadOnly...)
	readOnly = append(readOnly, benchmarkReadOnlyPaths()...)
	requestPath := filepath.Join(tmp, "request.json")
	body, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return WorkerResult{}, fmt.Errorf("bench: encode worker request: %w", marshalErr)
	}
	if err := os.WriteFile(requestPath, body, 0o600); err != nil { //nolint:gosec // private worker request
		return WorkerResult{}, fmt.Errorf("bench: write worker request: %w", err)
	}
	writable, err := commandWritablePaths(req.Workspace, req.Task.MutableScope, tmp)
	if err != nil {
		return WorkerResult{}, err
	}
	spec := sandbox.Spec{
		Network: network, ReadOnly: uniqueSorted(append(readOnly, req.Workspace)),
		ReadWrite: writable, Dir: req.Workspace, TmpDir: tmp,
		Env: append(commandEnv(w.ExtraEnv, req, tmp), "BENCH_TASK_FILE="+requestPath),
	}
	if err := spec.Validate(); err != nil {
		return WorkerResult{}, err
	}
	argv := append([]string(nil), w.Command...)
	for i := range argv {
		argv[i] = strings.ReplaceAll(argv[i], "{workspace}", req.Workspace)
		argv[i] = strings.ReplaceAll(argv[i], "{task}", req.Task.ID)
		argv[i] = strings.ReplaceAll(argv[i], "{run_id}", req.RunID)
	}
	if w.Shell {
		argv = append([]string{"/bin/sh", "-c"}, argv...)
	}
	cmd, err := w.Runner.Command(ctx, spec, argv...)
	if err != nil {
		return WorkerResult{}, err
	}
	stdout := newCappedBuffer(w.MaxOutput)
	stderr := newCappedBuffer(w.MaxOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started, runErr := runProcessStarted(ctx, cmd)
	res := WorkerResult{Model: req.Model, WorkerStarted: started, Stdout: RedactText(stdout.String()), Stderr: RedactText(stderr.String())}
	if ctx.Err() != nil {
		res.Termination = "timeout"
		res.ReportedStatus = "timeout"
		return res, ctx.Err()
	}
	if stdout.Overflowed() || stderr.Overflowed() {
		res.ReportedStatus = "output_limit"
		res.FailureReason = errWorkerOutputLimit.Error()
		return res, errWorkerOutputLimit
	}
	code := exitCode(runErr)
	res.ReportedSuccess = runErr == nil || code == w.SuccessExit
	if w.ClaimPattern != nil {
		res.ReportedSuccess = w.ClaimPattern.MatchString(res.Stdout + "\n" + res.Stderr)
	}
	if res.ReportedSuccess {
		res.ReportedStatus = "reported_success"
	} else {
		res.ReportedStatus = "reported_failure"
		res.FailureReason = strings.TrimSpace(res.Stderr)
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return res, runErr
		}
	}
	return res, nil
}

func (w *CommandWorker) networkFor(policy string) (sandbox.Network, error) {
	if w.networkSet {
		return w.Network, nil
	}
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "":
		// An omitted policy follows the harness-wide no-network default. A
		// model endpoint must be selected explicitly with `host`.
		return sandbox.NetworkNone, nil
	case "host":
		return sandbox.NetworkHost, nil
	case "none":
		return sandbox.NetworkNone, nil
	case "allowlist":
		return "", errors.New("bench: network_policy=allowlist needs a task-specific sandbox policy; refusing to silently grant host networking")
	default:
		return "", fmt.Errorf("bench: unsupported network policy %q", policy)
	}
}

func commandEnv(extra map[string]string, req WorkerRequest, tmp string) []string {
	env := []string{
		"HOME=" + tmp, "TMPDIR=" + tmp, "XDG_CACHE_HOME=" + tmp, "XDG_CONFIG_HOME=" + tmp,
		"XDG_STATE_HOME=" + tmp, "PATH=" + os.Getenv("PATH"), "LANG=C", "LC_ALL=C",
		"NO_COLOR=1", "TERM=dumb", "BENCH_WORKSPACE=" + req.Workspace,
		"BENCH_TASK_ID=" + req.Task.ID, "BENCH_RUN_ID=" + req.RunID,
	}
	if req.Seed != 0 {
		env = append(env, fmt.Sprintf("BENCH_SEED=%d", req.Seed))
	}
	if req.Limits.MaxGenerationRequests > 0 {
		env = append(env, fmt.Sprintf("BENCH_MAX_GENERATION_REQUESTS=%d", req.Limits.MaxGenerationRequests))
	}
	if req.Limits.MaxVerificationAttempts > 0 {
		env = append(env, fmt.Sprintf("BENCH_MAX_VERIFICATION_ATTEMPTS=%d", req.Limits.MaxVerificationAttempts))
	}
	if req.Limits.MaxTokens > 0 {
		env = append(env, fmt.Sprintf("BENCH_MAX_TOKENS=%d", req.Limits.MaxTokens))
	}
	values := make(map[string]string, len(req.Environment)+len(extra))
	for k, v := range req.Environment {
		values[k] = v
	}
	for k, v := range extra {
		values[k] = v
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		if !safeWorkerEnvName(k) || strings.ContainsRune(values[k], '\x00') {
			continue
		}
		env = append(env, k+"="+values[k])
	}
	return env
}

// AppendSafeEnvironment adds validated task variables to a production
// sandbox environment without allowing them to replace harness-owned paths or
// runtime controls. It is used by the BOUNDED adapter so setup/verification
// commands see the same declared environment as RAW.
func AppendSafeEnvironment(base []string, extra map[string]string) []string {
	out := append([]string(nil), base...)
	values := make(map[string]string, len(extra))
	keys := make([]string, 0, len(extra))
	for key, value := range extra {
		if !safeWorkerEnvName(key) || strings.ContainsRune(value, '\x00') {
			continue
		}
		if _, exists := values[key]; !exists {
			keys = append(keys, key)
		}
		values[key] = value
	}
	sortStrings(keys)
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func safeWorkerEnvName(name string) bool {
	if !validEnvName(name) || reservedEnvName(name) {
		return false
	}
	lk := strings.ToLower(name)
	if strings.HasPrefix(lk, "bench_") || strings.HasPrefix(lk, "xdg_") ||
		lk == "home" || lk == "tmpdir" || lk == "path" || lk == "lang" || lk == "lc_all" ||
		lk == "no_color" || lk == "term" {
		return false
	}
	return !strings.Contains(lk, "key") && !strings.Contains(lk, "token") && !strings.Contains(lk, "secret") &&
		!strings.Contains(lk, "password") && !strings.Contains(lk, "credential") && !strings.Contains(lk, "authorization") &&
		!strings.Contains(lk, "oracle") && !strings.Contains(lk, "evaluator") && !strings.Contains(lk, "hidden")
}

func commandWritablePaths(root string, scope []string, tmp string) ([]string, error) {
	if len(scope) == 0 {
		return nil, errors.New("bench: RAW command requires an explicit mutable_scope")
	}
	paths := make([]string, 0, len(scope)+1)
	if policy.Covers(scope, ".") {
		paths = append(paths, root)
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return errors.New("bench: candidate .git is not a directory")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", rel)
		}
		if !policy.Covers(scope, rel) {
			return nil
		}
		paths = append(paths, path)
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths = append(paths, tmp)
	paths = uniqueSorted(paths)
	if len(paths) == 0 {
		return nil, errors.New("bench: mutable_scope matches no candidate path")
	}
	return paths, nil
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sortStrings(out)
	return out
}

// ScriptedWorker is a deterministic infrastructure adapter used by self-tests
// and the explicitly non-official smoke suite. It is not a model substitute for
// an official run and refuses to run for an official task.
type ScriptedWorker struct {
	Apply        func(context.Context, WorkerRequest) error
	ClaimSuccess bool
	Delay        time.Duration
	Events       []Event
	Metrics      Metrics
	Stdout       string
	Stderr       string
}

func (w ScriptedWorker) Run(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if err := req.ValidateRequest(); err != nil {
		return WorkerResult{}, err
	}
	if w.Delay > 0 {
		t := time.NewTimer(w.Delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return WorkerResult{WorkerStarted: true, ReportedStatus: "timeout", Termination: "timeout"}, ctx.Err()
		case <-t.C:
		}
	}
	if w.Apply != nil {
		if err := w.Apply(ctx, req); err != nil {
			return WorkerResult{WorkerStarted: true, ReportedStatus: "error", FailureReason: err.Error()}, err
		}
	}
	return WorkerResult{
		Model:           req.Model,
		WorkerStarted:   true,
		ReportedSuccess: w.ClaimSuccess, ReportedStatus: statusForClaim(w.ClaimSuccess),
		FailureReason: w.Stderr, Events: append([]Event(nil), w.Events...), Metrics: w.Metrics,
		Stdout: RedactText(w.Stdout), Stderr: RedactText(w.Stderr),
	}, nil
}

func statusForClaim(success bool) string {
	if success {
		return "reported_success"
	}
	return "reported_failure"
}
