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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	Direct       DirectConfig     `yaml:"direct"`
	// Languages and BuildSystems add to the built-in ones (internal/lang,
	// internal/ecosystem). Only the user's configuration sets them: their
	// commands run after every edit.
	Languages    []LanguageConfig    `yaml:"languages,omitempty"`
	BuildSystems []BuildSystemConfig `yaml:"build_systems,omitempty"`
}

// LanguageConfig describes a language: how its files are recognised, its
// definitions and tests (regular expressions; a group named "name" is the
// symbol), the check run after each edit, and its tools.
type LanguageConfig struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Extensions  []string `yaml:"extensions,omitempty"`
	Filenames   []string `yaml:"filenames,omitempty"`
	Shebangs    []string `yaml:"shebangs,omitempty"`
	Definitions []string `yaml:"definitions,omitempty"`
	Tests       []string `yaml:"tests,omitempty"`
	Check       *struct {
		What  string   `yaml:"what"`
		Tools []string `yaml:"tools"`
		Args  []string `yaml:"args"`
		Root  []string `yaml:"root,omitempty"`
	} `yaml:"check,omitempty"`
	Format []string `yaml:"format,omitempty"` // executables
	Lint   []string `yaml:"lint,omitempty"`
	LSP    []string `yaml:"lsp,omitempty"`
}

// BuildSystemConfig describes a build system: projects are directories
// with one of Markers.
type BuildSystemConfig struct {
	ID        string   `yaml:"id"`
	Kind      string   `yaml:"kind"`
	Markers   []string `yaml:"markers"`
	Languages []string `yaml:"languages,omitempty"`
	Tool      string   `yaml:"tool"`
	Build     string   `yaml:"build,omitempty"`
	Test      string   `yaml:"test,omitempty"`
}

