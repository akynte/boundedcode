package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sse renders events as a server-sent-events body ("" event = data only).
func sse(events ...[2]string) string {
	var b strings.Builder
	for _, e := range events {
		if e[0] != "" {
			fmt.Fprintf(&b, "event: %s\n", e[0])
		}
		fmt.Fprintf(&b, "data: %s\n\n", e[1])
	}
	return b.String()
}

var sseHeader = http.Header{"Content-Type": {"text/event-stream"}}

// collect gathers streamed text and reasoning.
type collect struct{ text, reasoning strings.Builder }

func (c *collect) on(d Delta) {
	c.text.WriteString(d.Text)
	c.reasoning.WriteString(d.Reasoning)
}

func TestAnthropicStream(t *testing.T) {
	stream := sse(
		[2]string{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":20,"cache_read_input_tokens":70}}}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`},
		[2]string{"ping", `{"type":"ping"}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me "}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think."}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-1"}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Running "}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"tests."}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"terminal","input":{}}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"go test ./...\"}"}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":2}`},
		[2]string{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":50}}`},
		[2]string{"message_stop", `{"type":"message_stop"}`},
	)
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet {
			return 200, `{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","max_input_tokens":1000000,"max_tokens":128000,
				"capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true}}}}}`, nil
		}
		if r.Body["stream"] == true {
			return 200, stream, sseHeader
		}
		return 200, `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn",
			"content":[{"type":"text","text":"Done."}],"usage":{"input_tokens":5,"output_tokens":2}}`, nil
	}}
	srv := rc.server(t)
	replay := &MemoryReplay{}
	u := &AnthropicUpstream{APIKey: "sk-ant-test", BaseURL: srv.URL, Model: "claude-opus-5-5", Replay: replay, Timeout: 5 * time.Second}
	var got collect
	status, resp, err := u.Stream(context.Background(), agentRequest(), got.on)
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	if got.text.String() != "Running tests." || got.reasoning.String() != "Let me think." {
		t.Fatalf("deltas: text %q reasoning %q", got.text.String(), got.reasoning.String())
	}
	req := rc.last(t, "/v1/messages")
	if req.Body["stream"] != true || req.Header.Get("X-Api-Key") != "sk-ant-test" || !strings.Contains(strings.Join(req.Header.Values("Anthropic-Beta"), ","), betaThinkingBinding) {
		t.Fatalf("request: stream %v headers %v", req.Body["stream"], req.Header)
	}
	// The folded answer is what a whole answer would have been.
	msg, finish := message(t, resp)
	tcs := msg["tool_calls"].([]any)
	if finish != "tool_calls" || msg["content"] != "Running tests." || len(tcs) != 1 {
		t.Fatalf("message %v finish %s", msg, finish)
	}
	if args := tcs[0].(map[string]any)["function"].(map[string]any)["arguments"]; args != `{"command":"go test ./..."}` {
		t.Fatalf("arguments = %v", args)
	}
	usage := decode(t, resp)["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 100 || usage["completion_tokens"].(float64) != 50 || usage["prompt_tokens_details"].(map[string]any)["cached_tokens"].(float64) != 70 {
		t.Fatalf("usage = %v", usage)
	}
	// The thinking block replays with its signature on the next turn.
	assistant := map[string]any{"role": "assistant", "content": "Running tests.", "tool_calls": tcs}
	toolRes := map[string]any{"role": "tool", "tool_call_id": "toolu_1", "content": "ok"}
	if _, _, err := u.Complete(context.Background(), agentRequest(assistant, toolRes)); err != nil {
		t.Fatal(err)
	}
	asst := rc.last(t, "/v1/messages").Body["messages"].([]any)[1].(map[string]any)["content"].([]any)
	first := asst[0].(map[string]any)
	if first["type"] != "thinking" || first["signature"] != "SIG-1" || first["thinking"] != "Let me think." {
		t.Fatalf("thinking block not replayed: %v", asst)
	}
	if tu := asst[2].(map[string]any); tu["type"] != "tool_use" || tu["input"].(map[string]any)["command"] != "go test ./..." {
		t.Fatalf("tool_use block: %v", asst)
	}
}

func TestAnthropicStreamErrors(t *testing.T) {
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet {
			return 404, `{"type":"error","error":{"type":"not_found_error","message":"no"}}`, nil
		}
		return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`, nil
	}}
	srv := rc.server(t)
	u := &AnthropicUpstream{APIKey: "k", BaseURL: srv.URL, Model: "claude-x", Timeout: 5 * time.Second}
	status, resp, err := u.Stream(context.Background(), agentRequest(), nil)
	if err != nil || status != 400 || !strings.Contains(string(resp), "prompt is too long") {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	// An error event in the middle of the stream fails the request.
	mid := sse([2]string{"message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`},
		[2]string{"error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`})
	rc.answer = func(n int, r recorded) (int, string, http.Header) {
		if r.Method == http.MethodGet {
			return 404, `{}`, nil
		}
		return 200, mid, sseHeader
	}
	if status, resp, err := u.Stream(context.Background(), agentRequest(), nil); err == nil && status == 200 {
		t.Fatalf("mid-stream error accepted: %d %s", status, resp)
	}
}

