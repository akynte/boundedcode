package judgeval

import (
	"fmt"

	"github.com/akynte/boundedcode/internal/judgment"
)

// Eligibility is the advisory report design instruction §15 asks for: what
// a site's evidence supports, never a configuration change. Nothing in this
// package, or anywhere else in this campaign, writes judgment.yaml.
type Eligibility struct {
	Site            string        `json:"site"`
	CurrentTier     judgment.Tier `json:"current_tier"`
	DeclaredCeiling judgment.Tier `json:"declared_ceiling"`

	OrderingEligible bool     `json:"ordering_eligible"`
	OrderingReasons  []string `json:"ordering_reasons,omitempty"`

	RoutingEligible bool     `json:"routing_eligible"`
	RoutingReasons  []string `json:"routing_reasons,omitempty"`

	// SafetyBlocked is true if a structural safety check failed (§26). It
	// overrides both eligibility flags regardless of every other figure:
	// "safety failure is not a metric tradeoff. It is an absolute blocker."
	SafetyBlocked bool     `json:"safety_blocked,omitempty"`
	SafetyReasons []string `json:"safety_reasons,omitempty"`
}

// EligibilityInput is everything a promotion decision reads, gathered from
// wherever it actually lives (the live registry, the shadow calibration
// report, a held-out campaign run) so Evaluate stays pure arithmetic over
// already-computed evidence rather than reaching into a store itself.
type EligibilityInput struct {
	Site   judgment.SiteInfo
	Policy SitePolicy

	// HeldoutShadow is the held-out, non-intervened Brier result — the
	// evidence a promotion decision reads, per design instruction §10 and
	// the existing shadow/operational split in internal/ledger. A zero
	// value (N: 0) is the honest state "no held-out campaign has been run."
	HeldoutShadow BrierResult
	// HasHeldoutRun says whether a held-out campaign actually ran, as
	// distinct from "ran and produced 0 usable rows" — Evaluate needs to
	// tell "not attempted" from "attempted, insufficient" apart in its
	// reasons.
	HasHeldoutRun bool

	// Stability is nil when no repeat-evaluation trial was run.
	Stability *StabilityResult
	// DistractorDelta is the max probability movement observed under a
	// permutation/distractor perturbation trial (§17, §28); NaN-equivalent
	// "not measured" is represented by MeasuredDistractor being false.
	DistractorDelta         float64
	MeasuredDistractor      bool
	LatencyP95Seconds       float64
	MeasuredLatency         bool
	BaselineBeaten          bool
	MeasuredBaselineCompare bool

	// SafetyViolations lists any monotone-scrutiny violation this site's
	// evaluation surfaced (§26). Ordinarily empty: the tier system's own
	// structural invariants (internal/judgment/monotone_test.go) already
	// make most of these impossible to represent, so this field exists to
	// carry an evaluation-time finding, not to duplicate that test.
	SafetyViolations []string
}

// Evaluate computes Eligibility from EligibilityInput. It never promotes
// anything; it is a pure function from evidence to a report.
func Evaluate(in EligibilityInput) Eligibility {
	e := Eligibility{
		Site: in.Site.Name, CurrentTier: judgment.TierLogged,
		DeclaredCeiling: in.Site.MaxEffect,
	}
	if len(in.SafetyViolations) > 0 {
		e.SafetyBlocked = true
		e.SafetyReasons = append([]string(nil), in.SafetyViolations...)
		return e // absolute blocker — §26; nothing below is evaluated.
	}

	orderingOK, orderingReasons := evaluateTier(in, judgment.TierOrdering)
	e.OrderingEligible = orderingOK
	e.OrderingReasons = orderingReasons

	if in.Site.MaxEffect != judgment.TierRouting {
		e.RoutingReasons = []string{fmt.Sprintf(
			"site's own code implements no effect above %q; routing is not applicable", in.Site.MaxEffect)}
	} else {
		routingOK, routingReasons := evaluateTier(in, judgment.TierRouting)
		e.RoutingEligible = routingOK
		e.RoutingReasons = routingReasons
	}
	return e
}

