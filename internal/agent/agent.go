// Package agent defines the boundary to agent runtimes (the LLM tool loop).
// The control plane owns task lifecycle; a runtime only executes turns.
package agent

import (
	"context"
	"encoding/json"

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
	// Masks are workspace-relative paths hidden from the agent.
	Masks []string
	// OnEvent receives runtime events (may be nil). Called sequentially.
	OnEvent func(Event)
	// Gateway meters and forwards the session's model calls. The task runner
	// supplies it so token budgets are enforced on the same counter.
	Gateway *inference.Gateway
}

// Session is an open agent conversation.
type Session interface {
	ID() string
	Resumed() bool
	// Send delivers a message and runs the agent until it stops.
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
