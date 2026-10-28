package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Delta is one piece of a streamed answer. ToolCall marks the start or a
// piece of a tool call (no text): it times the first token of an answer
// that is only tool calls, and is not passed on to Gateway.Stream callers.
// Progress reports prompt processing before the first token (llama.cpp's
// prompt_progress). Reset tells the caller that the text streamed so far
// is void: the stream broke off and the request is being sent again.
type Delta struct {
	Text      string
	Reasoning string
	ToolCall  bool
	Progress  *PromptProgress
	Reset     bool
}

// PromptProgress is how far the server is through the prompt: of Total
// tokens, Cache came from its prompt cache and Processed have been
// evaluated so far, in TimeMS.
type PromptProgress struct {
	Total     int     `json:"total"`
	Cache     int     `json:"cache"`
	Processed int     `json:"processed"`
	TimeMS    float64 `json:"time_ms"`
}

// ErrStreamInterrupted is a streamed answer that ended before it was
// complete: no finish reason and no end marker. Its partial text and tool
// calls must not be taken for an answer.
var ErrStreamInterrupted = errors.New("the model's answer stream ended before the answer was complete")

// Streamer is an Upstream that can stream a completion. The callback
// receives text as it is generated; the returned body is the complete
// answer in the non-streaming chat-completions shape (one message, the
// usage and, for llama.cpp, the timings), so metering and parsing are the
// same as for Complete.
type Streamer interface {
	Stream(ctx context.Context, body map[string]any, onDelta func(Delta)) (status int, resp []byte, err error)
}

// Stream implements Streamer for OpenAI-compatible servers: the request is
// sent with stream=true and stream_options.include_usage, the server-sent
// events are folded into one response.
func (u *OpenAIUpstream) Stream(ctx context.Context, body map[string]any, onDelta func(Delta)) (int, []byte, error) {
	u.prepare(ctx, body)
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}
	if u.progress(ctx) {
		body["return_progress"] = true
	}
	for adapted := false; ; adapted = true {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		var resp *http.Response
		var last error
		status, err := u.Retry.Do(ctx, func() (int, error) {
			resp, last = u.Client.RawStream(ctx, u.path(), raw) //nolint:bodyclose // closed after foldSSE below
			return statusOf(last)
		})
		if err != nil {
			return 0, nil, err
		}
		if resp == nil {
			// A non-2xx answer: an OpenAI-style error body.
			var he *HTTPError
			if errors.As(last, &he) {
				if status == http.StatusBadRequest && !adapted {
					if param := rejectedParam([]byte(he.Body)); param != "" && u.drop(body, param) {
						continue
					}
				}
				return status, []byte(he.Body), nil
			}
			return status, errorBody("no answer", "upstream_error"), nil
		}
		out, failed, err := foldSSE(resp.Body, onDelta)
		resp.Body.Close()
		if err != nil {
			return 0, nil, err
		}
		if failed != 0 {
			return failed, out, nil
		}
		u.remember(out)
		return http.StatusOK, out, nil
	}
}

// RawStream POSTs body and returns the response for the caller to read.
// A non-2xx answer is read, closed and returned as an HTTPError.
func (c *Client) RawStream(ctx context.Context, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.addHeaders(req)
	// The client's timeout bounds the whole request; a streamed answer
	// takes as long as the generation, so the context is the bound.
	hc := *c.HTTP
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		msg := string(b)
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		return nil, &HTTPError{Status: resp.StatusCode, Body: msg, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	return resp, nil
}

// sseChunk is one streamed chat-completions chunk.
type sseChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage          *json.RawMessage `json:"usage"`
	Timings        *json.RawMessage `json:"timings"`
	Error          *json.RawMessage `json:"error"`
	PromptProgress *PromptProgress  `json:"prompt_progress"`
}

