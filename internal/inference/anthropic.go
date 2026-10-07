package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicUpstream translates OpenAI chat-completions requests to the
// Anthropic Messages API (through the official Go SDK) and the answers back.
//
// Thinking blocks are kept in Replay and sent back verbatim with the turn
// that produced them. The agent's history condensation rewrites earlier
// turns, which invalidates later thinking blocks, so requests ask the API
// to drop such blocks instead of failing (thinking.block_binding,
// beta thinking-binding-controls-2026-08-01).
type AnthropicUpstream struct {
	APIKey  string
	BaseURL string // "" = the SDK's production endpoint
	Model   string
	// Effort is output_config.effort ("" = the model's default).
	Effort string
	// Timeout bounds one request attempt.
	Timeout time.Duration
	Replay  ReplayStore
	// HTTP overrides the transport (tests).
	HTTP *http.Client

	once   sync.Once
	client anthropic.Client

	limitsOnce sync.Once
	limits     ModelLimits
	adaptive   bool
	limitsErr  error
}

// Beta features the translation uses.
const (
	betaThinkingBinding = "thinking-binding-controls-2026-08-01"
	betaFallback        = "server-side-fallback-2026-07-01"
)

// fallbackModels accept server-side refusal fallbacks ("fallbacks":
// "default"): a request a safety classifier declines is re-served by a
// fallback model in the same call instead of ending the agent's turn.
var fallbackModels = map[string]bool{"claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true}

// Provider implements Upstream.
func (u *AnthropicUpstream) Provider() string { return ProviderAnthropic }

func (u *AnthropicUpstream) init() {
	u.once.Do(func() {
		// Only the key the user configured: no ANTHROPIC_API_KEY, profile or
		// federation pickup from the environment.
		opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(u.APIKey), option.WithMaxRetries(3)}
		if u.BaseURL != "" {
			opts = append(opts, option.WithBaseURL(u.BaseURL))
		}
		if u.HTTP != nil {
			opts = append(opts, option.WithHTTPClient(u.HTTP))
		}
		u.client = anthropic.NewClient(opts...)
	})
}

// Limits returns the model's context window, output cap and whether it
// takes adaptive thinking, from the Models API (cached).
func (u *AnthropicUpstream) Limits(ctx context.Context) (ModelLimits, error) {
	u.init()
	u.limitsOnce.Do(func() {
		m, err := u.client.Models.Get(ctx, u.Model, anthropic.ModelGetParams{})
		if err != nil {
			u.limitsErr = classifyAnthropic(err)
			return
		}
		u.limits = ModelLimits{ID: m.ID, DisplayName: m.DisplayName, ContextWindow: int(m.MaxInputTokens),
			MaxOutputTokens: int(m.MaxTokens), Thinking: m.Capabilities.Thinking.Supported}
		u.adaptive = m.Capabilities.Thinking.Types.Adaptive.Supported
	})
	return u.limits, u.limitsErr
}

// Models lists the models the key can use.
func (u *AnthropicUpstream) Models(ctx context.Context) ([]ModelLimits, error) {
	u.init()
	var out []ModelLimits
	iter := u.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for iter.Next() {
		m := iter.Current()
		out = append(out, ModelLimits{ID: m.ID, DisplayName: m.DisplayName, ContextWindow: int(m.MaxInputTokens),
			MaxOutputTokens: int(m.MaxTokens), Thinking: m.Capabilities.Thinking.Supported})
	}
	if err := iter.Err(); err != nil {
		return nil, classifyAnthropic(err)
	}
	return out, nil
}

// Complete implements Upstream.
func (u *AnthropicUpstream) Complete(ctx context.Context, body map[string]any) (int, []byte, error) {
	u.init()
	req, err := parseOARequest(body)
	if err != nil {
		return http.StatusBadRequest, errorBody(err.Error(), "invalid_request_error"), nil
	}
	_, _ = u.Limits(ctx) // unknown limits only mean no thinking config is sent
	payload, betas := u.translate(req)
	status, resp, err := u.send(ctx, payload, betas)
	if err == nil && status == http.StatusBadRequest && bindingRejected(resp) {
		// A model that rejected replayed thinking blocks (no binding
		// control on this request): drop them all, once.
		stripThinking(payload)
		status, resp, err = u.send(ctx, payload, betas)
	}
	if err != nil || status != http.StatusOK {
		return status, resp, err
	}
	res, native, perr := u.parse(resp)
	if perr != nil {
		return http.StatusBadGateway, errorBody(perr.Error(), "upstream_error"), nil
	}
	if u.Replay != nil {
		u.Replay.Put(res.replayKey(ProviderAnthropic), native)
	}
	return http.StatusOK, res.marshal(), nil
}

