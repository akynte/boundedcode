// Package config defines the typed, validated configuration. Configuration is
// layered: built-in defaults < user config (~/.config/boundedcode/config.yaml)
// < workspace overrides (<config dir>/workspaces/<workspace>.yaml, limited to
// task policy: budgets, escalation and a few switches; see WorkspaceOverride).
// Unknown keys are rejected so typos fail loudly.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the config schema version this binary writes.
const CurrentVersion = 1

// Config is the user-level configuration.
type Config struct {
	Version      int              `yaml:"version"`
	ModelsDir    string           `yaml:"models_dir"`
	DefaultModel string           `yaml:"default_model"`
	Inference    InferenceConfig  `yaml:"inference"`
	Agent        AgentConfig      `yaml:"agent"`
	RepoIntel    RepoIntelConfig  `yaml:"repointel"`
	Frontier     FrontierConfig   `yaml:"frontier"`
	Sandbox      SandboxConfig    `yaml:"sandbox"`
	Budgets      Budgets          `yaml:"budgets"`
	Escalation   EscalationConfig `yaml:"escalation"`
	Task         TaskConfig       `yaml:"task"`
}

// TaskConfig configures how requests are read before implementation.
type TaskConfig struct {
	// Contract derives a compact task contract (required behaviour, allowed
	// alternatives, material ambiguities) before the first attempt.
	Contract bool `yaml:"contract"`
	// Ambiguity: "ask" blocks a materially ambiguous task until the user
	// clarifies it (task run --clarify); "proceed" records SPEC_AMBIGUOUS and
	// has the agent state and demonstrate its reading (benchmarks).
	Ambiguity string `yaml:"ambiguity"`
}

// InferenceConfig selects and configures the local inference runtime.
type InferenceConfig struct {
	// Mode is "managed" (we start llama-server) or "external" (user runs it).
	Mode           string   `yaml:"mode"`
	ServerBinary   string   `yaml:"server_binary"`
	BenchBinary    string   `yaml:"bench_binary"`
	ExternalURL    string   `yaml:"external_url"`
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	StartupTimeout Duration `yaml:"startup_timeout"`
	// RequestTimeout bounds a single completion; it doubles as stall detection.
	RequestTimeout Duration `yaml:"request_timeout"`
	// IdleSleep makes a managed llama-server unload the model after this much
	// inactivity (llama-server --sleep-idle-seconds) and reload it on the
	// next request, so an idle server does not hold RAM/VRAM. 0 disables.
	IdleSleep Duration `yaml:"idle_sleep"`
}

// AgentConfig configures the agent runtime adapter.
type AgentConfig struct {
	Runtime string `yaml:"runtime"` // "openhands"
	// AdapterDir is the OpenHands adapter project (contains pyproject.toml).
	AdapterDir string `yaml:"adapter_dir"`
	// Image is the container image used when the sandbox is docker.
	Image         string `yaml:"image"`
	MaxIterations int    `yaml:"max_iterations"`
	// CondenserMaxEvents triggers OpenHands' summarizing condenser.
	CondenserMaxEvents int `yaml:"condenser_max_events"`
	// MaxOutputTokens caps one model response (thinking plus visible
	// output; thinking alone is capped per model profile by
	// server.reasoning_budget).
	MaxOutputTokens int `yaml:"max_output_tokens"`
	// Strategy bounds one attempt's approach by its progress.
	Strategy StrategyBudget `yaml:"strategy"`
}

// StrategyBudget stops an attempt that keeps generating without progress
// (see orchestrator/governor.go). Progress is the attempt's first edit, a new
// test file, or an agent-run test going from failing to passing. Measured on
// the 2026-10 validation runs: productive attempts generated at most ~49K
// tokens; unproductive ones 72-109K over 40-64 minutes.
type StrategyBudget struct {
	// NoProgressTokens: generated tokens allowed since the last progress.
	NoProgressTokens int `yaml:"no_progress_tokens"`
	// MaxTokens: generated tokens allowed for the whole attempt.
	MaxTokens int `yaml:"max_tokens"`
	// MaxDuration: wall-clock allowed for the attempt's agent turn.
	MaxDuration Duration `yaml:"max_duration"`
}

