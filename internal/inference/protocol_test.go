package inference

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// localServer is a fake llama-server: GET /props answers with template
// (empty: 404), and chat requests get answer(n, body).
func localServer(t *testing.T, template string, answer func(n int, r recorded) (int, string, http.Header)) (*recorder, *httptest.Server) {
	t.Helper()
	rc := &recorder{}
	rc.answer = func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet && r.Path == "/props" {
			if template == "" {
				return 404, `{"error":"not found"}`, nil
			}
			b, _ := json.Marshal(map[string]any{"chat_template": template, "build_info": "b11146-test",
				"chat_template_caps": map[string]any{"supports_tool_calls": true, "supports_preserve_reasoning": true}})
			return 200, string(b), nil
		}
		return answer(n, r)
	}
	return rc, rc.server(t)
}

// chatAnswer is a chat completion with reasoning and one tool call.
func chatAnswer(reasoning, callID string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{
		"role": "assistant", "content": "", "reasoning_content": reasoning,
		"tool_calls": []any{map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": "terminal", "arguments": `{"command":"ls"}`}}}}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}})
	return string(b)
}

func lastChat(t *testing.T, rc *recorder) []any {
	t.Helper()
	return rc.last(t, "/v1/chat/completions").Body["messages"].([]any)
}

// The model's reasoning goes back to a local model whose template renders
// it: kept when the caller sent it, restored from the gateway when the
// caller's history dropped it (OpenHands does), and never put on user or
// tool messages.
func TestLocalReasoningRoundTrip(t *testing.T) {
	rc, srv := localServer(t, `{% if message.reasoning_content is string %}<think>{{ message.reasoning_content }}</think>{% endif %}`,
		func(n int, r recorded) (int, string, http.Header) {
			return 200, chatAnswer(fmt.Sprintf("why %d", n), fmt.Sprintf("c%d", n)), nil
		})
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
	ctx := context.Background()
	if caps := g.Capabilities(ctx); !caps.ReasoningRoundTrip || !caps.PreserveReasoning || !caps.PromptProgress || caps.Source != "server" || caps.Server != "b11146-test" {
		t.Fatalf("capabilities %+v", caps)
	}
	if status, _, _ := g.Forward(ctx, "/v1/chat/completions", agentRequest()); status != 200 {
		t.Fatal(status)
	}
	// The first answer came from request 2 (request 1 was GET /props): its
	// tool call is c2 and its reasoning "why 2".
	dropped := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c2", "type": "function",
		"function": map[string]any{"name": "terminal", "arguments": `{"command":"ls"}`}}}}
	tool := map[string]any{"role": "tool", "tool_call_id": "c2", "content": "a.go", "reasoning_content": "not mine"}
	if status, _, _ := g.Forward(ctx, "/v1/chat/completions", agentRequest(dropped, tool)); status != 200 {
		t.Fatal(status)
	}
	msgs := lastChat(t, rc)
	if got := msgs[2].(map[string]any)["reasoning_content"]; got != "why 2" {
		t.Fatalf("restored reasoning = %v", got)
	}
	if _, ok := msgs[3].(map[string]any)["reasoning_content"]; ok {
		t.Fatalf("reasoning on a tool message: %v", msgs[3])
	}
	// A caller that keeps its own reasoning (direct mode) sends it as is.
	own := map[string]any{"role": "assistant", "content": "done", "reasoning_content": "mine"}
	g.Forward(ctx, "/v1/chat/completions", agentRequest(own))
	if got := lastChat(t, rc)[2].(map[string]any)["reasoning_content"]; got != "mine" {
		t.Fatalf("own reasoning = %v", got)
	}
}

