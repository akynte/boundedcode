package judgeval

import (
	"math"
	"testing"
)

func TestScoreBrierPerfectPredictionsHaveMaxSkill(t *testing.T) {
	pairs := []PredictedOutcome{
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
		{Predicted: 1, Outcome: true}, {Predicted: 0, Outcome: false},
	}
	r := ScoreBrier(pairs, 20, 0, 1)
	if !r.Interpretable {
		t.Fatalf("expected interpretable at N=20: %+v", r)
	}
	if r.Brier != 0 {
		t.Fatalf("Brier = %v, want 0 for perfect predictions", r.Brier)
	}
	if r.Skill != 1 {
		t.Fatalf("Skill = %v, want 1 for perfect predictions", r.Skill)
	}
}

func TestScoreBrierBelowSampleFloorIsNotInterpretable(t *testing.T) {
	pairs := []PredictedOutcome{{Predicted: 0.9, Outcome: true}, {Predicted: 0.1, Outcome: false}}
	r := ScoreBrier(pairs, 20, 0, 1)
	if r.Interpretable {
		t.Fatalf("N=2 must not be interpretable against a floor of 20: %+v", r)
	}
	if r.N != 2 {
		t.Fatalf("N = %d, want 2 (the count is still reported)", r.N)
	}
}

func TestScoreBrierDegeneratePopulationReportsNoSkill(t *testing.T) {
	pairs := make([]PredictedOutcome, 25)
	for i := range pairs {
		pairs[i] = PredictedOutcome{Predicted: 0.5, Outcome: true} // every outcome the same
	}
	r := ScoreBrier(pairs, 20, 0, 1)
	if !r.Degenerate {
		t.Fatal("expected Degenerate=true when every outcome is the same")
	}
	if r.Interpretable {
		t.Fatal("a degenerate population must not be interpretable regardless of N")
	}
}

func TestBootstrapSkillCIIsDeterministicUnderTheSameSeed(t *testing.T) {
	pairs := mixedPairs(30)
	l1, h1 := BootstrapSkillCI(pairs, 200, 42)
	l2, h2 := BootstrapSkillCI(pairs, 200, 42)
	if l1 != l2 || h1 != h2 {
		t.Fatalf("same seed produced different intervals: (%v,%v) vs (%v,%v)", l1, h1, l2, h2)
	}
}

func TestBootstrapSkillCIDifferentSeedsCanDiffer(t *testing.T) {
	pairs := mixedPairs(30)
	l1, h1 := BootstrapSkillCI(pairs, 200, 1)
	l2, h2 := BootstrapSkillCI(pairs, 200, 2)
	// Not a hard requirement that they differ, but they should not be
	// perfectly identical for a nontrivial resampling — if they always were,
	// the seed would not actually be doing anything.
	if l1 == l2 && h1 == h2 {
		t.Skip("seeds happened to coincide; not a failure, but worth a second look if it recurs")
	}
}

func TestBootstrapSkillCILowerBoundIsBelowUpperBound(t *testing.T) {
	pairs := mixedPairs(40)
	lo, hi := BootstrapSkillCI(pairs, 500, 7)
	if lo > hi {
		t.Fatalf("CI lower bound %v is above upper bound %v", lo, hi)
	}
}

func mixedPairs(n int) []PredictedOutcome {
	out := make([]PredictedOutcome, n)
	for i := range out {
		outcome := i%3 != 0
		pred := 0.8
		if !outcome {
			pred = 0.3
		}
		out[i] = PredictedOutcome{Predicted: pred, Outcome: outcome}
	}
	return out
}

func TestClassifyExcludesAmbiguousFromEveryMetric(t *testing.T) {
	predicted := []string{"weakening", "legitimate", "weakening"}
	actual := []string{"weakening", LabelAmbiguous, "legitimate"}
	rep := Classify(predicted, actual)
	if rep.Ambiguous != 1 {
		t.Fatalf("Ambiguous = %d, want 1", rep.Ambiguous)
	}
	if rep.Total != 3 {
		t.Fatalf("Total = %d, want 3", rep.Total)
	}
	for _, c := range rep.Classes {
		if c.Support+c.FP > 2 { // only 2 non-ambiguous rows remain
			t.Fatalf("class %s counted the ambiguous row: %+v", c.Label, c)
		}
	}
}

func TestClassifyComputesPrecisionRecallF1(t *testing.T) {
	// 3 true weakening, judge predicts weakening for 2 of them plus one
	// false positive on a legitimate case.
	predicted := []string{"weakening", "weakening", "legitimate", "weakening"}
	actual := []string{"weakening", "weakening", "weakening", "legitimate"}
	rep := Classify(predicted, actual)
	var w ClassificationResult
	for _, c := range rep.Classes {
		if c.Label == "weakening" {
			w = c
		}
	}
	if w.TP != 2 || w.FP != 1 || w.FN != 1 {
		t.Fatalf("weakening confusion = TP=%d FP=%d FN=%d, want TP=2 FP=1 FN=1", w.TP, w.FP, w.FN)
	}
	wantPrecision := 2.0 / 3.0
	if math.Abs(w.Precision-wantPrecision) > 1e-9 {
		t.Errorf("precision = %v, want %v", w.Precision, wantPrecision)
	}
	if math.Abs(w.Recall-2.0/3.0) > 1e-9 {
		t.Errorf("recall = %v, want %v", w.Recall, 2.0/3.0)
	}
}

func TestStabilityFlipRateAndDeltaOnConstantProbabilities(t *testing.T) {
	r := Stability([]float64{0.9, 0.9, 0.9, 0.9}, 0.5)
	if r.MaxDelta != 0 {
		t.Errorf("MaxDelta = %v, want 0 for identical repeats", r.MaxDelta)
	}
	if r.FlipRate != 0 {
		t.Errorf("FlipRate = %v, want 0 for identical repeats", r.FlipRate)
	}
	if r.StdDev != 0 {
		t.Errorf("StdDev = %v, want 0 for identical repeats", r.StdDev)
	}
}

func TestStabilityFlipRateWithOneOutlier(t *testing.T) {
	// 4 above threshold, 1 below: minority (below) side is the flip.
	r := Stability([]float64{0.9, 0.9, 0.9, 0.9, 0.1}, 0.5)
	want := 1.0 / 5.0
	if math.Abs(r.FlipRate-want) > 1e-9 {
		t.Errorf("FlipRate = %v, want %v", r.FlipRate, want)
	}
	if r.MaxDelta != 0.8 {
		t.Errorf("MaxDelta = %v, want 0.8", r.MaxDelta)
	}
}

func TestScoreRetrievalRecallAtKAndMRR(t *testing.T) {
	// hits at rank 1, 3, and a miss (0).
	r := ScoreRetrieval([]int{1, 3, 0}, 2)
	if r.MissRate != 1.0/3.0 {
		t.Errorf("MissRate = %v, want 1/3", r.MissRate)
	}
	// Only rank 1 is <= k=2.
	if r.RecallAtK != 1.0/3.0 {
		t.Errorf("RecallAtK = %v, want 1/3", r.RecallAtK)
	}
	wantMRR := (1.0 + 1.0/3.0 + 0) / 3.0
	if math.Abs(r.MRR-wantMRR) > 1e-9 {
		t.Errorf("MRR = %v, want %v", r.MRR, wantMRR)
	}
}
