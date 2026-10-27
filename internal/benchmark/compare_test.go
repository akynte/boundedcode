package benchmark

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareRuns(t *testing.T) {
	dir := t.TempDir()
	load := func(v any, id string) *Run {
		t.Helper()
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, id+".json")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := LoadRun(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// A run of three tasks; scale multiplies every task's time and calls,
	// passB says whether task b passed.
	direct := func(id string, scale float64, passB bool) *Run {
		r := &DirectReport{ID: id, Results: []DirectResult{
			{ID: "a", Success: true, WallSeconds: 10 * scale, Calls: int(10 * scale), PromptTokens: 1000},
			{ID: "b", Success: passB, WallSeconds: 20 * scale, Calls: int(20 * scale), PromptTokens: 2000},
			{ID: "c", Success: true, WallSeconds: 40 * scale, Calls: int(40 * scale), PromptTokens: 4000},
			{ID: "rust", Skipped: "cargo not on PATH"},
		}}
		r.Summary = summarizeDirect("", r.Results)
		return load(r, id)
	}
	base := []*Run{direct("direct-a", 1.0, true), direct("direct-b", 1.05, true), direct("direct-c", 0.95, true)}

	var out bytes.Buffer
	// The same build again: within noise.
	Compare(&out, base, []*Run{direct("direct-d", 1.02, true), direct("direct-e", 0.98, true)})
	if s := out.String(); !strings.Contains(s, "| seconds | 1.00 | 3 | 0.78–1.23 | within noise |") || strings.Contains(s, "**worse**") {
		t.Fatalf("same build:\n%s", s)
	}
	// Twice as slow, and task b now always fails.
	out.Reset()
	Compare(&out, base, []*Run{direct("direct-f", 2, false), direct("direct-g", 2.1, false)})
	s := out.String()
	for _, want := range []string{
		"| seconds | 2.05 | 3 | 0.78–1.23 | **worse** |",
		"| b | 3/3 | 0/2 | regressed |",
		"always failed in the candidate: b.",
		"| prompt tokens | 1.00 | 3 |",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "| rust |") {
		t.Fatal("a skipped task is not compared")
	}
	// Two baseline runs: no band, no verdict.
	out.Reset()
	Compare(&out, base[:2], []*Run{direct("direct-h", 2, true)})
	if s := out.String(); !strings.Contains(s, "no change is called a difference") || strings.Contains(s, "**worse**") {
		t.Fatalf("two baseline runs:\n%s", s)
	}

	// A bounded suite report loads, with verification accuracy.
	sr := &SuiteReport{ID: "suite-x", Results: []TaskResult{
		{ID: "a", Success: false, SelfVerified: true, Calls: CallSummary{InferenceSeconds: 10, PromptTokens: 100}, CachedTokens: 50, VerifySeconds: 4},
		{ID: "b", Success: true, SelfVerified: false},
	}}
	sr.Summary = summarize(sr.Results)
	r := load(sr, "suite-x")
	if r.Kind != "bounded" || r.Metrics["false verifications"] != 1 || r.Metrics["missed verifications"] != 1 || r.Metrics["verification seconds per task"] != 2 || r.Metrics["prompt-cache hit rate"] != 50 || r.Task["a"]["verification seconds"] != 4 {
		t.Fatalf("bounded run: %+v", r)
	}
	b, _ := json.Marshal(map[string]string{"id": "infra-1"})
	_ = os.WriteFile(filepath.Join(dir, "other.json"), b, 0o644)
	if _, err := LoadRun(filepath.Join(dir, "other.json")); err == nil {
		t.Fatal("an unknown report kind loads")
	}
}

// A baseline whose runs agree closely still gets a band of at least ±10%,
// and one candidate run never makes a task regressed.
func TestCompareFloorAndSingleRun(t *testing.T) {
	mk := func(id string, scale float64, pass bool) *Run {
		return &Run{ID: id, Passed: map[string]bool{"a": pass}, Task: map[string]map[string]float64{"a": {"seconds": 100 * scale}}}
	}
	base := []*Run{mk("x", 1, true), mk("y", 1.01, true), mk("z", 0.99, true)}
	var out bytes.Buffer
	Compare(&out, base, []*Run{mk("c", 1.08, false)})
	s := out.String()
	for _, want := range []string{"| seconds | 1.08 | 1 | 0.90–1.10 | within noise |", "failed in the only candidate run", "With one candidate run no task is called regressed"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}