func TestGeminiStream(t *testing.T) {
	stream := sse(
		[2]string{"", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Plan: ","thought":true}]}}],"modelVersion":"gemini-x","responseId":"r1"}`},
		[2]string{"", `{"candidates":[{"content":{"role":"model","parts":[{"text":"run tests","thought":true}]}}]}`},
		[2]string{"", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Run"}]}}]}`},
		[2]string{"", `{"candidates":[{"content":{"role":"model","parts":[{"text":"ning tests."}]}}]}`},
		[2]string{"", `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"terminal","args":{"command":"go test ./..."}},"thoughtSignature":"GSIG"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":200,"cachedContentTokenCount":150,"candidatesTokenCount":20,"thoughtsTokenCount":30}}`},
	)
	rc := &recorder{answer: func(n int, r recorded) (int, string, http.Header) {
		if strings.HasSuffix(r.Path, ":streamGenerateContent") {
			return 200, stream, sseHeader
		}
		return 200, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`, nil
	}}
	srv := rc.server(t)
	replay := &MemoryReplay{}
	u := &GeminiUpstream{APIKey: "AIza-test", BaseURL: srv.URL, Model: "gemini-x", Replay: replay, Timeout: 5 * time.Second}
	var got collect
	status, resp, err := u.Stream(context.Background(), agentRequest(), got.on)
	if err != nil || status != 200 {
		t.Fatalf("status %d err %v %s", status, err, resp)
	}
	req := rc.last(t, ":streamGenerateContent")
	if req.Query != "alt=sse" || req.Header.Get("X-Goog-Api-Key") != "AIza-test" {
		t.Fatalf("query %q key %q", req.Query, req.Header.Get("X-Goog-Api-Key"))
	}
	if got.text.String() != "Running tests." || got.reasoning.String() != "Plan: run tests" {
		t.Fatalf("deltas: text %q reasoning %q", got.text.String(), got.reasoning.String())
	}
	msg, finish := message(t, resp)
	if finish != "tool_calls" || msg["content"] != "Running tests." || len(msg["tool_calls"].([]any)) != 1 {
		t.Fatalf("message %v finish %s", msg, finish)
	}
	usage := decode(t, resp)["usage"].(map[string]any)
	if usage["completion_tokens"].(float64) != 50 || usage["prompt_tokens_details"].(map[string]any)["cached_tokens"].(float64) != 150 {
		t.Fatalf("usage = %v", usage)
	}
	// The stored turn has the merged parts and the signature.
	tcs := msg["tool_calls"].([]any)
	assistant := map[string]any{"role": "assistant", "content": "Running tests.", "tool_calls": tcs}
	toolRes := map[string]any{"role": "tool", "tool_call_id": tcs[0].(map[string]any)["id"], "content": "PASS"}
	if _, _, err := u.Complete(context.Background(), agentRequest(assistant, toolRes)); err != nil {
		t.Fatal(err)
	}
	parts := rc.last(t, ":generateContent").Body["contents"].([]any)[1].(map[string]any)["parts"].([]any)
	if len(parts) != 3 || parts[0].(map[string]any)["text"] != "Plan: run tests" || parts[1].(map[string]any)["text"] != "Running tests." || parts[2].(map[string]any)["thoughtSignature"] != "GSIG" {
		t.Fatalf("replayed parts: %v", parts)
	}
}

// A signature that arrives on an empty final text part stays with the text.
func TestGeminiFoldKeepsTrailingSignature(t *testing.T) {
	body := sse(
		[2]string{"", `{"candidates":[{"content":{"parts":[{"text":"Do"}]}}]}`},
		[2]string{"", `{"candidates":[{"content":{"parts":[{"text":"ne."}]}}]}`},
		[2]string{"", `{"candidates":[{"content":{"parts":[{"text":"","thoughtSignature":"S2"}]},"finishReason":"STOP"}]}`},
	)
	out, err := foldGemini(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Candidates []struct {
			Content struct {
				Parts []map[string]any `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(out, &g); err != nil {
		t.Fatal(err)
	}
	p := g.Candidates[0].Content.Parts
	if len(p) != 1 || p[0]["text"] != "Done." || p[0]["thoughtSignature"] != "S2" || g.Candidates[0].FinishReason != "STOP" {
		t.Fatalf("parts %v", g)
	}
	if _, err := foldGemini(strings.NewReader(sse([2]string{"", `{"error":{"code":500}}`})), nil); err == nil {
		t.Fatal("an error chunk must fail the fold")
	}
}

// Events are split on blank lines, multi-line data is joined, and a
// server that leaves out the blank line still yields one event per line.
func TestSSEDataFraming(t *testing.T) {
	var got []string
	body := ": comment\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\nevent: x\ndata: {\"b\":2}\ndata: {\"c\":3}\n\ndata: [DONE]\n\ndata: {\"d\":4}\n\n"
	done, err := sseData(strings.NewReader(body), func(d []byte) (bool, error) {
		got = append(got, string(d))
		return true, nil
	})
	if err != nil || !done {
		t.Fatal(done, err)
	}
	if strings.Join(got, "|") != "{\"a\":\n1}|{\"b\":2}|{\"c\":3}" {
		t.Fatalf("events %q", got)
	}
}
