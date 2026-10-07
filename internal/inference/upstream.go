package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Upstream completes one OpenAI chat-completions request against a model
// provider. Requests and responses are in the OpenAI chat-completions shape
// whatever the provider speaks: the agent runtime and the host-side callers
// see one format, and each Upstream translates (ADR-0010).
//
// A non-2xx answer is returned as status and an OpenAI-style error body, not
// as err; err is a transport failure (no answer at all).
type Upstream interface {
	Complete(ctx context.Context, body map[string]any) (status int, resp []byte, err error)
	// Provider names the provider for metering ("local", "openai", ...).
	Provider() string
}

// Provider names.
const (
	ProviderLocal            = "local"
	ProviderOpenAI           = "openai"
	ProviderAnthropic        = "anthropic"
	ProviderGemini           = "gemini"
	ProviderOpenAICompatible = "openai-compatible"
)

// Providers lists the providers in display order.
var Providers = []string{ProviderLocal, ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderOpenAICompatible}

// IsCloud reports whether a provider is reached over the network with an
// API key.
func IsCloud(provider string) bool { return provider != "" && provider != ProviderLocal }

// errorBody is an OpenAI-style error response.
func errorBody(msg, typ string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": typ}})
	return b
}

// llamaOnly are request fields only llama.cpp understands; cloud providers
// reject unknown fields.
var llamaOnly = []string{"chat_template_kwargs", "cache_prompt", "ignore_eos", "top_k", "min_p", "repeat_penalty", "timings_per_token", "id_slot"}

// OpenAIUpstream speaks the OpenAI chat-completions API: llama.cpp's server,
// OpenAI itself, and OpenAI-compatible services.
type OpenAIUpstream struct {
	Client *Client
	Name   string // provider name for metering
	// Path is the chat-completions path under Client.BaseURL:
	// "/v1/chat/completions" for llama.cpp (BaseURL is the server root),
	// "/chat/completions" for cloud services (BaseURL ends in the API
	// version, as in OpenAI's SDKs: https://api.openai.com/v1).
	Path string
	// Cloud strips llama.cpp-only fields and adapts parameters the provider
	// rejects (see Complete).
	Cloud bool
	// Retry bounds retries of rate-limited and overloaded answers.
	Retry RetryPolicy

	mu      sync.Mutex
	dropped map[string]bool // parameters the provider rejected
}

// Provider implements Upstream.
func (u *OpenAIUpstream) Provider() string { return u.Name }

// Complete implements Upstream. For a cloud provider, llama.cpp-only fields
// are removed; a 400 naming an unsupported parameter (OpenAI's reasoning
// models reject temperature, top_p and max_tokens) drops or renames it and
// retries once, and the parameter stays dropped for this upstream's life.
func (u *OpenAIUpstream) Complete(ctx context.Context, body map[string]any) (int, []byte, error) {
	if u.Cloud {
		for _, k := range llamaOnly {
			delete(body, k)
		}
		u.applyDropped(body)
	}
	for adapted := false; ; adapted = true {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		var resp []byte
		status, err := u.Retry.Do(ctx, func() (int, error) {
			var e error
			resp, _, e = u.Client.Raw(ctx, u.path(), raw)
			return statusOf(e)
		})
		if err != nil {
			return 0, nil, err
		}
		if status == http.StatusBadRequest && u.Cloud && !adapted {
			if param := rejectedParam(resp); param != "" && u.drop(body, param) {
				continue
			}
		}
		return status, resp, nil
	}
}

func (u *OpenAIUpstream) path() string {
	if u.Path == "" {
		return "/v1/chat/completions"
	}
	return u.Path
}

// statusOf turns a client error into a status: an HTTP error is an answer,
// anything else a transport failure.
func statusOf(err error) (int, error) {
	var he *HTTPError
	switch {
	case err == nil:
		return http.StatusOK, nil
	case errors.As(err, &he):
		return he.Status, &retryHint{after: he.RetryAfter}
	default:
		return 0, err
	}
}

// retryHint carries a server's Retry-After through RetryPolicy.Do; it is not
// a failure.
type retryHint struct{ after time.Duration }

func (*retryHint) Error() string { return "http error" }

