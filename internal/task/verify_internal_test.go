package task

import (
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

// Failure output names the snapshot, which is gone by the time anyone reads
// it. Everything downstream resolves paths against the worktree, so the
// results must name the worktree.
func TestRebaseResultsNamesTheWorktree(t *testing.T) {
	const snap, wt = "/ws/vf-t1", "/ws/wt-t1"
	in := []recipe.Result{{
		Err: "exec " + snap + "/tool failed",
		Summary: recipe.Summary{
			Headline: "panic in " + snap + "/a.go",
			Findings: []recipe.Finding{{File: snap + "/a.go", Message: "at " + snap + "/b.go:3"}},
		},
	}}
	original := in[0].Summary.Findings[0]

	got := rebaseResults(in, snap, wt)[0]
	if got.Err != "exec "+wt+"/tool failed" {
		t.Errorf("Err = %q", got.Err)
	}
	if got.Summary.Headline != "panic in "+wt+"/a.go" {
		t.Errorf("Headline = %q", got.Summary.Headline)
	}
	f := got.Summary.Findings[0]
	if f.File != wt+"/a.go" || f.Message != "at "+wt+"/b.go:3" {
		t.Errorf("finding = %+v", f)
	}
	if original.File != snap+"/a.go" {
		t.Error("rebasing must not mutate a findings slice another result may share")
	}
}

// The preset rules look results up by preset name, so a hidden result is
// invisible to them. checkVerification must still reject on it, with or
// without a baseline in force.
func TestCheckVerificationJudgesHiddenChecksUnderABaseline(t *testing.T) {
	const cand = "cand-1"
	s := &workflow.State{
		Candidate: cand,
		Presets:   []recipe.Preset{{Name: "go test", Kind: recipe.KindTest, Argv: []string{"go", "test"}}},
		Results: []recipe.Result{
			{Recipe: "go test", Kind: recipe.KindTest, Status: recipe.Pass, Candidate: cand},
			{Recipe: "hidden: add-sums", Kind: recipe.KindHidden, Status: recipe.Fail, Candidate: cand,
				Summary: recipe.Summary{Headline: "hidden acceptance check add-sums did not pass"}},
		},
	}
	for name, base := range map[string]*recipe.Baseline{
		"no baseline":   nil,
		"with baseline": {Entries: map[string]recipe.BaselineEntry{}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &Runner{Baseline: base}
			ok, reasons, _ := r.checkVerification(t.TempDir(), s)
			if ok {
				t.Fatal("a failing hidden check must reject the candidate")
			}
			found := false
			for _, reason := range reasons {
				found = found || reason == "hidden acceptance check add-sums did not pass"
			}
			if !found {
				t.Errorf("reasons must name the hidden check: %v", reasons)
			}
		})
	}
}
