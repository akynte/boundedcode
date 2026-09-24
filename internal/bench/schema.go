// Package bench implements the versioned, paired RAW/BOUNDED benchmark
// harness.  It deliberately sits outside package supervisor: the harness may
// call production task execution, but it does not get a second Supervisor or
// a benchmark-only completion contract.
package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// SuiteSchemaVersion and TaskSchemaVersion are intentionally independent.
	// A suite may add scheduling metadata without making every task file a
	// different format, and a task may be reviewed without accepting a new
	// suite revision.
	SuiteSchemaVersion = 1
	TaskSchemaVersion  = 1

	ResultSchemaVersion   = 1
	ManifestSchemaVersion = 1
)

// Mode is the experimental arm. The values are part of the result schema.
type Mode string

const (
	Raw     Mode = "raw"
	Bounded Mode = "bounded"
)

func (m Mode) Valid() bool { return m == Raw || m == Bounded }

// Command is an evaluator/setup command.  A YAML scalar is accepted for
// ergonomic task files and is executed as a shell command; a sequence is the
// preferred form because it preserves argv boundaries.  Workers never receive
// an evaluator Command: it is removed at the worker boundary.
type Command struct {
	Argv  []string `yaml:"-" json:"argv,omitempty"`
	Shell bool     `yaml:"-" json:"shell,omitempty"`
}

func (c *Command) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if strings.TrimSpace(node.Value) == "" {
			return fmt.Errorf("command is empty")
		}
		c.Argv = []string{node.Value}
		c.Shell = true
		return nil
	case yaml.SequenceNode:
		var argv []string
		if err := node.Decode(&argv); err != nil {
			return err
		}
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			return fmt.Errorf("command has no executable")
		}
		c.Argv = argv
		return nil
	default:
		return fmt.Errorf("command must be a string or argv sequence")
	}
}

func (c Command) Empty() bool { return len(c.Argv) == 0 || strings.TrimSpace(c.Argv[0]) == "" }

func (c Command) Validate() error {
	if c.Empty() {
		return fmt.Errorf("command is empty")
	}
	for i, arg := range c.Argv {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("command argument %d is empty", i)
		}
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("command argument %d contains NUL", i)
		}
	}
	return nil
}

// ModelConfig is the part of a model route that must be held constant across
// paired arms.  Values which cannot be measured are pointers: null means
// unavailable, not zero and not "same as the other arm".
type ModelConfig struct {
	Provider        string   `yaml:"provider,omitempty" json:"provider"`
	Model           string   `yaml:"model,omitempty" json:"model"`
	Runtime         string   `yaml:"runtime,omitempty" json:"runtime"`
	ModelFile       string   `yaml:"model_file,omitempty" json:"model_file"`
	ModelFileSHA256 string   `yaml:"model_file_sha256,omitempty" json:"model_file_sha256"`
	Quantization    string   `yaml:"quantization,omitempty" json:"quantization"`
	ContextTokens   int      `yaml:"context_tokens,omitempty" json:"context_tokens"`
	Temperature     *float64 `yaml:"temperature,omitempty" json:"temperature"`
	TopP            *float64 `yaml:"top_p,omitempty" json:"top_p"`
	TopK            *int     `yaml:"top_k,omitempty" json:"top_k"`
	Seed            *int64   `yaml:"seed,omitempty" json:"seed"`
}

// Limits are externally enforced experiment limits.  A task may tighten these;
// the scheduler may tighten them further, but it may not silently give one arm
// more time or more requests than its pair.
type Limits struct {
	WallClockSeconds        int `yaml:"wall_clock_seconds,omitempty" json:"wall_clock_seconds"`
	MaxGenerationRequests   int `yaml:"max_generation_requests,omitempty" json:"max_generation_requests"`
	MaxVerificationAttempts int `yaml:"max_verification_attempts,omitempty" json:"max_verification_attempts"`
	MaxTokens               int `yaml:"max_tokens,omitempty" json:"max_tokens"`
	Concurrency             int `yaml:"concurrency,omitempty" json:"concurrency"`
}

const (
	DefaultWallClockSeconds  = 10 * 60
	DefaultVerificationTries = 3
)

