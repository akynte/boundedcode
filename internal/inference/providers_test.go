package inference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a fake provider: it records requests and answers with the
// next scripted response.
type recorder struct {
	mu       sync.Mutex
	requests []recorded
	answer   func(n int, r recorded) (int, string, http.Header)
}

type recorded struct {
	Method, Path, Query string
	Header              http.Header
	Body                map[string]any
}

func (rc *recorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		rc.mu.Lock()
		rec := recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body}
		rc.requests = append(rc.requests, rec)
		n := len(rc.requests)
		rc.mu.Unlock()
		status, resp, h := rc.answer(n, rec)
		for k, v := range h {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (rc *recorder) last(t *testing.T, path string) recorded {
	t.Helper()
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for i := len(rc.requests) - 1; i >= 0; i-- {
		if strings.HasSuffix(rc.requests[i].Path, path) {
			return rc.requests[i]
		}
	}
	t.Fatalf("no request to %s; got %+v", path, rc.requests)
	return recorded{}
}

// agentRequest is a typical agent request in the OpenAI shape, with
// llama.cpp-only fields a host-side call adds.
func agentRequest(history ...map[string]any) map[string]any {
	msgs := []any{
		map[string]any{"role": "system", "content": "You are a coding agent."},
		map[string]any{"role": "user", "content": "Fix the bug in calc.go."},
	}
	for _, h := range history {
		msgs = append(msgs, h)
	}
	return map[string]any{
		"model": "alias", "messages": msgs, "max_completion_tokens": 4096, "temperature": 0.2,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "terminal", "description": "Run a command",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required": []any{"command"}, "additionalProperties": false}}}},
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

func message(t *testing.T, resp []byte) (map[string]any, string) {
	t.Helper()
	m := decode(t, resp)
	ch := m["choices"].([]any)[0].(map[string]any)
	return ch["message"].(map[string]any), ch["finish_reason"].(string)
}

func TestOpenAIUpstreamCloud(t *testing.T) {
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		switch n {
		case 1:
			return 429, `{"error":{"message":"slow down"}}`, http.Header{"Retry-After": {"0"}}
		case 2:
			return 400, `{"error":{"message":"Unsupported parameter: 'temperature'","param":"temperature","code":"unsupported_parameter"}}`, nil
		}
		return 200, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":60}}}`, nil
	}}
	srv := rc.server(t)
	c := NewClient(srv.URL+"/v1", 5*time.Second)
	c.Header = http.Header{"Authorization": {"Bearer sk-test"}}
	u := &OpenAIUpstream{Client: c, Name: ProviderOpenAI, Path: "/chat/completions", Cloud: true, Retry: RetryPolicy{Attempts: 3, Base: time.Millisecond}}
	status, resp, err := u.Complete(context.Background(), agentRequest())
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	last := rc.last(t, "/v1/chat/completions")
	if last.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("auth header %q", last.Header.Get("Authorization"))
	}
	for _, k := range []string{"chat_template_kwargs", "temperature"} {
		if _, ok := last.Body[k]; ok {
			t.Errorf("%s sent to a cloud provider", k)
		}
	}
	if len(rc.requests) != 3 {
		t.Fatalf("requests = %d, want 3 (429 retried, rejected parameter dropped)", len(rc.requests))
	}
	// The rejected parameter stays dropped.
	if _, _, err := u.Complete(context.Background(), agentRequest()); err != nil {
		t.Fatal(err)
	}
	if _, ok := rc.last(t, "/chat/completions").Body["temperature"]; ok {
		t.Error("temperature sent again after the provider rejected it")
	}
}