func (u *AnthropicUpstream) send(ctx context.Context, payload map[string]any, betas []string) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	var out []byte
	opts := []option.RequestOption{option.WithRequestBody("application/json", raw), option.WithResponseBodyInto(&out),
		option.WithRequestTimeout(timeout)}
	for _, b := range betas {
		opts = append(opts, option.WithHeaderAdd("anthropic-beta", b))
	}
	_, err = u.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{}, opts...)
	var apierr *anthropic.Error
	switch {
	case err == nil:
		return http.StatusOK, out, nil
	case errors.As(err, &apierr):
		return apierr.StatusCode, errorBody(anthropicMessage(apierr), string(apierr.Type())), nil
	default:
		return 0, nil, err
	}
}

func anthropicMessage(e *anthropic.Error) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(e.RawJSON()), &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	return e.Error()
}

// classifyAnthropic turns an SDK error into ErrAuth where it is one.
func classifyAnthropic(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) && (apierr.StatusCode == http.StatusUnauthorized || apierr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("anthropic: %w (%s)", ErrAuth, trunc(anthropicMessage(apierr), 300))
	}
	return err
}

// bindingRejected reports a 400 caused by replayed thinking blocks that no
// longer match the conversation.
func bindingRejected(resp []byte) bool {
	m := errMessage(resp)
	return strings.Contains(m, "thinking` block") || strings.Contains(m, "bound to a different conversation")
}

func stripThinking(payload map[string]any) {
	msgs, _ := payload["messages"].([]map[string]any)
	for _, m := range msgs {
		blocks, _ := m["content"].([]any)
		keep := blocks[:0]
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok && (bm["type"] == "thinking" || bm["type"] == "redacted_thinking") {
				continue
			}
			keep = append(keep, b)
		}
		m["content"] = keep
	}
	if th, ok := payload["thinking"].(map[string]any); ok {
		delete(th, "block_binding")
	}
}

// translate builds the Messages API body and the betas it needs.
func (u *AnthropicUpstream) translate(r oaRequest) (map[string]any, []string) {
	var system []string
	var msgs []map[string]any
	add := func(role string, blocks []any) {
		if len(blocks) == 0 {
			return
		}
		if n := len(msgs); n > 0 && msgs[n-1]["role"] == role {
			msgs[n-1]["content"] = append(msgs[n-1]["content"].([]any), blocks...)
			return
		}
		msgs = append(msgs, map[string]any{"role": role, "content": blocks})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case "system", "developer":
			if len(msgs) == 0 {
				system = append(system, m.text())
			} else {
				// A later system message (a condensation summary) is a user
				// note: the top-level system prompt must stay stable.
				add("user", []any{map[string]any{"type": "text", "text": "[system note] " + m.text()}})
			}
		case "user":
			add("user", anthropicBlocks(m.parts()))
		case "assistant":
			add("assistant", u.assistantBlocks(m))
		case "tool":
			res := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID}
			if parts := m.parts(); hasImage(parts) {
				res["content"] = anthropicBlocks(parts)
			} else {
				res["content"] = m.text()
			}
			add("user", []any{res})
		}
	}
	if len(msgs) == 0 || msgs[0]["role"] != "user" {
		msgs = append([]map[string]any{{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Continue."}}}}, msgs...)
	}

	payload := map[string]any{"model": u.Model, "messages": msgs,
		// Automatic prompt caching: the agent resends a growing history.
		"cache_control": map[string]any{"type": "ephemeral"}}
	if len(system) > 0 {
		payload["system"] = strings.Join(system, "\n\n")
	}
	var betas []string
	maxTokens := r.maxOutput()
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	if u.adaptive {
		payload["thinking"] = map[string]any{"type": "adaptive",
			"block_binding": map[string]any{"prefix_mismatch_behavior": "drop_block"}}
		betas = append(betas, betaThinkingBinding)
		// Thinking counts against max_tokens; leave it room.
		maxTokens = max(maxTokens, 16000)
	}
	if u.limits.MaxOutputTokens > 0 {
		maxTokens = min(maxTokens, u.limits.MaxOutputTokens)
	}
	payload["max_tokens"] = maxTokens
	if u.Effort != "" {
		payload["output_config"] = map[string]any{"effort": u.Effort}
	}
	if stops := r.stops(); len(stops) > 0 {
		payload["stop_sequences"] = stops
	}
	// Sampling parameters are not sent: current models reject them.
	if len(r.Tools) > 0 {
		tools := make([]any, 0, len(r.Tools))
		for _, t := range r.Tools {
			schema := t.Function.Parameters
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{"name": t.Function.Name, "description": t.Function.Description, "input_schema": schema})
		}
		payload["tools"] = tools
		// Forced tool use is rejected by current models; "required" becomes
		// "auto" (the agent's prompt already asks for tool calls).
		choice := map[string]any{"type": "auto"}
		if r.toolChoice() == "none" {
			choice = map[string]any{"type": "none"}
		} else if r.ParallelToolCalls != nil && !*r.ParallelToolCalls {
			choice["disable_parallel_tool_use"] = true
		}
		payload["tool_choice"] = choice
	}
	if fallbackModels[u.Model] && u.BaseURL == "" {
		payload["fallbacks"] = "default"
		betas = append(betas, betaFallback)
	}
	return payload, betas
}

func hasImage(parts []oaPart) bool {
	for _, p := range parts {
		if p.Type == "image" {
			return true
		}
	}
	return false
}

func anthropicBlocks(parts []oaPart) []any {
	var out []any
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": p.Text})
		case "image":
			out = append(out, map[string]any{"type": "image",
				"source": map[string]any{"type": "base64", "media_type": p.MimeType, "data": p.Data}})
		}
	}
	return out
}

