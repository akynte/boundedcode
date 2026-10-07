package inference

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The OpenAI chat-completions request and response shapes, as far as the
// translating upstreams need them.

type oaMessage struct {
	Role       string       `json:"role"`
	Content    any          `json:"content"` // string, []part or null
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Name       string       `json:"name,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// oaRequest is the subset of a chat-completions request the translators
// read. Fields they do not know are ignored (logged nowhere: requests carry
// repository content).
type oaRequest struct {
	Messages            []oaMessage `json:"messages"`
	Tools               []oaTool    `json:"tools"`
	ToolChoice          any         `json:"tool_choice"`
	ParallelToolCalls   *bool       `json:"parallel_tool_calls"`
	MaxTokens           int         `json:"max_tokens"`
	MaxCompletionTokens int         `json:"max_completion_tokens"`
	Stop                any         `json:"stop"`
	Temperature         *float64    `json:"temperature"`
	TopP                *float64    `json:"top_p"`
}

func parseOARequest(body map[string]any) (oaRequest, error) {
	var r oaRequest
	b, err := json.Marshal(body)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("chat request: %w", err)
	}
	return r, nil
}

// maxOutput is the request's output limit (0 = unset).
func (r oaRequest) maxOutput() int {
	if r.MaxCompletionTokens > 0 {
		return r.MaxCompletionTokens
	}
	return r.MaxTokens
}

// stops returns the stop sequences.
func (r oaRequest) stops() []string {
	var out []string
	switch v := r.Stop.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, s := range v {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
	}
	var keep []string
	for _, s := range out {
		if strings.TrimSpace(s) != "" { // providers reject whitespace-only stops
			keep = append(keep, s)
		}
	}
	return keep
}

// toolChoice returns "auto", "none" or "required".
func (r oaRequest) toolChoice() string {
	switch v := r.ToolChoice.(type) {
	case string:
		return v
	case map[string]any:
		return "required" // a named function: forced use
	}
	return "auto"
}

// oaPart is one element of an OpenAI content array.
type oaPart struct {
	Type     string // "text" or "image"
	Text     string
	MimeType string // images: from a data URL
	Data     string // images: base64 payload
}

// parts normalizes a message's content into text and inline-image parts.
// Images that are not data URLs are kept as a text placeholder (the agent's
// tools produce text; a remote image would need a fetch the host does not
// make).
func (m oaMessage) parts() []oaPart {
	switch c := m.Content.(type) {
	case string:
		if c == "" {
			return nil
		}
		return []oaPart{{Type: "text", Text: c}}
	case []any:
		var out []oaPart
		for _, raw := range c {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch p["type"] {
			case "text":
				if t, _ := p["text"].(string); t != "" {
					out = append(out, oaPart{Type: "text", Text: t})
				}
			case "image_url":
				url := ""
				if iu, ok := p["image_url"].(map[string]any); ok {
					url, _ = iu["url"].(string)
				}
				if mime, data, ok := dataURL(url); ok {
					out = append(out, oaPart{Type: "image", MimeType: mime, Data: data})
				} else {
					out = append(out, oaPart{Type: "text", Text: "[image omitted: not inline data]"})
				}
			}
		}
		return out
	}
	return nil
}

// text joins a message's text parts.
func (m oaMessage) text() string {
	var b strings.Builder
	for _, p := range m.parts() {
		if p.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func dataURL(u string) (mime, data string, ok bool) {
	rest, found := strings.CutPrefix(u, "data:")
	if !found {
		return "", "", false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found || !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(meta, ";base64"), payload, true
}

// toolArgs parses a tool call's JSON arguments; invalid JSON is passed as a
// single string field so the call still round-trips.
func toolArgs(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil || m == nil {
		return map[string]any{"_arguments": s}
	}
	return m
}

// oaResult is a translated response.
type oaResult struct {
	ID           string
	Model        string
	Text         string
	ToolCalls    []oaToolCall
	FinishReason string // stop | length | tool_calls | content_filter
	Prompt       int    // all prompt tokens, cached ones included
	Cached       int    // prompt tokens read from the provider's cache
	Completion   int    // output tokens, thinking included
}

func (r oaResult) marshal() []byte {
	msg := map[string]any{"role": "assistant", "content": nil}
	if r.Text != "" {
		msg["content"] = r.Text
	}
	if len(r.ToolCalls) > 0 {
		msg["tool_calls"] = r.ToolCalls
	}
	b, _ := json.Marshal(map[string]any{
		"id": r.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": r.Model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": r.FinishReason}},
		"usage": map[string]any{
			"prompt_tokens": r.Prompt, "completion_tokens": r.Completion, "total_tokens": r.Prompt + r.Completion,
			"prompt_tokens_details": map[string]any{"cached_tokens": r.Cached},
		},
	})
	return b
}

// replayKey is the key of the assistant turn a response produced.
func (r oaResult) replayKey(provider string) string {
	ids := make([]string, 0, len(r.ToolCalls))
	for _, tc := range r.ToolCalls {
		ids = append(ids, tc.ID)
	}
	return ReplayKey(provider, ids, r.Text)
}

// replayKeyOf is the key of an assistant turn in the request history.
func replayKeyOf(provider string, m oaMessage) string {
	ids := make([]string, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		ids = append(ids, tc.ID)
	}
	return ReplayKey(provider, ids, m.text())
}
