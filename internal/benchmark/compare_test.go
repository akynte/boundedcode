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
	write := func(name string, v any) string {
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	direct := func(id string, wall float64, pass bool) *DirectReport {
		r := &DirectReport{ID: id, Results: []DirectResult{{ID: "fix", Success: pass, WallSeconds: wall}, {ID: "rust", Skipped: "cargo not on PATH"}}}
		r.Summary = summarizeDirect("", r.Results)
		return r
	}
	var base, cand []*Run
	for i, d := range []*DirectReport{direct("direct-a", 30, true), direct("direct-b", 34, true)} {
		run, err := LoadRun(write(d.ID+".json", d))
		if err != nil {
			t.Fatal(i, err)
		}
		base = append(base, run)
	}
	run, err := LoadRun(write("direct-c.json", direct("direct-c", 60, false)))
	if err != nil {
		t.Fatal(err)
	}
	cand = append(cand, run)
	var out bytes.Buffer
	Compare(&out, base, cand)
	s := out.String()
	for _, want := range []string{
		"| seconds per task | 32.0 (30.0–34.0, n=2) | 60.0 | +88% | **worse**, outside baseline range |",
		"| pass rate | 100% (100%–100%, n=2) | 0% | -100% | **worse**, outside baseline range |",
		"| fix | 2/2 | 0/1 ← |",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "| rust |") {
		t.Fatal("a skipped task is not compared")
	}

	// One baseline run: no difference is claimed.
	out.Reset()
	Compare(&out, base[:1], cand)
	if !strings.Contains(out.String(), "noise is unknown") || strings.Contains(out.String(), "outside baseline range") {
		t.Fatalf("single baseline:\n%s", out.String())
	}

	// A bounded suite report loads too, with verification accuracy.
	sr := &SuiteReport{ID: "suite-x", Results: []TaskResult{
		{ID: "a", Success: false, SelfVerified: true, Calls: CallSummary{InferenceSeconds: 10, PromptTokens: 100}, CachedTokens: 50, VerifySeconds: 4},
		{ID: "b", Success: true, SelfVerified: false},
	}}
	sr.Summary = summarize(sr.Results)
	r, err := LoadRun(write("suite-x.json", sr))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "bounded" || r.Metrics["false verifications"] != 1 || r.Metrics["missed verifications"] != 1 || r.Metrics["verification seconds per task"] != 2 || r.Metrics["prompt-cache hit rate"] != 50 {
		t.Fatalf("bounded run: %+v", r)
	}
	if _, err := LoadRun(write("other.json", map[string]string{"id": "infra-1"})); err == nil {
		t.Fatal("an unknown report kind loads")
	}
}
