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

func (l Limits) WallClock() time.Duration {
	if l.WallClockSeconds <= 0 {
		return 10 * time.Minute
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

	path        string
	oracleRoot  string
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
	return body, ok
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
	if strings.TrimSpace(t.BaseCommit) == "" {
		problems = append(problems, "base_commit is required")
	}
	if t.Fixture == "" && t.BaseCommit == "fixture" {
		problems = append(problems, "base_commit=fixture requires fixture")
	}
	if t.Evaluator.Command.Empty() && len(t.Evaluator.Steps) == 0 {
		problems = append(problems, "evaluator.command or evaluator.steps is required")
	}
	for _, step := range t.Evaluator.Steps {
		if strings.TrimSpace(step.Name) == "" || step.Command.Empty() {
			problems = append(problems, "every evaluator step needs a name and command")
		}
	}
	if len(t.Evaluator.Files) == 0 && len(t.Evaluator.InlineFiles) == 0 && t.Evaluator.Oracle == "" {
		// A command-only oracle is valid for projects whose tests are already
		// hidden by the evaluator image, but a benchmark with no declared
		// hidden material is almost always a visible-test benchmark by
		// accident. Keep it possible while making the omission explicit.
		t.Evaluator.Required = false
	}
	if err := t.Limits.Validate(); err != nil {
		problems = append(problems, err.Error())
	}
	if t.NetworkPolicy != "" && t.NetworkPolicy != "none" && t.NetworkPolicy != "allowlist" && t.NetworkPolicy != "host" {
		problems = append(problems, fmt.Sprintf("network_policy %q is not none, allowlist, or host", t.NetworkPolicy))
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
	Tasks         []Task            `yaml:"-" json:"tasks,omitempty"`

	path string
	hash string
}

type RunnerConfig struct {
	Name            string            `yaml:"name,omitempty" json:"name,omitempty"`
	Version         string            `yaml:"version,omitempty" json:"version,omitempty"`
	Config          map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	OpenCodeVersion string            `yaml:"opencode_version,omitempty" json:"opencode_version,omitempty"`
	Concurrency     int               `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	CachePolicy     string            `yaml:"cache_policy,omitempty" json:"cache_policy,omitempty"`
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
	if s.Official && s.Smoke {
		return fmt.Errorf("bench suite %q: a smoke suite cannot be official", s.ID)
	}
	if s.Runner.Concurrency < 0 {
		return fmt.Errorf("bench suite %q: runner.concurrency cannot be negative", s.ID)
	}
	if len(s.Tasks) == 0 {
		return fmt.Errorf("bench suite %q: no tasks", s.ID)
	}
	seen := map[string]bool{}
	for i := range s.Tasks {
		if err := s.Tasks[i].Validate(); err != nil {
			return err
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
		if skip != nil && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			paths = append(paths, rel)
		}
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
		fmt.Fprintf(h, "%d:%s=%d:", len(rel), rel, len(body))
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
