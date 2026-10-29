package benchmark

import (
	"context"
	"sort"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/inference"
)

// CallSummary attributes a task's model time and tokens, from the
// gateway's call log.
type CallSummary struct {
	Calls int `json:"calls"`
	// InferenceSeconds is the sum of the calls' times as the gateway
	// measured them (request to complete answer); PrefillSeconds and
	// DecodeSeconds are the server's own split (llama.cpp only).
	InferenceSeconds float64 `json:"inference_seconds"`
	PrefillSeconds   float64 `json:"prefill_seconds,omitempty"`
	DecodeSeconds    float64 `json:"decode_seconds,omitempty"`
	// TTFT over streamed calls: median and largest, in milliseconds.
	TTFTMedianMS float64 `json:"ttft_median_ms,omitempty"`
	TTFTMaxMS    float64 `json:"ttft_max_ms,omitempty"`
	// ReasoningTokens and how they were counted: "provider" (reported),
	// "tokenizer" (the model's own, exact), "estimate" (bytes), or a mix.
	ReasoningTokens int    `json:"reasoning_tokens"`
	ReasoningCount  string `json:"reasoning_count,omitempty"`
	// Invalidations are calls whose prompt reused less of the previous
	// call's prompt from the server's cache than it shared with it (more
	// than invalidationSlack tokens recomputed); AfterRewrite is how many
	// of them are expected: after a rewrite of the history (results set
	// aside, a summary, the condenser) or a change of caller (planner and
	// agent). RecomputedTokens is what they cost.
	Invalidations    int `json:"cache_invalidations"`
	AfterRewrite     int `json:"cache_invalidations_after_rewrite"`
	RecomputedTokens int `json:"recomputed_prompt_tokens"`
	// EvaluatedTokens are the prompt tokens the server processed (not
	// served from its cache), over all calls.
	EvaluatedTokens int `json:"evaluated_prompt_tokens,omitempty"`
	Failed          int `json:"failed_calls,omitempty"`
	PromptTokens    int `json:"prompt_tokens,omitempty"` // all calls' prompts, cached included (bounded reports)
	PeakCall        int `json:"peak_call"`               // index of the largest prompt
	// GeneratedTokens is the calls' output, for decode speed with
	// DecodeSeconds. Drafted and Accepted are speculative decoding's
	// drafted and kept tokens.
	GeneratedTokens int `json:"generated_tokens,omitempty"`
	Drafted         int `json:"drafted_tokens,omitempty"`
	Accepted        int `json:"accepted_tokens,omitempty"`
	// Roles are the calls by routing role (inference.WithRoute).
	Roles map[string]RoleCalls `json:"roles,omitempty"`
}

// RoleCalls are the model calls of one routing role.
type RoleCalls struct {
	Calls     int     `json:"calls"`
	NoThink   int     `json:"no_thinking,omitempty"` // calls routed with thinking off
	Generated int     `json:"generated_tokens"`
	Reasoning int     `json:"reasoning_tokens,omitempty"`
	Seconds   float64 `json:"seconds"`
}

// Add adds b's calls to r's.
func (r RoleCalls) Add(b RoleCalls) RoleCalls {
	return RoleCalls{Calls: r.Calls + b.Calls, NoThink: r.NoThink + b.NoThink, Generated: r.Generated + b.Generated,
		Reasoning: r.Reasoning + b.Reasoning, Seconds: r.Seconds + b.Seconds}
}

// invalidationSlack: the end of the previous prompt (the generation
// prompt) is not part of the next one, so a few tokens always differ.
const invalidationSlack = 32

// countReasoning fills in reasoning tokens the provider did not report,
// with the model's tokenizer when there is one, otherwise by estimate.
func countReasoning(ctx context.Context, log []inference.CallStat, tokenize func(context.Context, string) (int, error)) {
	for i := range log {
		c := &log[i]
		if c.ReasoningSource != "" || c.ReasoningText == "" {
			continue
		}
		if tokenize != nil {
			if n, err := tokenize(ctx, c.ReasoningText); err == nil {
				c.Reasoning, c.ReasoningSource = n, "tokenizer"
				continue
			}
		}
		c.Reasoning, c.ReasoningSource = contextplan.EstimateTokens(c.ReasoningText), "estimate"
	}
}

