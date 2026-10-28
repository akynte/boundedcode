package inference

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// GeminiUpstream translates OpenAI chat-completions requests to the Gemini
// API's generateContent (v1beta REST) and the answers back.
//
// Gemini attaches opaque thought signatures to the parts of a model turn
// and expects them back unchanged (a missing one ends a turn with
// MISSING_THOUGHT_SIGNATURE). Each model turn is stored in Replay and sent
// back verbatim.
type GeminiUpstream struct {
	APIKey  string
	BaseURL string // "" = https://generativelanguage.googleapis.com
	Model   string // e.g. "gemini-…" (without the "models/" prefix)
	// Effort maps to thinkingConfig.thinkingLevel ("low", "medium",
	// "high"; "" = the model's default).
	Effort  string
	Timeout time.Duration
	Replay  ReplayStore
	Retry   RetryPolicy
	// HTTP overrides the transport (tests).
	HTTP *http.Client

	once   sync.Once
	client *Client
}

// DefaultGeminiURL is the Gemini API endpoint.
const DefaultGeminiURL = "https://generativelanguage.googleapis.com"

// Provider implements Upstream.
func (u *GeminiUpstream) Provider() string { return ProviderGemini }

func (u *GeminiUpstream) init() {
	u.once.Do(func() {
		base := u.BaseURL
		if base == "" {
			base = DefaultGeminiURL
		}
		timeout := u.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Minute
		}
		u.client = NewClient(base, timeout)
		if u.HTTP != nil {
			u.client.HTTP = u.HTTP
		}
		// The key goes in a header, not the ?key= query parameter, so it
		// never appears in a URL.
		u.client.Header = http.Header{"X-Goog-Api-Key": {u.APIKey}}
	})
}

func (u *GeminiUpstream) modelPath() string {
	return "/v1beta/models/" + url.PathEscape(strings.TrimPrefix(u.Model, "models/"))
}

// geminiTurn is a stored model turn: its parts exactly as returned, and the
// OpenAI tool-call ids given to its function calls mapped to Gemini's own
// call ids ("" when Gemini sent none).
type geminiTurn struct {
	Parts []any             `json:"parts"`
	IDs   map[string]string `json:"ids,omitempty"`
}

// Complete implements Upstream.
func (u *GeminiUpstream) Complete(ctx context.Context, body map[string]any) (int, []byte, error) {
	u.init()
	req, err := parseOARequest(body)
	if err != nil {
		return http.StatusBadRequest, errorBody(err.Error(), "invalid_request_error"), nil
	}
	payload := u.translate(req)
	var resp []byte
	var status int
	for formatted := req.schema() != nil; ; formatted = false {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		status, err = u.Retry.Do(ctx, func() (int, error) {
			var e error
			resp, _, e = u.client.Raw(ctx, u.modelPath()+":generateContent", raw)
			return statusOf(e)
		})
		if err != nil {
			return 0, nil, err
		}
		if formatted && status == http.StatusBadRequest && formatRejected(resp) {
			// A model without JSON-schema output: the prompt asks for the
			// JSON on its own.
			withoutFormat(payload)
			continue
		}
		break
	}
	if status != http.StatusOK {
		return status, errorBody(errMessage(resp), "upstream_error"), nil
	}
	return u.answer(resp)
}

