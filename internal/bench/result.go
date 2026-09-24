package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RunStatus is the benchmark harness' classification. It is intentionally
// separate from a system's reported success and from evaluator status.
type RunStatus string

const (
	RunCompleted           RunStatus = "completed"
	RunTaskFailed          RunStatus = "task_failed"
	RunTimeout             RunStatus = "timeout"
	RunInfrastructure      RunStatus = "infrastructure_error"
	RunProviderUnavailable RunStatus = "provider_unavailable"
	RunEvaluatorError      RunStatus = "evaluator_error"
	RunEvaluatorTimeout    RunStatus = "evaluator_timeout"
	RunInvalid             RunStatus = "invalid"
	RunCancelled           RunStatus = "cancelled"
)

// Evidence reports whether a run is eligible for the primary completion rate.
// A timeout with a meaningful candidate remains eligible; infrastructure and
// evaluator errors do not become ordinary task failures.
func (s RunStatus) Valid() bool {
	switch s {
	case RunCompleted, RunTaskFailed, RunTimeout, RunInfrastructure, RunProviderUnavailable,
		RunEvaluatorError, RunEvaluatorTimeout, RunInvalid, RunCancelled:
		return true
	default:
		return false
	}
}

func (s RunStatus) Evidence() bool {
	return s == RunCompleted || s == RunTaskFailed || s == RunTimeout
}

// Retryable reports whether a result represents a transient harness/provider
// fault that a later resume may safely attempt again. Invalid runs are held
// for explicit operator review, while an ordinary completed/failed/timeout
// cell is already complete evidence.
func (s RunStatus) Retryable() bool {
	return s == RunInfrastructure || s == RunProviderUnavailable || s == RunEvaluatorError || s == RunEvaluatorTimeout || s == RunCancelled
}

