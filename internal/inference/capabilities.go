package inference

import (
	"context"
	"strings"
	"time"
)

// Capabilities are the features a provider's model supports, learned from
// the provider where it says (llama.cpp's GET /props) and otherwise
// assumed from what its API documents. Callers ask before using a feature
// instead of assuming every model has it.
type Capabilities struct {
	Streaming bool `json:"streaming"`
	ToolCalls bool `json:"tool_calls"`
	// ReasoningRoundTrip: the model's reasoning goes back to it with the
	// history (reasoning.go).
	ReasoningRoundTrip bool `json:"reasoning_round_trip"`
	// PreserveReasoning: the chat template can keep the reasoning of every
	// earlier turn, not only of the current one (llama-server
	// --reasoning-preserve, on by default where the template supports it).
	PreserveReasoning bool `json:"preserve_reasoning"`
	// StructuredOutput: answers can be constrained to a JSON schema
	// (response_format; structured.go).
	StructuredOutput bool `json:"structured_output"`
	// PromptProgress: a streamed answer reports prompt processing before
	// the first token.
	PromptProgress bool `json:"prompt_progress"`
	// Source says where the capabilities come from: "server" (read from
	// the server), "provider" (the provider's documented API) or "assumed".
	Source string `json:"source"`
	// Server is the server's build, when it reports one.
	Server string `json:"server,omitempty"`
}

// CapabilityReporter is an upstream that can say what its model supports.
type CapabilityReporter interface {
	Capabilities(ctx context.Context) Capabilities
}

// Capabilities reports what the gateway's provider and model support.
func (g *Gateway) Capabilities(ctx context.Context) Capabilities {
	if r, ok := g.upstream().(CapabilityReporter); ok {
		return r.Capabilities(ctx)
	}
	_, streams := g.upstream().(Streamer)
	return Capabilities{Streaming: streams, ToolCalls: true, Source: "assumed"}
}

// serverProps is the part of llama-server's GET /props the capabilities
// are read from.
type serverProps struct {
	ChatTemplate string `json:"chat_template"`
	Caps         *struct {
		ToolCalls         bool `json:"supports_tool_calls"`
		PreserveReasoning bool `json:"supports_preserve_reasoning"`
	} `json:"chat_template_caps"`
	BuildInfo string `json:"build_info"`
}

// Capabilities implements CapabilityReporter. A cloud service's are what
// its API documents: OpenAI and OpenAI-compatible services take a JSON
// schema (a service that rejects one is answered without it, see
// adaptable) and never get the reasoning back unless configured. A local
// server's are read once from GET /props: only llama.cpp answers it, and
// another server gets the conservative defaults.
func (u *OpenAIUpstream) Capabilities(ctx context.Context) Capabilities {
	u.capsOnce.Do(func() {
		if u.Cloud {
			u.caps = Capabilities{Streaming: true, ToolCalls: true, StructuredOutput: true, Source: "provider",
				ReasoningRoundTrip: u.Reasoning == ReasoningKeep}
			return
		}
		u.caps = Capabilities{Streaming: true, ToolCalls: true, StructuredOutput: true, Source: "assumed",
			ReasoningRoundTrip: u.Reasoning == ReasoningKeep}
		if u.Client == nil {
			return
		}
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var p serverProps
		if code, err := u.Client.Get(pctx, "/props", &p); err != nil || code != 200 || p.Caps == nil {
			return
		}
		u.caps.Source, u.caps.Server = "server", p.BuildInfo
		u.caps.ToolCalls = p.Caps.ToolCalls
		u.caps.PreserveReasoning = p.Caps.PreserveReasoning
		u.caps.PromptProgress = true
		// The template renders reasoning it is given back: the turn the
		// model generated is the turn it sees, and the server's cached
		// tokens still match it.
		renders := strings.Contains(p.ChatTemplate, "reasoning_content")
		switch u.Reasoning {
		case ReasoningKeep:
			u.caps.ReasoningRoundTrip = true
		case ReasoningDrop:
			u.caps.ReasoningRoundTrip = false
		default:
			u.caps.ReasoningRoundTrip = renders
		}
	})
	return u.caps
}

// progress reports whether a streamed request should ask for prompt
// progress (llama.cpp's return_progress).
func (u *OpenAIUpstream) progress(ctx context.Context) bool {
	return !u.Cloud && u.Capabilities(ctx).PromptProgress
}

// Capabilities implements CapabilityReporter: the Messages API keeps
// thinking blocks itself (Replay) and takes a JSON schema
// (output_config.format).
func (u *AnthropicUpstream) Capabilities(context.Context) Capabilities {
	return Capabilities{Streaming: true, ToolCalls: true, ReasoningRoundTrip: u.Replay != nil, PreserveReasoning: u.Replay != nil,
		StructuredOutput: true, Source: "provider"}
}

// Capabilities implements CapabilityReporter: thought signatures are
// replayed (Replay) and generationConfig takes a JSON schema.
func (u *GeminiUpstream) Capabilities(context.Context) Capabilities {
	return Capabilities{Streaming: true, ToolCalls: true, ReasoningRoundTrip: u.Replay != nil, StructuredOutput: true, Source: "provider"}
}
