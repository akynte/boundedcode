// Package direct is BoundedCode's native agent loop for everyday
// development: it works directly in the user's checkout, with file, search
// and shell tools, through the same metered model gateway as the sandboxed
// task runner (ADR-0011).
//
// Direct mode is the fast path. It has no worktree, no container and no
// verification gate; the deterministic command policy and the secret-path
// rules still apply, and every model call is metered and audited. When a
// change deserves isolation and test-backed evidence, the conversation is
// handed to bounded mode (a task), which is the product's other half.
package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/inference"
)

// Approval modes (config direct.approval).
const (
	ApprovalAsk   = "ask"
	ApprovalEdits = "edits"
	ApprovalAuto  = "auto"
)

// Message is one OpenAI chat-completions message. Tool calls and results
// are kept in that shape so any Upstream can replay them.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	// Reasoning is the model's reasoning text (llama.cpp reasoning_content),
	// kept for display and not sent back.
	Reasoning string `json:"reasoning,omitempty"`
}

// ToolCall is an OpenAI-shaped function call.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// EventKind classifies Events.
type EventKind string

// Event kinds.
const (
	EventText      EventKind = "text"       // assistant text
	EventReasoning EventKind = "reasoning"  // model reasoning (when the provider returns it)
	EventToolStart EventKind = "tool_start" // a tool is about to run (after approval)
	EventToolEnd   EventKind = "tool_end"   // a tool finished
	EventDenied    EventKind = "denied"     // a tool call was refused (policy or user)
	EventCompact   EventKind = "compact"    // the history was summarised
	EventNote      EventKind = "note"       // informational
	EventDone      EventKind = "done"       // the turn ended
	EventMode      EventKind = "mode"       // the mode changed during the turn (Text: new mode, Summary: why)
)

// Event is what a Sink receives while a message is processed.
type Event struct {
	Kind EventKind `json:"kind"`
	Text string    `json:"text,omitempty"`
	// Tool events.
	Tool    string         `json:"tool,omitempty"`
	CallID  string         `json:"call_id,omitempty"`
	Summary string         `json:"summary,omitempty"` // one line: what the call does
	Args    map[string]any `json:"args,omitempty"`
	Output  string         `json:"output,omitempty"` // tool result (may be long)
	Err     string         `json:"err,omitempty"`
	Elapsed time.Duration  `json:"elapsed,omitempty"`
	// Done event.
	Usage *Usage `json:"usage,omitempty"`
	// Changed lists the files whose git status changed during the turn
	// (nil outside a git repository), so a claimed change that never
	// happened is visible.
	Changed []string `json:"changed,omitempty"`
}

// Usage is the token accounting of a turn or a session.
type Usage struct {
	Calls     int `json:"calls"`
	Processed int `json:"processed"` // uncached prompt + completion tokens
	Generated int `json:"generated"`
	Cached    int `json:"cached"`
}

// Sink receives Events; it is called from the loop's goroutine.
type Sink func(Event)

// Approver decides whether a tool call may run. It is asked only when the
// approval mode requires it. Returning false denies the call; the model is
// told and continues.
type Approver func(ctx context.Context, req ApprovalRequest) bool

// ApprovalRequest describes a tool call awaiting consent.
type ApprovalRequest struct {
	Tool    string
	Summary string // one line
	Detail  string // what will happen (the command, the diff, ...)
}

// Options configure a Runner.
type Options struct {
	// Root is the project directory (the git top level when there is one).
	// Tools are confined to it.
	Root string
	// Dir is the working directory: where commands run and what relative
	// paths are resolved against. Defaults to Root. It is inside Root.
	Dir string
	// Approval is one of the Approval* modes.
	Approval string
	// MaxTurns bounds model calls per message (0 = 80).
	MaxTurns int
	// CommandTimeout bounds one shell command (0 = 3 minutes).
	CommandTimeout time.Duration
	// ContextTokens is the model's working context; the history is
	// summarised when it nears this size (0 = 32768).
	ContextTokens int
	// MaxOutputTokens caps one completion (0 = provider default).
	MaxOutputTokens int
	// Instructions lists project instruction files tried at Root, in order.
	Instructions []string
	// UserInstructions is an optional path to the user's own instruction
	// file (outside the project).
	UserInstructions string
	// Env describes the environment for the system prompt (OS, shell, git
	// branch...). Filled by Runner when empty.
	Env map[string]string
	// Now returns the current time (tests).
	Now func() time.Time
	// Mode is how this message is handled (ModeChat, ModeCode; "" = code).
	// In ModeChat the first file change switches the turn to ModeCode. A
	// change to a risky path (RiskyPath) ends the turn with ErrHandoff so
	// the request can run as a bounded task, unless AllowRisky is set.
	Mode       string
	AllowRisky bool
}