// DirectConfig configures direct mode: the native agent loop that works in
// the user's checkout for everyday development (ADR-0011). Bounded mode
// (tasks in a sandboxed worktree with verification) is configured by the
// other sections.
type DirectConfig struct {
	// Approval: "ask" asks before every file change and command; "edits"
	// (default) applies file changes inside the project and runs known
	// build, test and read-only commands without asking, and asks for
	// other commands; "auto" asks for nothing. The deterministic command
	// policy (no pushes, no infrastructure mutation, ...) applies in every
	// mode.
	Approval string `yaml:"approval"`
	// MaxTurns bounds the model calls one message may cause.
	MaxTurns int `yaml:"max_turns"`
	// CommandTimeout bounds one shell command.
	CommandTimeout Duration `yaml:"command_timeout"`
	// MaxTokens bounds processed tokens per session (0 = unlimited).
	MaxTokens int `yaml:"max_tokens"`
	// Instructions lists project instruction files, tried in order at the
	// project root; the first that exists is included in the system prompt.
	Instructions []string `yaml:"instructions,omitempty"`
	// Mode: "auto" (default) routes each message to chat, code or verify
	// from its intent, its risk and what the environment can run; "chat",
	// "code" or "verify" forces one.
	Mode string `yaml:"mode"`
	// MaxFiles: a change that names more files than this is routed to
	// verification.
	MaxFiles int `yaml:"max_files"`
	// Web configures web_fetch and web_search.
	Web WebConfig `yaml:"web"`
	// Permissions are allow and deny rules, "tool(pattern)", applied before
	// approval: deny refuses, allow skips the prompt.
	Permissions PermissionRules `yaml:"permissions"`
	// Hooks run shell commands around tools and turns.
	Hooks []HookConfig `yaml:"hooks,omitempty"`
	// MCP servers (stdio) whose tools are offered to the model, by name;
	// .boundedcode/mcp.json in the project adds more ("mcpServers" form).
	MCP map[string]MCPServerConfig `yaml:"mcp,omitempty"`
	// Checkpoints keeps copies of files before direct mode changes them,
	// for /undo (default true).
	Checkpoints *bool `yaml:"checkpoints,omitempty"`
	// WorkingSetTokens is the history budget the model is asked to attend
	// to: older tool results are set aside (recallable) and the history is
	// summarised to stay within it. 0 = 32K tokens for a local model, 96K
	// for a cloud provider, never above three quarters of the context
	// window.
	WorkingSetTokens int `yaml:"working_set_tokens,omitempty"`
	// MaxToolOutput caps one tool result in bytes (0 = 16 KiB for a local
	// model, 48 KiB for a cloud provider).
	MaxToolOutput int `yaml:"max_tool_output,omitempty"`
	// Prune shortens command output that has errors, failing tests,
	// stack traces, progress, downloads or passing tests to sort, keeping
	// the failures, their locations and the outcome; the whole output stays
	// available to recall (default true).
	Prune *bool `yaml:"prune,omitempty"`
	// ObservationBytes is the size such output is pruned to (0 = 8 KiB
	// for a local model, 24 KiB for a cloud provider).
	ObservationBytes int `yaml:"observation_bytes,omitempty"`
	// ParallelTools is how many calls of one model reply that change
	// nothing (reads, searches, fetches) may run at once (0 = 4; 1 runs
	// every call in turn).
	ParallelTools int `yaml:"parallel_tools,omitempty"`
	// MCPToolSearch defers MCP tool definitions behind mcp_search and
	// mcp_call: auto (default: when they would take more than a tenth of
	// the working set), on or off.
	MCPToolSearch string `yaml:"mcp_tool_search,omitempty"`
	// Routing chooses the local model's thinking per model call from the
	// call's role: "adaptive", or "off" (the default: every call as the
	// model's default). Experimental. RoutingThinking overrides the
	// adaptive choice per role (classify, plan, explore, edit, debug,
	// answer: on or off).
	Routing         string            `yaml:"routing,omitempty"`
	RoutingThinking map[string]string `yaml:"routing_thinking,omitempty"`
	// Compaction is how the history is summarised when setting results
	// aside is not enough: "structured" (the default: the session state,
	// the session's record plus the model's notes) or "legacy" (a free-text
	// summary of a transcript).
	Compaction string `yaml:"compaction,omitempty"`
	// Checks runs a syntax check (and for Go a build of the package) after
	// every file change and tells the model the result (default true).
	Checks *bool `yaml:"checks,omitempty"`
	// SeedPack looks up the names a message mentions (identifiers, paths,
	// quoted errors) in the project before the first model call and sends
	// the places found with the message (default true).
	SeedPack *bool `yaml:"seed_pack,omitempty"`
	// Retrieval chooses how that lookup works: "adaptive" (the default:
	// strategies selected from the request and the evidence collected so
	// far, internal/retrieve) or "basic" (the request's names by grep).
	Retrieval string `yaml:"retrieval,omitempty"`
	// HistoryNotes names, after a file change in a git repository, the
	// files its history usually changes together with that file
	// (default true).
	HistoryNotes *bool `yaml:"history_notes,omitempty"`
	// Stream shows the answer as it is generated when the provider can
	// stream (default true).
	Stream *bool `yaml:"stream,omitempty"`
	// VerifyFallback is what happens to a change that deserves a verified
	// task when bounded mode cannot run here: "ask" (default: the user
	// decides, and without someone to ask it is refused), "refuse", or
	// "allow" (make it directly; audited).
	VerifyFallback string `yaml:"verify_fallback,omitempty"`
	// Sandbox contains the commands direct mode runs (internal/execbox).
	Sandbox DirectSandboxConfig `yaml:"sandbox,omitempty"`
	// Workloads set tunables for projects in particular languages (the
	// first match applies; none by default). Only tunables can be set:
	// see Tunables.
	Workloads []WorkloadOverride `yaml:"workloads,omitempty"`
}

// WebConfig configures the web tools.
type WebConfig struct {
	// AllowHosts need no approval for web_fetch (host, or ".suffix").
	AllowHosts []string `yaml:"allow_hosts,omitempty"`
	// SearchURL is a SearXNG-style JSON search endpoint with {query};
	// empty uses DuckDuckGo's HTML endpoint.
	SearchURL string `yaml:"search_url,omitempty"`
}