// evaluateTier checks every requirement for one candidate tier and returns
// whether all are satisfied, plus the human-readable reason for every one
// that is not — satisfied requirements are silent, so a report reads as a
// short list of what is actually blocking, matching design instruction
// §15's example output.
func evaluateTier(in EligibilityInput, tier judgment.Tier) (bool, []string) {
	var reasons []string
	ok := true
	fail := func(format string, args ...any) {
		ok = false
		reasons = append(reasons, fmt.Sprintf(format, args...))
	}

	if !in.Site.MaxEffect.Permits(tier) {
		fail("site's own code implements no effect above %q", in.Site.MaxEffect)
	}

	if !in.HasHeldoutRun {
		fail("no held-out campaign has been run for this site")
	} else {
		if !in.Policy.MinPromotionSamples.Configured() {
			fail("minimum_promotion_samples is not configured in policy.yaml (%s)",
				in.Policy.MinPromotionSamples.Note)
		} else if in.HeldoutShadow.N < int(in.Policy.MinPromotionSamples.Value) {
			fail("held-out shadow N=%d is below the configured minimum_promotion_samples=%d",
				in.HeldoutShadow.N, int(in.Policy.MinPromotionSamples.Value))
		}
		if in.HeldoutShadow.Degenerate {
			fail("held-out shadow population is degenerate (every outcome the same); skill is undefined")
		} else if !in.HeldoutShadow.Interpretable && in.HeldoutShadow.N > 0 {
			fail("held-out shadow N=%d is below the reporting floor of %d",
				in.HeldoutShadow.N, in.Policy.MinReportableSamples)
		}
		if !in.Policy.MinSkillLowerBound.Configured() {
			fail("min_skill_lower_bound is not configured in policy.yaml (%s)",
				in.Policy.MinSkillLowerBound.Note)
		} else if in.HeldoutShadow.Interpretable &&
			in.HeldoutShadow.SkillCILow <= in.Policy.MinSkillLowerBound.Value {
			fail("lower bound of the Brier skill confidence interval (%.3f) does not clear "+
				"the configured floor (%.3f) — the point estimate (%.3f) is not the criterion",
				in.HeldoutShadow.SkillCILow, in.Policy.MinSkillLowerBound.Value, in.HeldoutShadow.Skill)
		}
	}

	if in.Policy.RequireBaselineBeaten {
		if !in.MeasuredBaselineCompare {
			fail("no deterministic-baseline comparison has been run, and this site's policy requires beating it")
		} else if !in.BaselineBeaten {
			fail("the Jev arm did not beat the deterministic baseline arm")
		}
	}

	if in.Stability != nil {
		if !in.Policy.MaxFlipRate.Configured() {
			fail("max_flip_rate is not configured in policy.yaml (%s)", in.Policy.MaxFlipRate.Note)
		} else if in.Stability.FlipRate > in.Policy.MaxFlipRate.Value {
			fail("repeat-evaluation flip rate (%.3f) exceeds the configured maximum (%.3f)",
				in.Stability.FlipRate, in.Policy.MaxFlipRate.Value)
		}
	} else {
		fail("no repeat-evaluation stability trial has been run")
	}

	if in.MeasuredDistractor {
		if !in.Policy.MaxDistractorDelta.Configured() {
			fail("max_distractor_delta is not configured in policy.yaml (%s)", in.Policy.MaxDistractorDelta.Note)
		} else if in.DistractorDelta > in.Policy.MaxDistractorDelta.Value {
			fail("distractor/permutation probability delta (%.3f) exceeds the configured maximum (%.3f)",
				in.DistractorDelta, in.Policy.MaxDistractorDelta.Value)
		}
	} else {
		fail("no distractor/permutation stability trial has been run")
	}

	if in.MeasuredLatency {
		if !in.Policy.MaxLatencyP95Seconds.Configured() {
			fail("max_latency_p95_seconds is not configured in policy.yaml (%s)", in.Policy.MaxLatencyP95Seconds.Note)
		} else if in.LatencyP95Seconds > in.Policy.MaxLatencyP95Seconds.Value {
			fail("p95 latency (%.2fs) exceeds the configured maximum (%.2fs)",
				in.LatencyP95Seconds, in.Policy.MaxLatencyP95Seconds.Value)
		}
	} else {
		fail("latency has not been measured")
	}

	return ok, reasons
}
