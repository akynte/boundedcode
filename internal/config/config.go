// Package config defines the typed, validated configuration. Configuration is
// layered: built-in defaults < user config (~/.config/boundedcode/config.yaml)
// < workspace overrides. Unknown keys are rejected so typos fail loudly.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
}

// RepoIntelConfig configures repository intelligence.
type RepoIntelConfig struct {
	Provider string `yaml:"provider"` // "codebase-memory-mcp"
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
		},
		Agent: AgentConfig{
			Runtime: "openhands", Image: "boundedcode-openhands:local",
			MaxIterations: 150, CondenserMaxEvents: 80,
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
	if c.Agent.Runtime != "openhands" && c.Agent.Runtime != "scripted" {
		errs = append(errs, fmt.Errorf("agent.runtime: unknown %q", c.Agent.Runtime))
	}
	switch c.Sandbox.Kind {
	case "docker", "none":
	default:
		errs = append(errs, fmt.Errorf("sandbox.kind: %q is not docker|none", c.Sandbox.Kind))
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
	}
	if c.Budgets.MaxAttempts < 1 {
		errs = append(errs, errors.New("budgets.max_attempts: must be >= 1"))
	}
	if c.Budgets.ContextPackTokens < 2000 {
		errs = append(errs, errors.New("budgets.context_pack_tokens: must be >= 2000"))
	}
	if c.Escalation.FailedAttempts < 1 || c.Escalation.RejectedStrategies < 1 {
		errs = append(errs, errors.New("escalation thresholds must be >= 1"))
	}
	return errors.Join(errs...)
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
