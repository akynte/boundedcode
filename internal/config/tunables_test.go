package config

import (
	"strings"
	"testing"
)

func TestTunablesApplyAndPairs(t *testing.T) {
	off := false
	tn := Tunables{Retrieval: "basic", Prune: &off, ObservationBytes: 4000}
	if got := strings.Join(tn.Pairs(), " "); got != "retrieval=basic observation_bytes=4000 prune=false" {
		t.Fatalf("pairs: %q", got)
	}
	d := Defaults().Direct
	tn.ApplyTo(&d)
	if d.Retrieval != "basic" || d.PruneOn() || d.ObservationBytes != 4000 {
		t.Fatalf("applied: %+v", d)
	}
	off = true // the applied value is a copy
	if d.PruneOn() {
		t.Fatal("ApplyTo kept a pointer into the tunables")
	}
	if !(Tunables{}).IsZero() || tn.IsZero() {
		t.Fatal("IsZero")
	}
}

// No experiment and no workload override can name a security setting:
// there is no field for it, and decoding is strict.
func TestTunablesCannotReachSecuritySettings(t *testing.T) {
	for _, y := range []string{
		"direct: {approval: auto}",
		"direct: {sandbox: {mode: off}}",
		"direct: {permissions: {allow: ['bash(*)']}}",
		"direct: {verify_fallback: allow}",
		"direct: {checks: false}",
		"direct: {hooks: []}",
		"direct: {mcp: {}}",
		"frontier: {enabled: true}",
		"escalation: {high_risk_review: []}",
	} {
		var s ExperimentSettings
		if err := DecodeStrict([]byte(y), &s); err == nil {
			t.Errorf("%s: decoded", y)
		}
	}
	var s ExperimentSettings
	if err := DecodeStrict([]byte("direct: {retrieval: basic, max_turns: 40}\nbudgets: {max_attempts: 4}\nescalation: {policy: adaptive}"), &s); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	c := Defaults()
	s.ApplyTo(&c)
	if c.Direct.Retrieval != "basic" || c.Direct.MaxTurns != 40 || c.Budgets.MaxAttempts != 4 || c.Escalation.Policy != "adaptive" {
		t.Fatalf("applied: %+v %+v %+v", c.Direct, c.Budgets, c.Escalation)
	}
	if got := strings.Join(s.Pairs(), " "); got != "direct.retrieval=basic direct.max_turns=40 budgets.max_attempts=4 escalation.policy=adaptive" {
		t.Fatalf("pairs: %q", got)
	}
	bad := ExperimentSettings{Direct: Tunables{Retrieval: "magic", ParallelTools: 99}}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "direct.retrieval") || !strings.Contains(err.Error(), "parallel_tools") {
		t.Fatalf("validate: %v", err)
	}
}

func TestWorkloads(t *testing.T) {
	c := Defaults()
	if _, ok := c.Direct.Workload([]string{"go"}); ok {
		t.Fatal("a default config has a workload override")
	}
	c.Direct.Workloads = []WorkloadOverride{
		{Languages: []string{"rust", "cpp"}, Set: Tunables{Retrieval: "basic"}},
		{Languages: []string{"rust"}, Set: Tunables{MaxTurns: 10}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	w, ok := c.Direct.Workload([]string{"python", "rust"})
	if !ok || w.Set.Retrieval != "basic" {
		t.Fatalf("first match: %+v %v", w, ok)
	}
	if _, ok := c.Direct.Workload([]string{"go"}); ok {
		t.Fatal("matched another language")
	}
	c.Direct.Workloads = []WorkloadOverride{{Set: Tunables{Routing: "sometimes"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "direct.workloads[0].languages") || !strings.Contains(err.Error(), "direct.workloads[0].set.routing") {
		t.Fatalf("validate: %v", err)
	}
	var d DirectConfig
	if err := DecodeStrict([]byte("workloads: [{languages: [go], set: {approval: auto}}]"), &d); err == nil {
		t.Fatal("a workload override set approval")
	}
}