type TaskReference struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TaskHash      string   `json:"task_hash"`
	EvaluatorHash string   `json:"evaluator_hash"`
	BaseCommit    string   `json:"base_commit"`
	FixtureHash   string   `json:"fixture_hash,omitempty"`
	Languages     []string `json:"languages,omitempty"`
	Limits        Limits   `json:"limits"`
	Verification  string   `json:"verification,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Category      string   `json:"category,omitempty"`
	Fixture       string   `json:"fixture,omitempty"`
	Repository    string   `json:"repository,omitempty"`
}

// RunConfiguration is safe to persist. It deliberately contains no provider
// credential, raw environment, or model response body.
type RunConfiguration struct {
	SuiteID      string            `json:"suite_id"`
	SuiteVersion string            `json:"suite_version"`
	SuiteHash    string            `json:"suite_hash"`
	TaskHash     string            `json:"task_hash"`
	Model        ModelConfig       `json:"model"`
	Limits       Limits            `json:"limits"`
	Network      string            `json:"network_policy"`
	Environment  map[string]string `json:"environment,omitempty"`
	Runner       RunnerConfig      `json:"runner"`
	Seed         int64             `json:"seed"`
}

type RepositoryResult struct {
	Source               string    `json:"source"`
	ConfiguredBase       string    `json:"configured_base_commit"`
	BaseCommit           string    `json:"base_commit"`
	BaseTree             string    `json:"base_tree"`
	FixtureHash          string    `json:"fixture_hash,omitempty"`
	StartingCandidate    string    `json:"starting_candidate_hash"`
	SetupCandidate       string    `json:"setup_candidate_hash,omitempty"`
	FinalCandidate       string    `json:"final_candidate_hash"`
	ChangedFiles         []string  `json:"changed_files"`
	DiffStats            DiffStats `json:"diff_stats"`
	OracleOutside        bool      `json:"hidden_oracle_outside_workspace"`
	StartingStateChecked bool      `json:"starting_state_checked"`
}

type DiffStats struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Bytes   int `json:"bytes"`
}

type ModelResult struct {
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Runtime          string   `json:"runtime"`
	ModelFile        string   `json:"model_file,omitempty"`
	ModelFileSHA256  string   `json:"model_file_sha256,omitempty"`
	Quantization     string   `json:"quantization,omitempty"`
	ContextTokens    *int     `json:"context_tokens"`
	Temperature      *float64 `json:"temperature"`
	TopP             *float64 `json:"top_p"`
	TopK             *int     `json:"top_k"`
	Seed             *int64   `json:"seed"`
	IdentityVerified bool     `json:"identity_verified"`
	Limitations      []string `json:"limitations,omitempty"`
}

type ExecutionResult struct {
	StartTime            time.Time `json:"start_time"`
	EndTime              time.Time `json:"end_time"`
	WallClockSeconds     float64   `json:"wall_clock_seconds"`
	TimeoutSeconds       float64   `json:"timeout_seconds"`
	GenerationRequests   *int      `json:"generation_requests"`
	AssistantTurns       *int      `json:"assistant_turns"`
	ToolCalls            *int      `json:"tool_calls"`
	Reads                *int      `json:"reads"`
	Edits                *int      `json:"edits"`
	VerificationAttempts *int      `json:"verification_attempts"`
	Retries              *int      `json:"retries"`
	RepeatedFailureCount *int      `json:"repeated_failure_count"`
	SessionRestarts      *int      `json:"session_restarts"`
	Compactions          *int      `json:"compactions"`
	InputTokens          *int64    `json:"input_tokens"`
	OutputTokens         *int64    `json:"output_tokens"`
	CachedInputTokens    *int64    `json:"cached_input_tokens"`
	TotalTokens          *int64    `json:"total_tokens"`
	TerminationReason    string    `json:"termination_reason"`
	Concurrency          int       `json:"concurrency"`
}

type SystemResult struct {
	ReportedSuccess       bool   `json:"reported_success"`
	ReportedStatus        string `json:"reported_status"`
	ReportedFailureReason string `json:"reported_failure_reason,omitempty"`
	BoundedTaskID         string `json:"bounded_task_id,omitempty"`
	ReportedCandidate     string `json:"reported_candidate,omitempty"`
}

type DerivedResult struct {
	IndependentVerifiedSuccess bool     `json:"independently_verified_success"`
	FalseSuccess               bool     `json:"false_success"`
	FalseFailure               bool     `json:"false_failure"`
	FalseSuccessRate           *float64 `json:"false_success_rate,omitempty"`
	FalseFailureRate           *float64 `json:"false_failure_rate,omitempty"`
}

type ArtifactResult struct {
	ResultPath          string `json:"result_path"`
	TaskSnapshotPath    string `json:"task_snapshot_path,omitempty"`
	ConfigurationPath   string `json:"configuration_path,omitempty"`
	StdoutPath          string `json:"stdout_path,omitempty"`
	StderrPath          string `json:"stderr_path,omitempty"`
	TranscriptPath      string `json:"transcript_path,omitempty"`
	DiffPath            string `json:"diff_path,omitempty"`
	EvaluatorOutputPath string `json:"evaluator_output_path,omitempty"`
	CandidatePath       string `json:"candidate_path,omitempty"`
	SchedulePath        string `json:"schedule_path,omitempty"`
	ManifestPath        string `json:"manifest_path,omitempty"`
}

// Result is the durable machine-readable record for one execution. The schema
// is append-only in meaning: new fields may be added, but old field meanings
// cannot change without a ResultSchemaVersion bump.
type Result struct {
	SchemaVersion       int                 `json:"schema_version"`
	BenchmarkSuite      string              `json:"benchmark_suite"`
	Official            bool                `json:"official"`
	Smoke               bool                `json:"smoke"`
	SuiteVersion        string              `json:"suite_version"`
	SuiteHash           string              `json:"suite_hash"`
	ManifestHash        string              `json:"manifest_hash,omitempty"`
	TaskID              string              `json:"task_id"`
	Mode                Mode                `json:"mode"`
	RunIndex            int                 `json:"run_index"`
	RunID               string              `json:"run_id"`
	ExecutionID         string              `json:"execution_id,omitempty"`
	PairID              string              `json:"pair_id"`
	Repetition          int                 `json:"repetition"`
	Seed                int64               `json:"benchmark_seed"`
	Timestamp           time.Time           `json:"timestamp"`
	Task                TaskReference       `json:"task"`
	Configuration       RunConfiguration    `json:"configuration"`
	Repository          RepositoryResult    `json:"repository"`
	Model               ModelResult         `json:"model"`
	Environment         EnvironmentSnapshot `json:"environment"`
	Execution           ExecutionResult     `json:"execution"`
	System              SystemResult        `json:"system_result"`
	Evaluator           EvaluatorResult     `json:"independent_evaluator"`
	Derived             DerivedResult       `json:"derived"`
	Fingerprints        Fingerprints        `json:"fingerprints"`
	Status              RunStatus           `json:"status"`
	InfrastructureError string              `json:"infrastructure_error,omitempty"`
	Events              []Event             `json:"events,omitempty"`
	Artifacts           ArtifactResult      `json:"artifacts"`
}

// NewResult initializes fields whose zero value would otherwise mean measured
// zero. The caller fills execution-specific values after the run.
func NewResult(s Suite, t Task, spec RunSpec, env EnvironmentSnapshot) Result {
	limits := EffectiveLimits(t.Limits)
	runner := s.Runner.effective()
	model := s.Model
	// A configured route is an input, not proof of what a provider served.
	// The runner upgrades this only when the adapter can observe the actual
	// model identity.
	verified := false
	limitations := []string{}
	if model.ModelFileSHA256 == "" {
		limitations = append(limitations, "model file hash unavailable")
	}
	if model.Runtime == "" {
		limitations = append(limitations, "runtime identity unavailable")
	}
	if model.Temperature == nil {
		limitations = append(limitations, "sampling temperature unavailable")
	}
	if model.TopP == nil {
		limitations = append(limitations, "sampling top_p unavailable")
	}
	if model.TopK == nil {
		limitations = append(limitations, "sampling top_k unavailable")
	}
	if model.Seed == nil {
		limitations = append(limitations, "sampling seed unavailable")
	}
	return Result{
		SchemaVersion: ResultSchemaVersion, BenchmarkSuite: s.ID, Official: s.Official, Smoke: s.Smoke, SuiteVersion: s.Version,
		SuiteHash: s.Hash(), TaskID: t.ID, Mode: spec.Mode, RunIndex: spec.RunIndex,
		RunID: spec.RunID, PairID: spec.PairID, Repetition: spec.Repetition, Seed: spec.Seed, Timestamp: time.Now().UTC(),
		Task: TaskReference{ID: t.ID, Title: t.Title, Description: t.Description,
			TaskHash: taskHash(t), EvaluatorHash: t.hiddenHash, BaseCommit: t.BaseCommit, FixtureHash: t.FixtureHash,
			Languages: append([]string(nil), t.Languages...), Limits: limits, Verification: t.Verification,
			Tags: append([]string(nil), t.Tags...), Category: t.Category, Fixture: t.Fixture,
			Repository: t.Repository},
		Configuration: RunConfiguration{SuiteID: s.ID, SuiteVersion: s.Version, SuiteHash: s.Hash(),
			TaskHash: taskHash(t), Model: model, Limits: limits, Network: t.NetworkPolicy,
			Environment: RedactSecrets(mergeStrings(s.Environment, t.Environment)), Runner: runner, Seed: spec.Seed},
		Model: ModelResult{Provider: model.Provider, Model: model.Model, Runtime: model.Runtime,
			ModelFile: model.ModelFile, ModelFileSHA256: model.ModelFileSHA256,
			Quantization: model.Quantization, ContextTokens: intPtr(model.ContextTokens),
			Temperature: cloneFloat(model.Temperature), TopP: cloneFloat(model.TopP), TopK: cloneInt(model.TopK),
			Seed: cloneInt64(model.Seed), IdentityVerified: verified, Limitations: limitations},
		Environment: env, Fingerprints: ComputeFingerprints(s, t, spec.Mode, env),
		Evaluator: EvaluatorResult{Status: EvaluatorError, Error: "evaluator has not run", Independent: true},
		Derived:   DerivedResult{}, Status: RunInfrastructure,
		Execution: ExecutionResult{TimeoutSeconds: limits.WallClock().Seconds(), Concurrency: 1},
	}
}

func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// measuredInt preserves a real zero measurement. intPtr is reserved for
// configuration values where zero means "not declared"; a budget counter that
// was installed and admitted no requests is different and must serialize as 0.
func measuredInt(v int) *int {
	x := v
	return &x
}
func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func cloneInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

// Derive computes the benchmark semantics from independent evaluator output.
// It never consults the evaluator's opinion of the system's self-report.
func (r *Result) Derive() {
	pass := r.Evaluator.Status == EvaluatorPass && r.Evaluator.Independent
	fail := r.Evaluator.Status == EvaluatorFail
	r.Derived.IndependentVerifiedSuccess = pass
	r.Derived.FalseSuccess = r.System.ReportedSuccess && fail
	r.Derived.FalseFailure = !r.System.ReportedSuccess && pass
}

// Classify applies explicit status precedence. Evaluator ERROR and provider/
// harness errors are not converted into task failures.
func (r *Result) Classify(workerErr error, timedOut bool) {
	if r.Evaluator.Status == EvaluatorPass && !r.Evaluator.Independent {
		r.Status = RunInfrastructure
		return
	}
	if r.System.ReportedStatus == "provider_unavailable" || strings.Contains(strings.ToLower(r.InfrastructureError), "provider") {
		r.Status = RunProviderUnavailable
		return
	}
	if r.Evaluator.Status == EvaluatorTimeout {
		r.Status = RunEvaluatorTimeout
		return
	}
	if r.Evaluator.Status == EvaluatorError {
		r.Status = RunEvaluatorError
		return
	}
	if timedOut {
		r.Status = RunTimeout
		return
	}
	if workerErr != nil && r.Evaluator.Status != EvaluatorPass && r.Evaluator.Status != EvaluatorFail {
		r.Status = RunInfrastructure
		return
	}
	switch r.Evaluator.Status {
	case EvaluatorPass:
		r.Status = RunCompleted
	case EvaluatorFail:
		r.Status = RunTaskFailed
	default:
		r.Status = RunInfrastructure
	}
}

// WriteResult atomically writes a redacted result and returns its path.
func WriteResult(path string, result Result) error {
	if path == "" {
		return errors.New("bench: result path is empty")
	}
	return withOutputLock(context.Background(), filepath.Dir(path), func() error {
		return writeResultUnlocked(path, result)
	})
}

func writeResultUnlocked(path string, result Result) error {
	result.Derive()
	result = RedactResult(result)
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, append(body, '\n'), 0o600)
}

// writeAtomicFile is shared by every small benchmark control artifact. A
// process killed during a write must leave either the previous complete file or
// no file, never a JSON prefix that looks resumable.
func writeAtomicFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bench-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func LoadResult(path string) (Result, error) {
	body, err := os.ReadFile(path) //nolint:gosec // result path supplied by operator
	if err != nil {
		return Result{}, err
	}
	var r Result
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Result{}, fmt.Errorf("bench: parse result %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Result{}, fmt.Errorf("bench: result %s contains multiple JSON values", path)
		}
		return Result{}, fmt.Errorf("bench: parse result %s: %w", path, err)
	}
	if r.SchemaVersion != ResultSchemaVersion {
		return Result{}, fmt.Errorf("bench: result schema %d is unsupported (want %d)", r.SchemaVersion, ResultSchemaVersion)
	}
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.PairID) == "" || strings.TrimSpace(r.TaskID) == "" {
		return Result{}, fmt.Errorf("bench: result %s has incomplete run identity", path)
	}
	if !r.Mode.Valid() || !r.Status.Valid() {
		return Result{}, fmt.Errorf("bench: result %s has invalid mode or status", path)
	}
	if err := validateResultSemantics(r); err != nil {
		return Result{}, fmt.Errorf("bench: result %s is semantically invalid: %w", path, err)
	}
	return r, nil
}

func validateResultSemantics(r Result) error {
	derived := r.Derived
	r.Derive()
	if body, err := CanonicalJSON(derived); err != nil {
		return err
	} else if actual, err := CanonicalJSON(r.Derived); err != nil {
		return err
	} else if string(body) != string(actual) {
		return errors.New("derived success/failure fields do not match evaluator and system results")
	}
	if r.Status == RunCompleted && (r.Evaluator.Status != EvaluatorPass || !r.Evaluator.Independent) {
		return errors.New("completed result has no independent evaluator pass")
	}
	if r.Status == RunTaskFailed && r.Evaluator.Status != EvaluatorFail {
		return errors.New("task_failed result has no evaluator failure")
	}
	for name, value := range map[string]*int{
		"generation_requests": r.Execution.GenerationRequests, "assistant_turns": r.Execution.AssistantTurns,
		"tool_calls": r.Execution.ToolCalls, "reads": r.Execution.Reads, "edits": r.Execution.Edits,
		"verification_attempts": r.Execution.VerificationAttempts, "retries": r.Execution.Retries,
	} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	for name, value := range map[string]*int64{
		"input_tokens": r.Execution.InputTokens, "output_tokens": r.Execution.OutputTokens,
		"cached_input_tokens": r.Execution.CachedInputTokens, "total_tokens": r.Execution.TotalTokens,
	} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	return nil
}

// RedactResult is defense in depth for future fields. It walks JSON keys and
// redacts credential-looking values before any artifact is persisted.
var credentialValuePattern = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret|password|authorization)([=:][[:space:]]*)[^[:space:],;]+`)
var bearerPattern = regexp.MustCompile(`(?i)(bearer[[:space:]]+)[A-Za-z0-9._~+/=-]+`)