// Stream implements Streamer: streamGenerateContent with server-sent
// events, each a partial generateContent response. The parts are joined
// (consecutive text of the same kind merged, function calls and thought
// signatures kept as sent) into one response that is then handled exactly
// as Complete handles the whole answer.
func (u *GeminiUpstream) Stream(ctx context.Context, body map[string]any, onDelta func(Delta)) (int, []byte, error) {
	u.init()
	req, err := parseOARequest(body)
	if err != nil {
		return http.StatusBadRequest, errorBody(err.Error(), "invalid_request_error"), nil
	}
	raw, err := json.Marshal(u.translate(req))
	if err != nil {
		return 0, nil, err
	}
	var resp *http.Response
	var last error
	status, err := u.Retry.Do(ctx, func() (int, error) {
		resp, last = u.client.RawStream(ctx, u.modelPath()+":streamGenerateContent?alt=sse", raw) //nolint:bodyclose // closed after the fold below
		return statusOf(last)
	})
	if err != nil {
		return 0, nil, err
	}
	if resp == nil {
		var he *HTTPError
		if errors.As(last, &he) {
			return status, errorBody(errMessage([]byte(he.Body)), "upstream_error"), nil
		}
		return status, errorBody("no answer", "upstream_error"), nil
	}
	whole, err := foldGemini(resp.Body, onDelta)
	resp.Body.Close()
	if err != nil {
		return 0, nil, err
	}
	return u.answer(whole)
}

// answer turns a whole generateContent response into the OpenAI shape and
// stores the model turn for replay.
func (u *GeminiUpstream) answer(resp []byte) (int, []byte, error) {
	res, turn, err := u.parse(resp)
	if err != nil {
		return http.StatusBadGateway, errorBody(err.Error(), "upstream_error"), nil
	}
	if u.Replay != nil && len(turn.Parts) > 0 {
		if b, err := json.Marshal(turn); err == nil {
			u.Replay.Put(res.replayKey(ProviderGemini), b)
		}
	}
	return http.StatusOK, res.marshal(), nil
}

