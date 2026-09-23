package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/llm"
)

// ErrOracleLeak is returned instead of sending a request that carries a
// hidden check's canary to a model.
var ErrOracleLeak = errors.New("hidden acceptance content was about to be sent to a model")

// leakGuard refuses to send a model any request containing a canary from the
// hidden acceptance suite.
//
// Redaction is how hidden checks are kept out of model context; this is how a
// failure of redaction, or a path nobody thought of, is caught before the
// content leaves the process rather than inferred afterwards. It sits on the
// provider because every model call the runner makes goes through one, so
// there is one place to check instead of one per phase.
//
// It never includes the canary in its error: the error travels back through
// code that may summarize it.
type leakGuard struct {
	llm.Provider
	canaries map[string]string // canary -> check ID
	order    []string
	onLeak   func(checkID string)
}

func newLeakGuard(p llm.Provider, canaries map[string]string, onLeak func(string)) llm.Provider {
	if p == nil || len(canaries) == 0 {
		return p
	}
	if _, already := p.(*leakGuard); already {
		return p
	}
	order := make([]string, 0, len(canaries))
	for c := range canaries {
		order = append(order, c)
	}
	sort.Strings(order)
	return &leakGuard{Provider: p, canaries: canaries, order: order, onLeak: onLeak}
}

func (g *leakGuard) check(texts ...string) error {
	for _, text := range texts {
		for _, canary := range g.order {
			if strings.Contains(text, canary) {
				id := g.canaries[canary]
				if g.onLeak != nil {
					g.onLeak(id)
				}
				return fmt.Errorf("%w: content of hidden check %s, request to %s refused",
					ErrOracleLeak, id, g.Provider.Name())
			}
		}
	}
	return nil
}

func (g *leakGuard) checkChat(req llm.ChatRequest) error {
	texts := make([]string, 0, len(req.Messages)*2)
	for _, m := range req.Messages {
		texts = append(texts, m.Content)
		for _, call := range m.ToolCalls {
			if body, err := json.Marshal(call); err == nil {
				texts = append(texts, string(body))
			}
		}
	}
	return g.check(texts...)
}

func (g *leakGuard) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := g.checkChat(req); err != nil {
		return nil, err
	}
	return g.Provider.Chat(ctx, req)
}

func (g *leakGuard) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	if err := g.checkChat(req); err != nil {
		return nil, err
	}
	return g.Provider.ChatStructured(ctx, req, schema)
}

func (g *leakGuard) Infill(ctx context.Context, req llm.InfillRequest) (*llm.ChatResponse, error) {
	body, err := json.Marshal(req)
	if err == nil {
		if err := g.check(string(body)); err != nil {
			return nil, err
		}
	}
	return g.Provider.Infill(ctx, req)
}

func (g *leakGuard) Embed(ctx context.Context, req llm.EmbedRequest) (*llm.EmbedResponse, error) {
	body, err := json.Marshal(req)
	if err == nil {
		if err := g.check(string(body)); err != nil {
			return nil, err
		}
	}
	return g.Provider.Embed(ctx, req)
}

// guardModels wraps every model the runner will call with the leak guard. It
// is idempotent, so a runner reused for several tasks is wrapped once.
func (r *Runner) guardModels() {
	canaries := r.Oracle.Canaries()
	if len(canaries) == 0 {
		return
	}
	onLeak := func(id string) {
		r.leaked = id
		r.logf("oracle: a request carrying content of hidden check %s was refused before it reached a model", id)
	}
	r.WorkflowModel = newLeakGuard(r.WorkflowModel, canaries, onLeak)
	r.ReviewModel = newLeakGuard(r.ReviewModel, canaries, onLeak)
	if r.Critic != nil {
		r.Critic.Provider = newLeakGuard(r.Critic.Provider, canaries, onLeak)
	}
	if w, ok := r.Engine.(interface {
		WrapProvider(func(llm.Provider) llm.Provider)
	}); ok {
		w.WrapProvider(func(p llm.Provider) llm.Provider { return newLeakGuard(p, canaries, onLeak) })
	}
}
