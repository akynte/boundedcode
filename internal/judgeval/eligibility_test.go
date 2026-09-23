package judgeval

import (
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

func integrityInfo(t *testing.T) judgment.SiteInfo {
	t.Helper()
	info, ok := judgment.Site(workflow.IntegritySite)
	if !ok {
		t.Fatal("verification_integrity is not registered")
	}
	return info
}

func TestEvaluateWithNoPolicySetIsIneligible(t *testing.T) {
	info := integrityInfo(t)
	e := Evaluate(EligibilityInput{
		Site:   info,
		Policy: DefaultPolicy(info), // every threshold unconfigured
	})
	if e.OrderingEligible {
		t.Fatal("must not be ordering-eligible with an unconfigured policy")
	}
	if e.RoutingEligible {
		t.Fatal("must not be routing-eligible with an unconfigured policy")
	}
	if len(e.OrderingReasons) == 0 {
		t.Fatal("expected reasons explaining ineligibility")
	}
}

func TestEvaluateSafetyViolationBlocksRegardlessOfEvidence(t *testing.T) {
	info := integrityInfo(t)
	policy := fullyPassingPolicy(info)
	e := Evaluate(EligibilityInput{
		Site: info, Policy: policy,
		HasHeldoutRun:      true,
		HeldoutShadow:      strongBrierResult(),
		Stability:          &StabilityResult{FlipRate: 0},
		MeasuredDistractor: true, DistractorDelta: 0,
		MeasuredLatency: true, LatencyP95Seconds: 0.1,
		MeasuredBaselineCompare: true, BaselineBeaten: true,
		SafetyViolations: []string{"a hypothetical path accepted a task"},
	})
	if !e.SafetyBlocked {
		t.Fatal("a safety violation must block eligibility regardless of every other figure")
	}
	if e.OrderingEligible || e.RoutingEligible {
		t.Fatal("safety-blocked eligibility must not report eligible for anything")
	}
}

func TestEvaluateAllRequirementsMetIsEligible(t *testing.T) {
	info := integrityInfo(t)
	policy := fullyPassingPolicy(info)
	e := Evaluate(EligibilityInput{
		Site: info, Policy: policy,
		HasHeldoutRun:      true,
		HeldoutShadow:      strongBrierResult(),
		Stability:          &StabilityResult{FlipRate: 0},
		MeasuredDistractor: true, DistractorDelta: 0,
		MeasuredLatency: true, LatencyP95Seconds: 0.1,
		MeasuredBaselineCompare: true, BaselineBeaten: true,
	})
	if !e.OrderingEligible {
		t.Fatalf("expected ordering-eligible with every requirement met: %+v", e.OrderingReasons)
	}
	if !e.RoutingEligible {
		t.Fatalf("expected routing-eligible with every requirement met: %+v", e.RoutingReasons)
	}
}

func TestEvaluateUsesLowerConfidenceBoundNotPointEstimate(t *testing.T) {
	info := integrityInfo(t)
	policy := fullyPassingPolicy(info)
	policy.MinSkillLowerBound = Threshold{Value: 0, RequiresSelection: false}

	br := strongBrierResult()
	br.Skill = 0.9       // a strong point estimate...
	br.SkillCILow = -0.1 // ...but the CI crosses zero.

	e := Evaluate(EligibilityInput{
		Site: info, Policy: policy,
		HasHeldoutRun: true, HeldoutShadow: br,
		Stability:          &StabilityResult{FlipRate: 0},
		MeasuredDistractor: true, MeasuredLatency: true, LatencyP95Seconds: 0.1,
		MeasuredBaselineCompare: true, BaselineBeaten: true,
	})
	if e.OrderingEligible {
		t.Fatal("a negative CI lower bound must block eligibility even with a strong point estimate")
	}
}

func TestEvaluateSiteAtOrderingCeilingReportsRoutingNotApplicable(t *testing.T) {
	// review_rubric's MaxEffect is ordering, not routing.
	info, ok := judgment.Site(workflow.ReviewRubricSite)
	if !ok {
		t.Fatal("review_rubric is not registered")
	}
	e := Evaluate(EligibilityInput{Site: info, Policy: DefaultPolicy(info)})
	if e.RoutingEligible {
		t.Fatal("a site whose own code implements no routing effect must never be routing-eligible")
	}
	if len(e.RoutingReasons) == 0 {
		t.Fatal("expected a reason explaining routing does not apply to this site")
	}
}

func fullyPassingPolicy(info judgment.SiteInfo) SitePolicy {
	p := DefaultPolicy(info)
	p.MinPromotionSamples = Threshold{Value: 100, RequiresSelection: false}
	p.MinSkillLowerBound = Threshold{Value: 0, RequiresSelection: false}
	p.MaxFlipRate = Threshold{Value: 0.1, RequiresSelection: false}
	p.MaxDistractorDelta = Threshold{Value: 0.1, RequiresSelection: false}
	p.MaxLatencyP95Seconds = Threshold{Value: 5, RequiresSelection: false}
	return p
}

func strongBrierResult() BrierResult {
	return BrierResult{
		N: 150, Positives: 60, BaseRate: 0.4,
		Brier: 0.05, BrierBaseline: 0.24, Skill: 0.79,
		SkillCILow: 0.5, SkillCIHigh: 0.9,
		Interpretable: true,
	}
}
