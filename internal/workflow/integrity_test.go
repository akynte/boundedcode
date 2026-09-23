package workflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

const sampleDiff = `diff --git a/pkg/order/order_test.go b/pkg/order/order_test.go
index 1111111..2222222 100644
--- a/pkg/order/order_test.go
+++ b/pkg/order/order_test.go
@@ -10,7 +10,6 @@ func TestCheckout(t *testing.T) {
 	err := Checkout(cart)
-	if err == nil {
-		t.Fatalf("expected an error for an empty cart")
-	}
+	_ = err
 }
diff --git a/pkg/order/order.go b/pkg/order/order.go
index 3333333..4444444 100644
--- a/pkg/order/order.go
+++ b/pkg/order/order.go
@@ -5,6 +5,9 @@ func Checkout(cart Cart) error {
 	if len(cart.Items) == 0 {
+		return ErrEmptyCart
 	}
 	return nil
 }
diff --git a/pkg/order/limit_test.go b/pkg/order/limit_test.go
new file mode 100644
index 0000000..5555555
--- /dev/null
+++ b/pkg/order/limit_test.go
@@ -0,0 +1,5 @@
+func TestDailyLimit(t *testing.T) {
+	if Limit() != 100 {
+		t.Fatalf("want 100")
+	}
+}
`

func TestSplitDiffFindsHunksAndClassifiesTests(t *testing.T) {
	hunks := workflow.SplitDiff(sampleDiff)
	if len(hunks) != 3 {
		t.Fatalf("got %d hunks, want 3:\n%+v", len(hunks), hunks)
	}
	byPath := map[string]workflow.Hunk{}
	for _, h := range hunks {
		byPath[h.Path] = h
	}
	if h, ok := byPath["pkg/order/order_test.go"]; !ok || !h.IsTest {
		t.Fatalf("order_test.go hunk missing or not classified as a test: %+v", h)
	}
	if h, ok := byPath["pkg/order/order.go"]; !ok || h.IsTest {
		t.Fatalf("order.go hunk missing or wrongly classified as a test: %+v", h)
	}
	if h, ok := byPath["pkg/order/limit_test.go"]; !ok || !h.IsTest {
		t.Fatalf("new file limit_test.go hunk missing or not classified as a test: %+v", h)
	}
	// The removed assertion must actually appear in what a judgment would
	// read, or the whole mechanism has nothing to judge.
	orderTest := byPath["pkg/order/order_test.go"]
	if !strings.Contains(orderTest.Body, "expected an error for an empty cart") {
		t.Fatalf("hunk body lost the removed assertion:\n%s", orderTest.Body)
	}
}

func TestSplitDiffOnFixtureAndSpecPaths(t *testing.T) {
	cases := map[string]bool{
		"internal/task/testdata/plan.json":       true,
		"web/src/checkout.spec.ts":               true,
		"web/src/__snapshots__/App.test.js.snap": true,
		"cmd/bcode/main.go":                      false,
		"docs/index.md":                          false,
	}
	for path, want := range cases {
		diff := "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path +
			"\n@@ -1,1 +1,1 @@\n-old\n+new\n"
		hunks := workflow.SplitDiff(diff)
		if len(hunks) != 1 {
			t.Fatalf("%s: got %d hunks, want 1", path, len(hunks))
		}
		if hunks[0].IsTest != want {
			t.Errorf("%s: IsTest = %v, want %v", path, hunks[0].IsTest, want)
		}
	}
}

func TestCheckTestIntegritySkipsUnderStrictRedaction(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactStrict}
	hunks := workflow.SplitDiff(sampleDiff)
	res, err := workflow.CheckTestIntegrity(context.Background(), j, "fix checkout", hunks,
		workflow.IntegrityTuning{}, nil)
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureRefused {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureRefused)
	}
	if res.Attempted {
		t.Fatalf("a diff hunk is repository source; strict mode must never attempt this check: %+v", res)
	}
	if res.SkipReason != "redact_strict" {
		t.Fatalf("SkipReason = %q, want redact_strict", res.SkipReason)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("strict mode must never place a call, got %d", len(j.Calls()))
	}
}