// PermissionRules list allow and deny rules.
type PermissionRules struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// HookConfig is one hook: event pre_tool|post_tool|user_prompt|stop, an
// optional tool-name regular expression, and the command.
type HookConfig struct {
	Event   string   `yaml:"event"`
	Matcher string   `yaml:"matcher,omitempty"`
	Command string   `yaml:"command"`
	Timeout Duration `yaml:"timeout,omitempty"`
}

// MCPServerConfig starts an MCP server over stdio.
type MCPServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Cwd     string            `yaml:"cwd,omitempty"`
	Timeout Duration          `yaml:"timeout,omitempty"`
}

// CheckpointsOn reports whether checkpoints are enabled (default true).
func (d DirectConfig) CheckpointsOn() bool { return d.Checkpoints == nil || *d.Checkpoints }

// ChecksOn reports whether post-change checks are enabled (default true).
func (d DirectConfig) ChecksOn() bool { return d.Checks == nil || *d.Checks }

// SeedPackOn reports whether the request lookup is enabled (default true).
func (d DirectConfig) SeedPackOn() bool { return d.SeedPack == nil || *d.SeedPack }

// HistoryNotesOn reports whether history notes are enabled (default true).
func (d DirectConfig) HistoryNotesOn() bool { return d.HistoryNotes == nil || *d.HistoryNotes }

// PruneOn reports whether command output is pruned (default true).
func (d DirectConfig) PruneOn() bool { return d.Prune == nil || *d.Prune }

// StreamOn reports whether streaming is enabled (default true).
func (d DirectConfig) StreamOn() bool { return d.Stream == nil || *d.Stream }

// DirectSandboxConfig configures operating-system containment of the commands
// direct mode runs. Only the user's configuration sets it: a workspace or
// repository cannot weaken it.
type DirectSandboxConfig struct {
	// Mode is "auto" (default: the strongest available backend) or "off".
	Mode string `yaml:"mode,omitempty"`
	// Backend forces "bwrap", "landlock" or "none" ("" picks).
	Backend string `yaml:"backend,omitempty"`
	// Fallback is what happens when containment is weaker than strong:
	// "ask" (default; refused when no one can be asked), "refuse" or
	// "allow".
	Fallback string `yaml:"fallback,omitempty"`
	// Network is "proxy" (default: no network except allowed hosts through
	// an allowlisting proxy), "none" or "host" (not confined).
	Network string `yaml:"network,omitempty"`
	// AllowHosts may always be reached ("docs.example.com", ".example.com").
	AllowHosts []string `yaml:"allow_hosts,omitempty"`
	// Registries lets commands reach the package registries of the
	// project's ecosystems (default true).
	Registries *bool `yaml:"registries,omitempty"`
	// ReadPaths and WritePaths expose further host paths.
	ReadPaths  []string `yaml:"read_paths,omitempty"`
	WritePaths []string `yaml:"write_paths,omitempty"`
	// Env names environment variables passed through although they look
	// secret (GH_TOKEN for gh, for example).
	Env []string `yaml:"env,omitempty"`
}

// RegistriesOn reports whether package registries are reachable (default
// true).
func (s DirectSandboxConfig) RegistriesOn() bool { return s.Registries == nil || *s.Registries }

// TaskConfig configures how requests are read before implementation.
type TaskConfig struct {
	// Contract derives a compact task contract (required behaviour, allowed
	// alternatives, material ambiguities) before the first attempt.
	Contract bool `yaml:"contract"`
	// Ambiguity: "ask" blocks a materially ambiguous task until the user
	// clarifies it (task run --clarify); "proceed" records SPEC_AMBIGUOUS and
	// has the agent state and demonstrate its reading (benchmarks).
	Ambiguity string `yaml:"ambiguity"`
	// Verification configures the optional verification layers.
	Verification VerificationLayers `yaml:"verification,omitempty"`
	// StructuredOutput constrains the replies of the calls that read the
	// request and propose counterexamples to their JSON schema where the
	// provider supports it (unset: on). Off, the prompt alone asks for the
	// JSON.
	StructuredOutput *bool `yaml:"structured_output,omitempty"`
}