// RepoIntelConfig configures repository intelligence.
type RepoIntelConfig struct {
	// Provider must be "codebase-memory-mcp", the only implementation; the
	// key is kept so existing config files still load.
	Provider string `yaml:"provider"`
	Binary   string `yaml:"binary"`
	// CrossService enables the built-in cross-service contract analyzers
	// (internal/xservice) in indexing, context packs and escalation policy.
	CrossService bool `yaml:"cross_service"`
	// Serena adds LSP-backed symbol navigation (ADR-0008).
	Serena SerenaConfig `yaml:"serena"`
}

// SerenaConfig governs the optional Serena integration. It sets policy only;
// Serena's own options are generated per instance by internal/repointel/serena.
type SerenaConfig struct {
	Enabled bool `yaml:"enabled"`
	// Command is the serena executable; empty means the environment installed
	// by `serena setup` under the data directory.
	Command string `yaml:"command"`
	// Version must be the pinned release. It is not a selector: changing it
	// fails validation until the integration is reviewed for a new release.
	Version string `yaml:"version"`
	// AutoUpgrade must stay false; upgrades are manual and need a license review.
	AutoUpgrade bool `yaml:"auto_upgrade"`
	// Transport is "stdio" (the only supported value; no network listener).
	Transport      string   `yaml:"transport"`
	MaxInstances   int      `yaml:"max_instances"`
	IdleTimeout    Duration `yaml:"idle_timeout"`
	StartupTimeout Duration `yaml:"startup_timeout"`
	CallTimeout    Duration `yaml:"call_timeout"`
}

// SerenaVersion is the only Serena release the integration accepts (the
// last MIT-licensed release; see internal/repointel/serena).
const SerenaVersion = "1.7.0"

// FrontierConfig configures frontier escalation.
type FrontierConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // "codex" | "manual"
	Binary   string `yaml:"binary"`
	Model    string `yaml:"model"` // empty: provider default
	// RequireApproval asks the user before sending any packet.
	RequireApproval bool `yaml:"require_approval"`
	// MaxPacketTokens caps the escalation packet size.
	MaxPacketTokens int      `yaml:"max_packet_tokens"`
	Timeout         Duration `yaml:"timeout"`
	// Contain runs the frontier CLI inside a container so its own agent
	// cannot read the host filesystem (only the packet is visible).
	Contain bool `yaml:"contain"`
}

// SandboxConfig configures where agent tools and verification run.
type SandboxConfig struct {
	Kind    string `yaml:"kind"`    // "docker" | "none"
	Engine  string `yaml:"engine"`  // docker-compatible CLI: "docker" or "podman"
	Network string `yaml:"network"` // "none" (default) or "bridge"
	Memory  string `yaml:"memory"`  // e.g. "8g"
	CPUs    string `yaml:"cpus"`    // e.g. "8"
}

// Budgets bound a task's resource use. Exhaustion parks the task, never
// discards work.
type Budgets struct {
	MaxAttempts       int      `yaml:"max_attempts"`
	MaxWallClock      Duration `yaml:"max_wall_clock"`
	MaxLocalTokens    int      `yaml:"max_local_tokens"`
	MaxEscalations    int      `yaml:"max_escalations"`
	ContextPackTokens int      `yaml:"context_pack_tokens"`
}