// Runner processes messages of one session.
type Runner struct {
	Gateway  *inference.Gateway
	Opts     Options
	Approve  Approver
	Sink     Sink
	Session  *Session
	tools    []Tool
	toolsErr error
	handoff  *Handoff
	mu       sync.Mutex
	// extraSystem is appended to the system prompt (tests, hand-offs).
	ExtraSystem string
}

// ErrInterrupted is returned when ctx ended while a message was processed;
// the work so far is kept in the session.
var ErrInterrupted = errors.New("interrupted")

// Handoff is returned by Send when the turn should continue as a bounded
// task: the agent was about to change a risky path. No file was changed by
// the refused call; earlier changes of the same turn are listed.
type Handoff struct {
	Reason  string
	Path    string
	Changed []string
}

func (h *Handoff) Error() string { return "hand-off to verification: " + h.Reason }

func (r *Runner) emit(e Event) {
	if r.Sink != nil {
		r.Sink(e)
	}
}

func (r *Runner) maxTurns() int {
	if r.Opts.MaxTurns > 0 {
		return r.Opts.MaxTurns
	}
	return 80
}

func (r *Runner) contextTokens() int {
	if r.Opts.ContextTokens > 0 {
		return r.Opts.ContextTokens
	}
	return 32768
}

// Send processes one user message: it runs the model until it answers
// without tool calls, running tools in between, and returns the final
// assistant text. Every message is appended to the Session as it happens,
// so an interrupted turn is kept.
func (r *Runner) Send(ctx context.Context, text string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Session == nil {
		r.Session = &Session{}
	}
	if r.Session.Title == "" {
		r.Session.Title = title(text)
	}
	if err := r.Session.Append(Message{Role: "user", Content: text}); err != nil {
		return "", err
	}
	tools := r.toolList()
	final := ""
	start := Usage{}
	start.Processed, start.Generated, start.Cached = r.Gateway.Stats()
	before := r.gitState()
	defer func() {
		u := r.usageSince(start)
		r.emit(Event{Kind: EventDone, Usage: &u, Changed: changedSince(before, r.gitState())})
	}()
	nudged := false
	lastFail, failures := "", 0
	for turn := 0; turn < r.maxTurns(); turn++ {
		if err := ctx.Err(); err != nil {
			return final, ErrInterrupted
		}
		if err := r.compactIfNeeded(ctx); err != nil {
			return final, err
		}
		msg, err := r.complete(ctx, tools)
		if err != nil {
			if ctx.Err() != nil {
				return final, ErrInterrupted
			}
			return final, err
		}
		if msg.Reasoning != "" {
			r.emit(Event{Kind: EventReasoning, Text: msg.Reasoning})
		}
		if msg.Content != "" {
			r.emit(Event{Kind: EventText, Text: msg.Content})
			final = msg.Content
		}
		if err := r.Session.Append(msg); err != nil {
			return final, err
		}
		if len(msg.ToolCalls) == 0 {
			// Smaller models sometimes narrate what they are about to do
			// instead of doing it. One nudge per message.
			if !nudged && looksLikeIntent(msg.Content) {
				nudged = true
				r.emit(Event{Kind: EventNote, Text: "The model described a plan without acting; asking it to continue."})
				if err := r.Session.Append(Message{Role: "user", Content: "Continue: use the tools now instead of describing what you will do."}); err != nil {
					return final, err
				}
				continue
			}
			return final, nil
		}
		for _, call := range msg.ToolCalls {
			result := r.runTool(ctx, call)
			if r.handoff != nil {
				h := r.handoff
				r.handoff = nil
				h.Changed = changedSince(before, r.gitState())
				_ = r.Session.Append(Message{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: result})
				return final, h
			}
			key := call.Function.Name + "\x00" + call.Function.Arguments
			if strings.HasPrefix(result, "error:") {
				if key == lastFail {
					failures++
				} else {
					lastFail, failures = key, 1
				}
				if failures >= 3 {
					result += fmt.Sprintf("\n\nThis exact call has now failed %d times with the same result. Do not repeat it: re-read the file, change the arguments, or explain the problem to the user.", failures)
				}
			}
			if err := r.Session.Append(Message{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: result}); err != nil {
				return final, err
			}
			if ctx.Err() != nil {
				return final, ErrInterrupted
			}
		}
	}
	r.emit(Event{Kind: EventNote, Text: fmt.Sprintf("Stopped after %d model calls for this message (direct.max_turns). Send a message to continue.", r.maxTurns())})
	return final, nil
}

