package inference

import (
	"context"
	"encoding/json"
)

// How the model's reasoning travels with the history (config
// inference.reasoning_history).
const (
	// ReasoningAuto keeps it where the model's chat template renders it
	// (llama.cpp: the template reads reasoning_content), and drops it for
	// every other model and API.
	ReasoningAuto = "auto"
	// ReasoningKeep always sends it back: for an OpenAI-compatible service
	// whose documentation asks for reasoning_content in tool-call turns.
	ReasoningKeep = "keep"
	// ReasoningDrop never sends it back.
	ReasoningDrop = "drop"
)

// A chat template that renders reasoning shows the model its earlier turns
// as it generated them: reasoning, then the answer and its tool calls.
// When the history arrives without the reasoning, the template renders an
// empty reasoning block where the model had reasoned. The model then reads
// a history it did not write, and llama-server's cached tokens (which hold
// the reasoning it generated) stop matching from that turn on, so every
// call processes the previous answer again.
//
// reasoningHistory puts each assistant turn's reasoning_content back when
// the model gets it and the caller's history lacks it (OpenHands drops it
// for models it does not know), and removes it otherwise. Reasoning stays
// on the host: Replay is the gateway's, never the agent's.
func (u *OpenAIUpstream) reasoningHistory(ctx context.Context, body map[string]any) {
	keep := u.Capabilities(ctx).ReasoningRoundTrip
	for _, m := range messagesOf(body) {
		if m["role"] != "assistant" {
			delete(m, "reasoning_content")
			continue
		}
		delete(m, "reasoning") // another server's name for it; never sent
		if !keep {
			delete(m, "reasoning_content")
			continue
		}
		if s, _ := m["reasoning_content"].(string); s != "" {
			continue
		}
		delete(m, "reasoning_content")
		if r, ok := u.replay().Get(turnKey(m)); ok {
			var text string
			if json.Unmarshal(r, &text) == nil && text != "" {
				m["reasoning_content"] = text
			}
		}
	}
}

// remember stores the reasoning of an answer under its turn's key.
func (u *OpenAIUpstream) remember(resp []byte) {
	if !u.caps.ReasoningRoundTrip {
		return
	}
	var r struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(resp, &r) != nil || len(r.Choices) == 0 || r.Choices[0].Message == nil {
		return
	}
	m := r.Choices[0].Message
	text, _ := m["reasoning_content"].(string)
	if text == "" {
		return
	}
	if b, err := json.Marshal(text); err == nil {
		u.replay().Put(turnKey(m), b)
	}
}

func (u *OpenAIUpstream) replay() ReplayStore {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Replay == nil {
		u.Replay = &MemoryReplay{}
	}
	return u.Replay
}

// turnKey is the replay key of an assistant message in the OpenAI shape.
func turnKey(m map[string]any) string {
	b, _ := json.Marshal(m)
	var om oaMessage
	_ = json.Unmarshal(b, &om)
	return replayKeyOf(ProviderLocal, om)
}

// messagesOf returns a request's messages as maps that can be changed in
// place, whether the body came from JSON ([]any) or from Go code.
func messagesOf(body map[string]any) []map[string]any {
	switch ms := body["messages"].(type) {
	case []map[string]any:
		return ms
	case []any:
		out := make([]map[string]any, 0, len(ms))
		for _, m := range ms {
			if mm, ok := m.(map[string]any); ok {
				out = append(out, mm)
			}
		}
		return out
	}
	return nil
}