// StructuredOutputOn reports whether structured output is on (the default).
func (t TaskConfig) StructuredOutputOn() bool {
	return t.StructuredOutput == nil || *t.StructuredOutput
}

// VerificationLayers configures mutation assessment (Layer 6) and
// counterexample checks (Layer 7) of a bounded task's change: "off", "on"
// (run, and report; the label is not lowered) or "required" (the label is
// task_verified only when the layer holds).
type VerificationLayers struct {
	// Mutation: default "on"; "strict" is "required" with every boundary
	// mutant killed as well.
	Mutation string `yaml:"mutation,omitempty"`
	// MutationMutants bounds the mutants per repository (default 8);
	// MutationBudget the time per repository (default 5m).
	MutationMutants int      `yaml:"mutation_mutants,omitempty"`
	MutationBudget  Duration `yaml:"mutation_budget,omitempty"`
	// Counterexamples: default "off". When on, the task's model, in a
	// context of its own (the request and the diff, not the author's
	// conversation), proposes tests; they run on the change and the base.
	Counterexamples string `yaml:"counterexamples,omitempty"`
}

// MutationMode is the mutation setting with its default.
func (v VerificationLayers) MutationMode() string {
	if v.Mutation == "" {
		return "on"
	}
	return v.Mutation
}

// CounterexampleMode is the counterexample setting with its default.
func (v VerificationLayers) CounterexampleMode() string {
	if v.Counterexamples == "" {
		return "off"
	}
	return v.Counterexamples
}

// InferenceConfig selects the model provider and configures the local
// inference runtime.
type InferenceConfig struct {
	// Provider is "local" (llama.cpp, the default) or a cloud API:
	// "openai", "anthropic", "gemini" or "openai-compatible". A cloud
	// provider's API key is not stored here (see internal/secrets).
	Provider string `yaml:"provider"`
	// Providers holds each cloud provider's settings, so switching between
	// providers keeps them.
	Providers map[string]ProviderConfig `yaml:"providers,omitempty"`
	// Mode is "managed" (we start llama-server) or "external" (user runs it).
	// It applies to the local provider.
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
	// ReasoningHistory is how the model's reasoning travels with the
	// conversation: "auto" (the default) sends it back where the local
	// model's chat template renders it, so the model reads its earlier
	// turns as it wrote them and the server's prompt cache stays valid;
	// "keep" always sends it (an OpenAI-compatible service that asks for
	// reasoning_content in tool-call turns); "drop" never does. Anthropic
	// and Gemini replay their own thinking whatever this says.
	ReasoningHistory string `yaml:"reasoning_history,omitempty"`
}

// ProviderConfig configures one cloud provider.
type ProviderConfig struct {
	// Model is the provider's model id (e.g. "claude-opus-5-5").
	Model string `yaml:"model"`
	// BaseURL overrides the provider's endpoint; required for
	// openai-compatible (for example https://api.groq.com/openai/v1).
	BaseURL string `yaml:"base_url,omitempty"`
	// ContextWindow is the model's input limit in tokens. 0 = ask the
	// provider (Anthropic and Gemini report it); required for OpenAI and
	// openai-compatible models, whose APIs do not.
	ContextWindow int `yaml:"context_window,omitempty"`
	// ContextLimit caps how much of the window the agent fills before it
	// condenses its history: cloud tokens are billed, and a 1M-token window
	// filled on every turn is expensive. 0 = 200000.
	ContextLimit int `yaml:"context_limit,omitempty"`
	// Effort is the provider's reasoning-effort setting (Anthropic
	// output_config.effort, Gemini thinkingLevel, OpenAI reasoning_effort);
	// "" = the model's default.
	Effort string `yaml:"effort,omitempty"`
	// InputPrice and OutputPrice are USD per million tokens, for cost
	// estimates in stats (0 = not shown). CachedInputPrice applies to
	// prompt tokens read from the provider's cache.
	InputPrice       float64 `yaml:"input_price,omitempty"`
	CachedInputPrice float64 `yaml:"cached_input_price,omitempty"`
	OutputPrice      float64 `yaml:"output_price,omitempty"`
}

