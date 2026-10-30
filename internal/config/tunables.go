package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Tunables are the direct-mode settings that change how well the agent
// works, never what it is allowed to do: retrieval, context, tool
// orchestration, routing and its turn budget. They are all an experiment
// of the harness laboratory may vary (internal/lab) and all a workload
// override may set. Approval, permissions, the sandbox, hooks, MCP
// servers, the web allowlist, post-change checks and the verification
// fallback are deliberately absent: no experiment or override can reach
// them, because there is no field to put them in.
type Tunables struct {
	Retrieval        string `yaml:"retrieval,omitempty"`
	SeedPack         *bool  `yaml:"seed_pack,omitempty"`
	WorkingSetTokens int    `yaml:"working_set_tokens,omitempty"`
	MaxToolOutput    int    `yaml:"max_tool_output,omitempty"`
	ObservationBytes int    `yaml:"observation_bytes,omitempty"`
	Prune            *bool  `yaml:"prune,omitempty"`
	HistoryNotes     *bool  `yaml:"history_notes,omitempty"`
	Compaction       string `yaml:"compaction,omitempty"`
	ParallelTools    int    `yaml:"parallel_tools,omitempty"`
	MCPToolSearch    string `yaml:"mcp_tool_search,omitempty"`
	Routing          string `yaml:"routing,omitempty"`
	MaxTurns         int    `yaml:"max_turns,omitempty"`
}

// IsZero reports whether t sets nothing.
func (t Tunables) IsZero() bool { return t == Tunables{} }

// ApplyTo sets d's fields from t's set ones.
func (t Tunables) ApplyTo(d *DirectConfig) {
	setS := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	setI := func(dst *int, v int) {
		if v != 0 {
			*dst = v
		}
	}
	setB := func(dst **bool, v *bool) {
		if v != nil {
			b := *v
			*dst = &b
		}
	}
	setS(&d.Retrieval, t.Retrieval)
	setB(&d.SeedPack, t.SeedPack)
	setI(&d.WorkingSetTokens, t.WorkingSetTokens)
	setI(&d.MaxToolOutput, t.MaxToolOutput)
	setI(&d.ObservationBytes, t.ObservationBytes)
	setB(&d.Prune, t.Prune)
	setB(&d.HistoryNotes, t.HistoryNotes)
	setS(&d.Compaction, t.Compaction)
	setI(&d.ParallelTools, t.ParallelTools)
	setS(&d.MCPToolSearch, t.MCPToolSearch)
	setS(&d.Routing, t.Routing)
	setI(&d.MaxTurns, t.MaxTurns)
}

// Pairs lists t's set fields as "key=value", in field order.
func (t Tunables) Pairs() []string {
	var out []string
	add := func(k string, v any) { out = append(out, fmt.Sprintf("%s=%v", k, v)) }
	if t.Retrieval != "" {
		add("retrieval", t.Retrieval)
	}
	if t.SeedPack != nil {
		add("seed_pack", *t.SeedPack)
	}
	if t.WorkingSetTokens != 0 {
		add("working_set_tokens", t.WorkingSetTokens)
	}
	if t.MaxToolOutput != 0 {
		add("max_tool_output", t.MaxToolOutput)
	}
	if t.ObservationBytes != 0 {
		add("observation_bytes", t.ObservationBytes)
	}
	if t.Prune != nil {
		add("prune", *t.Prune)
	}
	if t.HistoryNotes != nil {
		add("history_notes", *t.HistoryNotes)
	}
	if t.Compaction != "" {
		add("compaction", t.Compaction)
	}
	if t.ParallelTools != 0 {
		add("parallel_tools", t.ParallelTools)
	}
	if t.MCPToolSearch != "" {
		add("mcp_tool_search", t.MCPToolSearch)
	}
	if t.Routing != "" {
		add("routing", t.Routing)
	}
	if t.MaxTurns != 0 {
		add("max_turns", t.MaxTurns)
	}
	return out
}

func (t Tunables) validate(prefix string) []error {
	var errs []error
	bad := func(key, msg string) { errs = append(errs, fmt.Errorf("%s%s: %s", prefix, key, msg)) }
	oneOf := func(key, v string, ok ...string) {
		if v != "" && !slices.Contains(ok, v) {
			bad(key, fmt.Sprintf("%q is not %s", v, strings.Join(ok, "|")))
		}
	}
	oneOf("retrieval", t.Retrieval, "adaptive", "basic")
	oneOf("compaction", t.Compaction, "structured", "legacy")
	oneOf("mcp_tool_search", t.MCPToolSearch, "auto", "on", "off")
	oneOf("routing", t.Routing, "off", "adaptive")
	for _, v := range []struct {
		key string
		n   int
	}{{"working_set_tokens", t.WorkingSetTokens}, {"max_tool_output", t.MaxToolOutput}, {"observation_bytes", t.ObservationBytes}, {"max_turns", t.MaxTurns}} {
		if v.n < 0 {
			bad(v.key, fmt.Sprintf("%d is negative", v.n))
		}
	}
	if t.ParallelTools < 0 || t.ParallelTools > 16 {
		bad("parallel_tools", fmt.Sprintf("%d is not between 0 and 16", t.ParallelTools))
	}
	return errs
}

