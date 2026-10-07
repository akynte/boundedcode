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

	mu        sync.Mutex
	used      int // processed tokens: uncached prompt + completion
	generated int // completion tokens
	cached    int // prompt tokens served from the server's prompt cache
}

// Stats returns processed, generated and cached token counts.
func (g *Gateway) Stats() (processed, generated, cached int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used, g.generated, g.cached
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
		g.record(ctx, 0, 0, 0, nil, total, "transport_error")
		return http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}}, nil //nolint:nilerr // see above
	}
	var parsed struct {
		Usage struct {
			Usage
			Details *struct {
				Cached int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
		Timings *Timings `json:"timings"`
	}
	var out any
	_ = json.Unmarshal(resp, &out)
	_ = json.Unmarshal(resp, &parsed)
	cached := 0
	switch {
	case parsed.Timings != nil:
		cached = parsed.Timings.CacheN
	case parsed.Usage.Details != nil:
		cached = parsed.Usage.Details.Cached
	}
	g.mu.Lock()
	g.used += max(parsed.Usage.PromptTokens-cached, 0) + parsed.Usage.CompletionTokens
	g.generated += parsed.Usage.CompletionTokens
	g.cached += cached
	g.mu.Unlock()
	st := "ok"
	if status != http.StatusOK {
		st = fmt.Sprintf("http_%d", status)
	}
	g.record(ctx, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens, cached, parsed.Timings, total, st)
	if out == nil {
		out = map[string]any{"error": map[string]any{"message": string(resp)}}
	}
	return status, out, nil
}

func (g *Gateway) upstream() Upstream {
	if g.Upstream != nil {
		return g.Upstream
	}
	return &OpenAIUpstream{Client: g.Client, Name: ProviderLocal}
}

// Provider names the gateway's provider.
func (g *Gateway) Provider() string { return g.upstream().Provider() }

func (g *Gateway) record(ctx context.Context, prompt, completion, cached int, t *Timings, total time.Duration, status string) {
	if g.DB == nil {
		return
	}
	var pms, dms float64
	if t != nil {
		pms, dms = t.PromptMS, t.PredictedMS
	}
	_, _ = g.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO model_calls(task_id, source, model, provider, prompt_tokens, completion_tokens, cached_tokens, prompt_ms, decode_ms, total_ms, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, g.TaskID, g.Source, g.Model, g.Provider(), prompt, completion, cached, pms, dms,
		float64(total)/float64(time.Millisecond), status, store.Now())
}