// summarizeCalls attributes the calls; rewrites marks the call indexes
// that follow a rewrite of the history.
func summarizeCalls(log []inference.CallStat, rewrites map[int]bool) CallSummary {
	s := CallSummary{Calls: len(log)}
	var ttft []float64
	sources := map[string]bool{}
	for i, c := range log {
		s.InferenceSeconds += c.TotalMS / 1000
		s.PrefillSeconds += c.PrefillMS / 1000
		s.DecodeSeconds += c.DecodeMS / 1000
		s.EvaluatedTokens += c.Evaluated
		s.GeneratedTokens += c.Completion
		s.Drafted += c.Drafted
		s.Accepted += c.Accepted
		if c.Role != "" {
			if s.Roles == nil {
				s.Roles = map[string]RoleCalls{}
			}
			rc := RoleCalls{Calls: 1, Generated: c.Completion, Reasoning: c.Reasoning, Seconds: c.TotalMS / 1000}
			if c.Thinking == "off" {
				rc.NoThink = 1
			}
			s.Roles[c.Role] = s.Roles[c.Role].Add(rc)
		}
		if c.Status != "ok" {
			s.Failed++
		}
		if c.TTFTMS > 0 {
			ttft = append(ttft, c.TTFTMS)
		}
		if c.Reasoning > 0 {
			s.ReasoningTokens += c.Reasoning
			sources[c.ReasoningSource] = true
		}
		if c.Prompt > log[s.PeakCall].Prompt {
			s.PeakCall = i
		}
		if i == 0 || c.Status != "ok" || log[i-1].Status != "ok" || c.Prompt == 0 {
			continue
		}
		shared := min(log[i-1].Prompt, c.Prompt)
		if c.Cached+invalidationSlack < shared {
			s.Invalidations++
			s.RecomputedTokens += shared - c.Cached
			// The gateway names the first message a request changed;
			// the caller's own marks cover what it rewrote.
			if rewrites[i] || c.Changed > 0 {
				s.AfterRewrite++
			}
		}
	}
	if len(ttft) > 0 {
		sort.Float64s(ttft)
		s.TTFTMedianMS = ttft[len(ttft)/2]
		s.TTFTMaxMS = ttft[len(ttft)-1]
	}
	switch len(sources) {
	case 0:
	case 1:
		for k := range sources {
			s.ReasoningCount = k
		}
	default:
		s.ReasoningCount = "mixed"
	}
	return s
}

// ContextBreakdown is what one request was made of, by section.
type ContextBreakdown struct {
	Call         int `json:"call"`          // index in the call log
	PromptTokens int `json:"prompt_tokens"` // as the server counted the whole request
	// Counting is "tokenizer" (each section's text counted with the
	// model's tokenizer) or "estimate" (bytes × 10 / 32).
	Counting string                 `json:"counting"`
	Sections map[string]SectionSize `json:"sections"`
	// Framing is the server's count less the sections: the chat template,
	// message delimiters, and how the template renders tool schemas.
	Framing int `json:"framing_tokens"`
}

// SectionSize is one section's size.
type SectionSize struct {
	Bytes  int `json:"bytes"`
	Tokens int `json:"tokens"`
}

// breakdown counts the sections of a request.
func breakdown(ctx context.Context, call, prompt int, sections map[string]string, tokenize func(context.Context, string) (int, error)) *ContextBreakdown {
	b := &ContextBreakdown{Call: call, PromptTokens: prompt, Counting: "tokenizer", Sections: map[string]SectionSize{}}
	sum := 0
	for k, text := range sections {
		n := 0
		if tokenize != nil && b.Counting == "tokenizer" {
			var err error
			if n, err = tokenize(ctx, text); err != nil {
				b.Counting = "estimate"
			}
		}
		if tokenize == nil || b.Counting == "estimate" {
			b.Counting = "estimate"
			n = contextplan.EstimateTokens(text)
		}
		b.Sections[k] = SectionSize{Bytes: len(text), Tokens: n}
		sum += n
	}
	if b.Counting == "estimate" {
		// A tokenizer failure part-way: count every section the same way.
		sum = 0
		for k, text := range sections {
			n := contextplan.EstimateTokens(text)
			b.Sections[k] = SectionSize{Bytes: len(text), Tokens: n}
			sum += n
		}
	}
	b.Framing = prompt - sum
	return b
}
