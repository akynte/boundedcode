// Package tui is the interactive terminal interface. It is a client of the
// same operations as the command-line interface: structured views read the
// ledger, audit log and status through a Backend, and actions run the real
// CLI commands in-process (Backend.Exec), so behaviour, policy and audit
// records are identical whichever interface started them.
package tui

import (
	"context"
	"io"
	"time"

	"github.com/akynte/boundedcode/internal/compat"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/stats"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Backend is what the interface needs from the control plane. Methods are
// called from background goroutines and must be safe for concurrent use.
type Backend interface {
	Info(ctx context.Context) Info
	Workspaces(ctx context.Context) ([]Workspace, error)
	Tasks(ctx context.Context, limit int) ([]*task.Task, error)
	Task(ctx context.Context, id string) (TaskDetail, error)
	Events(ctx context.Context, taskID string, after int64, limit int) ([]telemetry.Event, error)
	Verifications(ctx context.Context, taskID string, limit int) ([]verify.Result, error)
	// Compat is the task's latest cross-repository compatibility report
	// (ok false: the gate never ran for it); stale results are marked.
	Compat(ctx context.Context, taskID string) (compat.Report, bool, error)
	Diffs(ctx context.Context, taskID string) ([]RepoDiff, error)
	Escalations(ctx context.Context, taskID string) ([]Escalation, error)
	EscalationSummary(ctx context.Context) ([]EscalationGroup, error)
	FrontierLogin(ctx context.Context) (string, error)
	Stats(ctx context.Context, since time.Duration) (stats.Summary, error)
	Doctor(ctx context.Context) []Check
	SerenaChecks(ctx context.Context) []Check
	Runtime(ctx context.Context) (inference.Status, error)
	RuntimeLog(ctx context.Context, lines int) (path string, tail []string, err error)
	Models(ctx context.Context) ([]ModelRow, error)
	// CreateTask creates a task without running it and returns its id.
	CreateTask(ctx context.Context, req CreateTaskRequest) (string, error)
	// Exec runs a CLI command line (without the program name) in-process,
	// writing its output to out. Interactive questions the command would ask
	// on a terminal are routed to the Prompter.
	Exec(ctx context.Context, args []string, out io.Writer) error
	// Commands lists the CLI command paths, for completion.
	Commands() []CommandInfo
	// Project finds (registering when needed) the workspace for the git
	// repository containing dir.
	Project(ctx context.Context, dir string) (Project, error)
	// GitInit makes dir a git repository with an initial commit.
	GitInit(ctx context.Context, dir string) error
	// Setup reports the prerequisites (see the `setup` command).
	Setup(ctx context.Context) []SetupStep
	// SetPrompter installs the function that asks the user a yes/no
	// question; it blocks until answered or ctx ends.
	SetPrompter(p Prompter)
	// Hardware describes this machine and the local model it suits.
	Hardware(ctx context.Context) HardwareInfo
	// Providers lists the model providers, the selected one, and which
	// have a stored API key.
	Providers(ctx context.Context) []ProviderRow
	// ProviderModels lists a cloud provider's models with its stored key
	// (baseURL overrides the configured endpoint; "" keeps it).
	ProviderModels(ctx context.Context, provider, baseURL string) ([]ProviderModel, error)
	// SetProviderKey stores an API key in the credential store and returns
	// where it went. Keys go through this call, never through Exec, so they
	// are not echoed in job output or written to the interface log.
	SetProviderKey(ctx context.Context, provider, key string) (string, error)
	// DeleteProviderKey removes a stored API key.
	DeleteProviderKey(ctx context.Context, provider string) error
	// TestProvider sends one short request through the selected provider and
	// returns a one-line result.
	TestProvider(ctx context.Context) (string, error)
}

// HardwareInfo is this machine's hardware and the local model it suits.
type HardwareInfo struct {
	Summary     string // one line: OS, CPU, RAM, GPU
	Recommended string // model profile; "" when none fits
	Reason      string
}

// ProviderRow is one model provider.
type ProviderRow struct {
	Name, Model, BaseURL string
	Selected             bool
	// KeySource is where the API key is stored ("" = none; always "" for
	// local).
	KeySource string
}

// ProviderModel is one model a cloud provider offers.
type ProviderModel struct {
	ID, Display   string
	ContextWindow int // 0 = the provider does not report it
}

// Prompter asks the user to confirm. It is called from background goroutines.
type Prompter func(ctx context.Context, title, body string) bool

// Info is the static and slowly changing environment shown in the header.
type Info struct {
	Name, Version, Commit string
	ConfigFile            string
	ConfigExists          bool
	StateDB               string
	LogFile               string
	DefaultModel          string
	Provider              string // local | openai | anthropic | gemini | openai-compatible
	ProviderModel         string // the cloud provider's model ("" for local)
	InferenceMode         string // managed | external
	ExternalURL           string
	SandboxKind           string
	AgentImage            string
	AdapterDir            string
	FrontierEnabled       bool
	FrontierProvider      string
	FrontierApproval      bool
	FrontierMaxPacket     int
	SerenaEnabled         bool
	CrossService          bool
	CurrentWorkspace      string
	Err                   string // configuration could not be loaded
}

// Workspace is a workspace with all its repositories.
type Workspace struct {
	workspace.Workspace
	Current bool
	Repos   []workspace.Repository
}

// TaskDetail is a task with its attempts, worktrees and run lease.
type TaskDetail struct {
	Task       *task.Task
	Strategies []task.Strategy
	Worktrees  []task.Worktree
	LeaseOwner string
}

// RepoDiff is one repository's changes against the task's base commit.
type RepoDiff struct {
	Repo, Diff string
}

// Escalation is one frontier escalation.
type Escalation struct {
	ID                                              int64
	Task, Trigger, Provider, Model, Status, Outcome string
	TaskOutcome, Created, Reason                    string
	PacketTokens                                    int
	AdviceChangedCode                               *bool
}

// EscalationGroup counts escalations by trigger, status and outcome.
type EscalationGroup struct {
	Trigger, Status, Outcome string
	Count, PacketTokens      int
}

// Check is one environment check.
type Check struct {
	Name, Status, Detail, Hint string // Status: ok | warn | fail
}

// ModelRow is a model profile.
type ModelRow struct {
	Name, Display, File, License string
	Present, Default             bool
	Description, Status          string
	SizeBytes                    int64
	// Fit is how the model fits this machine (model.Fit levels) and why;
	// Recommended marks the model set-up proposes.
	Fit, FitDetail string
	Recommended    bool
}

// CreateTaskRequest are the inputs of `task create`.
type CreateTaskRequest struct {
	Workspace string
	Request   string
	Repos     []string
	Criteria  []string
	Model     string
	// FromTask makes a follow-up that starts from that task's branch.
	FromTask string
}

// CommandInfo is one runnable CLI command path.
type CommandInfo struct {
	Path  string // e.g. "task run"
	Use   string // e.g. "run TASK"
	Short string
}

// Project is the repository the interface was started in.
type Project struct {
	Dir       string // where the interface was started
	Root      string // repository root ("" when Dir is not in a git repository)
	Workspace string
	Repo      string
	Branch    string
	Dirty     bool
	Indexed   bool
	// Created is set when this call registered the workspace.
	Created bool
}

// SetupStep is one prerequisite.
type SetupStep struct {
	Name, Title, Detail string
	OK                  bool
}
