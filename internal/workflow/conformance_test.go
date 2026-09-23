package workflow_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

const conformanceDiff = `diff --git a/pkg/order/order.go b/pkg/order/order.go
--- a/pkg/order/order.go
+++ b/pkg/order/order.go
@@ -5,6 +5,9 @@ func Checkout(cart Cart) error {
 	if len(cart.Items) == 0 {
+		return ErrEmptyCart
 	}
 	return nil
 }
diff --git a/pkg/order/logging.go b/pkg/order/logging.go
--- a/pkg/order/logging.go
+++ b/pkg/order/logging.go
@@ -1,3 +1,4 @@
 package order
+import "unrelated/pkg"
 var x int
`

func TestCheckDiffConformanceSkipsUnderStrictRedaction(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactStrict}
	res, err := workflow.CheckDiffConformance(context.Background(), j, "reject empty carts",
		[]workflow.PlanReason{{Path: "pkg/order/order.go", Reason: "reject empty carts"}},
		workflow.SplitDiff(conformanceDiff), workflow.ConformanceTuning{}, nil)
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureRefused {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureRefused)
	}
	if res.Attempted || res.SkipReason != "redact_strict" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckDiffConformanceJudgesHunksAgainstPlanReasons(t *testing.T) {
	hunks := workflow.SplitDiff(conformanceDiff)
	if len(hunks) != 2 {
		t.Fatalf("fixture drifted: got %d hunks", len(hunks))
	}
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers: map[string]judgment.Answer{
			"addresses": {Noul: 0.85},
			// order.go has a plan reason and is candidate c0; logging.go has
			// no plan reason and is filtered out before judging.
			"c0": {Choice: "serves_plan", Confidence: 0.9},
		},
	}
	res, err := workflow.CheckDiffConformance(context.Background(), j, "reject empty carts",
		[]workflow.PlanReason{{Path: "pkg/order/order.go", Reason: "reject empty carts"}},
		hunks, workflow.ConformanceTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if res.Total != 2 || res.Candidates != 1 {
		t.Fatalf("Total=%d Candidates=%d, want 2 and 1 (only order.go has a plan reason)",
			res.Total, res.Candidates)
	}
	if len(res.Findings) != 1 || res.Findings[0].Path != "pkg/order/order.go" {
		t.Fatalf("Findings = %+v", res.Findings)
	}
	if res.Findings[0].Category != workflow.ConformanceServesPlan {
		t.Fatalf("category = %q, want serves_plan", res.Findings[0].Category)
	}
	if !res.AddressesObjectiveAnswered || res.AddressesObjective != 0.85 {
		t.Fatalf("AddressesObjective = %v (answered=%v), want 0.85/true",
			res.AddressesObjective, res.AddressesObjectiveAnswered)
	}

	// logging.go's hunk must never have reached the judge as a per-hunk
	// candidate (no plan reason names it).
	for _, call := range j.Calls() {
		for id := range call.Questions {
			if id == "c1" {
				t.Fatalf("a hunk with no plan reason was judged")
			}
		}
	}
}

func TestCheckDiffConformanceWithNoPlanReasonsStillAsksWholeDiff(t *testing.T) {
	hunks := workflow.SplitDiff(conformanceDiff)
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers:    map[string]judgment.Answer{"addresses": {Noul: 0.1}},
	}
	res, err := workflow.CheckDiffConformance(context.Background(), j, "reject empty carts",
		nil, hunks, workflow.ConformanceTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}
	if res.Candidates != 0 {
		t.Fatalf("Candidates = %d, want 0 (no plan reasons at all)", res.Candidates)
	}
	if !res.AddressesObjectiveAnswered || res.AddressesObjective != 0.1 {
		t.Fatalf("whole-diff question must still be asked: %+v", res)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("no candidate hunks means no per-hunk findings: %+v", res.Findings)
	}
}

func TestCheckDiffConformanceDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: map[string]judgment.Answer{"addresses": {Noul: 0.5}}}
	res, _ := workflow.CheckDiffConformance(context.Background(), j, "obj", nil,
		workflow.SplitDiff(conformanceDiff), workflow.ConformanceTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged", res.Tier)
	}
}