// A template that does not render reasoning, an explicit "drop", and a
// cloud API never get it; "keep" sends it even then.
func TestReasoningDroppedWhereNotRead(t *testing.T) {
	own := map[string]any{"role": "assistant", "content": "done", "reasoning_content": "mine", "reasoning": "x"}
	cases := []struct {
		name, template, mode string
		cloud, want          bool
	}{
		{"template without reasoning", "{{ message.content }}", "", false, false},
		{"not llama.cpp (no /props)", "", "", false, false},
		{"drop", "{{ message.reasoning_content }}", ReasoningDrop, false, false},
		{"keep", "{{ message.content }}", ReasoningKeep, false, true},
		{"cloud", "", "", true, false},
		{"cloud keep", "", ReasoningKeep, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc, srv := localServer(t, c.template, func(int, recorded) (int, string, http.Header) { return 200, chatAnswer("r", "c"), nil })
			u := LocalUpstream(NewClient(srv.URL, 0), c.mode, nil)
			u.Cloud = c.cloud
			if _, _, err := u.Complete(context.Background(), agentRequest(copyMap(own))); err != nil {
				t.Fatal(err)
			}
			m := lastChat(t, rc)[2].(map[string]any)
			if _, has := m["reasoning_content"]; has != c.want {
				t.Fatalf("reasoning_content sent = %v, want %v (%v)", has, c.want, m)
			}
			if _, has := m["reasoning"]; has {
				t.Fatalf("reasoning field sent: %v", m)
			}
		})
	}
}

func copyMap(m map[string]any) map[string]any {
	c := map[string]any{}
	for k, v := range m {
		c[k] = v
	}
	return c
}

// A task's gateway keeps the reasoning on disk, so a resumed run (a new
// upstream) still restores it.
func TestLocalReasoningSurvivesResume(t *testing.T) {
	rc, srv := localServer(t, "{{ message.reasoning_content }}", func(n int, r recorded) (int, string, http.Header) {
		return 200, chatAnswer("kept", "call_9"), nil
	})
	path := filepath.Join(t.TempDir(), "local-replay.jsonl")
	u := LocalUpstream(NewClient(srv.URL, 0), "", &FileReplay{Path: path})
	u.Complete(context.Background(), agentRequest())
	again := LocalUpstream(NewClient(srv.URL, 0), "", &FileReplay{Path: path})
	turn := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call_9", "type": "function",
		"function": map[string]any{"name": "terminal", "arguments": `{"command":"ls"}`}}}}
	again.Complete(context.Background(), agentRequest(turn))
	if got := lastChat(t, rc)[2].(map[string]any)["reasoning_content"]; got != "kept" {
		t.Fatalf("after resume: %v", got)
	}
}

// A stream that ends without its end (no finish reason, no [DONE]) is
// not an answer: the caller is told to drop what it showed, the request
// is sent once more, and a second break is an error, not a partial answer.
func TestInterruptedStreamIsRetried(t *testing.T) {
	var n atomic.Int32
	breaks := int32(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n.Add(1) <= breaks {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Par\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c\",\"function\":{\"name\":\"rm\",\"arguments\":\"{\\\"pa\"}}]}}]}\n\n")
			return // the connection ends mid-answer
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Whole\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m", KeepLog: true}
	var deltas []Delta
	status, resp, err := g.Stream(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}}, func(d Delta) { deltas = append(deltas, d) })
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v", status, err)
	}
	if len(deltas) != 3 || deltas[0].Text != "Par" || !deltas[1].Reset || deltas[2].Text != "Whole" {
		t.Fatalf("deltas %+v", deltas)
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "rm") || !strings.Contains(string(raw), "Whole") {
		t.Fatalf("answer %s", raw)
	}
	if log := g.CallLog(); len(log) != 1 || log[0].Retried != 1 || log[0].Status != "ok" {
		t.Fatalf("call log %+v", log)
	}

	breaks = 100
	status, resp, _ = g.Stream(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}}, nil)
	raw, _ = json.Marshal(resp)
	if status != http.StatusBadGateway || !strings.Contains(string(raw), "ended before the answer was complete") {
		t.Fatalf("twice cut: %d %s", status, raw)
	}
}

// An error event in a stream is an error answer with its status, not a
// 200 the caller then fails to read.
func TestStreamErrorEventKeepsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"code\":400,\"message\":\"the request exceeds the available context size, try increasing it\",\"type\":\"exceed_context_size_error\"}}\n\n")
	}))
	t.Cleanup(srv.Close)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
	status, resp, _ := g.Stream(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}}, nil)
	raw, _ := json.Marshal(resp)
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "context_length_exceeded") {
		t.Fatalf("%d %s", status, raw)
	}
}

