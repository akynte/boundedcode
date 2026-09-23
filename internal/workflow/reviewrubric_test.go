package workflow_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

func TestCheckReviewRubricSkipsUnderStrictRedaction(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactStrict}
	res := workflow.CheckReviewRubric(context.Background(), j, "fix checkout",
		workflow.SplitDiff(conformanceDiff), workflow.ReviewRubricTuning{}, nil)
	if res.Attempted || res.SkipReason != "redact_strict" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckReviewRubricOrdersHunksByReadWeight(t *testing.T) {
	hunks := workflow.SplitDiff(conformanceDiff)
	answers := map[string]judgment.Answer{
		"depth": {Score: 2.0},
		// order.go (r0): trips several rubric Nouls.
		"exported_signature_r0": {Noul: 0.9},
		"error_handling_r0":     {Noul: 0.85},
		"concurrency_r0":        {Noul: 0.1},
		"hardcoded_config_r0":   {Noul: 0.1},
		"stub_r0":               {Noul: 0.1},
		"duplication_r0":        {Noul: 0.1},
		"outside_objective_r0":  {Noul: 0.1},
		// logging.go (r1): trips nothing.
		"exported_signature_r1": {Noul: 0.05},
		"error_handling_r1":     {Noul: 0.05},
		"concurrency_r1":        {Noul: 0.05},
		"hardcoded_config_r1":   {Noul: 0.05},
		"stub_r1":               {Noul: 0.05},
		"duplication_r1":        {Noul: 0.05},
		"outside_objective_r1":  {Noul: 0.05},
	}
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: answers}
	res := workflow.CheckReviewRubric(context.Background(), j, "reject empty carts", hunks,
		workflow.ReviewRubricTuning{}, nil)

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if !res.ReadDepthAnswered || res.ReadDepth != 2.0 {
		t.Fatalf("ReadDepth = %v (answered=%v), want 2.0/true", res.ReadDepth, res.ReadDepthAnswered)
	}
	if res.ReadDepthLegend == "" {
		t.Fatalf("ReadDepthLegend must not be empty when ReadDepthAnswered")
	}
	if len(res.Findings) != 2 {
		t.Fatalf("Findings = %+v, want 2", res.Findings)
	}
	// order.go's hunk must sort first: it has the higher read weight.
	if res.Findings[0].Path != "pkg/order/order.go" {
		t.Fatalf("Findings not ordered by read weight: %+v", res.Findings)
	}
	if res.Findings[0].ReadWeight < res.Findings[1].ReadWeight {
		t.Fatalf("ReadWeight ordering inverted: %+v", res.Findings)
	}
}

func TestCheckReviewRubricDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: map[string]judgment.Answer{"depth": {Score: 1}}}
	res := workflow.CheckReviewRubric(context.Background(), j, "obj", workflow.SplitDiff(conformanceDiff),
		workflow.ReviewRubricTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged", res.Tier)
	}
}
