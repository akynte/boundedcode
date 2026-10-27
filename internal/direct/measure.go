package direct

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Measurement hooks for benchmarks: what each model request is made of,
// and where a turn's time goes besides the model and the tools. Nothing
// here costs anything unless a benchmark sets OnRequest or reads Phases.

// Phases a turn's time is attributed to (Runner.Phases). Checks and
// history notes run inside a tool call, so their time is also part of
// that tool's elapsed time.
const (
	PhaseProjectMap   = "project_map"
	PhaseSeedPack     = "seed_pack"
	PhaseWorkingSet   = "working_set"
	PhaseChecks       = "checks"
	PhaseHistoryNotes = "history_notes"
)

type phaseClock struct {
	mu sync.Mutex
	d  map[string]time.Duration
}

// phase adds the time since start to the named phase.
func (r *Runner) phase(name string, start time.Time) {
	r.phases.mu.Lock()
	defer r.phases.mu.Unlock()
	if r.phases.d == nil {
		r.phases.d = map[string]time.Duration{}
	}
	r.phases.d[name] += time.Since(start)
}

// Phases returns the time spent per phase so far.
func (r *Runner) Phases() map[string]time.Duration {
	r.phases.mu.Lock()
	defer r.phases.mu.Unlock()
	out := make(map[string]time.Duration, len(r.phases.d))
	for k, v := range r.phases.d {
		out[k] = v
	}
	return out
}

// Sections of a request (requestSections). Tool results are keyed
// "tool:NAME"; set-aside placeholders are counted apart.
const (
	SectionSystem      = "system"
	SectionToolSchemas = "tool_schemas"
	SectionEnvironment = "environment"
	SectionProjectMap  = "project_map"
	SectionSeedPack    = "seed_pack"
	SectionUser        = "user"
	SectionSummary     = "summary"
	SectionAssistant   = "assistant"
	SectionToolCalls   = "tool_calls"
	SectionSetAside    = "set_aside"
)

// requestSections splits what the next request sends by kind, as text,
// so a benchmark can count each part's tokens (exactly, with the model's
// tokenizer, or by estimate). Chat-template tokens around messages are
// not in any section.
func (r *Runner) requestSections(tools []Tool) map[string]string {
	var b = map[string]*strings.Builder{}
	add := func(k, s string) {
		if s == "" {
			return
		}
		if b[k] == nil {
			b[k] = &strings.Builder{}
		}
		b[k].WriteString(s)
	}
	add(SectionSystem, r.systemPrompt())
	if len(tools) > 0 {
		raw, _ := json.Marshal(toolSchemas(tools))
		add(SectionToolSchemas, string(raw))
	}
	for _, m := range r.Session.Messages {
		switch m.Role {
		case "tool":
			if strings.HasPrefix(m.Content, "[") && strings.Contains(m.Content, " set aside to save context;") {
				add(SectionSetAside, m.Content)
			} else {
				add("tool:"+m.Name, m.Content)
			}
		case "assistant":
			add(SectionAssistant, m.Content)
			for _, c := range m.ToolCalls {
				add(SectionToolCalls, c.Function.Name+c.Function.Arguments)
			}
		default:
			if strings.HasPrefix(m.Content, "[Earlier conversation, summarised") {
				add(SectionSummary, m.Content)
				continue
			}
			add(SectionUser, m.Content)
			for _, p := range m.Parts {
				if t, ok := p["text"].(string); ok {
					add(SectionUser, t)
				}
			}
			for _, block := range strings.Split(m.Context, "\n\n") {
				switch {
				case strings.HasPrefix(block, "Project map ("):
					add(SectionProjectMap, block)
				case strings.HasPrefix(block, "Likely relevant places"):
					add(SectionSeedPack, block)
				default:
					add(SectionEnvironment, block)
				}
			}
		}
	}
	out := make(map[string]string, len(b))
	for k, v := range b {
		out[k] = v.String()
	}
	return out
}