func (r *Runner) usageSince(start Usage) Usage {
	p, g, c := r.Gateway.Stats()
	return Usage{Processed: p - start.Processed, Generated: g - start.Generated, Cached: c - start.Cached}
}

// Usage returns the session's token accounting from the gateway.
func (r *Runner) Usage() Usage {
	p, g, c := r.Gateway.Stats()
	return Usage{Processed: p, Generated: g, Cached: c}
}

// complete makes one model call with the current history.
func (r *Runner) complete(ctx context.Context, tools []Tool) (Message, error) {
	body := map[string]any{
		"messages": r.wireMessages(),
	}
	if len(tools) > 0 {
		body["tools"] = toolSchemas(tools)
		body["tool_choice"] = "auto"
	}
	if r.Opts.MaxOutputTokens > 0 {
		body["max_tokens"] = r.Opts.MaxOutputTokens
	}
	status, resp, err := r.Gateway.Forward(ctx, "/v1/chat/completions", body)
	if err != nil {
		if errors.Is(err, inference.ErrBudgetExhausted) {
			return Message{}, fmt.Errorf("the session's token budget is spent (direct.max_tokens); start a new session")
		}
		return Message{}, err
	}
	raw, _ := json.Marshal(resp)
	if status != http.StatusOK {
		msg := errMessage(raw)
		if status == http.StatusBadRequest && looksLikeContextOverflow(msg) {
			// Summarise and retry once.
			if cerr := r.compact(ctx); cerr == nil {
				body["messages"] = r.wireMessages()
				status, resp, err = r.Gateway.Forward(ctx, "/v1/chat/completions", body)
				if err != nil {
					return Message{}, err
				}
				raw, _ = json.Marshal(resp)
				if status == http.StatusOK {
					return parseCompletion(raw)
				}
				msg = errMessage(raw)
			}
		}
		return Message{}, fmt.Errorf("model request failed (http %d): %s", status, msg)
	}
	return parseCompletion(raw)
}

// wireMessages is the history as sent upstream: the system prompt first,
// without display-only fields.
func (r *Runner) wireMessages() []map[string]any {
	out := []map[string]any{{"role": "system", "content": r.systemPrompt()}}
	for _, m := range r.Session.Messages {
		w := map[string]any{"role": m.Role}
		switch m.Role {
		case "tool":
			w["tool_call_id"] = m.ToolCallID
			w["content"] = m.Content
			if m.Name != "" {
				w["name"] = m.Name
			}
		case "assistant":
			w["content"] = m.Content
			if len(m.ToolCalls) > 0 {
				w["tool_calls"] = m.ToolCalls
			}
		default:
			w["content"] = m.Content
		}
		out = append(out, w)
	}
	return out
}

func parseCompletion(raw []byte) (Message, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Role             string     `json:"role"`
				Content          any        `json:"content"`
				ReasoningContent string     `json:"reasoning_content"`
				Reasoning        string     `json:"reasoning"`
				ToolCalls        []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Message{}, fmt.Errorf("decode model response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return Message{}, errors.New("model response has no choices")
	}
	c := resp.Choices[0].Message
	m := Message{Role: "assistant", Content: contentText(c.Content), Reasoning: c.ReasoningContent}
	if m.Reasoning == "" {
		m.Reasoning = c.Reasoning
	}
	for i, tc := range c.ToolCalls {
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano()%1_000_000, i)
		}
		if tc.Type == "" {
			tc.Type = "function"
		}
		m.ToolCalls = append(m.ToolCalls, tc)
	}
	if resp.Choices[0].FinishReason == "length" && len(m.ToolCalls) == 0 {
		m.Content += "\n\n[output cut at the completion limit]"
	}
	return m, nil
}