// foldGemini joins streamed generateContent chunks into one response.
func foldGemini(r io.Reader, onDelta func(Delta)) ([]byte, error) {
	out := map[string]any{}
	var parts []map[string]any
	var finish, finishMsg any
	_, err := sseData(r, func(data []byte) (bool, error) {
		var ch struct {
			Candidates []struct {
				Content struct {
					Parts []map[string]any `json:"parts"`
				} `json:"content"`
				FinishReason  string `json:"finishReason"`
				FinishMessage string `json:"finishMessage"`
			} `json:"candidates"`
			PromptFeedback json.RawMessage `json:"promptFeedback"`
			Usage          json.RawMessage `json:"usageMetadata"`
			ModelVersion   string          `json:"modelVersion"`
			ResponseID     string          `json:"responseId"`
			Error          json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(data, &ch); err != nil {
			return false, fmt.Errorf("decode gemini stream chunk: %w", err)
		}
		if len(ch.Error) > 0 {
			return false, fmt.Errorf("gemini stream error: %s", trunc(string(ch.Error), 500))
		}
		if len(ch.PromptFeedback) > 0 {
			out["promptFeedback"] = ch.PromptFeedback
		}
		if len(ch.Usage) > 0 {
			out["usageMetadata"] = ch.Usage // cumulative: the last one counts
		}
		if ch.ModelVersion != "" {
			out["modelVersion"] = ch.ModelVersion
		}
		if ch.ResponseID != "" {
			out["responseId"] = ch.ResponseID
		}
		if len(ch.Candidates) == 0 {
			return true, nil
		}
		c := ch.Candidates[0]
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
		if c.FinishMessage != "" {
			finishMsg = c.FinishMessage
		}
		for _, p := range c.Content.Parts {
			text, isText := p["text"].(string)
			thought, _ := p["thought"].(bool)
			if isText && onDelta != nil && text != "" {
				if thought {
					onDelta(Delta{Reasoning: text})
				} else {
					onDelta(Delta{Text: text})
				}
			}
			if _, call := p["functionCall"]; call && onDelta != nil {
				onDelta(Delta{ToolCall: true})
			}
			parts = mergeGeminiPart(parts, p)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if finish == nil {
		// Gemini sends no end marker: the last chunk carries the finish
		// reason, so a stream without one was cut off.
		return nil, ErrStreamInterrupted
	}
	if parts != nil || finish != nil {
		cand := map[string]any{"content": map[string]any{"role": "model", "parts": parts}}
		if finish != nil {
			cand["finishReason"] = finish
		}
		if finishMsg != nil {
			cand["finishMessage"] = finishMsg
		}
		out["candidates"] = []any{cand}
	}
	return json.Marshal(out)
}

// mergeGeminiPart appends p, joining it to the previous part when both
// are plain text of the same kind (answer or thought) and at most one of
// them carries a thought signature, which the joined part keeps.
func mergeGeminiPart(parts []map[string]any, p map[string]any) []map[string]any {
	if len(parts) > 0 {
		prev := parts[len(parts)-1]
		pt, ok1 := prev["text"].(string)
		nt, ok2 := p["text"].(string)
		pthought, _ := prev["thought"].(bool)
		nthought, _ := p["thought"].(bool)
		_, psig := prev["thoughtSignature"]
		_, nsig := p["thoughtSignature"]
		if ok1 && ok2 && pthought == nthought && (!psig || !nsig) && plainText(prev) && plainText(p) {
			prev["text"] = pt + nt
			if nsig {
				prev["thoughtSignature"] = p["thoughtSignature"]
			}
			return parts
		}
	}
	cp := make(map[string]any, len(p))
	for k, v := range p {
		cp[k] = v
	}
	return append(parts, cp)
}

// plainText reports a part that is only text (with its thought flag and
// signature).
func plainText(p map[string]any) bool {
	for k := range p {
		if k != "text" && k != "thought" && k != "thoughtSignature" {
			return false
		}
	}
	return true
}

// translate builds the generateContent body.
func (u *GeminiUpstream) translate(r oaRequest) map[string]any {
	var system []string
	var contents []map[string]any
	add := func(role string, parts []any) {
		if len(parts) == 0 {
			return
		}
		if n := len(contents); n > 0 && contents[n-1]["role"] == role {
			contents[n-1]["parts"] = append(contents[n-1]["parts"].([]any), parts...)
			return
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	// Function responses must name their function and carry Gemini's call
	// id; both come from the assistant turn that made the call.
	names := map[string]string{}
	nativeIDs := map[string]string{}
	for _, m := range r.Messages {
		switch m.Role {
		case "system", "developer":
			if len(contents) == 0 {
				system = append(system, m.text())
			} else {
				add("user", []any{map[string]any{"text": "[system note] " + m.text()}})
			}
		case "user":
			add("user", geminiParts(m.parts()))
		case "assistant":
			for _, tc := range m.ToolCalls {
				names[tc.ID] = tc.Function.Name
				nativeIDs[tc.ID] = tc.ID
			}
			add("model", u.modelParts(m, nativeIDs))
		case "tool":
			fr := map[string]any{"name": names[m.ToolCallID], "response": map[string]any{"output": m.text()}}
			if id := nativeIDs[m.ToolCallID]; id != "" {
				fr["id"] = id
			}
			if fr["name"] == "" {
				fr["name"] = m.Name
			}
			add("user", []any{map[string]any{"functionResponse": fr}})
		}
	}
	if len(contents) == 0 || contents[0]["role"] != "user" {
		contents = append([]map[string]any{{"role": "user", "parts": []any{map[string]any{"text": "Continue."}}}}, contents...)
	}
	payload := map[string]any{"contents": contents}
	if len(system) > 0 {
		payload["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": strings.Join(system, "\n\n")}}}
	}
	gen := map[string]any{}
	if n := r.maxOutput(); n > 0 {
		gen["maxOutputTokens"] = n
	}
	if r.Temperature != nil {
		gen["temperature"] = *r.Temperature
	}
	if r.TopP != nil {
		gen["topP"] = *r.TopP
	}
	if stops := r.stops(); len(stops) > 0 {
		gen["stopSequences"] = stops
	}
	if u.Effort != "" {
		gen["thinkingConfig"] = map[string]any{"thinkingLevel": strings.ToUpper(u.Effort)}
	}
	if schema := r.schema(); schema != nil {
		gen["responseMimeType"] = "application/json"
		gen["responseJsonSchema"] = cloneJSON(schema)
	}
	if len(gen) > 0 {
		payload["generationConfig"] = gen
	}
	if len(r.Tools) > 0 {
		decls := make([]any, 0, len(r.Tools))
		for _, t := range r.Tools {
			d := map[string]any{"name": t.Function.Name, "description": t.Function.Description}
			if d["description"] == "" {
				d["description"] = t.Function.Name
			}
			if t.Function.Parameters != nil {
				// Full JSON Schema is accepted here (parameters takes only
				// the OpenAPI subset).
				d["parametersJsonSchema"] = t.Function.Parameters
			}
			decls = append(decls, d)
		}
		payload["tools"] = []any{map[string]any{"functionDeclarations": decls}}
		mode := "AUTO"
		switch r.toolChoice() {
		case "none":
			mode = "NONE"
		case "required":
			mode = "ANY"
		}
		payload["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}
	}
	return payload
}

func geminiParts(parts []oaPart) []any {
	var out []any
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, map[string]any{"text": p.Text})
		case "image":
			out = append(out, map[string]any{"inlineData": map[string]any{"mimeType": p.MimeType, "data": p.Data}})
		}
	}
	return out
}

// modelParts returns a stored model turn verbatim (with its thought
// signatures) or a rebuild from the OpenAI fields.
func (u *GeminiUpstream) modelParts(m oaMessage, nativeIDs map[string]string) []any {
	if u.Replay != nil {
		if native, ok := u.Replay.Get(replayKeyOf(ProviderGemini, m)); ok {
			var turn geminiTurn
			if json.Unmarshal(native, &turn) == nil && len(turn.Parts) > 0 {
				for oa, id := range turn.IDs {
					nativeIDs[oa] = id
				}
				return turn.Parts
			}
		}
	}
	var out []any
	if t := m.text(); t != "" {
		out = append(out, map[string]any{"text": t})
	}
	for _, tc := range m.ToolCalls {
		out = append(out, map[string]any{"functionCall": map[string]any{"id": tc.ID, "name": tc.Function.Name, "args": toolArgs(tc.Function.Arguments)}})
	}
	return out
}

func (u *GeminiUpstream) parse(resp []byte) (oaResult, geminiTurn, error) {
	var g struct {
		Candidates []struct {
			Content struct {
				Parts []map[string]any `json:"parts"`
			} `json:"content"`
			FinishReason  string `json:"finishReason"`
			FinishMessage string `json:"finishMessage"`
		} `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Usage struct {
			Prompt     int `json:"promptTokenCount"`
			ToolPrompt int `json:"toolUsePromptTokenCount"`
			Cached     int `json:"cachedContentTokenCount"`
			Candidates int `json:"candidatesTokenCount"`
			Thoughts   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
		ModelVersion string `json:"modelVersion"`
		ResponseID   string `json:"responseId"`
	}
	if err := json.Unmarshal(resp, &g); err != nil {
		return oaResult{}, geminiTurn{}, fmt.Errorf("decode gemini response: %w", err)
	}
	res := oaResult{ID: g.ResponseID, Model: g.ModelVersion, Prompt: g.Usage.Prompt + g.Usage.ToolPrompt,
		Cached: g.Usage.Cached, Completion: g.Usage.Candidates + g.Usage.Thoughts, Reasoning: g.Usage.Thoughts}
	if res.ID == "" {
		res.ID = "gemini-" + randomID()
	}
	if len(g.Candidates) == 0 {
		res.FinishReason = "content_filter"
		res.Text = "The model returned no answer."
		if g.PromptFeedback != nil && g.PromptFeedback.BlockReason != "" {
			res.Text = "The request was blocked (" + g.PromptFeedback.BlockReason + ")."
		}
		return res, geminiTurn{}, nil
	}
	c := g.Candidates[0]
	turn := geminiTurn{IDs: map[string]string{}}
	var text strings.Builder
	for _, p := range c.Content.Parts {
		turn.Parts = append(turn.Parts, p)
		if fc, ok := p["functionCall"].(map[string]any); ok {
			native, _ := fc["id"].(string)
			id := native
			if id == "" {
				id = "call_" + randomID()
			}
			turn.IDs[id] = native
			var tc oaToolCall
			tc.ID, tc.Type = id, "function"
			tc.Function.Name, _ = fc["name"].(string)
			args, _ := json.Marshal(fc["args"])
			if string(args) == "null" {
				args = []byte("{}")
			}
			tc.Function.Arguments = string(args)
			res.ToolCalls = append(res.ToolCalls, tc)
			continue
		}
		if thought, _ := p["thought"].(bool); thought {
			continue // thought summaries are not answer text
		}
		if t, ok := p["text"].(string); ok {
			text.WriteString(t)
		}
	}
	res.Text = text.String()
	switch c.FinishReason {
	case "MAX_TOKENS":
		res.FinishReason = "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT":
		res.FinishReason = "content_filter"
		if res.Text == "" {
			res.Text = "The model declined this request (" + c.FinishReason + ")."
		}
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "MISSING_THOUGHT_SIGNATURE":
		res.FinishReason = "stop"
		if res.Text == "" {
			res.Text = "The model's answer could not be used (" + c.FinishReason + "); try again."
		}
	default:
		res.FinishReason = "stop"
	}
	if len(res.ToolCalls) > 0 {
		res.FinishReason = "tool_calls"
	}
	return res, turn, nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Limits returns the model's token limits from the Gemini models API.
func (u *GeminiUpstream) Limits(ctx context.Context) (ModelLimits, error) {
	u.init()
	var m geminiModel
	status, err := u.client.Get(ctx, u.modelPath(), &m)
	if err := httpGetErr("gemini", status, err); err != nil {
		return ModelLimits{}, err
	}
	return m.limits(), nil
}

// Models lists the models that support generateContent.
func (u *GeminiUpstream) Models(ctx context.Context) ([]ModelLimits, error) {
	u.init()
	var out []ModelLimits
	token := ""
	for {
		path := "/v1beta/models?pageSize=1000"
		if token != "" {
			path += "&pageToken=" + url.QueryEscape(token)
		}
		var page struct {
			Models []geminiModel `json:"models"`
			Next   string        `json:"nextPageToken"`
		}
		status, err := u.client.Get(ctx, path, &page)
		if err := httpGetErr("gemini", status, err); err != nil {
			return nil, err
		}
		for _, m := range page.Models {
			for _, meth := range m.Methods {
				if meth == "generateContent" {
					out = append(out, m.limits())
					break
				}
			}
		}
		if page.Next == "" {
			return out, nil
		}
		token = page.Next
	}
}

type geminiModel struct {
	Name     string   `json:"name"`
	Display  string   `json:"displayName"`
	Input    int      `json:"inputTokenLimit"`
	Output   int      `json:"outputTokenLimit"`
	Thinking bool     `json:"thinking"`
	Methods  []string `json:"supportedGenerationMethods"`
}

func (m geminiModel) limits() ModelLimits {
	return ModelLimits{ID: strings.TrimPrefix(m.Name, "models/"), DisplayName: m.Display, ContextWindow: m.Input,
		MaxOutputTokens: m.Output, Thinking: m.Thinking}
}

// httpGetErr turns a Client.Get result into an error (ErrAuth for 401/403).
func httpGetErr(provider string, status int, err error) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%s: http %d: %w", provider, status, ErrAuth)
	case err != nil:
		return err
	case status/100 != 2:
		return fmt.Errorf("%s: http %d", provider, status)
	}
	return nil
}
