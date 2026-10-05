// Package agent defines the boundary to agent runtimes (the LLM tool loop).
// The control plane owns task lifecycle; a runtime only executes turns.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/akynte/boundedcode/internal/inference"
)

// Runtime opens agent sessions. Implementations: openhands (adapter), and a
// scripted runtime for tests.
type Runtime interface {
	Name() string
	// Open starts a new session, or resumes SessionID when set.
	Open(ctx context.Context, req OpenRequest) (Session, error)
}

// OpenRequest configures a session. Paths are host paths; the runtime maps
// them into its sandbox.
type OpenRequest struct {
	TaskID         string
	SessionID      string   // empty: new session
	Workspace      string   // agent working root: one directory per repo worktree (host path)
	GitCommonDirs  []string // repository .git dirs backing the worktrees (mounted read-only)
	GitAdminDirs   []string // per-worktree admin dirs (<common>/worktrees/<name>, mounted read-write)
	PersistenceDir string   // runtime-owned conversation state (host path)
	Model          string
	MaxIterations  int
	// MaxInputTokens is the model context; the condenser keeps prompts below it.
	MaxInputTokens     int
	MaxOutputTokens    int
	CondenserMaxEvents int
	CondenserMaxTokens int
	// LLMTimeout bounds one model call inside the runtime; pass the gateway's
	// request timeout (inference.request_timeout). Zero keeps the runtime's
	// default.
	LLMTimeout time.Duration
	// Masks are workspace-relative paths hidden from the agent.
	Masks []string
	// DependencyMounts are installed dependencies (node_modules of the
	// repository's own checkout) mounted read-only into the worktrees, so
	// the agent can build and test offline. Host paths.
	DependencyMounts []DependencyMount
	// DependencyScratch are writable tool-cache directories inside them.
	DependencyScratch []string
	// Toolchain gives the agent the same offline build environment as
	// verification, so it can build and run the tests it is judged by.
	Toolchain Toolchain
	// OnEvent receives runtime events (may be nil). Called sequentially.
	OnEvent func(Event)
	// Gateway meters and forwards the session's model calls. The task runner
	// supplies it so token budgets are enforced on the same counter.
	Gateway *inference.Gateway
}

// Toolchain is the agent sandbox's build environment (host paths).
type Toolchain struct {
	// GoModCache is mounted read-only with GOPROXY=off, as for verification.
	GoModCache string
	// GoCache is the agent's own Go build cache (writable). It must not be
	// verification's: Go caches test results, and an agent-written cache
	// could otherwise turn a failing verification test into a cached "ok".
	GoCache string
}

// DependencyMount maps an installed-dependency directory read-only to Target.
type DependencyMount struct {
	Host   string
	Target string
}

// Session is an open agent conversation.
type Session interface {
	ID() string
	Resumed() bool
	// Send delivers a message and runs the agent until it stops. When ctx
	// ends first the runtime interrupts the agent (falling back to killing
	// it) and returns ctx's error; bound a turn with a ctx deadline.
	Send(ctx context.Context, message string) (Result, error)
	// Condense forces a context condensation.
	Condense(ctx context.Context) error
	Interrupt(ctx context.Context) error
	State(ctx context.Context) (State, error)
	// Close ends the session process; persisted state is kept.
	Close() error
}

// Result is the outcome of one Send.
type Result struct {
	Status       string `json:"status"` // finished | paused | stuck | error | idle | running
	EventsTotal  int    `json:"events_total"`
	EventsNew    int    `json:"events_new"`
	FinalMessage string `json:"final_message"`
	Stuck        bool   `json:"stuck"`
	Error        string `json:"error"`
	Usage        struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// State is a lightweight status snapshot.
type State struct {
	Status         string `json:"status"`
	EventCount     int    `json:"event_count"`
	ConversationID string `json:"conversation_id"`
}

// Event is a runtime-neutral event summary.
type Event struct {
	Kind    string          `json:"kind"`
	Tool    string          `json:"tool,omitempty"`
	Text    string          `json:"text,omitempty"`
	Error   string          `json:"error,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Raw     json.RawMessage `json:"-"`
}
