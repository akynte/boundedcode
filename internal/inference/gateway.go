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

// Gateway forwards agent LLM requests to the inference server, meters them
// into model_calls, and enforces a token budget. It is the only path from a
// sandboxed agent to a model (ADR-0004).
type Gateway struct {
	Client *Client
	Model  string // alias sent upstream regardless of what the agent asked for
	DB     *sql.DB
	TaskID string
	Source string
	// MaxTokens bounds prompt+completion tokens across the gateway's life
	// (0 = unlimited).
	MaxTokens int

	mu   sync.Mutex
	used int
}

// ErrBudgetExhausted is returned when the token budget is spent.
var ErrBudgetExhausted = errors.New("local token budget exhausted")

// Used returns tokens consumed so far.
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
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	start := time.Now()
	resp, _, err := g.Client.Raw(ctx, "/v1/chat/completions", raw)
	total := time.Since(start)
	status := http.StatusOK
	var he *HTTPError
	switch {
	case errors.As(err, &he):
		status = he.Status
	case err != nil:
		g.record(ctx, 0, 0, 0, nil, total, "transport_error")
		return http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}}, nil
	}
	var parsed struct {
		Usage   Usage    `json:"usage"`
		Timings *Timings `json:"timings"`
	}
	var out any
	_ = json.Unmarshal(resp, &out)
	_ = json.Unmarshal(resp, &parsed)
	g.mu.Lock()
	g.used += parsed.Usage.PromptTokens + parsed.Usage.CompletionTokens
	g.mu.Unlock()
	cached := 0
	if parsed.Timings != nil {
		cached = parsed.Timings.CacheN
	}
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

func (g *Gateway) record(ctx context.Context, prompt, completion, cached int, t *Timings, total time.Duration, status string) {
	if g.DB == nil {
		return
	}
	var pms, dms float64
	if t != nil {
		pms, dms = t.PromptMS, t.PredictedMS
	}
	_, _ = g.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO model_calls(task_id, source, model, prompt_tokens, completion_tokens, cached_tokens, prompt_ms, decode_ms, total_ms, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, g.TaskID, g.Source, g.Model, prompt, completion, cached, pms, dms,
		float64(total)/float64(time.Millisecond), status, store.Now())
}
