package workflow_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func failureResult(recipeName, headline string, findings ...recipe.Finding) recipe.Result {
	return recipe.Result{Recipe: recipeName, Status: recipe.Fail,
		Summary: recipe.Summary{Headline: headline, Findings: findings}}
}

func TestTriageFailuresSkipsBelowOutputTier(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText}
	res, err := workflow.TriageFailures(context.Background(), j, "fix checkout",
		[]recipe.Result{failureResult("go test", "FAIL")}, nil, "", workflow.TriageTuning{}, nil)
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureRefused {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureRefused)
	}
	if res.Attempted {
		t.Fatalf("output tier is required; result = %+v", res)
	}
	if res.SkipReason != "redact_below_output" {
		t.Fatalf("SkipReason = %q, want redact_below_output", res.SkipReason)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("must place no call below output tier")
	}
}

func TestTriageFailuresClassifiesACategory(t *testing.T) {
	j := &judgment.Fake{
		RedactMode: judgment.RedactOutput,
		Answers: map[string]judgment.Answer{
			"t0":     {Choice: "build_break", Confidence: 0.95},
			"obj_t0": {Noul: 0.1},
		},
	}
	res, err := workflow.TriageFailures(context.Background(), j, "fix checkout",
		[]recipe.Result{failureResult("go build", "undefined: Foo",
			recipe.Finding{File: "order.go", Line: 12, Message: "undefined: Foo"})},
		nil, "/repo", workflow.TriageTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("Findings = %+v, want one", res.Findings)
	}
	f := res.Findings[0]
	if f.Category != workflow.TriageBuildBreak || f.CategoryConfidence != 0.95 {
		t.Fatalf("finding = %+v", f)
	}
	if f.HasSameCause {
		t.Fatalf("no previous record was supplied; HasSameCause must be false: %+v", f)
	}

	// The raw finding text must have gone out as output, never as repo_text.
	if got := j.RepoTextSent(); got != 0 {
		t.Fatalf("repo_text fields sent = %d, want 0", got)
	}
	if got := j.OutputSent(); got == 0 {
		t.Fatalf("output fields sent = %d, want at least one", got)
	}
}

func TestTriageFailuresAsksSameCauseAgainstADifferentFingerprintSharingASymbol(t *testing.T) {
	j := &judgment.Fake{
		RedactMode: judgment.RedactOutput,
		Answers: map[string]judgment.Answer{
			"t0":      {Choice: "code_defect", Confidence: 0.8},
			"obj_t0":  {Noul: 0.2},
			"same_t0": {Noul: 0.92},
		},
	}
	current := failureResult("go test", "panic: nil pointer at order.go:50",
		recipe.Finding{File: "order.go", Line: 50, Message: "panic: nil pointer"})
	previous := []workflow.FailureRecord{
		{Fingerprint: "different-fp", Recipe: "go test", Symbols: []string{"Checkout"},
			Headline: "panic: nil pointer at order.go:41"},
	}
	res, err := workflow.TriageFailures(context.Background(), j, "fix checkout",
		[]recipe.Result{current}, previous, "", workflow.TriageTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}

	if len(res.Findings) != 1 {
		t.Fatalf("Findings = %+v", res.Findings)
	}
	f := res.Findings[0]
	// The test fixture only guarantees a recipe match, not a symbol overlap
	// (symbols require tree-sitter, unavailable in this unit test), so
	// same-cause is asked via the recency fallback either way. What matters
	// here is that it was asked at all and the answer was captured.
	if !f.HasSameCause || f.SameCause != 0.92 {
		t.Fatalf("finding = %+v, want same-cause asked and captured", f)
	}
}

func TestTriageFailuresSkipsSameCauseAgainstAnIdenticalFingerprint(t *testing.T) {
	j := &judgment.Fake{
		RedactMode: judgment.RedactOutput,
		Answers:    map[string]judgment.Answer{"t0": {Choice: "code_defect", Confidence: 0.8}, "obj_t0": {Noul: 0.1}},
	}
	current := failureResult("go test", "same headline")
	// A record with the *same* fingerprint the current failure would produce
	// must never trigger a same-cause question: Classify has already called
	// that SAME with certainty.
	same := workflow.Classify(current, nil, nil, "")
	previous := []workflow.FailureRecord{same}
	res, err := workflow.TriageFailures(context.Background(), j, "obj", []recipe.Result{current},
		previous, "", workflow.TriageTuning{}, nil)
	if err != nil {
		t.Fatalf("unexpected requirement failure: %v", err)
	}
	if len(res.Findings) != 1 || res.Findings[0].HasSameCause {
		t.Fatalf("an identical fingerprint must not trigger same-cause: %+v", res.Findings)
	}
}

func TestTriageFailuresDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactOutput,
		Answers: map[string]judgment.Answer{"t0": {Choice: "code_defect", Confidence: 0.8}, "obj_t0": {Noul: 0.1}}}
	res, _ := workflow.TriageFailures(context.Background(), j, "obj",
		[]recipe.Result{failureResult("go test", "x")}, nil, "", workflow.TriageTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged", res.Tier)
	}
}
