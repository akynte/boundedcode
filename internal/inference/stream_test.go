package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer answers a streamed chat completion: text in pieces, a tool
// call in fragments, then usage and llama.cpp timings in the last chunk.
func sseServer(t *testing.T, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope","type":"server_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"lo","reasoning_content":"think"}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"re","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"ad","arguments":"th\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":9},"timings":{"cache_n":100,"prompt_n":20,"predicted_n":9}}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestStreamFoldsServerSentEvents(t *testing.T) {
	srv, got := sseServer(t, http.StatusOK)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
	var deltas []Delta
	status, resp, err := g.Stream(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}}, func(d Delta) { deltas = append(deltas, d) })
	if err != nil || status != http.StatusOK {
		t.Fatalf("status %d err %v", status, err)
	}
	if (*got)["stream"] != true {
		t.Fatalf("request did not ask for a stream: %v", *got)
	}
	if len(deltas) != 2 || deltas[0].Text != "Hel" || deltas[1].Text != "lo" || deltas[1].Reasoning != "think" {
		t.Fatalf("deltas: %+v", deltas)
	}
	raw, _ := json.Marshal(resp)
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) != 1 {
		t.Fatalf("folded answer: %v %s", err, raw)
	}
	c := parsed.Choices[0]
	if c.Message.Content != "Hello" || c.Message.ReasoningContent != "think" || c.FinishReason != "tool_calls" {
		t.Fatalf("message: %+v", c)
	}
	if len(c.Message.ToolCalls) != 1 || c.Message.ToolCalls[0].ID != "c1" || c.Message.ToolCalls[0].Function.Name != "read" || c.Message.ToolCalls[0].Function.Arguments != `{"path":"x"}` {
		t.Fatalf("tool calls: %+v", c.Message.ToolCalls)
	}
	if parsed.Usage.PromptTokens != 120 || parsed.Usage.CompletionTokens != 9 {
		t.Fatalf("usage: %+v", parsed.Usage)
	}
	// Metered like a plain call: 20 processed prompt tokens + 9, 100 cached.
	if p, gen, cached := g.Stats(); p != 29 || gen != 9 || cached != 100 || g.Calls() != 1 {
		t.Fatalf("stats: %d %d %d calls %d", p, gen, cached, g.Calls())
	}
}

func TestStreamErrorsAndFallback(t *testing.T) {
	srv, _ := sseServer(t, http.StatusInternalServerError)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
	called := false
	status, resp, err := g.Stream(context.Background(), "/v1/chat/completions", map[string]any{}, func(Delta) { called = true })
	if err != nil || status != http.StatusInternalServerError || called {
		t.Fatalf("status %d err %v called %v", status, err, called)
	}
	if raw, _ := json.Marshal(resp); !strings.Contains(string(raw), "nope") {
		t.Fatalf("error body: %s", raw)
	}
	// An upstream that cannot stream answers whole; the callback is unused.
	plain := &plainUpstream{}
	g = &Gateway{Upstream: plain, Model: "m"}
	status, resp, err = g.Stream(context.Background(), "/v1/chat/completions", map[string]any{}, func(Delta) { called = true })
	if err != nil || status != http.StatusOK || called {
		t.Fatalf("fallback: status %d err %v called %v", status, err, called)
	}
	if raw, _ := json.Marshal(resp); !strings.Contains(string(raw), "whole") {
		t.Fatalf("fallback body: %s", raw)
	}
}

type plainUpstream struct{}

func (plainUpstream) Provider() string { return "test" }
func (plainUpstream) Complete(context.Context, map[string]any) (int, []byte, error) {
	return http.StatusOK, []byte(`{"choices":[{"message":{"role":"assistant","content":"whole"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`), nil
}
