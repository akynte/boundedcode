package inference

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/store"
)

// Gateway forwards agent LLM requests to the model provider, meters them
// into model_calls, and enforces a token budget. It is the only path from a
// sandboxed agent to a model (ADR-0004); for a cloud provider it is also the
// only holder of the API key (ADR-0010).
type Gateway struct {
	// Upstream is the provider. Client is the legacy form: a local
	// OpenAI-compatible server (used when Upstream is nil).
	Upstream Upstream
	Client   *Client
	Model    string // model sent upstream regardless of what the agent asked for
	DB       *sql.DB
	TaskID   string
	Source   string
	// MaxTokens bounds prompt+completion tokens across the gateway's life
	// (0 = unlimited).
	MaxTokens int
	// KeepLog keeps every call's figures in memory (CallLog), for
	// benchmarks; the ledger gets them either way.
	KeepLog bool

	mu        sync.Mutex
	used      int // processed tokens: uncached prompt + completion
	generated int // completion tokens
	cached    int // prompt tokens served from the server's prompt cache
	calls     int // requests forwarded
	peak      int // the largest prompt of one request, cached tokens included
	log       []CallStat
}

// CallStat is one model call as the gateway measured it.
type CallStat struct {
	Prompt     int `json:"prompt_tokens"` // all prompt tokens, cached ones included
	Cached     int `json:"cached_tokens"`
	Completion int `json:"completion_tokens"` // output tokens, reasoning included
	// Reasoning is the reasoning (thinking) part of Completion, and
	// ReasoningSource where the count came from: "provider" when the
	// response reported it. A caller may count ReasoningText itself and
	// set "tokenizer" (exact) or "estimate".
	Reasoning       int    `json:"reasoning_tokens,omitempty"`
	ReasoningSource string `json:"reasoning_source,omitempty"`
	ReasoningText   string `json:"-"`
	// TTFTMS is the time from the request to the first streamed delta
	// (text, reasoning or a tool call); 0 when the call was not streamed.
	TTFTMS float64 `json:"ttft_ms,omitempty"`
	// PrefillMS and DecodeMS are the server's own prompt-processing and
	// generation times (llama.cpp timings); 0 when it reports none.
	PrefillMS float64 `json:"prefill_ms,omitempty"`
	DecodeMS  float64 `json:"decode_ms,omitempty"`
	TotalMS   float64 `json:"total_ms"` // request to complete answer
	Status    string  `json:"status"`
	Streamed  bool    `json:"streamed,omitempty"`
}

// CallLog returns the calls so far (KeepLog only).
func (g *Gateway) CallLog() []CallStat {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]CallStat(nil), g.log...)
}

// Stats returns processed, generated and cached token counts.
func (g *Gateway) Stats() (processed, generated, cached int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used, g.generated, g.cached
}

// Calls is how many requests were forwarded.
func (g *Gateway) Calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// PeakPrompt is the largest prompt one request sent, in tokens (cached
// tokens included): how much history the model was given at most.
func (g *Gateway) PeakPrompt() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

// ErrBudgetExhausted is returned when the token budget is spent.
var ErrBudgetExhausted = errors.New("task token budget exhausted")

// Used returns processed tokens (uncached prompt + completion) so far; this
// is what the token budget limits, since cached prefix tokens cost almost no
// compute.
func (g *Gateway) Used() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used
}

// Forward sends an OpenAI-compatible request body to path and returns the
// HTTP status and decoded response body.
func (g *Gateway) Forward(ctx context.Context, path string, body map[string]any) (int, any, error) {
	if path != "/v1/chat/completions" && path != "/chat/completions" {
		return http.StatusNotFound, map[string]any{"error": map[string]any{"message": "unsupported path " + path}}, nil
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
	start := time.Now()
	status, resp, err := g.upstream().Complete(ctx, body)
	total := time.Since(start)
	if err != nil {
		// Transport errors become a 502 for the agent, not a Go error.
		g.record(ctx, CallStat{TotalMS: ms(total), Status: "transport_error"})
		return http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}}, nil //nolint:nilerr // see above
	}
	return g.account(ctx, status, resp, CallStat{TotalMS: ms(total)}), g.decode(resp), nil
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// account meters an answer (tokens, cache hits, timings) and records the
// call; it returns the status unchanged. cs carries what the caller
// measured (total time, time to first token).
func (g *Gateway) account(ctx context.Context, status int, resp []byte, cs CallStat) int {
	var parsed struct {
		Usage struct {
			Usage
			Details *struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			Completion *struct {
				Reasoning int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
		Timings *Timings `json:"timings"`
		Choices []struct {
			Message struct {
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(resp, &parsed)
	cached := 0
	switch {
	case parsed.Timings != nil:
		cached = parsed.Timings.CacheN
	case parsed.Usage.Details != nil:
		cached = parsed.Usage.Details.Cached
	}
	g.mu.Lock()
	g.calls++
	g.used += max(parsed.Usage.PromptTokens-cached, 0) + parsed.Usage.CompletionTokens
	g.generated += parsed.Usage.CompletionTokens
	g.cached += cached
	g.peak = max(g.peak, parsed.Usage.PromptTokens)
	g.mu.Unlock()
	cs.Status = "ok"
	if status != http.StatusOK {
		cs.Status = fmt.Sprintf("http_%d", status)
	}
	cs.Prompt, cs.Completion, cs.Cached = parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, cached
	if parsed.Timings != nil {
		cs.PrefillMS, cs.DecodeMS = parsed.Timings.PromptMS, parsed.Timings.PredictedMS
	}
	if c := parsed.Usage.Completion; c != nil && c.Reasoning > 0 {
		cs.Reasoning, cs.ReasoningSource = c.Reasoning, "provider"
	} else if len(parsed.Choices) > 0 && g.KeepLog {
		m := parsed.Choices[0].Message
		cs.ReasoningText = m.ReasoningContent
		if cs.ReasoningText == "" {
			cs.ReasoningText = m.Reasoning
		}
	}
	g.record(ctx, cs)
	return status
}

// decode returns the answer as generic JSON, or an error body when it is
// not JSON.
func (g *Gateway) decode(resp []byte) any {
	var out any
	if json.Unmarshal(resp, &out) != nil || out == nil {
		return map[string]any{"error": map[string]any{"message": string(resp)}}
	}
	return out
}

func (g *Gateway) upstream() Upstream {
	if g.Upstream != nil {
		return g.Upstream
	}
	return &OpenAIUpstream{Client: g.Client, Name: ProviderLocal}
}

// Provider names the gateway's provider.
func (g *Gateway) Provider() string { return g.upstream().Provider() }

func (g *Gateway) record(ctx context.Context, cs CallStat) {
	if g.KeepLog {
		g.mu.Lock()
		g.log = append(g.log, cs)
		g.mu.Unlock()
	}
	if g.DB == nil {
		return
	}
	_, _ = g.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO model_calls(task_id, source, model, provider, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, prompt_ms, decode_ms, total_ms, ttft_ms, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, g.TaskID, g.Source, g.Model, g.Provider(), cs.Prompt, cs.Completion, cs.Cached, cs.Reasoning,
		cs.PrefillMS, cs.DecodeMS, cs.TotalMS, cs.TTFTMS, cs.Status, store.Now())
}