// EffectiveLimits makes the harness defaults explicit before they cross an
// adapter boundary. A zero in a task file means "use the production default",
// not an unlimited BOUNDED run paired with a RAW process that happens to have
// its own default.
func EffectiveLimits(l Limits) Limits {
	if l.WallClockSeconds <= 0 {
		l.WallClockSeconds = DefaultWallClockSeconds
	}
	if l.MaxGenerationRequests <= 0 {
		l.MaxGenerationRequests = DefaultGenerationRequests
	}
	if l.MaxVerificationAttempts <= 0 {
		l.MaxVerificationAttempts = DefaultVerificationTries
	}
	if l.Concurrency <= 0 {
		l.Concurrency = 1
	}
	return l
}

func (l Limits) WallClock() time.Duration {
	if l.WallClockSeconds <= 0 {
		return time.Duration(DefaultWallClockSeconds) * time.Second
	}
	return time.Duration(l.WallClockSeconds) * time.Second
}

func (l Limits) Validate() error {
	if l.WallClockSeconds < 0 || l.MaxGenerationRequests < 0 || l.MaxVerificationAttempts < 0 || l.MaxTokens < 0 {
		return fmt.Errorf("limits cannot be negative")
	}
	if l.Concurrency < 0 {
		return fmt.Errorf("limits.concurrency cannot be negative")
	}
	return nil
}

func validateEnvironment(label string, values map[string]string) error {
	for key, value := range values {
		if !validEnvName(key) {
			return fmt.Errorf("%s variable %q has an invalid name", label, key)
		}
		if reservedEnvName(key) {
			return fmt.Errorf("%s variable %q is reserved by the benchmark harness", label, key)
		}
		lk := strings.ToLower(key)
		if strings.Contains(lk, "key") || strings.Contains(lk, "token") || strings.Contains(lk, "secret") ||
			strings.Contains(lk, "password") || strings.Contains(lk, "credential") || strings.Contains(lk, "authorization") ||
			strings.Contains(lk, "oracle") || strings.Contains(lk, "evaluator") || strings.Contains(lk, "hidden") || strings.Contains(lk, "database") || strings.Contains(lk, "dsn") || (strings.Contains(lk, "url") && strings.Contains(value, "@")) {
			return fmt.Errorf("%s variable %q is credential- or evaluator-shaped and cannot cross the benchmark boundary", label, key)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%s variable %q contains NUL", label, key)
		}
	}
	return nil
}

func safeSuiteID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func reservedEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "BENCH_") || strings.HasPrefix(upper, "XDG_") || strings.HasPrefix(upper, "BC_") || strings.HasPrefix(upper, "GIT_") {
		return true
	}
	switch upper {
	case "HOME", "TMPDIR", "GOTMPDIR", "PATH", "LANG", "LC_ALL", "NO_COLOR", "TERM",
		"GOCACHE", "GOMODCACHE", "GOPATH", "GOPROXY", "GOTOOLCHAIN", "GOFLAGS", "GOENV", "GO111MODULE",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "LD_PRELOAD", "LD_AUDIT", "LD_LIBRARY_PATH",
		"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "BASH_ENV", "ENV", "CDPATH", "PYTHONPATH", "PYTHONSTARTUP",
		"NODE_OPTIONS", "NODE_PATH", "RUBYOPT", "RUBYLIB", "PERL5LIB", "PERL5OPT", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS":
		return true
	default:
		return false
	}
}

func validateModel(m ModelConfig) error {
	if m.ContextTokens < 0 {
		return fmt.Errorf("model.context_tokens cannot be negative")
	}
	if m.Temperature != nil && (math.IsNaN(*m.Temperature) || math.IsInf(*m.Temperature, 0) || *m.Temperature < 0 || *m.Temperature > 2) {
		return fmt.Errorf("model.temperature must be finite and between 0 and 2")
	}
	if m.TopP != nil && (math.IsNaN(*m.TopP) || math.IsInf(*m.TopP, 0) || *m.TopP < 0 || *m.TopP > 1) {
		return fmt.Errorf("model.top_p must be finite and between 0 and 1")
	}
	if m.TopK != nil && *m.TopK < 0 {
		return fmt.Errorf("model.top_k cannot be negative")
	}
	if m.Seed != nil {
		maxInt := int64(^uint(0) >> 1)
		if *m.Seed > maxInt || *m.Seed < -maxInt-1 {
			return fmt.Errorf("model.seed does not fit the provider's integer seed type")
		}
	}
	return nil
}

