package inference

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// history compares a request's messages with the previous request of the
// same purpose and returns the message count and the index of the first
// message that differs (-1: none, the history only grew). The tools are
// part of the first message's hash: they are rendered before it.
//
// A local server reuses its cached tokens up to the first difference, so
// an earlier message that changed (a result set aside, a summary, an
// edited system prompt) is the cause of a cache miss that a growing
// history cannot be.
func (g *Gateway) history(purpose string, body map[string]any) (n, changed int) {
	msgs := messagesOf(body)
	hashes := make([]string, len(msgs))
	for i, m := range msgs {
		v := any(m)
		if i == 0 {
			v = []any{m, body["tools"]}
		}
		b, _ := json.Marshal(v)
		h := sha256.Sum256(b)
		hashes[i] = hex.EncodeToString(h[:8])
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.prev == nil {
		g.prev = map[string][]string{}
	}
	prev, seen := g.prev[purpose]
	g.prev[purpose] = hashes
	if !seen {
		return len(msgs), 0
	}
	for i := range min(len(prev), len(hashes)) {
		if prev[i] != hashes[i] {
			return len(msgs), i
		}
	}
	if len(hashes) < len(prev) {
		return len(msgs), len(hashes) // shorter: the history lost its tail
	}
	return len(msgs), -1
}

// normalize checks an answer before the caller sees it. A 200 that is not
// a chat completion becomes a 502 naming the problem; tool calls get the
// fields every caller relies on (an id, the type, JSON arguments); and a
// provider's context-overflow error gets one shape, OpenAI's
// context_length_exceeded, so every caller can recognise it and condense
// instead of failing.
func normalize(status int, resp []byte) (int, []byte) {
	if status != http.StatusOK {
		if (status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge) && IsContextOverflow(errMessage(resp)) {
			msg := errMessage(resp)
			if !strings.Contains(strings.ToLower(msg), "maximum context length") {
				msg = "This model's maximum context length is exceeded: " + msg
			}
			b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "invalid_request_error", "code": "context_length_exceeded"}})
			return http.StatusBadRequest, b
		}
		return status, resp
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return http.StatusBadGateway, errorBody("the model server's answer is not JSON: "+trunc(string(resp), 200), "malformed_response")
	}
	choices, _ := r["choices"].([]any)
	if len(choices) == 0 {
		return http.StatusBadGateway, errorBody("the model server's answer has no choices: "+trunc(string(resp), 200), "malformed_response")
	}
	fixed := false
	for _, c := range choices {
		cm, _ := c.(map[string]any)
		msg, _ := cm["message"].(map[string]any)
		if msg == nil {
			return http.StatusBadGateway, errorBody("the model server's answer has no message", "malformed_response")
		}
		calls, _ := msg["tool_calls"].([]any)
		for _, tc := range calls {
			call, _ := tc.(map[string]any)
			if call == nil {
				continue
			}
			if id, _ := call["id"].(string); id == "" {
				call["id"], fixed = newCallID(), true
			}
			if typ, _ := call["type"].(string); typ == "" {
				call["type"], fixed = "function", true
			}
			if fn, _ := call["function"].(map[string]any); fn != nil {
				if a, _ := fn["arguments"].(string); strings.TrimSpace(a) == "" {
					fn["arguments"], fixed = "{}", true
				}
			}
		}
	}
	if !fixed {
		return status, resp
	}
	b, err := json.Marshal(r)
	if err != nil {
		return status, resp
	}
	return status, b
}

// newCallID returns a tool-call id for a call the server sent without one.
func newCallID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return "call_" + hex.EncodeToString(b[:])
}

// IsContextOverflow reports a provider's error message for a prompt longer
// than the model's context: llama.cpp, OpenAI and compatible servers,
// Anthropic and Gemini each word it differently.
func IsContextOverflow(msg string) bool {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "context_length_exceeded"),
		strings.Contains(m, "maximum context length"),
		strings.Contains(m, "exceeds the available context size"),                              // llama.cpp
		strings.Contains(m, "prompt is too long"),                                              // Anthropic
		strings.Contains(m, "input token count") && strings.Contains(m, "exceeds the maximum"), // Gemini
		strings.Contains(m, "context window") && strings.Contains(m, "exceed"),
		strings.Contains(m, "context length") && strings.Contains(m, "exceed"):
		return true
	}
	return false
}
