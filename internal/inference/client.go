package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible chat-completions server and extracts
// llama.cpp timing data when present.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Header is added to every request (an API key for a cloud provider).
	Header http.Header
}

// NewClient returns a client with the given per-request timeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: timeout}}
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the subset of the chat-completions request we use directly.
// Agent traffic is forwarded as raw JSON and does not go through this type.
type ChatRequest struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	TopK        int       `json:"top_k,omitempty"`
	Seed        *int      `json:"seed,omitempty"`
	CachePrompt *bool     `json:"cache_prompt,omitempty"`
	// IgnoreEOS is a llama.cpp extension used by benchmarks to force exactly
	// MaxTokens of decode.
	IgnoreEOS bool `json:"ignore_eos,omitempty"`
	// ChatTemplateKwargs is forwarded to llama.cpp's Jinja templates
	// (e.g. {"enable_thinking": false}).
	ChatTemplateKwargs map[string]bool `json:"chat_template_kwargs,omitempty"`
}

// Usage is OpenAI-style token accounting.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Timings is llama-server's per-request timing block.
type Timings struct {
	CacheN             int     `json:"cache_n"`
	PromptN            int     `json:"prompt_n"`
	PromptMS           float64 `json:"prompt_ms"`
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedN         int     `json:"predicted_n"`
	PredictedMS        float64 `json:"predicted_ms"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
}

// ChatResponse is the parsed response.
type ChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage   Usage    `json:"usage"`
	Timings *Timings `json:"timings,omitempty"`
	// WallMS is measured client-side.
	WallMS float64 `json:"-"`
}

// Text returns the first choice's content.
func (r *ChatResponse) Text() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

// Chat sends a non-streaming chat completion.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw, wall, err := c.Raw(ctx, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	out.WallMS = float64(wall) / float64(time.Millisecond)
	return &out, nil
}

// HTTPError is a non-2xx response.
type HTTPError struct {
	Status int
	Body   string
	// RetryAfter is the server's Retry-After delay, if it sent one.
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Body) }

// Raw POSTs body to path and returns the response body and wall time.
func (c *Client) Raw(ctx context.Context, path string, body []byte) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addHeaders(req)
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, time.Since(start), err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	wall := time.Since(start)
	if err != nil {
		return nil, wall, err
	}
	if resp.StatusCode/100 != 2 {
		msg := string(b)
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		return b, wall, &HTTPError{Status: resp.StatusCode, Body: msg, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	return b, wall, nil
}

func (c *Client) addHeaders(req *http.Request) {
	for k, vs := range c.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}

// retryAfter parses a Retry-After header (seconds or an HTTP date).
func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

// Get fetches path and decodes JSON into v (v may be nil).
func (c *Client) Get(ctx context.Context, path string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return 0, err
	}
	c.addHeaders(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if v != nil && len(b) > 0 {
		if err := json.Unmarshal(b, v); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

// Reachable reports whether an HTTP server answers at the client's base URL.
// Any HTTP response counts: an external OpenAI-compatible server need not
// implement /health, but a refused or timed-out connection fails.
func (c *Client) Reachable(ctx context.Context) error {
	if code, err := c.Get(ctx, "/health", nil); err != nil && code == 0 {
		return err
	}
	return nil
}

// Healthy reports whether GET /health returns 200. llama-server returns 503
// while the model is loading.
func (c *Client) Healthy(ctx context.Context) (bool, error) {
	code, err := c.Get(ctx, "/health", nil)
	if err != nil {
		return false, err
	}
	return code == http.StatusOK, nil
}

// Tokenize returns the token count of text using llama-server's /tokenize.
func (c *Client) Tokenize(ctx context.Context, text string) (int, error) {
	body, err := json.Marshal(map[string]any{"content": text})
	if err != nil {
		return 0, err
	}
	raw, _, err := c.Raw(ctx, "/tokenize", body)
	if err != nil {
		return 0, err
	}
	var out struct {
		Tokens []json.RawMessage `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, err
	}
	return len(out.Tokens), nil
}

// ErrNotReady is returned while a server is still loading.
var ErrNotReady = errors.New("inference server not ready")