// DefaultContextLimit is ProviderConfig.ContextLimit's default.
const DefaultContextLimit = 200000

// CloudProviders are the provider names other than "local".
var CloudProviders = []string{"openai", "anthropic", "gemini", "openai-compatible"}

// IsCloud reports whether the selected provider is a cloud API.
func (c InferenceConfig) IsCloud() bool { return c.Provider != "" && c.Provider != "local" }

// Cloud returns the selected cloud provider's settings.
func (c InferenceConfig) Cloud() ProviderConfig { return c.Providers[c.Provider] }

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
// test file, or an agent-run test going from failing to passing. Calibrated
// by replaying these rules over the 2026-10 runs: attempts of tasks that
// passed went at most ~20K tokens without progress and ~30K in total;
// unproductive attempts 34-56K without progress and 57-107K in total, over
// 33-64 minutes.
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
	// CompatGate runs the cross-repository compatibility gate
	// (internal/compat) when verification passes: gRPC, protobuf and
	// OpenAPI links the change affects must be shown compatible by the
	// repositories' own checks before a task is TASK_VERIFIED. Needs
	// CrossService.
	CompatGate bool `yaml:"compat_gate"`
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
	MaxPacketTokens int `yaml:"max_packet_tokens"`
	// Data is what a packet may carry: "full" (the default: code, the
	// diff and contracts, sanitized and redacted) or "abstract" (the
	// problem without the source; see frontier.DataAbstract). A
	// repository's .boundedcode/data-policy.yaml can refuse external models
	// altogether.
	Data    string   `yaml:"data,omitempty"`
	Timeout Duration `yaml:"timeout"`
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
	// Policy is "rules" (the default) or "adaptive" (experimental: it
	// uses progress, ambiguity and the remaining budget; see
	// frontier.EvaluatePolicy).
	Policy string `yaml:"policy,omitempty"`
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
			Provider: "local",
			Mode:     "managed", ServerBinary: "llama-server", BenchBinary: "llama-bench",
			Host: "127.0.0.1", Port: 8765,
			StartupTimeout: Duration(5 * time.Minute), RequestTimeout: Duration(10 * time.Minute),
			IdleSleep: Duration(30 * time.Minute),
		},
		Task: TaskConfig{Contract: true, Ambiguity: "ask"},
		Direct: DirectConfig{Approval: "edits", MaxTurns: 80, CommandTimeout: Duration(3 * time.Minute), Mode: "auto", MaxFiles: 8,
			Instructions: []string{"AGENTS.md", "BOUNDEDCODE.md", "CLAUDE.md", ".boundedcode/instructions.md"}},
		Agent: AgentConfig{
			Runtime: "openhands", Image: "boundedcode-openhands:local",
			MaxIterations: 150, CondenserMaxEvents: 80, MaxOutputTokens: 8192,
			Strategy: StrategyBudget{NoProgressTokens: 40000, MaxTokens: 50000, MaxDuration: Duration(35 * time.Minute)},
		},
		RepoIntel: RepoIntelConfig{Provider: "codebase-memory-mcp", Binary: "codebase-memory-mcp", CrossService: true, CompatGate: true,
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
	errs = append(errs, c.Inference.validateProvider()...)
	if c.Inference.IdleSleep < 0 {
		errs = append(errs, errors.New("inference.idle_sleep: must be >= 0 (0 disables)"))
	}
	switch c.Inference.ReasoningHistory {
	case "", "auto", "keep", "drop":
	default:
		errs = append(errs, fmt.Errorf("inference.reasoning_history: %q is not auto|keep|drop", c.Inference.ReasoningHistory))
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
	switch c.Direct.Approval {
	case "ask", "edits", "auto":
	default:
		errs = append(errs, fmt.Errorf("direct.approval: %q is not ask|edits|auto", c.Direct.Approval))
	}
	if c.Direct.MaxTurns <= 0 {
		errs = append(errs, errors.New("direct.max_turns: must be positive"))
	}
	if c.Direct.WorkingSetTokens < 0 {
		errs = append(errs, errors.New("direct.working_set_tokens: must not be negative"))
	}
	if c.Direct.MaxToolOutput < 0 {
		errs = append(errs, errors.New("direct.max_tool_output: must not be negative"))
	}
	for i, l := range c.Languages {
		if l.ID == "" || len(l.Extensions)+len(l.Filenames)+len(l.Shebangs) == 0 {
			errs = append(errs, fmt.Errorf("languages[%d]: id and at least one of extensions, filenames, shebangs are required", i))
		}
		for _, p := range append(append([]string{}, l.Definitions...), l.Tests...) {
			if _, err := regexp.Compile(p); err != nil {
				errs = append(errs, fmt.Errorf("languages[%d] (%s): %w", i, l.ID, err))
			}
		}
		if l.Check != nil && (len(l.Check.Tools) == 0 || len(l.Check.Args) == 0) {
			errs = append(errs, fmt.Errorf("languages[%d] (%s): check needs tools and args", i, l.ID))
		}
	}
	for i, b := range c.BuildSystems {
		if b.ID == "" || len(b.Markers) == 0 || b.Tool == "" {
			errs = append(errs, fmt.Errorf("build_systems[%d]: id, markers and tool are required", i))
		}
	}
	for _, v := range []struct{ key, val string }{{"task.verification.mutation", c.Task.Verification.Mutation},
		{"task.verification.counterexamples", c.Task.Verification.Counterexamples}} {
		switch v.val {
		case "", "off", "on", "required":
		case "strict":
			if v.key != "task.verification.mutation" {
				errs = append(errs, fmt.Errorf("%s: %q is not off|on|required", v.key, v.val))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: %q is not off|on|required", v.key, v.val))
		}
	}
	sb := c.Direct.Sandbox
	for _, v := range []struct{ key, val, ok string }{{"mode", sb.Mode, "|auto|off|"}, {"backend", sb.Backend, "|bwrap|landlock|none|"},
		{"fallback", sb.Fallback, "|ask|refuse|allow|"}, {"network", sb.Network, "|proxy|none|host|"}} {
		if v.val != "" && !strings.Contains(v.ok, "|"+v.val+"|") {
			errs = append(errs, fmt.Errorf("direct.sandbox.%s: %q is not %s", v.key, v.val, strings.Trim(v.ok, "|")))
		}
	}
	switch c.Direct.Retrieval {
	case "", "adaptive", "basic":
	default:
		errs = append(errs, fmt.Errorf("direct.retrieval: %q is not adaptive|basic", c.Direct.Retrieval))
	}
	switch c.Direct.Compaction {
	case "", "structured", "legacy":
	default:
		errs = append(errs, fmt.Errorf("direct.compaction: %q is not structured|legacy", c.Direct.Compaction))
	}
	switch c.Direct.MCPToolSearch {
	case "", "auto", "on", "off":
	default:
		errs = append(errs, fmt.Errorf("direct.mcp_tool_search: %q is not auto, on or off", c.Direct.MCPToolSearch))
	}
	switch c.Frontier.Data {
	case "", "full", "abstract":
	default:
		errs = append(errs, fmt.Errorf("frontier.data: %q is not full or abstract", c.Frontier.Data))
	}
	switch c.Escalation.Policy {
	case "", "rules", "adaptive":
	default:
		errs = append(errs, fmt.Errorf("escalation.policy: %q is not rules or adaptive", c.Escalation.Policy))
	}
	switch c.Direct.Routing {
	case "", "off", "adaptive":
	default:
		errs = append(errs, fmt.Errorf("direct.routing: %q is not off or adaptive", c.Direct.Routing))
	}
	for role, v := range c.Direct.RoutingThinking {
		if !slices.Contains([]string{"classify", "plan", "explore", "edit", "debug", "answer"}, role) || (v != "on" && v != "off" && (v != "skip" || role != "classify")) {
			errs = append(errs, fmt.Errorf("direct.routing_thinking: %s: %s (roles: classify, plan, explore, edit, debug, answer; values: on, off; classify also skip)", role, v))
		}
	}
	if c.Direct.ParallelTools < 0 || c.Direct.ParallelTools > 16 {
		errs = append(errs, fmt.Errorf("direct.parallel_tools: %d is not between 0 and 16", c.Direct.ParallelTools))
	}
	errs = append(errs, c.Direct.validateWorkloads()...)
	if c.Direct.ObservationBytes < 0 {
		errs = append(errs, fmt.Errorf("direct.observation_bytes: %d is negative", c.Direct.ObservationBytes))
	}
	switch c.Direct.VerifyFallback {
	case "", "ask", "refuse", "allow":
	default:
		errs = append(errs, fmt.Errorf("direct.verify_fallback: %q is not ask|refuse|allow", c.Direct.VerifyFallback))
	}
	switch c.Direct.Mode {
	case "auto", "chat", "code", "verify":
	default:
		errs = append(errs, fmt.Errorf("direct.mode: %q is not auto|chat|code|verify", c.Direct.Mode))
	}
	for i, h := range c.Direct.Hooks {
		switch h.Event {
		case "pre_tool", "post_tool", "user_prompt", "stop":
		default:
			errs = append(errs, fmt.Errorf("direct.hooks[%d].event: %q is not pre_tool|post_tool|user_prompt|stop", i, h.Event))
		}
		if strings.TrimSpace(h.Command) == "" {
			errs = append(errs, fmt.Errorf("direct.hooks[%d].command: required", i))
		}
	}
	for name, m := range c.Direct.MCP {
		if strings.TrimSpace(m.Command) == "" {
			errs = append(errs, fmt.Errorf("direct.mcp.%s.command: required", name))
		}
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
	if perm&0o077 == 0 {
		if err := restrictToOwner(tmp, false); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}

func (c InferenceConfig) validateProvider() []error {
	var errs []error
	switch c.Provider {
	case "local", "":
	case "openai", "anthropic", "gemini", "openai-compatible":
		p := c.Providers[c.Provider]
		if p.Model == "" {
			errs = append(errs, fmt.Errorf("inference.providers.%s.model: required when %s is the provider", c.Provider, c.Provider))
		}
		if c.Provider == "openai-compatible" && p.BaseURL == "" {
			errs = append(errs, errors.New("inference.providers.openai-compatible.base_url: required"))
		}
	default:
		errs = append(errs, fmt.Errorf("inference.provider: %q is not local|%s", c.Provider, strings.Join(CloudProviders, "|")))
	}
	for name, p := range c.Providers {
		if !slices.Contains(CloudProviders, name) {
			errs = append(errs, fmt.Errorf("inference.providers: unknown provider %q", name))
			continue
		}
		if p.BaseURL != "" {
			if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				errs = append(errs, fmt.Errorf("inference.providers.%s.base_url: %q is not an http(s) URL", name, p.BaseURL))
			}
		}
		if p.ContextWindow < 0 || p.ContextLimit < 0 {
			errs = append(errs, fmt.Errorf("inference.providers.%s: context_window and context_limit must be >= 0", name))
		}
		if p.InputPrice < 0 || p.OutputPrice < 0 || p.CachedInputPrice < 0 {
			errs = append(errs, fmt.Errorf("inference.providers.%s: prices must be >= 0", name))
		}
		switch p.Effort {
		case "", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			errs = append(errs, fmt.Errorf("inference.providers.%s.effort: %q is not minimal|low|medium|high|xhigh|max", name, p.Effort))
		}
	}
	return errs
}