// Evaluator is declarative independent verification.  Files and Oracle are
// evaluator-only.  They are loaded into unexported task state and never enter
// WorkerRequest, a model prompt, a candidate workspace, or a result's system
// result section.
type StepSpec struct {
	Name           string  `yaml:"name" json:"name"`
	Command        Command `yaml:"command" json:"command"`
	TimeoutSeconds int     `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Required       *bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type Evaluator struct {
	Oracle           string            `yaml:"oracle,omitempty" json:"oracle,omitempty"`
	Files            []string          `yaml:"files,omitempty" json:"files,omitempty"`
	InlineFiles      map[string]string `yaml:"inline_files,omitempty" json:"-"`
	Command          Command           `yaml:"command,omitempty" json:"command,omitempty"`
	Steps            []StepSpec        `yaml:"steps,omitempty" json:"steps,omitempty"`
	TimeoutSeconds   int               `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	MustNotChange    []string          `yaml:"must_not_change,omitempty" json:"must_not_change,omitempty"`
	Canaries         []string          `yaml:"canaries,omitempty" json:"canaries,omitempty"`
	Environment      map[string]string `yaml:"environment,omitempty" json:"-"`
	Required         bool              `yaml:"required,omitempty" json:"required,omitempty"`
	EvaluatorVersion string            `yaml:"version,omitempty" json:"version,omitempty"`
}

// Task is one immutable benchmark case.  Repository is deliberately a string:
// URLs, local paths, and fixture references can all be represented without a
// benchmark package inheriting one language's checkout conventions.
type Task struct {
	SchemaVersion     int               `yaml:"schema_version,omitempty" json:"schema_version"`
	ID                string            `yaml:"id" json:"id"`
	Title             string            `yaml:"title" json:"title"`
	Description       string            `yaml:"description" json:"description"`
	Repository        string            `yaml:"repository" json:"repository"`
	BaseCommit        string            `yaml:"base_commit" json:"base_commit"`
	Fixture           string            `yaml:"fixture,omitempty" json:"fixture,omitempty"`
	Languages         []string          `yaml:"languages,omitempty" json:"languages,omitempty"`
	Setup             Command           `yaml:"setup,omitempty" json:"setup,omitempty"`
	VisibleValidation []Command         `yaml:"visible_validation,omitempty" json:"visible_validation,omitempty"`
	Evaluator         Evaluator         `yaml:"evaluator" json:"evaluator"`
	Verification      string            `yaml:"verification,omitempty" json:"verification,omitempty"`
	Limits            Limits            `yaml:"limits" json:"limits"`
	NetworkPolicy     string            `yaml:"network_policy,omitempty" json:"network_policy"`
	Environment       map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	MutableScope      []string          `yaml:"mutable_scope,omitempty" json:"mutable_scope,omitempty"`
	Tags              []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Category          string            `yaml:"category,omitempty" json:"category,omitempty"`
	Difficulty        map[string]string `yaml:"difficulty,omitempty" json:"difficulty,omitempty"`
	Provenance        map[string]string `yaml:"provenance,omitempty" json:"provenance,omitempty"`
	// WorkerDriver is an infrastructure hook for deterministic smoke tests. It
	// is refused for an official manifest; real runs use the configured raw or
	// bounded adapter instead.
	WorkerDriver string `yaml:"worker_driver,omitempty" json:"worker_driver,omitempty"`
	// FixtureHash is the loader-computed identity of the immutable fixture
	// bytes. It is persisted in manifests/results but never crosses the
	// worker boundary.
	FixtureHash string `yaml:"-" json:"fixture_hash,omitempty"`

	path        string
	oracleRoot  string
	oracleFiles map[string][]byte
	hiddenFiles map[string][]byte
	hiddenHash  string
}

// TaskPath returns the file from which a task was loaded.
func (t Task) TaskPath() string { return t.path }

// OracleRoot returns the evaluator-only directory. It is intentionally exposed
// to the evaluator and loader tests, never to WorkerRequest.
func (t Task) OracleRoot() string { return t.oracleRoot }

// EvaluatorHashForDisplay exposes only the content identity, never oracle
// bytes, for validation and planning output.
func (t Task) EvaluatorHashForDisplay() string {
	if t.hiddenHash == "" {
		return "unavailable"
	}
	return t.hiddenHash
}

// HiddenFile returns one evaluator-only file. Callers must not serialize this
// value into a worker request.
func (t Task) HiddenFile(name string) ([]byte, bool) {
	body, ok := t.hiddenFiles[filepath.ToSlash(name)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), body...), true
}

