package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/inference"
)

// keepTail is how many trailing messages survive a compaction verbatim
// (the current user message and recent exchange).
const keepTail = 6

// compactIfNeeded summarises the history when it is still well above the
// working set after the oldest tool results were set aside (a tenth of
// headroom: the estimate is rough, and a summary costs a model call and
// loses detail).
func (r *Runner) compactIfNeeded(ctx context.Context) error {
	if r.estimateTokens() <= r.workingSetTokens()*11/10 {
		return nil
	}
	return r.compact(ctx)
}

// compact replaces the older history with a summary, keeping the most
// recent messages: the session state (state.go) unless direct.compaction is
// "legacy".
func (r *Runner) compact(ctx context.Context) error {
	if r.Opts.Compaction == CompactionLegacy {
		return r.compactLegacy(ctx)
	}
	return r.compactStructured(ctx)
}

// Compaction strategies.
const (
	CompactionStructured = "structured"
	CompactionLegacy     = "legacy"
)

// cutPoint is where the kept tail starts: at a user message, so tool calls
// and results stay paired.
func cutPoint(msgs []Message) int {
	cut := len(msgs) - keepTail
	for cut > 0 && msgs[cut].Role != "user" {
		cut--
	}
	if cut == 0 {
		cut = len(msgs) - keepTail
		for cut < len(msgs) && msgs[cut].Role == "tool" {
			cut++
		}
	}
	return cut
}

// transcript is the messages as text for a summary request, cut to limit
// bytes in the middle.
func transcript(head []Message, limit int) string {
	var b strings.Builder
	for _, m := range head {
		switch m.Role {
		case "user":
			b.WriteString("USER: " + m.Content + "\n")
		case "assistant":
			if m.Content != "" {
				b.WriteString("ASSISTANT: " + m.Content + "\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("ASSISTANT CALLED " + tc.Function.Name + " " + oneLine(tc.Function.Arguments, 300) + "\n")
			}
		case "tool":
			b.WriteString("TOOL RESULT: " + oneLine(m.Content, 400) + "\n")
		}
	}
	text := b.String()
	if len(text) > limit {
		text = text[:limit/3] + "\n…\n" + text[len(text)-limit*2/3:]
	}
	return text
}

// compactStructured summarises into the session state: the record built
// from the session and the model's notes. The notes request continues the
// conversation exactly as the model last saw it, so the server's prompt
// cache serves all of it and only the request is new; when that does not
// fit, a shortened transcript is used, and when no notes can be had the
// record alone is the summary. An interrupted compaction changes nothing.
func (r *Runner) compactStructured(ctx context.Context) error {
	msgs := r.Session.Messages
	if len(msgs) <= keepTail {
		return nil
	}
	t0 := time.Now()
	cut := cutPoint(msgs)
	head, tail := msgs[:cut], msgs[cut:]
	all := append(append([]Message{}, r.Session.Archive...), head...)
	st := r.buildState(all, r.Session.Compactions+1)
	notes, source, err := r.sessionNotes(ctx, head)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		st.NotesError = err.Error()
	} else {
		st.Notes, st.NotesSource = notes, source
	}
	// A summary is derived from everything before it, tool output
	// included: it is data, never the user's words.
	summary := Message{Role: "user", Content: st.render(recallIndex(r.toolList(), all)), Provenance: untrusted(SourceSummary, "session state")}
	if err := r.Session.replaceWithState(st, summary, append([]Message(nil), tail...)); err != nil {
		return err
	}
	r.setAside = nil
	how := "with the model's notes"
	switch {
	case source == "transcript":
		how = "with the model's notes from a transcript"
	case st.Notes == nil:
		how = "without the model's notes (" + oneLine(st.NotesError, 120) + ")"
	}
	r.emit(Event{Kind: EventCompact, Text: fmt.Sprintf("Summarised %d earlier messages into the session state, %s.", len(head), how),
		Args: map[string]any{"ms": time.Since(t0).Milliseconds(), "notes": source, "summary_bytes": len(summary.Content)}})
	return nil
}

// sessionNotes asks the model for its notes on the session.
func (r *Runner) sessionNotes(ctx context.Context, head []Message) (*ModelNotes, string, error) {
	format := inference.JSONSchemaFormat("session_notes", json.RawMessage(notesSchema))
	nothink := map[string]any{"enable_thinking": false}
	// The history as the last call sent it, when it fits: results set
	// aside since then would change it near the start, and the server
	// would process the conversation again instead of continuing it.
	wire := r.wireMessages()
	if len(r.setAside) > 0 {
		extra := 0
		for i := range r.setAside {
			if m := r.Session.Messages[i]; m.Elided != "" {
				extra += contextplan.EstimateTokens(m.Elided) - contextplan.EstimateTokens(m.Content)
			}
		}
		if r.estimateTokens()+extra+2000 <= r.contextTokens()*9/10 {
			wire = r.wireMessagesWith(r.setAside)
		}
	}
	wire = append(wire, map[string]any{"role": "user", "content": notesPrompt})
	body := map[string]any{"messages": wire, "max_tokens": 1500, "response_format": format, "chat_template_kwargs": nothink}
	if tools := r.toolList(); len(tools) > 0 {
		// The same tools as the conversation's calls, so the prefix is
		// the same; none may be called.
		body["tools"] = toolSchemas(tools)
		body["tool_choice"] = "none"
	}
	notes, err := r.notesCall(ctx, body)
	if err == nil {
		return notes, "conversation", nil
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	limit := r.contextTokens() * 32 / 10 / 2 // bytes ≈ tokens*3.2, half the window
	body = map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You write notes on a coding session so another assistant can continue it. Be factual and specific."},
			{"role": "user", "content": notesPrompt + "\n\nThe session so far:\n\n" + transcript(head, limit)},
		},
		"max_tokens": 1500, "response_format": format, "chat_template_kwargs": nothink,
	}
	notes, err2 := r.notesCall(ctx, body)
	if err2 == nil {
		return notes, "transcript", nil
	}
	return nil, "", fmt.Errorf("%w; from a transcript: %w", err, err2)
}