// EscalationConfig holds the Z1-Z4 policy thresholds.
type EscalationConfig struct {
	// Z2: escalate after this many consecutive failed verification attempts...
	FailedAttempts int `yaml:"failed_attempts"`
	// ...or this many rejected, materially different strategies.
	RejectedStrategies int `yaml:"rejected_strategies"`
	// Z1/Z3 keyword sets matched against task text, paths and symbols.
	ArchitecturalRisk []string `yaml:"architectural_risk"`
	HighRiskReview    []string `yaml:"high_risk_review"`
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		Version:      CurrentVersion,
		DefaultModel: "qwen3.6-35b-a3b",
		Inference: InferenceConfig{
			Mode: "managed", ServerBinary: "llama-server", BenchBinary: "llama-bench",
			Host: "127.0.0.1", Port: 8765,
			StartupTimeout: Duration(5 * time.Minute), RequestTimeout: Duration(10 * time.Minute),
			IdleSleep: Duration(30 * time.Minute),
		},
		Task: TaskConfig{Contract: true, Ambiguity: "ask"},
		Agent: AgentConfig{
			Runtime: "openhands", Image: "boundedcode-openhands:local",
			MaxIterations: 150, CondenserMaxEvents: 80, MaxOutputTokens: 8192,
			Strategy: StrategyBudget{NoProgressTokens: 60000, MaxTokens: 100000, MaxDuration: Duration(45 * time.Minute)},
		},
		RepoIntel: RepoIntelConfig{Provider: "codebase-memory-mcp", Binary: "codebase-memory-mcp", CrossService: true,
			Serena: SerenaConfig{Enabled: false, Version: SerenaVersion, Transport: "stdio", MaxInstances: 2,
				IdleTimeout: Duration(10 * time.Minute), StartupTimeout: Duration(90 * time.Second), CallTimeout: Duration(30 * time.Second)}},
		Frontier: FrontierConfig{
			Enabled: false, Provider: "codex", Binary: "codex",
			RequireApproval: true, MaxPacketTokens: 24000, Timeout: Duration(15 * time.Minute), Contain: true,
		},
		Sandbox: SandboxConfig{Kind: "docker", Engine: "docker", Network: "none", Memory: "8g", CPUs: "8"},
		Budgets: Budgets{
			MaxAttempts: 6, MaxWallClock: Duration(4 * time.Hour), MaxLocalTokens: 4_000_000,
			MaxEscalations: 2, ContextPackTokens: 24000,
		},
		Escalation: EscalationConfig{
			FailedAttempts: 3, RejectedStrategies: 2,
			ArchitecturalRisk: []string{
				"idempotency", "idempotent", "outbox", "distributed transaction", "saga", "two-phase",
				"ledger", "accounting", "double-entry", "payment", "event contract", "schema migration",
				"breaking change", "protobuf", "openapi", "authentication", "authorization", "oauth",
				"jwt", "cryptograph", "encrypt", "signature",
			},
			HighRiskReview: []string{
				"auth", "permission", "rbac", "acl", "crypto", "password", "token", "payment", "money",
				"currency", "balance", "ledger", "iam", "terraform", "helm", "kubernetes", "k8s",
			},
		},
	}
}

// Load reads the user config at path (missing file means defaults) and
// validates the result.
func Load(path string) (Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, cfg.Validate()
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := decodeStrict(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to path atomically.
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b, 0o600)
}

