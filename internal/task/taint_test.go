package task

import (
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

// M4's effect, at the boundary it now runs at.
//
// The taint is applied in VERIFY, to the results as they come back, rather
// than in REVIEW where it used to be. The property that matters is not where
// the call sits but what the taint can and cannot do to the evidence, so
// that is what these assert.

func TestTaintMarksTestResultsWithoutChangingTheirVerdict(t *testing.T) {
	results := []recipe.Result{
		{Recipe: "go test", Kind: recipe.KindTest, Status: recipe.Pass},
		{Recipe: "go build", Kind: recipe.KindBuild, Status: recipe.Pass},
	}
	findings := []workflow.IntegrityFinding{
		{Path: "order_test.go", Weakens: 0.93, Detail: "may weaken what it checks"},
	}

	out := taintTestResults(results, findings)

	if !out[0].Tainted || out[0].TaintReason == "" {
		t.Fatalf("the test result must carry the taint: %+v", out[0])
	}
	if out[1].Tainted {
		t.Fatalf("a build result is not what the finding was about: %+v", out[1])
	}
	for i := range out {
		if out[i].Status != recipe.Pass || !out[i].Passed() {
			t.Fatalf("a taint changed a verdict: %+v", out[i])
		}
	}
	// The caller's own slice is untouched, so nothing that held the
	// pre-taint evidence sees it change under them.
	if results[0].Tainted {
		t.Fatalf("taintTestResults mutated its input")
	}
}

func TestTaintWithNoFindingsChangesNothing(t *testing.T) {
	results := []recipe.Result{{Recipe: "go test", Kind: recipe.KindTest, Status: recipe.Pass}}
	out := taintTestResults(results, nil)
	if out[0].Tainted {
		t.Fatalf("no findings must mean no taint: %+v", out[0])
	}
}

func TestTaintConcernsReadWhatVerifyRecorded(t *testing.T) {
	results := []recipe.Result{
		{Recipe: "go test", Kind: recipe.KindTest, Status: recipe.Pass,
			Tainted: true, TaintReason: "hunk in order_test.go may weaken what it checks"},
		{Recipe: "go build", Kind: recipe.KindBuild, Status: recipe.Pass},
	}
	concerns := taintConcerns(results)
	if len(concerns) != 1 {
		t.Fatalf("concerns = %+v, want the one tainted result", concerns)
	}
	if concerns[0] == "" || concerns[0] == "go test" {
		t.Fatalf("a concern must carry the reason, not just the recipe: %q", concerns[0])
	}
}

// The reviewer reads the results after the taint was applied, which is the
// whole reason M4 moved out of REVIEW: at the old placement the taint was
// written after the reviewer's evidence had already been snapshotted, so it
// reached nobody.
func TestTaintIsVisibleToAReaderOfTheResults(t *testing.T) {
	results := taintTestResults(
		[]recipe.Result{{Recipe: "go test", Kind: recipe.KindTest, Status: recipe.Pass}},
		[]workflow.IntegrityFinding{{Path: "a_test.go", Weakens: 0.9, Detail: "weakens"}},
	)
	if len(taintConcerns(reviewResults(results))) != 1 {
		t.Fatalf("the reviewer's copy of the results must carry the taint")
	}
}