func (r *Runner) notesCall(ctx context.Context, body map[string]any) (*ModelNotes, error) {
	status, resp, err := r.Gateway.ForwardFor(ctx, "compaction", "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(resp)
	if status != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", status, errMessage(raw))
	}
	m, err := parseCompletion(raw)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(m.Content)
	text = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```")
	var n ModelNotes
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &n); err != nil {
		return nil, fmt.Errorf("the notes are not the schema's JSON: %w", err)
	}
	return &n, nil
}

// compactLegacy asks the model for a free-text summary of a transcript of
// the conversation so far and replaces the history with it, keeping the
// most recent messages (direct.compaction: legacy). The summary is stored
// as a user message so every provider accepts it.
func (r *Runner) compactLegacy(ctx context.Context) error {
	msgs := r.Session.Messages
	if len(msgs) <= keepTail {
		return nil
	}
	cut := cutPoint(msgs)
	head, tail := msgs[:cut], msgs[cut:]
	// Keep the summary request itself within the window.
	text := transcript(head, r.contextTokens()*32/10/2) // bytes ≈ tokens*3.2, half the window
	body := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You summarise a coding session so another assistant can continue it. Be factual and specific."},
			{"role": "user", "content": "Summarise this conversation for continuation. Include: the user's goals and constraints; what was learned about the code (files, functions, findings, with paths); every change made so far (file and what changed); test or build results; open questions and what remains to do. Use short bullet points.\n\n" + text},
		},
		"max_tokens": 2000,
	}
	status, resp, err := r.Gateway.ForwardFor(ctx, "compaction", "/v1/chat/completions", body)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(resp)
	if status != http.StatusOK {
		return fmt.Errorf("summarising the conversation failed (http %d): %s", status, errMessage(raw))
	}
	m, err := parseCompletion(raw)
	if err != nil {
		return err
	}
	// A summary is derived from everything before it, tool output
	// included: it is data, never the user's words.
	summary := Message{Role: "user", Content: "[Earlier conversation, summarised to save context]\n\n" + strings.TrimSpace(m.Content) + recallIndex(r.toolList(), head) + "\n\n[End of summary. The conversation continues below.]",
		Provenance: untrusted(SourceSummary, "")}
	if err := r.Session.replace(summary, append([]Message(nil), tail...)); err != nil {
		return err
	}
	r.emit(Event{Kind: EventCompact, Text: fmt.Sprintf("Summarised %d earlier messages to stay within the model's context.", len(head))})
	return nil
}

// Compact summarises the history on request (the /compact command).
func (r *Runner) Compact(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Session == nil || len(r.Session.Messages) == 0 {
		return nil
	}
	return r.compact(ctx)
}

// recallIndex lists the larger tool results a summary replaces, so the
// model can bring any of them back with recall (the originals stay in the
// session's archive): the details a summary drops stay recoverable.
func recallIndex(tools []Tool, head []Message) string {
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	what := map[string]string{}
	for _, m := range head {
		for _, tc := range m.ToolCalls {
			if t, ok := byName[tc.Function.Name]; ok && t.Summary != nil {
				what[tc.ID] = t.Summary(callArgs(tc))
			} else {
				what[tc.ID] = tc.Function.Name
			}
		}
	}
	var lines []string
	for _, m := range head {
		text := m.Content
		if m.Elided != "" {
			text = m.Elided
		}
		if m.Role != "tool" || len(text) < elideMin || strings.HasPrefix(text, "error:") {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (%d bytes)", m.ToolCallID, oneLine(what[m.ToolCallID], 100), len(text)))
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > 40 {
		lines = append(lines[:40:40], fmt.Sprintf("- … %d more", len(lines)-40))
	}
	return "\n\nEarlier tool results kept in full; call recall with the id to see one again:\n" + strings.Join(lines, "\n")
}

// Resume reads the session back from its file and forgets what the runner
// kept in memory about it, as a new process continuing the session would.
func (r *Runner) Resume() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Session == nil || r.Session.Path == "" {
		return errors.New("the session has no file")
	}
	s, err := LoadSession(r.Session.Path)
	if err != nil {
		return err
	}
	r.Session, r.seen, r.fulls, r.setAside = s, 0, nil, nil
	r.readsMu.Lock()
	r.lastRead, r.noted = nil, nil
	r.readsMu.Unlock()
	return nil
}