func RedactText(in string) string {
	in = credentialValuePattern.ReplaceAllString(in, `${1}${2}[REDACTED]`)
	return bearerPattern.ReplaceAllString(in, `${1}[REDACTED]`)
}

func RedactResult(in Result) Result {
	body, err := json.Marshal(in)
	if err != nil {
		return in
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return in
	}
	value = redactJSON(value)
	clean, err := json.Marshal(value)
	if err != nil {
		return in
	}
	var out Result
	if json.Unmarshal(clean, &out) != nil {
		return in
	}
	return out
}

func redactJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "api_key") ||
				strings.Contains(lk, "credential") || strings.Contains(lk, "authorization") || strings.Contains(lk, "database") || strings.Contains(lk, "dsn") ||
				strings.Contains(lk, "token") && k != "input_tokens" && k != "output_tokens" && k != "total_tokens" && k != "cached_input_tokens" {
				// Do not redact count fields; redact only string values or
				// explicitly credential-named fields.
				if _, ok := child.(string); ok || strings.Contains(lk, "key") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") || strings.Contains(lk, "credential") || strings.Contains(lk, "authorization") {
					out[k] = "[REDACTED]"
					continue
				}
			}
			out[k] = redactJSON(child)
		}
		return out
	case []any:
		for i := range v {
			v[i] = redactJSON(v[i])
		}
		return v
	case string:
		return RedactText(v)
	default:
		return value
	}
}

// ResultExists reports whether a completed machine-readable result is present.
func ResultExists(path string) bool {
	r, err := LoadResult(path)
	return err == nil && r.RunID != "" && r.Status != ""
}

// cloneMetrics is retained for callers that used the old value-copy helper.
//
//nolint:unused
func cloneMetrics(m Metrics) Metrics { return m }

// resultContext is retained for adapters that used the old context helper.
//
//nolint:unused
func resultContext(ctx context.Context, r Result) context.Context { return ctx }