// Validate checks semantic constraints.
func (c Config) Validate() error {
	var errs []error
	if c.Version != CurrentVersion {
		errs = append(errs, fmt.Errorf("version: unsupported config version %d (want %d)", c.Version, CurrentVersion))
	}
	switch c.Inference.Mode {
	case "managed":
		if c.Inference.ServerBinary == "" {
			errs = append(errs, errors.New("inference.server_binary: required in managed mode"))
		}
		if c.Inference.Port <= 0 || c.Inference.Port > 65535 {
			errs = append(errs, fmt.Errorf("inference.port: %d out of range", c.Inference.Port))
		}
	case "external":
		if c.Inference.ExternalURL == "" {
			errs = append(errs, errors.New("inference.external_url: required in external mode"))
		}
	default:
		errs = append(errs, fmt.Errorf("inference.mode: %q is not managed|external", c.Inference.Mode))
	}
	if c.Inference.IdleSleep < 0 {
		errs = append(errs, errors.New("inference.idle_sleep: must be >= 0 (0 disables)"))
	}
	// The scripted runtime exists for Go tests, which wire it directly; a
	// config file can only select a runtime the CLI can run.
	if c.Agent.Runtime != "openhands" {
		errs = append(errs, fmt.Errorf("agent.runtime: %q is not supported (openhands)", c.Agent.Runtime))
	}
	if c.Agent.MaxIterations < 1 {
		errs = append(errs, errors.New("agent.max_iterations: must be >= 1"))
	}
	if c.Task.Ambiguity != "ask" && c.Task.Ambiguity != "proceed" {
		errs = append(errs, fmt.Errorf("task.ambiguity: %q is not ask|proceed", c.Task.Ambiguity))
	}
	if c.Agent.MaxOutputTokens < 512 {
		errs = append(errs, errors.New("agent.max_output_tokens: must be >= 512"))
	}
	if st := c.Agent.Strategy; st.NoProgressTokens < 0 || st.MaxTokens < 0 || st.MaxDuration < 0 ||
		st.MaxTokens > 0 && st.NoProgressTokens > st.MaxTokens {
		errs = append(errs, errors.New("agent.strategy: budgets must be >= 0 (0 disables) and no_progress_tokens <= max_tokens"))
	}
	if c.RepoIntel.Provider != "codebase-memory-mcp" {
		errs = append(errs, fmt.Errorf("repointel.provider: %q is not supported (codebase-memory-mcp)", c.RepoIntel.Provider))
	}
	switch c.Sandbox.Kind {
	case "docker", "none":
	default:
		errs = append(errs, fmt.Errorf("sandbox.kind: %q is not docker|none", c.Sandbox.Kind))
	}
	switch c.Sandbox.Engine {
	case "docker", "podman":
	default:
		errs = append(errs, fmt.Errorf("sandbox.engine: %q is not docker|podman", c.Sandbox.Engine))
	}
	switch c.Sandbox.Network {
	case "none", "bridge":
	default:
		errs = append(errs, fmt.Errorf("sandbox.network: %q is not none|bridge", c.Sandbox.Network))
	}
	switch c.Frontier.Provider {
	case "codex", "manual":
	default:
		errs = append(errs, fmt.Errorf("frontier.provider: %q is not codex|manual", c.Frontier.Provider))
	}
	if c.Frontier.MaxPacketTokens < 1 {
		errs = append(errs, errors.New("frontier.max_packet_tokens: must be >= 1"))
	}
	if sc := c.RepoIntel.Serena; true {
		if sc.Version != SerenaVersion {
			errs = append(errs, fmt.Errorf("repointel.serena.version: %q is not supported; the integration is pinned to the MIT-licensed Serena %s (upgrades need a license review, see docs/licensing/policy.md)", sc.Version, SerenaVersion))
		}
		if sc.AutoUpgrade {
			errs = append(errs, errors.New("repointel.serena.auto_upgrade: must be false; Serena upgrades are manual and require a license review"))
		}
		if sc.Transport != "stdio" {
			errs = append(errs, fmt.Errorf("repointel.serena.transport: %q is not supported (stdio only; Serena is never exposed on a network interface)", sc.Transport))
		}
		if sc.MaxInstances < 1 {
			errs = append(errs, errors.New("repointel.serena.max_instances: must be >= 1"))
		}
		if sc.Enabled {
			for _, t := range []struct {
				name string
				d    Duration
			}{{"idle_timeout", sc.IdleTimeout}, {"startup_timeout", sc.StartupTimeout}, {"call_timeout", sc.CallTimeout}} {
				if t.d <= 0 {
					errs = append(errs, fmt.Errorf("repointel.serena.%s: must be > 0 when serena is enabled", t.name))
				}
			}
		}
	}
	if c.Budgets.MaxAttempts < 1 {
		errs = append(errs, errors.New("budgets.max_attempts: must be >= 1"))
	}
	if c.Budgets.MaxWallClock < 0 || c.Budgets.MaxLocalTokens < 0 || c.Budgets.MaxEscalations < 0 {
		errs = append(errs, errors.New("budgets: max_wall_clock, max_local_tokens and max_escalations must be >= 0"))
	}
	if c.Budgets.ContextPackTokens < 2000 {
		errs = append(errs, errors.New("budgets.context_pack_tokens: must be >= 2000"))
	}
	if c.Escalation.FailedAttempts < 1 || c.Escalation.RejectedStrategies < 1 {
		errs = append(errs, errors.New("escalation thresholds must be >= 1"))
	}
	return errors.Join(errs...)
}