func TestAnthropicUpstream(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key-must-not-be-used")
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet {
			return 200, `{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","max_input_tokens":1000000,"max_tokens":128000,
				"capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"enabled":{"supported":false},"disabled":{"supported":false}}}}}`, nil
		}
		return 200, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"tool_use",
			"content":[{"type":"thinking","thinking":"","signature":"SIG-1"},{"type":"text","text":"Running tests."},
			{"type":"tool_use","id":"toolu_1","name":"terminal","input":{"command":"go test ./..."}}],
			"usage":{"input_tokens":10,"output_tokens":50,"cache_creation_input_tokens":20,"cache_read_input_tokens":70}}`, nil
	}}
	srv := rc.server(t)
	replay := &MemoryReplay{}
	u := &AnthropicUpstream{APIKey: "sk-ant-test", BaseURL: srv.URL, Model: "claude-opus-5-5", Effort: "high", Replay: replay, Timeout: 5 * time.Second}
	status, resp, err := u.Complete(context.Background(), agentRequest())
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	req := rc.last(t, "/v1/messages")
	if got := req.Header.Get("X-Api-Key"); got != "sk-ant-test" {
		t.Fatalf("x-api-key = %q (the environment key must not be used)", got)
	}
	if req.Header.Get("Anthropic-Version") == "" {
		t.Error("no anthropic-version header")
	}
	betas := strings.Join(req.Header.Values("Anthropic-Beta"), ",")
	if !strings.Contains(betas, betaThinkingBinding) {
		t.Errorf("betas %q lack %s", betas, betaThinkingBinding)
	}
	if strings.Contains(betas, betaFallback) {
		t.Errorf("fallbacks sent to a non-default endpoint: %q", betas)
	}
	b := req.Body
	if b["system"] != "You are a coding agent." {
		t.Errorf("system = %v", b["system"])
	}
	for _, k := range []string{"temperature", "chat_template_kwargs", "top_p"} {
		if _, ok := b[k]; ok {
			t.Errorf("%s sent to Anthropic", k)
		}
	}
	th := b["thinking"].(map[string]any)
	if th["type"] != "adaptive" || th["block_binding"].(map[string]any)["prefix_mismatch_behavior"] != "drop_block" {
		t.Errorf("thinking = %v", th)
	}
	if b["max_tokens"].(float64) < 16000 || b["output_config"].(map[string]any)["effort"] != "high" {
		t.Errorf("max_tokens %v output_config %v", b["max_tokens"], b["output_config"])
	}
	tool := b["tools"].([]any)[0].(map[string]any)
	if tool["name"] != "terminal" || tool["input_schema"] == nil {
		t.Errorf("tool = %v", tool)
	}
	msg, finish := message(t, resp)
	tcs := msg["tool_calls"].([]any)
	if finish != "tool_calls" || len(tcs) != 1 || msg["content"] != "Running tests." {
		t.Fatalf("message %v finish %s", msg, finish)
	}
	usage := decode(t, resp)["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 100 || usage["prompt_tokens_details"].(map[string]any)["cached_tokens"].(float64) != 70 {
		t.Errorf("usage = %v", usage)
	}

	// The next turn: the history comes back in the OpenAI shape, and the
	// thinking block is replayed verbatim with its turn.
	assistant := map[string]any{"role": "assistant", "content": "Running tests.", "tool_calls": tcs}
	toolRes := map[string]any{"role": "tool", "tool_call_id": "toolu_1", "content": "ok"}
	if _, _, err := u.Complete(context.Background(), agentRequest(assistant, toolRes)); err != nil {
		t.Fatal(err)
	}
	msgs := rc.last(t, "/v1/messages").Body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	asst := msgs[1].(map[string]any)["content"].([]any)
	if first := asst[0].(map[string]any); first["type"] != "thinking" || first["signature"] != "SIG-1" {
		t.Fatalf("thinking block not replayed: %v", asst)
	}
	result := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool result = %v", result)
	}
}

func TestAnthropicErrors(t *testing.T) {
	var calls int
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet {
			return 404, `{"type":"error","error":{"type":"not_found_error","message":"no"}}`, nil
		}
		calls++
		if r.Body["model"] == "bad-key" {
			return 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, nil
		}
		msgs := r.Body["messages"].([]any)
		for _, m := range msgs {
			for _, blk := range m.(map[string]any)["content"].([]any) {
				if blk.(map[string]any)["type"] == "thinking" {
					return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid ` + "`signature`" + ` in ` + "`thinking`" + ` block. The block is bound to a different conversation."}}`, nil
				}
			}
		}
		return 200, `{"id":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, nil
	}}
	srv := rc.server(t)
	u := &AnthropicUpstream{APIKey: "k", BaseURL: srv.URL, Model: "bad-key", Timeout: 5 * time.Second}
	status, resp, err := u.Complete(context.Background(), agentRequest())
	if err != nil || status != 401 || !strings.Contains(string(resp), "invalid x-api-key") {
		t.Fatalf("401: status %d err %v %s", status, err, resp)
	}
	// A stale thinking block is stripped and the request retried once.
	replay := &MemoryReplay{}
	replay.Put(ReplayKey(ProviderAnthropic, []string{"toolu_9"}, ""), json.RawMessage(`[{"type":"thinking","thinking":"","signature":"OLD"},{"type":"tool_use","id":"toolu_9","name":"terminal","input":{}}]`))
	u = &AnthropicUpstream{APIKey: "k", BaseURL: srv.URL, Model: "m", Replay: replay, Timeout: 5 * time.Second}
	calls = 0
	asst := map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "toolu_9", "type": "function", "function": map[string]any{"name": "terminal", "arguments": "{}"}}}}
	status, resp, err = u.Complete(context.Background(), agentRequest(asst, map[string]any{"role": "tool", "tool_call_id": "toolu_9", "content": "x"}))
	if err != nil || status != 200 || calls != 2 {
		t.Fatalf("binding recovery: status %d calls %d err %v %s", status, calls, err, resp)
	}
}

func TestGeminiUpstream(t *testing.T) {
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		return 200, `{"candidates":[{"content":{"role":"model","parts":[
			{"text":"thinking...","thought":true},
			{"functionCall":{"name":"terminal","args":{"command":"go test ./..."}},"thoughtSignature":"GSIG"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":200,"cachedContentTokenCount":150,"candidatesTokenCount":20,"thoughtsTokenCount":30},
			"modelVersion":"gemini-x","responseId":"r1"}`, nil
	}}
	srv := rc.server(t)
	replay := &MemoryReplay{}
	u := &GeminiUpstream{APIKey: "AIza-test", BaseURL: srv.URL, Model: "gemini-x", Replay: replay, Timeout: 5 * time.Second}
	status, resp, err := u.Complete(context.Background(), agentRequest())
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	req := rc.last(t, "/v1beta/models/gemini-x:generateContent")
	if req.Header.Get("X-Goog-Api-Key") != "AIza-test" || strings.Contains(req.Query, "key=") {
		t.Fatalf("key header %q query %q", req.Header.Get("X-Goog-Api-Key"), req.Query)
	}
	decl := req.Body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if decl["parametersJsonSchema"] == nil || decl["parameters"] != nil {
		t.Errorf("declaration = %v", decl)
	}
	if req.Body["systemInstruction"] == nil {
		t.Error("no systemInstruction")
	}
	msg, finish := message(t, resp)
	tcs := msg["tool_calls"].([]any)
	if finish != "tool_calls" || len(tcs) != 1 || msg["content"] != nil {
		t.Fatalf("message %v finish %s (thought text must not be answer text)", msg, finish)
	}
	usage := decode(t, resp)["usage"].(map[string]any)
	if usage["completion_tokens"].(float64) != 50 || usage["prompt_tokens_details"].(map[string]any)["cached_tokens"].(float64) != 150 {
		t.Errorf("usage = %v", usage)
	}
	callID := tcs[0].(map[string]any)["id"].(string)
	assistant := map[string]any{"role": "assistant", "content": nil, "tool_calls": tcs}
	toolRes := map[string]any{"role": "tool", "tool_call_id": callID, "content": "PASS"}
	if _, _, err := u.Complete(context.Background(), agentRequest(assistant, toolRes)); err != nil {
		t.Fatal(err)
	}
	contents := rc.last(t, ":generateContent").Body["contents"].([]any)
	model := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Fatalf("contents = %v", contents)
	}
	var sig string
	for _, p := range model["parts"].([]any) {
		if s, ok := p.(map[string]any)["thoughtSignature"].(string); ok {
			sig = s
		}
	}
	if sig != "GSIG" {
		t.Fatalf("thought signature not replayed: %v", model["parts"])
	}
	fr := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "terminal" || fr["id"] != nil {
		t.Fatalf("functionResponse = %v (name from the call; no id Gemini did not issue)", fr)
	}
}

func TestGatewayMetersCloudUsage(t *testing.T) {
	rc := &recorder{answer: func(int, recorded) (int, string, http.Header) {
		return 200, `{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1000,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":900}}}`, nil
	}}
	srv := rc.server(t)
	gw := &Gateway{Upstream: &OpenAIUpstream{Client: NewClient(srv.URL, time.Second), Name: ProviderOpenAI, Path: "/chat/completions", Cloud: true}, Model: "gpt-x"}
	if _, _, err := gw.Forward(context.Background(), "/v1/chat/completions", agentRequest()); err != nil {
		t.Fatal(err)
	}
	processed, generated, cached := gw.Stats()
	if processed != 110 || generated != 10 || cached != 900 || gw.Provider() != ProviderOpenAI {
		t.Fatalf("processed %d generated %d cached %d provider %s", processed, generated, cached, gw.Provider())
	}
}
