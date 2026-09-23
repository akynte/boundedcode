package ledger_test

import (
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
)

func TestCalibrateWithNoPairsIsEmptyNotPanic(t *testing.T) {
	r := ledger.Calibrate("site", nil, 3, 0)
	if r.Shadow.N != 0 || r.Pending != 3 {
		t.Fatalf("report = %+v", r)
	}
	if r.Shadow.Interpretable {
		t.Fatalf("an empty report must not claim to be interpretable")
	}
}

func TestCalibrateBelowSampleFloorReportsFactsNotSkill(t *testing.T) {
	pairs := []ledger.Pair{
		{Predicted: 0.9, Outcome: true},
		{Predicted: 0.1, Outcome: false},
	}
	r := ledger.Calibrate("site", pairs, 0, 0)
	if r.Shadow.N != 2 || r.Shadow.Positives != 1 {
		t.Fatalf("report = %+v", r)
	}
	if r.Shadow.Interpretable {
		t.Fatalf("2 paired rows is below MinCalibrationSample and must not be Interpretable")
	}
	if len(r.Shadow.Bins) != 0 {
		t.Fatalf("bins must be empty when not interpretable, got %d", len(r.Shadow.Bins))
	}
}

func TestCalibratePerfectPredictionsHaveZeroBrierAndPositiveSkill(t *testing.T) {
	var pairs []ledger.Pair
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		outcome := i%2 == 0
		predicted := 0.0
		if outcome {
			predicted = 1.0
		}
		pairs = append(pairs, ledger.Pair{Predicted: predicted, Outcome: outcome})
	}
	r := ledger.Calibrate("site", pairs, 0, 0)
	if !r.Shadow.Interpretable {
		t.Fatalf("a sample at the floor must be Interpretable")
	}
	if r.Shadow.Brier != 0 {
		t.Fatalf("Brier = %v, want 0 for perfect predictions", r.Shadow.Brier)
	}
	if r.Shadow.Skill <= 0 {
		t.Fatalf("Skill = %v, want positive: perfect predictions must beat the base rate", r.Shadow.Skill)
	}
	if r.Shadow.BaseRate != 0.5 {
		t.Fatalf("BaseRate = %v, want 0.5", r.Shadow.BaseRate)
	}
}

func TestADegeneratePopulationReportsNoSkillRatherThanZero(t *testing.T) {
	var pairs []ledger.Pair
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		pairs = append(pairs, ledger.Pair{Predicted: 0.9, Outcome: true})
	}
	r := ledger.Calibrate("site", pairs, 0, 0)
	if !r.Shadow.Degenerate {
		t.Fatalf("every outcome identical must be reported as degenerate: %+v", r.Shadow)
	}
	if r.Shadow.Interpretable {
		t.Fatalf("a degenerate population has no skill figure to interpret: %+v", r.Shadow)
	}
}

func TestCalibrateAlwaysGuessingTheBaseRateHasZeroSkill(t *testing.T) {
	var pairs []ledger.Pair
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		outcome := i%4 == 0 // base rate 0.25
		pairs = append(pairs, ledger.Pair{Predicted: 0.25, Outcome: outcome})
	}
	r := ledger.Calibrate("site", pairs, 0, 0)
	if r.Shadow.Skill < -0.001 || r.Shadow.Skill > 0.001 {
		t.Fatalf("Skill = %v, want ~0 when every prediction equals the base rate", r.Shadow.Skill)
	}
}

func TestReliabilityBinsPlacePredictionsByProbability(t *testing.T) {
	var pairs []ledger.Pair
	// Mostly true, so the top band's observed rate is high — but not all
	// true: a population whose outcomes never vary is Degenerate and
	// reports no bins, which is a different property tested elsewhere.
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		pairs = append(pairs, ledger.Pair{Predicted: 0.95, Outcome: i != 0})
	}
	r := ledger.Calibrate("site", pairs, 0, 10)
	if len(r.Shadow.Bins) != 10 {
		t.Fatalf("len(Bins) = %d, want 10", len(r.Shadow.Bins))
	}
	last := r.Shadow.Bins[len(r.Shadow.Bins)-1]
	if last.Count != len(pairs) {
		t.Fatalf("the top bin should hold every 0.95 prediction, got count=%d in %+v", last.Count, last)
	}
	if last.Observed <= 0.9 {
		t.Fatalf("Observed = %v, want the high rate these outcomes actually show", last.Observed)
	}
}

func TestCalibratePendingIsReportedSeparatelyFromN(t *testing.T) {
	pairs := make([]ledger.Pair, ledger.MinCalibrationSample)
	for i := range pairs {
		pairs[i] = ledger.Pair{Predicted: 0.5, Outcome: i%2 == 0}
	}
	r := ledger.Calibrate("site", pairs, 7, 0)
	if r.Shadow.N != ledger.MinCalibrationSample || r.Pending != 7 {
		t.Fatalf("report = %+v", r)
	}
}