// Answers the caller cannot use become a 502 that says why; tool calls get
// an id, a type and JSON arguments.
func TestMalformedAnswers(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       string
	}{
		{"not JSON", "<html>bad gateway</html>", 502, "not JSON"},
		{"no choices", `{"choices":[]}`, 502, "no choices"},
		{"tool call without id", `{"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"function":{"name":"ls","arguments":""}}]}}]}`, 200, `"arguments":"{}"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, c.body) }))
			t.Cleanup(srv.Close)
			g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
			status, resp, _ := g.Forward(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}})
			raw, _ := json.Marshal(resp)
			if status != c.status || !strings.Contains(string(raw), c.want) {
				t.Fatalf("%d %s", status, raw)
			}
			if c.status == 200 && (!strings.Contains(string(raw), `"id":"call_`) || !strings.Contains(string(raw), `"type":"function"`)) {
				t.Fatalf("tool call not completed: %s", raw)
			}
		})
	}
}

// llama.cpp answers a malformed tool call with a 500; a local upstream
// samples the request again. Local requests carry an Idempotency-Key,
// cloud ones do not.
func TestLocalServerErrorIsSampledAgain(t *testing.T) {
	rc, srv := localServer(t, "", func(n int, r recorded) (int, string, http.Header) {
		if n == 2 { // request 1 was GET /props
			return 500, `{"error":{"code":500,"message":"Failed to parse tool call arguments as JSON: [json.exception.parse_error.101] parse error","type":"server_error"}}`, nil
		}
		return 200, chatAnswer("", "c"), nil
	})
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m"}
	if status, resp, _ := g.Forward(context.Background(), "/v1/chat/completions", agentRequest()); status != 200 {
		t.Fatalf("%d %v", status, resp)
	}
	if n := len(rc.requests); n != 3 {
		t.Fatalf("%d requests", n)
	}
	if rc.requests[1].Header.Get("Idempotency-Key") == "" || rc.requests[1].Header.Get("Idempotency-Key") == rc.requests[2].Header.Get("Idempotency-Key") {
		t.Fatalf("idempotency keys %q %q", rc.requests[1].Header.Get("Idempotency-Key"), rc.requests[2].Header.Get("Idempotency-Key"))
	}
	cloud := &OpenAIUpstream{Client: NewClient(srv.URL, 0), Name: ProviderOpenAI, Path: "/v1/chat/completions", Cloud: true}
	cloud.Complete(context.Background(), agentRequest())
	if k := rc.last(t, "/v1/chat/completions").Header.Get("Idempotency-Key"); k != "" {
		t.Fatalf("cloud request has an idempotency key %q", k)
	}
}