// assistantBlocks returns the provider-native turn if it was stored, else a
// rebuild from the OpenAI fields (without thinking).
func (u *AnthropicUpstream) assistantBlocks(m oaMessage) []any {
	if u.Replay != nil {
		if native, ok := u.Replay.Get(replayKeyOf(ProviderAnthropic, m)); ok {
			var blocks []any
			if json.Unmarshal(native, &blocks) == nil && len(blocks) > 0 {
				return blocks
			}
		}
	}
	var out []any
	if t := m.text(); t != "" {
		out = append(out, map[string]any{"type": "text", "text": t})
	}
	for _, tc := range m.ToolCalls {
		out = append(out, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": toolArgs(tc.Function.Arguments)})
	}
	return out
}

// parse turns a Messages API response into an OpenAI result and the
// native content to replay.
func (u *AnthropicUpstream) parse(resp []byte) (oaResult, json.RawMessage, error) {
	var m struct {
		ID         string          `json:"id"`
		Model      string          `json:"model"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
		Details    *struct {
			Category    string `json:"category"`
			Explanation string `json:"explanation"`
		} `json:"stop_details"`
		Usage struct {
			Input       int `json:"input_tokens"`
			Output      int `json:"output_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp, &m); err != nil {
		return oaResult{}, nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return oaResult{}, nil, fmt.Errorf("decode anthropic content: %w", err)
	}
	res := oaResult{ID: m.ID, Model: m.Model, Prompt: m.Usage.Input + m.Usage.CacheCreate + m.Usage.CacheRead,
		Cached: m.Usage.CacheRead, Completion: m.Usage.Output}
	var text strings.Builder
	for _, b := range blocks {
		switch b["type"] {
		case "text":
			s, _ := b["text"].(string)
			text.WriteString(s)
		case "tool_use":
			var tc oaToolCall
			tc.ID, _ = b["id"].(string)
			tc.Type = "function"
			tc.Function.Name, _ = b["name"].(string)
			args, _ := json.Marshal(b["input"])
			tc.Function.Arguments = string(args)
			res.ToolCalls = append(res.ToolCalls, tc)
		}
	}
	res.Text = text.String()
	switch m.StopReason {
	case "tool_use":
		res.FinishReason = "tool_calls"
	case "max_tokens", "model_context_window_exceeded":
		res.FinishReason = "length"
	case "refusal":
		res.FinishReason = "content_filter"
		if res.Text == "" {
			res.Text = "The model declined this request."
			if m.Details != nil && m.Details.Category != "" {
				res.Text += " (" + m.Details.Category + ")"
			}
		}
	default:
		res.FinishReason = "stop"
	}
	if len(res.ToolCalls) > 0 {
		res.FinishReason = "tool_calls"
	}
	return res, m.Content, nil
}
