package task

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

func waivedPlan(reason string) workflow.Plan {
	return workflow.Plan{
		RootCause: "raise the daily transfer limit",
		Obligations: []workflow.Obligation{
			{
				Symbol: "Validate", Path: "internal/billing/validate.go",
				Reason: "calls DailyLimit",
				Resolution: workflow.Resolution{
					Action: workflow.ActionNoChange, Reason: reason,
				},
			},
		},
	}
}

func waivedImpact() *graph.Impact {
	return &graph.Impact{
		Consumers: []graph.Consumer{
			{Node: graph.Node{
				Kind: graph.KindFunction, Name: "Validate", FQN: "Validate",
				Path: "internal/billing/validate.go", StartLine: 12, EndLine: 20,
			}},
		},
	}
}

func TestCheckObligationReasonsSkipsWithNoWaivers(t *testing.T) {
	j := &judgment.Fake{}
	res, err := CheckObligationReasons(context.Background(), j, "obj", workflow.Plan{}, waivedImpact(), nil)
	if err != nil {
		t.Fatalf("a plan with no waivers has nothing to decide: %v", err)
	}
	if res.Attempted {
		t.Fatalf("a plan with no waivers must never attempt a check: %+v", res)
	}
	if res.SkipReason != "no_waivers" {
		t.Fatalf("SkipReason = %q, want no_waivers", res.SkipReason)
	}
}

func TestCheckObligationReasonsSkipsWithNilImpact(t *testing.T) {
	j := &judgment.Fake{}
	res, err := CheckObligationReasons(context.Background(), j, "obj", waivedPlan("because"), nil, nil)
	if err != nil {
		t.Fatalf("no impact has nothing to decide: %v", err)
	}
	if res.Attempted || res.SkipReason != "no_impact" {
		t.Fatalf("result = %+v", res)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("nil impact must place no call")
	}
}

func TestCheckObligationReasonsFlagsAVagueReason(t *testing.T) {
	j := &judgment.Fake{
		Answers: map[string]judgment.Answer{"c0": {Noul: 0.1}},
	}
	res, err := CheckObligationReasons(context.Background(), j, "raise the daily transfer limit",
		waivedPlan("not related"), waivedImpact(), nil)
	if err != nil {
		t.Fatalf("CheckObligationReasons: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if res.Total != 1 || res.Judged != 1 {
		t.Fatalf("Total=%d Judged=%d, want 1 and 1", res.Total, res.Judged)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("Findings = %+v, want one", res.Findings)
	}
	if res.Findings[0].Symbol != "Validate" || res.Findings[0].Path != "internal/billing/validate.go" {
		t.Fatalf("finding names the wrong obligation: %+v", res.Findings[0])
	}

	// Strict mode: only path/symbol/kind/lines and the reason text may have
	// gone out, never a signature or a file body.
	if got := j.RepoTextSent(); got != 0 {
		t.Fatalf("strict mode sent %d repo_text field(s), want 0", got)
	}
}

func TestCheckObligationReasonsAcceptsASpecificReason(t *testing.T) {
	j := &judgment.Fake{
		Answers: map[string]judgment.Answer{"c0": {Noul: 0.9}},
	}
	res, err := CheckObligationReasons(context.Background(), j, "raise the daily transfer limit",
		waivedPlan("Validate reads the limit through DailyLimit(), which this change updates "+
			"without altering Validate's signature or control flow"),
		waivedImpact(), nil)
	if err != nil {
		t.Fatalf("CheckObligationReasons: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("a judged-plausible reason must not be flagged: %+v", res.Findings)
	}
	if res.Judged != 1 {
		t.Fatalf("Judged = %d, want 1", res.Judged)
	}
}

func TestCheckObligationReasonsSkipsAnEmptyReason(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.1}}}
	res, err := CheckObligationReasons(context.Background(), j, "obj", waivedPlan(""), waivedImpact(), nil)
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureRefused {
		t.Fatalf("a waiver this site cannot ask about is a local refusal; class = %q (ok %v)", class, ok)
	}
	if res.Applied {
		t.Fatalf("an empty reason has nothing to judge; result = %+v", res)
	}
	if res.SkipReason != "no_eligible_waivers" {
		t.Fatalf("SkipReason = %q, want no_eligible_waivers", res.SkipReason)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("an empty reason must place no call")
	}
}

func TestCheckObligationReasonsIgnoresUnwaivedObligations(t *testing.T) {
	plan := workflow.Plan{
		Obligations: []workflow.Obligation{
			{
				Symbol: "Validate", Path: "internal/billing/validate.go",
				Resolution: workflow.Resolution{Action: workflow.ActionEdit},
			},
		},
	}
	j := &judgment.Fake{}
	res, err := CheckObligationReasons(context.Background(), j, "obj", plan, waivedImpact(), nil)
	if err != nil {
		t.Fatalf("an edit-resolved obligation is not a waiver, so nothing is decided: %v", err)
	}
	if res.Attempted || res.Total != 0 {
		t.Fatalf("an edit-resolved obligation is not a waiver; result = %+v", res)
	}
}

func TestCheckObligationReasonsDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.1}}}
	res, _ := CheckObligationReasons(context.Background(), j, "raise the daily transfer limit",
		waivedPlan("not related"), waivedImpact(), nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged (no site configuration was given)", res.Tier)
	}
}