// foldSSE reads server-sent events until [DONE] (or the end of the body)
// and returns the answer in the non-streaming shape. An error event ends
// the stream as an error answer: its body and an HTTP status (failed).
// A stream that ends with neither [DONE] nor a finish reason is
// ErrStreamInterrupted.
func foldSSE(r io.Reader, onDelta func(Delta)) (out []byte, failed int, err error) {
	type toolCall struct {
		ID, Type, Name string
		Args           strings.Builder
	}
	var (
		content, reasoning strings.Builder
		calls              []*toolCall
		finish             string
		usage, timings     json.RawMessage
		model              string
	)
	var errBody []byte
	done, err := sseData(r, func(data []byte) (bool, error) {
		var ch sseChunk
		if err := json.Unmarshal(data, &ch); err != nil {
			return false, fmt.Errorf("decode stream chunk: %w", err)
		}
		if ch.Error != nil {
			errBody = []byte(`{"error":` + string(*ch.Error) + `}`)
			return false, nil
		}
		if ch.PromptProgress != nil && onDelta != nil {
			onDelta(Delta{Progress: ch.PromptProgress})
		}
		if ch.Model != "" {
			model = ch.Model
		}
		if ch.Usage != nil {
			usage = *ch.Usage
		}
		if ch.Timings != nil {
			timings = *ch.Timings
		}
		for _, c := range ch.Choices {
			if c.Index != 0 {
				continue
			}
			d := Delta{Text: c.Delta.Content, Reasoning: c.Delta.ReasoningContent}
			if d.Reasoning == "" {
				d.Reasoning = c.Delta.Reasoning
			}
			content.WriteString(d.Text)
			reasoning.WriteString(d.Reasoning)
			if (d.Text != "" || d.Reasoning != "") && onDelta != nil {
				onDelta(d)
			}
			if len(c.Delta.ToolCalls) > 0 && onDelta != nil {
				onDelta(Delta{ToolCall: true})
			}
			for _, tc := range c.Delta.ToolCalls {
				for len(calls) <= tc.Index {
					calls = append(calls, &toolCall{})
				}
				cur := calls[tc.Index]
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Type != "" {
					cur.Type = tc.Type
				}
				if tc.Function.Name != "" {
					cur.Name += tc.Function.Name
				}
				cur.Args.WriteString(tc.Function.Arguments)
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				finish = *c.FinishReason
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if errBody != nil {
		return errBody, errorStatus(errBody), nil
	}
	if !done && finish == "" {
		return nil, 0, ErrStreamInterrupted
	}
	msg := map[string]any{"role": "assistant", "content": content.String()}
	if reasoning.Len() > 0 {
		msg["reasoning_content"] = reasoning.String()
	}
	if len(calls) > 0 {
		var tcs []map[string]any
		for _, c := range calls {
			typ := c.Type
			if typ == "" {
				typ = "function"
			}
			tcs = append(tcs, map[string]any{"id": c.ID, "type": typ, "function": map[string]any{"name": c.Name, "arguments": c.Args.String()}})
		}
		msg["tool_calls"] = tcs
	}
	if finish == "" {
		finish = "stop"
	}
	whole := map[string]any{
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
	}
	if len(usage) > 0 {
		whole["usage"] = usage
	}
	if len(timings) > 0 {
		whole["timings"] = timings
	}
	b, err := json.Marshal(whole)
	return b, 0, err
}

// errorStatus is the HTTP status an error event stands for: its numeric
// code when it has one in the 4xx-5xx range, else 500.
func errorStatus(body []byte) int {
	var e struct {
		Error struct {
			Code any `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		if c, ok := e.Error.Code.(float64); ok && c >= 400 && c < 600 {
			return int(c)
		}
	}
	return http.StatusInternalServerError
}

// sseData calls fn with the data of each server-sent event until fn
// returns false, the stream sends [DONE], or the body ends. Events whose
// data spans several lines are joined as the specification says. done
// reports a [DONE] marker.
func sseData(r io.Reader, fn func(data []byte) (more bool, err error)) (done bool, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var data []string
	flush := func() (bool, error) {
		if len(data) == 0 {
			return true, nil
		}
		d := strings.Join(data, "\n")
		data = data[:0]
		if strings.TrimSpace(d) == "[DONE]" {
			done = true
			return false, nil
		}
		return fn([]byte(d))
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			more, err := flush()
			if err != nil || !more {
				return done, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if d, ok := strings.CutPrefix(line, "data:"); ok {
			// A server that omits the blank line between events: a
			// complete JSON value already buffered is an event of its own.
			if len(data) > 0 && json.Valid([]byte(strings.Join(data, "\n"))) {
				more, err := flush()
				if err != nil || !more {
					return done, err
				}
			}
			data = append(data, strings.TrimPrefix(d, " "))
		}
	}
	if err := sc.Err(); err != nil {
		return done, fmt.Errorf("%w: read: %w", ErrStreamInterrupted, err)
	}
	_, err = flush()
	return done, err
}

// Stream forwards a chat-completions request and streams the answer's
// text through onDelta when the provider can; otherwise it completes the
// request as Forward does and onDelta is never called. The result is the
// whole answer, metered like any other request.
func (g *Gateway) Stream(ctx context.Context, path string, body map[string]any, onDelta func(Delta)) (int, any, error) {
	if path != "/v1/chat/completions" && path != "/chat/completions" {
		return http.StatusNotFound, map[string]any{"error": map[string]any{"message": "unsupported path " + path}}, nil
	}
	s, ok := g.upstream().(Streamer)
	if !ok {
		return g.Forward(ctx, path, body)
	}
	g.mu.Lock()
	over := g.MaxTokens > 0 && g.used >= g.MaxTokens
	g.mu.Unlock()
	if over {
		return http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": ErrBudgetExhausted.Error(), "type": "budget"}}, ErrBudgetExhausted
	}
	body["model"] = g.Model
	delete(body, "stream")
	delete(body, "stream_options")
	n, changed := g.history("", body)
	start := time.Now()
	var first time.Duration
	streamed := false
	relay := func(d Delta) {
		if d.Progress != nil {
			if onDelta != nil {
				onDelta(d)
			}
			return
		}
		if first == 0 {
			first = max(time.Since(start), time.Nanosecond)
		}
		if d.Text != "" || d.Reasoning != "" {
			streamed = true
			if onDelta != nil {
				onDelta(d)
			}
		}
	}
	status, resp, err := s.Stream(ctx, copyBody(body), relay)
	retried := 0
	if err != nil && ctx.Err() == nil && (errors.Is(err, ErrStreamInterrupted) || connectionFailure(err)) {
		// The answer broke off (the server closed the connection, or the
		// stream ended without its end): what was streamed is void, and
		// the request is sent once more.
		retried = 1
		if streamed && onDelta != nil {
			onDelta(Delta{Reset: true})
		}
		first, streamed = 0, false
		status, resp, err = s.Stream(ctx, body, relay)
	}
	total := time.Since(start)
	if err != nil {
		g.record(ctx, CallStat{TotalMS: ms(total), TTFTMS: ms(first), Streamed: true, Status: "transport_error", Messages: n, Changed: changed, Retried: retried})
		return http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}}, nil //nolint:nilerr // transport errors become a 502 for the caller
	}
	status, resp = normalize(status, resp)
	return g.account(ctx, status, resp, CallStat{TotalMS: ms(total), TTFTMS: ms(first), Streamed: true, Messages: n, Changed: changed, Retried: retried}), g.decode(resp), nil
}

// copyBody copies a request's top level, so a first attempt's changes
// (stream flags, dropped parameters) do not leak into the second.
func copyBody(body map[string]any) map[string]any {
	c := make(map[string]any, len(body))
	for k, v := range body {
		c[k] = v
	}
	return c
}
