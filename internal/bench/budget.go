package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/task"
)

// DefaultGenerationRequests matches the production supervisor's default when a
// benchmark task leaves max_generation_requests unset. Keeping the fallback
// explicit prevents a BOUNDED run from receiving an accidental unlimited
// provider while a paired RAW command receives the production default.
const DefaultGenerationRequests = 64

// GenerationBudget is the provider-side admission counter for one physical
// BOUNDED execution. The task ledger remains the durable task record; this
// counter is the executable boundary that also covers structured workflow and
// review calls, which do not pass through the native editor's MaxSteps setting.
type GenerationBudget struct {
	mu   sync.Mutex
	max  int
	used int
}

func NewGenerationBudget(max int) *GenerationBudget {
	if max <= 0 {
		max = DefaultGenerationRequests
	}
	return &GenerationBudget{max: max}
}

func (b *GenerationBudget) reserve() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.max {
		return fmt.Errorf("bench: generation request budget exhausted (%d/%d)", b.used, b.max)
	}
	b.used++
	return nil
}

func (b *GenerationBudget) Used() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

type budgetProvider struct {
	llm.Provider
	budget *GenerationBudget
}

func (p *budgetProvider) reserve(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.budget.reserve()
}

func (p *budgetProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := p.reserve(ctx); err != nil {
		return nil, err
	}
	return p.Provider.Chat(ctx, req)
}

func (p *budgetProvider) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	if err := p.reserve(ctx); err != nil {
		return nil, err
	}
	return p.Provider.ChatStructured(ctx, req, schemaOrEmpty(schema))
}

// schemaOrEmpty keeps the call site's argument order explicit while avoiding a
// mutable schema alias. json.RawMessage is already a byte slice, so this is
// only a readability guard for future implementations.
func schemaOrEmpty(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), schema...)
}

func (p *budgetProvider) Embed(ctx context.Context, req llm.EmbedRequest) (*llm.EmbedResponse, error) {
	if err := p.reserve(ctx); err != nil {
		return nil, err
	}
	return p.Provider.Embed(ctx, req)
}

func (p *budgetProvider) Infill(ctx context.Context, req llm.InfillRequest) (*llm.ChatResponse, error) {
	if err := p.reserve(ctx); err != nil {
		return nil, err
	}
	return p.Provider.Infill(ctx, req)
}

func (p *budgetProvider) ModelName() string {
	if named, ok := p.Provider.(interface{ ModelName() string }); ok {
		return named.ModelName()
	}
	return ""
}

func (p *budgetProvider) ServedModelIdentity() llm.ServedModelIdentity {
	if identified, ok := p.Provider.(llm.ServedModelIdentityProvider); ok {
		return identified.ServedModelIdentity()
	}
	return llm.ServedModelIdentity{}
}

func (p *budgetProvider) BenchmarkGenerationRequests() int {
	return p.budget.Used()
}

type frozenProvider struct {
	llm.Provider
	model ModelConfig
}

func (p *frozenProvider) applyChat(req llm.ChatRequest) llm.ChatRequest {
	if p.model.Model != "" && req.Model == "" {
		req.Model = p.model.Model
	}
	if p.model.Temperature != nil {
		v := *p.model.Temperature
		req.Temperature = &v
	}
	if p.model.TopP != nil {
		v := *p.model.TopP
		req.TopP = &v
	}
	if p.model.TopK != nil {
		v := *p.model.TopK
		req.TopK = &v
	}
	if p.model.Seed != nil {
		v := int(*p.model.Seed)
		req.Seed = &v
	}
	return req
}

func (p *frozenProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return p.Provider.Chat(ctx, p.applyChat(req))
}

func (p *frozenProvider) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	return p.Provider.ChatStructured(ctx, p.applyChat(req), schema)
}

func (p *frozenProvider) Embed(ctx context.Context, req llm.EmbedRequest) (*llm.EmbedResponse, error) {
	if p.model.Model != "" {
		req.Model = p.model.Model
	}
	return p.Provider.Embed(ctx, req)
}

func (p *frozenProvider) Infill(ctx context.Context, req llm.InfillRequest) (*llm.ChatResponse, error) {
	if p.model.Model != "" {
		req.Model = p.model.Model
	}
	return p.Provider.Infill(ctx, req)
}

func (p *frozenProvider) Capabilities() llm.Capabilities {
	caps := p.Provider.Capabilities()
	if p.model.ContextTokens > 0 {
		caps.MaxContext = p.model.ContextTokens
	}
	return caps
}

func (p *frozenProvider) ModelName() string {
	if named, ok := p.Provider.(interface{ ModelName() string }); ok {
		return named.ModelName()
	}
	return p.model.Model
}

func (p *frozenProvider) ServedModelIdentity() llm.ServedModelIdentity {
	if identified, ok := p.Provider.(llm.ServedModelIdentityProvider); ok {
		return identified.ServedModelIdentity()
	}
	return llm.ServedModelIdentity{}
}

func (p *frozenProvider) BenchmarkGenerationRequests() int {
	if counted, ok := p.Provider.(interface{ BenchmarkGenerationRequests() int }); ok {
		return counted.BenchmarkGenerationRequests()
	}
	return 0
}

// ApplyFrozenModelConfig applies the frozen model/sampling context to every
// provider used by the production task lifecycle, not just the native editor.
func ApplyFrozenModelConfig(r *task.Runner, e engine.Engine, model ModelConfig) {
	wrap := func(p llm.Provider) llm.Provider {
		if p == nil {
			return nil
		}
		return &frozenProvider{Provider: p, model: model}
	}
	if wrapper, ok := e.(interface {
		WrapProvider(func(llm.Provider) llm.Provider)
	}); ok {
		wrapper.WrapProvider(wrap)
	}
	if r != nil {
		r.WorkflowModel = wrap(r.WorkflowModel)
		r.ReviewModel = wrap(r.ReviewModel)
		if r.Critic != nil {
			r.Critic.Provider = wrap(r.Critic.Provider)
		}
	}
}

// ApplyGenerationBudget wires the counter into the coding engine and every
// optional structured role already assembled by production Supervisor. It
// returns the shared counter so the adapter can report actual admissions in the
// durable result rather than reporting a configured limit as usage.
func ApplyGenerationBudget(r *task.Runner, e engine.Engine, max int) *GenerationBudget {
	budget := NewGenerationBudget(max)
	wrap := func(p llm.Provider) llm.Provider {
		if p == nil {
			return nil
		}
		return &budgetProvider{Provider: p, budget: budget}
	}
	if wrapper, ok := e.(interface {
		WrapProvider(func(llm.Provider) llm.Provider)
	}); ok {
		wrapper.WrapProvider(wrap)
	}
	if r != nil {
		r.WorkflowModel = wrap(r.WorkflowModel)
		r.ReviewModel = wrap(r.ReviewModel)
		if r.Critic != nil {
			r.Critic.Provider = wrap(r.Critic.Provider)
		}
	}
	return budget
}
