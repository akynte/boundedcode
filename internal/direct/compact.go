package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// compact asks the model for a summary of the conversation so far and
// replaces the history with it, keeping the most recent messages. The
// summary is stored as a user message so every provider accepts it.
func (r *Runner) compact(ctx context.Context) error {
	msgs := r.Session.Messages
	if len(msgs) <= keepTail {
		return nil
	}
	// Cut at a user message so tool calls and results stay paired.
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
	head, tail := msgs[:cut], msgs[cut:]
	var transcript strings.Builder
	for _, m := range head {
		switch m.Role {
		case "user":
			transcript.WriteString("USER: " + m.Content + "\n")
		case "assistant":
			if m.Content != "" {
				transcript.WriteString("ASSISTANT: " + m.Content + "\n")
			}
			for _, tc := range m.ToolCalls {
				transcript.WriteString("ASSISTANT CALLED " + tc.Function.Name + " " + oneLine(tc.Function.Arguments, 300) + "\n")
			}
		case "tool":
			transcript.WriteString("TOOL RESULT: " + oneLine(m.Content, 400) + "\n")
		}
	}
	text := transcript.String()
	// Keep the summary request itself within the window.
	limit := r.contextTokens() * 32 / 10 / 2 // bytes ≈ tokens*3.2, half the window
	if len(text) > limit {
		text = text[:limit/3] + "\n…\n" + text[len(text)-limit*2/3:]
	}
	body := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You summarise a coding session so another assistant can continue it. Be factual and specific."},
			{"role": "user", "content": "Summarise this conversation for continuation. Include: the user's goals and constraints; what was learned about the code (files, functions, findings, with paths); every change made so far (file and what changed); test or build results; open questions and what remains to do. Use short bullet points.\n\n" + text},
		},
		"max_tokens": 2000,
	}
	status, resp, err := r.Gateway.Forward(ctx, "/v1/chat/completions", body)
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