// contentText flattens string or multipart content.
func contentText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	return ""
}

func errMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := string(raw)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func looksLikeContextOverflow(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "context") && (strings.Contains(m, "length") || strings.Contains(m, "window") || strings.Contains(m, "exceed") || strings.Contains(m, "too long") || strings.Contains(m, "maximum"))
}

// estimateTokens is the rough prompt size of the current history.
func (r *Runner) estimateTokens() int {
	n := contextplan.EstimateTokens(r.systemPrompt())
	for _, m := range r.Session.Messages {
		n += contextplan.EstimateTokens(m.Content) + 8
		for _, tc := range m.ToolCalls {
			n += contextplan.EstimateTokens(tc.Function.Arguments) + 12
		}
	}
	n += 1500 // tool schemas
	return n
}

func title(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if len(t) > 72 {
		t = t[:72] + "…"
	}
	return t
}

// looksLikeIntent reports a short answer whose last sentence announces an
// action ("Let me look at…", "I'll start by…") without performing one.
func looksLikeIntent(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || len(t) > 600 || strings.Contains(t, " if you ") || strings.Contains(t, "would you like") {
		return false
	}
	parts := sentenceEnd.Split(t, -1)
	last := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(parts[i]); p != "" {
			last = p
			break
		}
	}
	for _, lead := range []string{"first, ", "first ", "now, ", "now ", "next, ", "then ", "so "} {
		last = strings.TrimPrefix(last, lead)
	}
	for _, p := range []string{"let me ", "let's ", "i'll ", "i will ", "i need to ", "i'm going to ", "i am going to ", "i should ", "we need to "} {
		if strings.HasPrefix(last, p) {
			return true
		}
	}
	return false
}

var sentenceEnd = regexp.MustCompile(`[.!?]+\s+|\n+`)

// gitState is the working tree's status per path (nil outside git).
func (r *Runner) gitState() map[string]string {
	// Not gitOutput: a leading space in the first status line is significant.
	c := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	c.Dir = r.Opts.Root
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	raw, err := c.Output()
	if err != nil {
		return nil
	}
	out := strings.TrimRight(string(raw), "\n")
	if out == "" {
		if gitOutput(r.Opts.Root, "rev-parse", "--is-inside-work-tree") != "true" {
			return nil
		}
		return map[string]string{}
	}
	st := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 3 {
			st[strings.TrimSpace(l[3:])] = l[:2]
		}
	}
	return st
}

// changedSince lists paths whose status differs between two states, with
// their modification time as the tie-breaker for files that were already
// modified before the turn (a second edit keeps the same status).
func changedSince(before, after map[string]string) []string {
	if before == nil || after == nil {
		return nil
	}
	out := []string{}
	for p, st := range after {
		if before[p] != st {
			out = append(out, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Mode returns the current mode of the runner ("" counts as code).
func (r *Runner) Mode() string {
	if r.Opts.Mode == "" {
		return ModeCode
	}
	return r.Opts.Mode
}

// mutating is called before a file change (or a non-safe command). It
// switches chat to code, and hands a risky path off to verification.
func (r *Runner) mutating(tool, relRoot string) bool {
	if r.Opts.Mode == ModeChat {
		r.Opts.Mode = ModeCode
		r.emit(Event{Kind: EventMode, Text: ModeCode, Summary: "the answer needs a change: switching from chat to code"})
	}
	if relRoot != "" && !r.Opts.AllowRisky {
		if why, ok := RiskyPath(relRoot); ok {
			r.handoff = &Handoff{Reason: why, Path: relRoot}
			r.emit(Event{Kind: EventMode, Text: ModeVerify, Summary: "the change touches a " + why + ": handing off to verification"})
			return false
		}
	}
	return true
}