func (t Task) Validate() error {
	var problems []string
	if t.SchemaVersion == 0 {
		t.SchemaVersion = TaskSchemaVersion
	}
	if t.SchemaVersion != TaskSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema_version %d is unsupported", t.SchemaVersion))
	}
	if strings.TrimSpace(t.ID) == "" {
		problems = append(problems, "id is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		problems = append(problems, "title is required")
	}
	if strings.TrimSpace(t.Description) == "" {
		problems = append(problems, "description is required")
	}
	if strings.TrimSpace(t.Repository) == "" {
		problems = append(problems, "repository is required")
	}
	base := strings.TrimSpace(t.BaseCommit)
	if base == "" {
		problems = append(problems, "base_commit is required")
	}
	if t.Fixture == "" && base == "fixture" {
		problems = append(problems, "base_commit=fixture requires fixture")
	}
	if t.Fixture != "" && base != "fixture" && !strings.HasPrefix(base, "content-sha256:") && !isHexObjectID(base) {
		problems = append(problems, "fixture base_commit must be fixture, content-sha256:<hash>, or a git object id")
	}
	if t.Fixture == "" && !isHexObjectID(base) {
		problems = append(problems, "repository base_commit must be a git object id")
	}
	if strings.HasPrefix(base, "content-sha256:") {
		hash := strings.TrimPrefix(base, "content-sha256:")
		if len(hash) != 32 && len(hash) != 64 {
			problems = append(problems, "content-sha256 base_commit must contain a 32 or 64 character hash")
		} else if _, err := hex.DecodeString(hash); err != nil {
			problems = append(problems, "content-sha256 base_commit is not hexadecimal")
		}
	}
	if t.Evaluator.Command.Empty() && len(t.Evaluator.Steps) == 0 {
		problems = append(problems, "evaluator.command or evaluator.steps is required")
	}
	if !t.Evaluator.Command.Empty() && len(t.Evaluator.Steps) > 0 {
		problems = append(problems, "declare evaluator.command or evaluator.steps, not both")
	}
	if !t.Evaluator.Command.Empty() {
		if err := t.Evaluator.Command.Validate(); err != nil {
			problems = append(problems, "evaluator.command: "+err.Error())
		}
	}
	if !t.Setup.Empty() {
		if err := t.Setup.Validate(); err != nil {
			problems = append(problems, "setup: "+err.Error())
		}
	}
	for i, command := range t.VisibleValidation {
		if err := command.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("visible_validation[%d]: %s", i, err))
		}
	}
	for i, step := range t.Evaluator.Steps {
		if strings.TrimSpace(step.Name) == "" || step.Command.Empty() {
			problems = append(problems, fmt.Sprintf("evaluator step %d needs a name and command", i))
			continue
		}
		if err := step.Command.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("evaluator step %q: %s", step.Name, err))
		}
		if step.TimeoutSeconds < 0 {
			problems = append(problems, fmt.Sprintf("evaluator step %q timeout_seconds cannot be negative", step.Name))
		}
	}
	if t.Evaluator.TimeoutSeconds < 0 {
		problems = append(problems, "evaluator.timeout_seconds cannot be negative")
	}
	canaries := map[string]bool{}
	for _, canary := range t.Evaluator.Canaries {
		canary = strings.TrimSpace(canary)
		if len(canary) < 12 {
			problems = append(problems, "evaluator canaries must be at least 12 characters")
		}
		if canaries[canary] {
			problems = append(problems, "evaluator canaries must be unique")
		}
		canaries[canary] = true
	}
	for _, name := range t.Evaluator.Files {
		if _, err := safeRelative(name); err != nil {
			problems = append(problems, fmt.Sprintf("evaluator file %q: %s", name, err))
		}
	}
	for name := range t.Evaluator.InlineFiles {
		if _, err := safeRelative(name); err != nil {
			problems = append(problems, fmt.Sprintf("inline evaluator file %q: %s", name, err))
		}
	}
	for _, path := range t.Evaluator.MustNotChange {
		if _, err := safeRelative(path); err != nil {
			problems = append(problems, fmt.Sprintf("evaluator must_not_change %q: %s", path, err))
		}
	}
	if err := validateEnvironment("task", t.Environment); err != nil {
		problems = append(problems, err.Error())
	}
	if err := validateEnvironment("evaluator", t.Evaluator.Environment); err != nil {
		problems = append(problems, err.Error())
	}
	if err := t.Limits.Validate(); err != nil {
		problems = append(problems, err.Error())
	}
	if t.NetworkPolicy != "" && t.NetworkPolicy != "none" && t.NetworkPolicy != "allowlist" && t.NetworkPolicy != "host" {
		problems = append(problems, fmt.Sprintf("network_policy %q is not none, allowlist, or host", t.NetworkPolicy))
	}
	if t.Verification != "" && t.Verification != "low" && t.Verification != "standard" && t.Verification != "high" {
		problems = append(problems, fmt.Sprintf("verification %q is not low, standard, or high", t.Verification))
	}
	if t.WorkerDriver != "" && t.WorkerDriver != "smoke" {
		problems = append(problems, fmt.Sprintf("unknown worker_driver %q", t.WorkerDriver))
	}
	if len(problems) > 0 {
		return fmt.Errorf("bench task %q: %s", t.ID, strings.Join(problems, "; "))
	}
	return nil
}

