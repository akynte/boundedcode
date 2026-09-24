package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/bwrap"
	"github.com/akynte/boundedcode/internal/sandbox/container"
	"github.com/akynte/boundedcode/internal/sandbox/landlock"
)

// WorkerRequest is the complete task-facing surface of a benchmark worker.
// Evaluator, oracle, hidden files, expected labels, and benchmark metadata are
// not fields and therefore cannot be passed by a careless adapter.
type WorkerRequest struct {
	RunID       string            `json:"run_id"`
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
}

// ValidateRequest is a final boundary check used before every adapter call.
func (r WorkerRequest) ValidateRequest() error {
	if strings.TrimSpace(r.Workspace) == "" {
		return errors.New("bench: worker workspace is empty")
	}
	if strings.TrimSpace(r.Task.ID) == "" {
		return errors.New("bench: worker task id is empty")
	}
	if strings.Contains(strings.ToLower(string(mustJSON(r))), "oracle") {
		// This is intentionally a structural smoke check. The request type has
		// no oracle field; a future field named Oracle must fail this test
		// rather than silently becoming a channel.
		return errors.New("bench: worker request contains evaluator/oracle metadata")
	}
	return nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// WorkerResult is the system's self-report and measured execution metadata. It
// is not the benchmark correctness decision.
type WorkerResult struct {
	ReportedSuccess bool    `json:"reported_success"`
	ReportedStatus  string  `json:"reported_status"`
	FailureReason   string  `json:"failure_reason,omitempty"`
	TaskID          string  `json:"bounded_task_id,omitempty"`
	CandidateDir    string  `json:"candidate_dir,omitempty"`
	Stdout          string  `json:"-"`
	Stderr          string  `json:"-"`
	Events          []Event `json:"events,omitempty"`
	Metrics         Metrics `json:"metrics"`
	Termination     string  `json:"termination_reason,omitempty"`
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
	if closer, ok := executor.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
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
	ReadOnly     []string
	ExtraEnv     map[string]string
	ClaimPattern *regexp.Regexp
	SuccessExit  int
	Name         string
	MaxOutput    int
}

func NewCommandWorker(command []string, opts ...CommandWorkerOption) (*CommandWorker, error) {
	if len(command) == 0 {
		return nil, errors.New("bench: worker command is empty")
	}
	w := &CommandWorker{Command: append([]string(nil), command...), SuccessExit: 0, Name: "command"}
	for _, opt := range opts {
		opt(w)
	}
	if w.Runner == nil {
		ll, _ := landlock.New()
		candidates := make([]sandbox.Runner, 0, 3)
		if ll != nil {
			candidates = append(candidates, bwrap.New(ll), ll)
		}
		candidates = append(candidates, &container.Runner{})
		r, _ := sandbox.Select(context.Background(), candidates)
		if r == nil {
			return nil, errors.New("bench: no filesystem sandbox is available for a command worker")
		}
		w.Runner = r
	}
	return w, nil
}

type CommandWorkerOption func(*CommandWorker)

func WithCommandSandbox(r sandbox.Runner) CommandWorkerOption {
	return func(w *CommandWorker) { w.Runner = r }
}
func WithCommandNetwork(n sandbox.Network) CommandWorkerOption {
	return func(w *CommandWorker) { w.Network = n }
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
	readOnly := append([]string(nil), w.ReadOnly...)
	readOnly = append(readOnly,
		"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl", "/etc/ca-certificates",
		"/usr/local/go", "/opt/bcode", "/proc", "/sys", "/etc/hosts", "/etc/resolv.conf")
	spec := sandbox.Spec{
		Network: w.Network, ReadOnly: uniqueSorted(readOnly),
		ReadWrite: []string{req.Workspace, tmp}, Dir: req.Workspace, TmpDir: tmp,
		Env: commandEnv(w.ExtraEnv, req, tmp),
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	res := WorkerResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		res.Termination = "timeout"
		res.ReportedStatus = "timeout"
		return res, ctx.Err()
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
	return res, runErr
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
			return WorkerResult{ReportedStatus: "timeout", Termination: "timeout"}, ctx.Err()
		case <-t.C:
		}
	}
	if w.Apply != nil {
		if err := w.Apply(ctx, req); err != nil {
			return WorkerResult{ReportedStatus: "error", FailureReason: err.Error()}, err
		}
	}
	return WorkerResult{
		ReportedSuccess: w.ClaimSuccess, ReportedStatus: statusForClaim(w.ClaimSuccess),
		FailureReason: w.Stderr, Events: append([]Event(nil), w.Events...), Metrics: w.Metrics,
		Stdout: w.Stdout, Stderr: w.Stderr,
	}, nil
}

func statusForClaim(success bool) string {
	if success {
		return "reported_success"
	}
	return "reported_failure"
}