// WorkspaceOverride is the part of the configuration a workspace may
// override, in <config dir>/workspaces/<workspace>.yaml. It covers task
// policy only; machine settings (inference, sandbox, binaries) stay global,
// and verification commands live in each repository's
// .boundedcode/verification.yaml. Keys left out keep the user config value;
// a list (escalation keywords) replaces the user list.
type WorkspaceOverride struct {
	Budgets    Budgets          `yaml:"budgets"`
	Escalation EscalationConfig `yaml:"escalation"`
	RepoIntel  struct {
		CrossService bool `yaml:"cross_service"`
		Serena       struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"serena"`
	} `yaml:"repointel"`
	Frontier struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"frontier"`
}

// WorkspaceFile is the override file of a workspace.
func WorkspaceFile(configDir, workspace string) string {
	return filepath.Join(configDir, "workspaces", workspace+".yaml")
}

// LoadForWorkspace loads the user config and applies the workspace's
// override file, if any.
func LoadForWorkspace(paths Paths, workspace string) (Config, error) {
	cfg, err := Load(filepath.Join(paths.Config, "config.yaml"))
	if err != nil {
		return cfg, err
	}
	return cfg.WithWorkspace(paths.Config, workspace)
}

// WithWorkspace returns c with the workspace's override file applied and
// validated. A missing file returns c unchanged.
func (c Config) WithWorkspace(configDir, workspace string) (Config, error) {
	if workspace == "" || workspace != filepath.Base(workspace) || strings.HasPrefix(workspace, ".") {
		return c, fmt.Errorf("invalid workspace name %q", workspace)
	}
	path := WorkspaceFile(configDir, workspace)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read workspace config: %w", err)
	}
	o := WorkspaceOverride{Budgets: c.Budgets, Escalation: c.Escalation}
	o.RepoIntel.CrossService = c.RepoIntel.CrossService
	o.RepoIntel.Serena.Enabled = c.RepoIntel.Serena.Enabled
	o.Frontier.Enabled = c.Frontier.Enabled
	if err := decodeStrict(b, &o); err != nil {
		return c, fmt.Errorf("parse %s (a workspace may override budgets, escalation, repointel.cross_service, repointel.serena.enabled and frontier.enabled): %w", path, err)
	}
	out := c
	out.Budgets, out.Escalation = o.Budgets, o.Escalation
	out.RepoIntel.CrossService = o.RepoIntel.CrossService
	out.RepoIntel.Serena.Enabled = o.RepoIntel.Serena.Enabled
	out.Frontier.Enabled = o.Frontier.Enabled
	if err := out.Validate(); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

func decodeStrict(b []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// DecodeStrict decodes YAML rejecting unknown fields. Exported for other
// typed config files (verification.yaml, workspace overrides).
func DecodeStrict(b []byte, v any) error { return decodeStrict(b, v) }

// WriteFileAtomic writes data to a temp file in the same directory and
// renames it over path, so readers never observe a partial file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