// Suite is the versioned collection and the common experiment configuration.
type Suite struct {
	SchemaVersion int               `yaml:"schema_version" json:"schema_version"`
	ID            string            `yaml:"id" json:"id"`
	Version       string            `yaml:"version" json:"version"`
	Title         string            `yaml:"title,omitempty" json:"title,omitempty"`
	Description   string            `yaml:"description,omitempty" json:"description,omitempty"`
	Official      bool              `yaml:"official,omitempty" json:"official"`
	Smoke         bool              `yaml:"smoke,omitempty" json:"smoke"`
	TaskRefs      []string          `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	Model         ModelConfig       `yaml:"model" json:"model"`
	Runner        RunnerConfig      `yaml:"runner,omitempty" json:"runner,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	Tasks         []Task            `yaml:"-" json:"-"`

	path string
	hash string
}

const UnattendedGatePolicyPermissive = "permissive"

func (r RunnerConfig) effective() RunnerConfig {
	if r.UnattendedGatePolicy == "" {
		r.UnattendedGatePolicy = UnattendedGatePolicyPermissive
	}
	return r
}

type RunnerConfig struct {
	Name            string            `yaml:"name,omitempty" json:"name,omitempty"`
	Version         string            `yaml:"version,omitempty" json:"version,omitempty"`
	Config          map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	OpenCodeVersion string            `yaml:"opencode_version,omitempty" json:"opencode_version,omitempty"`
	Concurrency     int               `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	CachePolicy     string            `yaml:"cache_policy,omitempty" json:"cache_policy,omitempty"`
	// UnattendedGatePolicy is the explicit broker policy used when no human
	// operator is present. Benchmarks currently support only the permissive
	// policy; recording it prevents a future gate change from silently altering
	// an experiment's identity.
	UnattendedGatePolicy string `yaml:"unattended_gate_policy,omitempty" json:"unattended_gate_policy,omitempty"`
}

func (s Suite) Validate() error {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SuiteSchemaVersion
	}
	if s.SchemaVersion != SuiteSchemaVersion {
		return fmt.Errorf("bench suite %q: schema_version %d is unsupported", s.ID, s.SchemaVersion)
	}
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("bench suite: id and version are required")
	}
	if !safeSuiteID(s.ID) {
		return fmt.Errorf("bench suite %q: id may contain only letters, digits, '.', '_' and '-'", s.ID)
	}
	if s.Official && s.Smoke {
		return fmt.Errorf("bench suite %q: a smoke suite cannot be official", s.ID)
	}
	if err := validateModel(s.Model); err != nil {
		return fmt.Errorf("bench suite %q: %w", s.ID, err)
	}
	if err := validateEnvironment("suite", s.Environment); err != nil {
		return fmt.Errorf("bench suite %q: %w", s.ID, err)
	}
	if err := validateEnvironment("runner", s.Runner.Config); err != nil {
		return fmt.Errorf("bench suite %q: %w", s.ID, err)
	}
	if s.Model.ModelFileSHA256 != "" {
		hash := strings.TrimPrefix(s.Model.ModelFileSHA256, "sha256:")
		if len(hash) != 64 {
			return fmt.Errorf("bench suite %q: model_file_sha256 must be a SHA-256 digest", s.ID)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fmt.Errorf("bench suite %q: model_file_sha256 is not hexadecimal", s.ID)
		}
	}
	if s.Official {
		if s.Model.Provider == "" || s.Model.Model == "" || s.Model.ContextTokens <= 0 {
			return fmt.Errorf("bench suite %q: official runs require provider, model, and context_tokens", s.ID)
		}
	}
	if s.Runner.Concurrency < 0 {
		return fmt.Errorf("bench suite %q: runner.concurrency cannot be negative", s.ID)
	}
	if s.Runner.UnattendedGatePolicy != "" && s.Runner.UnattendedGatePolicy != UnattendedGatePolicyPermissive {
		return fmt.Errorf("bench suite %q: runner.unattended_gate_policy %q is not supported", s.ID, s.Runner.UnattendedGatePolicy)
	}
	if len(s.Tasks) == 0 {
		return fmt.Errorf("bench suite %q: no tasks", s.ID)
	}
	seen := map[string]bool{}
	for i := range s.Tasks {
		if err := s.Tasks[i].Validate(); err != nil {
			return err
		}
		if s.Official && s.Tasks[i].WorkerDriver != "" {
			return fmt.Errorf("bench suite %q: official task %q cannot select worker_driver", s.ID, s.Tasks[i].ID)
		}
		if s.Official && len(s.Tasks[i].Evaluator.Files) == 0 && len(s.Tasks[i].Evaluator.InlineFiles) == 0 && s.Tasks[i].Evaluator.Oracle == "" {
			return fmt.Errorf("bench suite %q: official task %q must declare hidden evaluator material", s.ID, s.Tasks[i].ID)
		}
		if s.Official && len(s.Tasks[i].Evaluator.Canaries) == 0 {
			return fmt.Errorf("bench suite %q: official task %q must declare an evaluator canary", s.ID, s.Tasks[i].ID)
		}
		if s.Official && len(s.Tasks[i].Evaluator.Steps) > 0 {
			required := false
			for _, step := range s.Tasks[i].Evaluator.Steps {
				if step.Required == nil || *step.Required {
					required = true
					break
				}
			}
			if !required {
				return fmt.Errorf("bench suite %q: official task %q needs at least one required evaluator step", s.ID, s.Tasks[i].ID)
			}
		}
		if seen[s.Tasks[i].ID] {
			return fmt.Errorf("bench suite %q: duplicate task id %q", s.ID, s.Tasks[i].ID)
		}
		seen[s.Tasks[i].ID] = true
	}
	return nil
}

func (s Suite) SuitePath() string { return s.path }

// WithWallClock returns a copy with one external timeout applied to every task
// and a recomputed suite identity. It keeps a CLI override paired and visible
// instead of leaving it as an unrecorded per-arm setting.
func (s Suite) WithWallClock(timeout time.Duration) Suite {
	if timeout <= 0 {
		return s
	}
	out := s
	out.Tasks = append([]Task(nil), s.Tasks...)
	for i := range out.Tasks {
		out.Tasks[i].Limits.WallClockSeconds = int(timeout / time.Second)
		if out.Tasks[i].Limits.WallClockSeconds <= 0 {
			out.Tasks[i].Limits.WallClockSeconds = 1
		}
	}
	out.hash = suiteHash(out)
	return out
}

// Hash is a stable content identity for the suite and all evaluator material.
// It is computed during loading and is safe to persist in a result.
func (s Suite) Hash() string { return s.hash }

// TaskByID returns a task by stable id.
func (s Suite) TaskByID(id string) (Task, bool) {
	for _, t := range s.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// CanonicalJSON returns deterministic JSON for values used in fingerprints.
// encoding/json sorts map keys, and the schema structs have fixed field order;
// callers should not depend on whitespace.
func CanonicalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func digestBytes(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		// Length prefix prevents concatenation collisions.
		fmt.Fprintf(h, "%d:", len(p))
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashDirectory(root string, skip func(string) bool) (string, error) {
	// The loader uses this for fixture/oracle identities.  Kept here rather
	// than in a command-specific package so all benchmark inputs hash alike.
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture contains symlink %s", rel)
		}
		if filepath.Base(rel) == ".git" {
			if !d.IsDir() {
				return fmt.Errorf("fixture .git is not a directory")
			}
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("fixture contains non-regular file %s", rel)
		}
		if skip != nil && skip(rel) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // path derived from the fixture root
		if err != nil {
			return "", err
		}
		info, infoErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if infoErr != nil {
			return "", infoErr
		}
		fmt.Fprintf(h, "%d:%s=%d:%o:", len(rel), rel, len(body), info.Mode().Perm())
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
