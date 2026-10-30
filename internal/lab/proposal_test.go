package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

// proposalFixture decides a synthetic experiment whose candidate is
// accepted and writes its proposal.
func proposalFixture(t *testing.T, langs []string, set config.ExperimentSettings) (string, []FileRef) {
	t.Helper()
	dir := t.TempDir()
	base := suite(langs, 4, func(id, _ string) bool { return strings.HasSuffix(id, "-3") })
	cand := suite(langs, 4, func(string, string) bool { return true })
	reports := map[string][]string{"base": writeReports(t, dir, "base", base, nil), "cand": writeReports(t, dir, "cand", cand, nil)}
	e := experiment(Arm{Name: "cand", Role: "candidate", Set: set})
	d, err := Decide(e, reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := EvidenceFor(dir, nil, reports)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := Proposals(dir, e, d, nil, ev)
	if err != nil || len(ps) != 1 {
		t.Fatalf("proposals: %v %v (%+v)", ps, err, d.Arms)
	}
	return ps[0], ev
}

func TestProposalLifecycle(t *testing.T) {
	path, ev := proposalFixture(t, []string{"go", "python", "rust"}, config.ExperimentSettings{Direct: config.Tunables{Retrieval: "basic"}})
	p, _, err := LoadProposal(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "pending-review" || p.Verdict != VerdictAccept || len(p.Scope) != 0 || len(p.Limitations) == 0 {
		t.Fatalf("proposal: %+v", p)
	}
	if md, _ := os.ReadFile(strings.TrimSuffix(path, ".json") + ".md"); !strings.Contains(string(md), "direct.retrieval=basic") {
		t.Fatalf("markdown:\n%s", md)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\n# keep me\ndirect:\n  max_turns: 70\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Nothing happens without an approval.
	if _, err := Promote(path, cfgPath, true); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("promote unapproved: %v", err)
	}
	if _, err := Approve(path, "", ""); err == nil {
		t.Fatal("approved without a reviewer")
	}
	// Evidence changed since the decision: no approval.
	if filepath.IsAbs(ev[0].Path) {
		t.Fatalf("evidence path %s is absolute: proposals must move with their directory", ev[0].Path)
	}
	evPath := filepath.Join(filepath.Dir(path), ev[0].Path)
	orig, _ := os.ReadFile(evPath)
	_ = os.WriteFile(evPath, []byte("{}"), 0o644)
	if _, err := Approve(path, "reviewer", ""); err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("approve with changed evidence: %v", err)
	}
	_ = os.WriteFile(evPath, orig, 0o644)
	if _, err := Approve(path, "reviewer", "looked at the per-language table"); err != nil {
		t.Fatal(err)
	}
	// Promote shows the change without --write.
	pr, err := Promote(path, cfgPath, false)
	if err != nil || pr.Written || !strings.Contains(pr.Change, "direct.retrieval=basic") {
		t.Fatalf("dry promote: %+v %v", pr, err)
	}
	if b, _ := os.ReadFile(cfgPath); strings.Contains(string(b), "retrieval") {
		t.Fatal("a dry promote wrote the configuration")
	}
	pr, err = Promote(path, cfgPath, true)
	if err != nil || !pr.Written {
		t.Fatalf("promote: %+v %v", pr, err)
	}
	b, _ := os.ReadFile(cfgPath)
	for _, want := range []string{"retrieval: basic", "max_turns: 70", "# keep me"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("promoted config lacks %q:\n%s", want, b)
		}
	}
	if c, err := config.Load(cfgPath); err != nil || c.Direct.Retrieval != "basic" {
		t.Fatalf("promoted config: %v", err)
	}
	if bak, _ := os.ReadFile(pr.Backup); strings.Contains(string(bak), "retrieval") {
		t.Fatal("the backup is not the old configuration")
	}
	// A proposal edited after approval is not promoted.
	pb, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.Replace(string(pb), `"basic"`, `"adaptive"`, 1)), 0o644)
	if _, err := Promote(path, cfgPath, true); err == nil || !strings.Contains(err.Error(), "changed after it was approved") {
		t.Fatalf("promote an edited proposal: %v", err)
	}
}

func TestPromoteWorkloadAndCode(t *testing.T) {
	// Accepted with only two languages covered: workload-specific.
	path, _ := proposalFixture(t, []string{"go", "rust"}, config.ExperimentSettings{Direct: config.Tunables{MaxTurns: 100}})
	p, _, _ := LoadProposal(path)
	if p.Verdict != VerdictWorkload || strings.Join(p.Scope, ",") != "go,rust" {
		t.Fatalf("two languages: %s %v", p.Verdict, p.Scope)
	}
	if _, err := Approve(path, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	pr, err := Promote(path, cfgPath, true)
	if err != nil || !pr.Written {
		t.Fatalf("promote: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	w, ok := c.Direct.Workload([]string{"rust"})
	if !ok || w.Set.MaxTurns != 100 || !strings.Contains(w.Source, p.ID) || c.Direct.MaxTurns == 100 {
		t.Fatalf("workload: %+v (global max_turns %d)", w, c.Direct.MaxTurns)
	}
	if _, ok := c.Direct.Workload([]string{"python"}); ok {
		t.Fatal("the override applies to python")
	}
	// A code proposal is reported, never applied.
	p.Patch, p.Settings, p.Pairs = &PatchInfo{Files: []string{"internal/direct/observe.go"}, SHA256: strings.Repeat("ab", 32)}, config.ExperimentSettings{}, nil
	code := filepath.Join(filepath.Dir(path), "proposal-code.json")
	if err := writeJSON(code, p); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(code, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cfgPath)
	pr, err = Promote(code, cfgPath, true)
	after, _ := os.ReadFile(cfgPath)
	if err != nil || pr.Written || !strings.Contains(pr.Change, "does not apply code") || string(before) != string(after) {
		t.Fatalf("code promote: %+v %v", pr, err)
	}
}

func TestApplySettingsKeepsTheRest(t *testing.T) {
	in := []byte("version: 1\n# models live here\nmodels_dir: /m\ndirect:\n  approval: ask # never auto\n")
	var s config.ExperimentSettings
	s.Direct.WorkingSetTokens = 48000
	s.Budgets.MaxAttempts = 3
	out, err := applySettings(in, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# models live here", "approval: ask # never auto", "working_set_tokens: 48000", "max_attempts: 3", "models_dir: /m"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