func TestCheckTestIntegrityFlagsAWeakenedAssertion(t *testing.T) {
	hunks := workflow.SplitDiff(sampleDiff)
	var testHunks []workflow.Hunk
	for _, h := range hunks {
		if h.IsTest {
			testHunks = append(testHunks, h)
		}
	}
	if len(testHunks) != 2 {
		t.Fatalf("fixture drifted: want 2 test hunks, got %d", len(testHunks))
	}

	// h0 is order_test.go (the weakened one), h1 is limit_test.go (untouched
	// assertion) — batch order follows SplitDiff's file order, which is
	// deterministic for this fixture.
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers: map[string]judgment.Answer{
			"weakens_h0":    {Noul: 0.93},
			"legitimate_h0": {Noul: 0.1},
			"weakens_h1":    {Noul: 0.05},
			"legitimate_h1": {Noul: 0.05},
		},
	}
	res, err := workflow.CheckTestIntegrity(context.Background(), j, "fix checkout", hunks,
		workflow.IntegrityTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if res.Candidates != 2 {
		t.Fatalf("Candidates = %d, want 2 (only test hunks are asked about)", res.Candidates)
	}
	if res.Judged != 2 {
		t.Fatalf("Judged = %d, want 2", res.Judged)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("Findings = %+v, want exactly one (the weakened assertion)", res.Findings)
	}
	if res.Findings[0].Path != "pkg/order/order_test.go" {
		t.Fatalf("finding on the wrong file: %+v", res.Findings[0])
	}

	// The non-test hunk (order.go) must never have been asked about at all.
	for _, call := range j.Calls() {
		for id := range call.Questions {
			if strings.Contains(id, "h2") {
				t.Fatalf("a non-test hunk reached the judge: question id %s", id)
			}
		}
	}
}

func TestCheckTestIntegrityLegitimateUpdateIsNotFlagged(t *testing.T) {
	diff := `diff --git a/pkg/order/limit_test.go b/pkg/order/limit_test.go
--- a/pkg/order/limit_test.go
+++ b/pkg/order/limit_test.go
@@ -1,5 +1,5 @@
 func TestDailyLimit(t *testing.T) {
-	if Limit() != 100 {
+	if Limit() != 200 {
 		t.Fatalf("want 200")
 	}
 }
`
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers: map[string]judgment.Answer{
			// The judge believes it both weakens (a value changed) and is a
			// legitimate update to new behaviour; the ceiling must win.
			"weakens_h0":    {Noul: 0.8},
			"legitimate_h0": {Noul: 0.85},
		},
	}
	res, err := workflow.CheckTestIntegrity(context.Background(), j, "raise the daily limit to 200",
		workflow.SplitDiff(diff), workflow.IntegrityTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("a judged-legitimate update must not be flagged: %+v", res.Findings)
	}
	if res.Judged != 1 {
		t.Fatalf("Judged = %d, want 1", res.Judged)
	}
}

func TestCheckTestIntegrityWithNoTestHunksIsCheapAndInert(t *testing.T) {
	diff := `diff --git a/pkg/order/order.go b/pkg/order/order.go
--- a/pkg/order/order.go
+++ b/pkg/order/order.go
@@ -1,1 +1,1 @@
-old
+new
`
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText}
	res, err := workflow.CheckTestIntegrity(context.Background(), j, "obj", workflow.SplitDiff(diff),
		workflow.IntegrityTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}
	if res.Attempted {
		t.Fatalf("a diff with no test hunks must never attempt a check: %+v", res)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("no candidate hunks means no call, got %d", len(j.Calls()))
	}
}

func TestCheckTestIntegrityDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText}
	res, _ := workflow.CheckTestIntegrity(context.Background(), j, "obj",
		workflow.SplitDiff(sampleDiff), workflow.IntegrityTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("a site with no configured tier must read as TierLogged, got %q", res.Tier)
	}
	if !judgment.TierLogged.Permits(judgment.TierLogged) {
		t.Fatalf("TierLogged must permit its own tier")
	}
	if judgment.TierLogged.Permits(judgment.TierOrdering) || judgment.TierLogged.Permits(judgment.TierRouting) {
		t.Fatalf("TierLogged must not permit ordering or routing effects")
	}
}