// WorkloadOverride sets tunables for projects whose code is in any of
// Languages (internal/lang IDs, as project detection reports them). The
// first override that matches applies; none matching, or none configured
// (the default), leaves direct mode as configured.
type WorkloadOverride struct {
	Languages []string `yaml:"languages"`
	Set       Tunables `yaml:"set"`
	// Source names what put it here, such as a laboratory proposal, for
	// audit.
	Source string `yaml:"source,omitempty"`
}

// Workload returns the first override matching langs, if any.
func (d DirectConfig) Workload(langs []string) (WorkloadOverride, bool) {
	for _, w := range d.Workloads {
		for _, l := range langs {
			if slices.Contains(w.Languages, l) {
				return w, true
			}
		}
	}
	return WorkloadOverride{}, false
}

func (d DirectConfig) validateWorkloads() []error {
	var errs []error
	for i, w := range d.Workloads {
		prefix := fmt.Sprintf("direct.workloads[%d].", i)
		if len(w.Languages) == 0 {
			errs = append(errs, errors.New(prefix+"languages: at least one language"))
		}
		if w.Set.IsZero() {
			errs = append(errs, errors.New(prefix+"set: sets nothing"))
		}
		errs = append(errs, w.Set.validate(prefix+"set.")...)
	}
	return errs
}

// ExperimentSettings are everything a laboratory experiment may vary: the
// direct-mode tunables, and for bounded mode its attempt and context-pack
// budgets, its escalation policy, and how reasoning is kept in the history.
type ExperimentSettings struct {
	Direct    Tunables `yaml:"direct,omitempty"`
	Inference struct {
		ReasoningHistory string `yaml:"reasoning_history,omitempty"`
	} `yaml:"inference,omitempty"`
	Budgets struct {
		MaxAttempts       int `yaml:"max_attempts,omitempty"`
		ContextPackTokens int `yaml:"context_pack_tokens,omitempty"`
	} `yaml:"budgets,omitempty"`
	Escalation struct {
		Policy         string `yaml:"policy,omitempty"`
		FailedAttempts int    `yaml:"failed_attempts,omitempty"`
	} `yaml:"escalation,omitempty"`
}

// IsZero reports whether s sets nothing.
func (s ExperimentSettings) IsZero() bool { return s == ExperimentSettings{} }

// ApplyTo sets c's fields from s's set ones.
func (s ExperimentSettings) ApplyTo(c *Config) {
	s.Direct.ApplyTo(&c.Direct)
	if s.Inference.ReasoningHistory != "" {
		c.Inference.ReasoningHistory = s.Inference.ReasoningHistory
	}
	if s.Budgets.MaxAttempts != 0 {
		c.Budgets.MaxAttempts = s.Budgets.MaxAttempts
	}
	if s.Budgets.ContextPackTokens != 0 {
		c.Budgets.ContextPackTokens = s.Budgets.ContextPackTokens
	}
	if s.Escalation.Policy != "" {
		c.Escalation.Policy = s.Escalation.Policy
	}
	if s.Escalation.FailedAttempts != 0 {
		c.Escalation.FailedAttempts = s.Escalation.FailedAttempts
	}
}

// Pairs lists s's set fields as "section.key=value".
func (s ExperimentSettings) Pairs() []string {
	var out []string
	for _, p := range s.Direct.Pairs() {
		out = append(out, "direct."+p)
	}
	if s.Inference.ReasoningHistory != "" {
		out = append(out, "inference.reasoning_history="+s.Inference.ReasoningHistory)
	}
	if s.Budgets.MaxAttempts != 0 {
		out = append(out, fmt.Sprintf("budgets.max_attempts=%d", s.Budgets.MaxAttempts))
	}
	if s.Budgets.ContextPackTokens != 0 {
		out = append(out, fmt.Sprintf("budgets.context_pack_tokens=%d", s.Budgets.ContextPackTokens))
	}
	if s.Escalation.Policy != "" {
		out = append(out, "escalation.policy="+s.Escalation.Policy)
	}
	if s.Escalation.FailedAttempts != 0 {
		out = append(out, fmt.Sprintf("escalation.failed_attempts=%d", s.Escalation.FailedAttempts))
	}
	return out
}

// Validate checks s's values.
func (s ExperimentSettings) Validate() error {
	errs := s.Direct.validate("direct.")
	switch s.Inference.ReasoningHistory {
	case "", "auto", "keep", "drop":
	default:
		errs = append(errs, fmt.Errorf("inference.reasoning_history: %q is not auto|keep|drop", s.Inference.ReasoningHistory))
	}
	switch s.Escalation.Policy {
	case "", "rules", "adaptive":
	default:
		errs = append(errs, fmt.Errorf("escalation.policy: %q is not rules or adaptive", s.Escalation.Policy))
	}
	if s.Budgets.MaxAttempts < 0 || s.Budgets.ContextPackTokens < 0 || s.Escalation.FailedAttempts < 0 {
		errs = append(errs, errors.New("budgets and escalation counts must not be negative"))
	}
	return errors.Join(errs...)
}