// A server that closes a kept-alive connection just as the next request
// arrives (llama-server closes idle ones) must not fail the request.
func TestClosedKeepAliveConnectionIsRetried(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var conns atomic.Int32
	answer := `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			first := conns.Add(1) == 1
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for served := 0; ; served++ {
					req, err := http.ReadRequest(br)
					if err != nil {
						return
					}
					io.Copy(io.Discard, req.Body)
					if first && served == 1 {
						return // closes the reused connection without answering
					}
					fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(answer), answer)
				}
			}(c)
		}
	}()
	g := &Gateway{Client: NewClient("http://"+ln.Addr().String(), 5*time.Second), Model: "m"}
	for i := range 2 {
		status, resp, _ := g.Forward(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}})
		if status != 200 {
			t.Fatalf("request %d: %d %v", i+1, status, resp)
		}
	}
	if conns.Load() < 2 {
		t.Fatalf("the second request was not sent again on a new connection (%d connections)", conns.Load())
	}
}

// Every provider's context overflow gets OpenAI's shape, which direct mode,
// LiteLLM and OpenHands recognise; other 4xx errors are left alone.
func TestContextOverflowNormalized(t *testing.T) {
	for _, msg := range []string{
		"the request exceeds the available context size, try increasing it",
		"prompt is too long: 215000 tokens > 200000 maximum",
		"The input token count (1200000) exceeds the maximum number of tokens allowed (1048576).",
		"This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens.",
	} {
		if !IsContextOverflow(msg) {
			t.Errorf("not recognised: %q", msg)
		}
		status, body := normalize(400, errorBody(msg, "invalid_request_error"))
		if status != 400 || !strings.Contains(string(body), `"code":"context_length_exceeded"`) || !strings.Contains(strings.ToLower(string(body)), "maximum context length") {
			t.Errorf("%q → %d %s", msg, status, body)
		}
	}
	for _, msg := range []string{"Request too large for gpt-x on tokens per min (TPM): Limit 30000", "invalid tool schema"} {
		if IsContextOverflow(msg) {
			t.Errorf("misread as an overflow: %q", msg)
		}
	}
}

// The call log names the first message a request changed: a growing
// history is -1, a rewritten one names the index, and a call of another
// purpose is compared with its own kind.
func TestHistoryChangeAttribution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":1},"timings":{"cache_n":1,"prompt_n":3}}`)
	}))
	t.Cleanup(srv.Close)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m", KeepLog: true}
	msg := func(role, text string) map[string]any { return map[string]any{"role": role, "content": text} }
	send := func(purpose string, ms ...map[string]any) {
		var arr []any
		for _, m := range ms {
			arr = append(arr, m)
		}
		g.ForwardFor(context.Background(), purpose, "/v1/chat/completions", map[string]any{"messages": arr})
	}
	s, u, a, tr := msg("system", "s"), msg("user", "u"), msg("assistant", "a"), msg("tool", "big result")
	send("", s, u)
	send("", s, u, a, tr)
	send("compaction", msg("user", "summarise"))
	send("", s, u, a, msg("tool", "[set aside]"), msg("user", "next"))
	log := g.CallLog()
	got := []int{log[0].Changed, log[1].Changed, log[2].Changed, log[3].Changed}
	if fmt.Sprint(got) != "[0 -1 0 3]" || log[1].Messages != 4 || log[1].Evaluated != 3 {
		t.Fatalf("changed %v, log %+v", got, log)
	}
}