// rejectedParam returns the parameter an OpenAI-style 400 names as
// unsupported.
func rejectedParam(resp []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Param   string `json:"param"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(resp, &e) != nil {
		return ""
	}
	if e.Error.Code != "unsupported_parameter" && e.Error.Code != "unsupported_value" &&
		!strings.Contains(strings.ToLower(e.Error.Message), "unsupported") {
		return ""
	}
	return e.Error.Param
}

// adaptable are the parameters a cloud upstream may drop or rename when the
// provider rejects them.
var adaptable = map[string]bool{"temperature": true, "top_p": true, "max_tokens": true, "stop": true,
	"seed": true, "presence_penalty": true, "frequency_penalty": true, "reasoning_effort": true}

func (u *OpenAIUpstream) drop(body map[string]any, param string) bool {
	if !adaptable[param] {
		return false
	}
	if _, ok := body[param]; !ok {
		return false
	}
	u.mu.Lock()
	if u.dropped == nil {
		u.dropped = map[string]bool{}
	}
	u.dropped[param] = true
	u.mu.Unlock()
	u.applyDropped(body)
	return true
}

func (u *OpenAIUpstream) applyDropped(body map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for p := range u.dropped {
		v, ok := body[p]
		if !ok {
			continue
		}
		delete(body, p)
		// max_tokens is renamed, not dropped: newer OpenAI models take
		// max_completion_tokens instead.
		if p == "max_tokens" {
			if _, set := body["max_completion_tokens"]; !set {
				body["max_completion_tokens"] = v
			}
		}
	}
}

// RetryPolicy retries rate-limited (429) and overloaded (5xx, 529) answers
// with exponential backoff, honouring Retry-After. The zero value makes one
// attempt.
type RetryPolicy struct {
	Attempts int           // total attempts (<= 1: no retry)
	Base     time.Duration // first backoff (default 2s)
	Max      time.Duration // longest single wait (default 60s)
}

// CloudRetry is the policy for cloud providers.
var CloudRetry = RetryPolicy{Attempts: 4, Base: 2 * time.Second, Max: 60 * time.Second}

// Retryable reports whether a status is worth retrying.
func Retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status == http.StatusInternalServerError
}

// Do calls f until it returns a non-retryable status or attempts run out.
// f returns a status and either nil, a *retryHint (an HTTP answer, possibly
// with Retry-After) or a transport error, which is returned as is.
func (p RetryPolicy) Do(ctx context.Context, f func() (int, error)) (int, error) {
	base, maxWait := p.Base, p.Max
	if base <= 0 {
		base = 2 * time.Second
	}
	if maxWait <= 0 {
		maxWait = 60 * time.Second
	}
	for attempt := 1; ; attempt++ {
		status, err := f()
		var hint *retryHint
		if err != nil && !errors.As(err, &hint) {
			return 0, err
		}
		if !Retryable(status) || attempt >= p.Attempts {
			return status, nil
		}
		wait := base << (attempt - 1)
		if hint != nil && hint.after > 0 {
			wait = hint.after
		}
		wait = min(wait, maxWait)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// ModelLimits are what a provider reports about a model.
type ModelLimits struct {
	ID              string
	DisplayName     string
	ContextWindow   int // input tokens; 0 = unknown
	MaxOutputTokens int // 0 = unknown
	Thinking        bool
}

// ErrAuth is a rejected API key (401/403).
var ErrAuth = errors.New("the provider rejected the API key")

// errMessage extracts a provider's error message from an error body.
func errMessage(body []byte) string {
	var e struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch v := e.Error.(type) {
		case string:
			return v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				return m
			}
		}
	}
	return strings.TrimSpace(string(body))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Models lists the model ids an OpenAI-compatible service offers (GET
// {base}/models). The API reports no token limits.
func (u *OpenAIUpstream) Models(ctx context.Context) ([]ModelLimits, error) {
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	status, err := u.Client.Get(ctx, "/models", &page)
	if err := httpGetErr(u.Name, status, err); err != nil {
		return nil, err
	}
	out := make([]ModelLimits, 0, len(page.Data))
	for _, m := range page.Data {
		out = append(out, ModelLimits{ID: m.ID})
	}
	return out, nil
}