// Structured output: the OpenAI shape goes as is to llama.cpp and
// OpenAI-compatible servers, becomes output_config.format for Anthropic
// and responseJsonSchema for Gemini, and is dropped where it is rejected.
func TestStructuredOutputPerProvider(t *testing.T) {
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []any{"ok"},
		"properties": map[string]any{"ok": map[string]any{"type": "boolean"}}}
	withFormat := func() map[string]any {
		b := agentRequest()
		b["response_format"] = JSONSchemaFormat("check", schema)
		return b
	}
	t.Run("llama.cpp", func(t *testing.T) {
		rc, srv := localServer(t, "", func(int, recorded) (int, string, http.Header) { return 200, chatAnswer("", "c"), nil })
		LocalUpstream(NewClient(srv.URL, 0), "", nil).Complete(context.Background(), withFormat())
		rf, _ := rc.last(t, "/v1/chat/completions").Body["response_format"].(map[string]any)
		if rf["type"] != "json_schema" || rf["json_schema"].(map[string]any)["schema"] == nil {
			t.Fatalf("response_format %v", rf)
		}
	})
	t.Run("openai-compatible rejects it", func(t *testing.T) {
		rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
			if _, has := r.Body["response_format"]; has {
				return 400, `{"error":{"message":"Unsupported parameter: 'response_format'","param":"response_format","code":"unsupported_parameter"}}`, nil
			}
			return 200, chatAnswer("", "c"), nil
		}}
		srv := rc.server(t)
		u := &OpenAIUpstream{Client: NewClient(srv.URL, 0), Name: ProviderOpenAICompatible, Path: "/chat/completions", Cloud: true}
		if status, _, _ := u.Complete(context.Background(), withFormat()); status != 200 || len(rc.requests) != 2 {
			t.Fatalf("status %d after %d requests", status, len(rc.requests))
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
			if r.Method == http.MethodGet {
				return 404, `{"type":"error","error":{"type":"not_found_error","message":"no"}}`, nil
			}
			if oc, _ := r.Body["output_config"].(map[string]any); oc != nil && oc["format"] != nil && n < 3 {
				return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.format: structured outputs are not supported by this model"}}`, nil
			}
			return 200, `{"id":"m","type":"message","role":"assistant","model":"x","stop_reason":"end_turn","content":[{"type":"text","text":"{\"ok\":true}"}],"usage":{"input_tokens":1,"output_tokens":1}}`, nil
		}}
		srv := rc.server(t)
		u := &AnthropicUpstream{APIKey: "k", BaseURL: srv.URL, Model: "x", Effort: "low", Timeout: 5 * time.Second}
		status, _, err := u.Complete(context.Background(), withFormat())
		if err != nil || status != 200 {
			t.Fatalf("%d %v", status, err)
		}
		var posts []recorded
		for _, r := range rc.requests {
			if r.Method == http.MethodPost {
				posts = append(posts, r)
			}
		}
		first := posts[0].Body["output_config"].(map[string]any)
		f := first["format"].(map[string]any)
		if f["type"] != "json_schema" || f["schema"] == nil || first["effort"] != "low" {
			t.Fatalf("first output_config %v", first)
		}
		if oc := posts[len(posts)-1].Body["output_config"].(map[string]any); oc["format"] != nil || oc["effort"] != "low" {
			t.Fatalf("retry output_config %v", oc)
		}
	})
	t.Run("gemini", func(t *testing.T) {
		rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
			gen, _ := r.Body["generationConfig"].(map[string]any)
			if gen["responseJsonSchema"] != nil && n == 1 {
				return 400, `{"error":{"code":400,"message":"Invalid JSON payload: responseJsonSchema is not supported","status":"INVALID_ARGUMENT"}}`, nil
			}
			return 200, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"ok\":true}"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`, nil
		}}
		srv := rc.server(t)
		u := &GeminiUpstream{APIKey: "k", BaseURL: srv.URL, Model: "g", Timeout: 5 * time.Second}
		if status, _, err := u.Complete(context.Background(), withFormat()); err != nil || status != 200 {
			t.Fatalf("%d %v", status, err)
		}
		first := rc.requests[0].Body["generationConfig"].(map[string]any)
		if first["responseMimeType"] != "application/json" || first["responseJsonSchema"] == nil {
			t.Fatalf("generationConfig %v", first)
		}
		if gen, _ := rc.requests[1].Body["generationConfig"].(map[string]any); gen["responseJsonSchema"] != nil {
			t.Fatalf("retry kept the schema: %v", gen)
		}
	})
}

// A local server's prompt progress reaches the caller, and is asked for
// only from a server that answers /props (llama.cpp).
func TestPromptProgressRelayed(t *testing.T) {
	for _, llama := range []bool{true, false} {
		var asked bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				if !llama {
					http.NotFound(w, r)
					return
				}
				io.WriteString(w, `{"chat_template":"x","chat_template_caps":{"supports_tool_calls":true}}`)
				return
			}
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			asked = b["return_progress"] == true
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[],\"prompt_progress\":{\"total\":900,\"cache\":800,\"processed\":50,\"time_ms\":40}}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m", KeepLog: true}
		var progress []PromptProgress
		g.Stream(context.Background(), "/v1/chat/completions", map[string]any{"messages": []any{}}, func(d Delta) {
			if d.Progress != nil {
				progress = append(progress, *d.Progress)
			}
		})
		srv.Close()
		if asked != llama || len(progress) != 1 || progress[0].Processed != 50 {
			t.Fatalf("llama %v: asked %v progress %+v", llama, asked, progress)
		}
		if log := g.CallLog(); log[0].TTFTMS <= 0 {
			t.Fatalf("time to first token counted progress: %+v", log[0])
		}
	}
}

// Anthropic and Gemini streams that end without their stop reason are
// cut off, not complete.
func TestCloudStreamsCutOff(t *testing.T) {
	if _, err := foldGemini(strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hi\"}]}}]}\n\n"), nil); err != ErrStreamInterrupted {
		t.Fatalf("gemini: %v", err)
	}
	var a anthropicFold
	for _, ev := range []string{
		`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
	} {
		if err := a.add([]byte(ev), nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.message(); err != ErrStreamInterrupted {
		t.Fatalf("anthropic: %v", err)
	}
}
