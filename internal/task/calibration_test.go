package task

// Proof that the calibration wiring is not just plumbing that compiles: a
// prediction made through a real hook (taskCard's hypothesis check) actually
// lands in a real, on-disk CalibrationStore with the values the call site
// recorded, and a later RecordOutcome/ResolveOpenForTask resolves it the way
// internal/ledger's own tests already prove that store does. Every other
// hook in phases.go follows the identical RecordPrediction/RecordOutcome
// pattern against the same store type, so this is the representative case
// rather than one of many that would each need the same scaffolding.

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

func newCalibrationTestStore(t *testing.T) *ledger.CalibrationStore {
	t.Helper()
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	id := workspace.DeriveID("/synthetic/root", "", "calibration-test")
	st, err := root.OpenWorkspace(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return ledger.NewCalibrationStore(st)
}

func TestTaskCardHypothesisCheckWritesARealPrediction(t *testing.T) {
	calib := newCalibrationTestStore(t)
	j := &judgment.Fake{
		Answers: map[string]judgment.Answer{"verdict": {Choice: "contradicted", Confidence: 0.87}},
	}
	r := &Runner{Judge: j, Calibration: calib}
	s := factHygieneTestState() // carries an obligation, so ConfirmedFacts is non-empty
	ctx := context.Background()

	card := r.taskCard(ctx, &Task{ID: "t-calib-integration", Title: "fix checkout"}, s)
	if card.Hypothesis == "" {
		t.Fatalf("HypothesisSite defaults to TierLogged; the card composition must not change " +
			"even though the underlying judgment ran and predicted a contradiction")
	}

	pending, err := calib.PendingCount(ctx, HypothesisSite)
	if err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("PendingCount(hypothesis_check) = %d, want 1: the prediction must be recorded "+
			"even though TierLogged left the card itself unchanged", pending)
	}

	// Resolve it the way stop() does, and confirm the exact value recorded
	// (0.87, the judged confidence) reads back correctly.
	if err := calib.ResolveOpenForTask(ctx, "t-calib-integration", HypothesisSite, true, "task failed"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	pairs, err := calib.Paired(ctx, HypothesisSite)
	if err != nil {
		t.Fatalf("paired: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("Paired = %+v, want one resolved row", pairs)
	}
	if pairs[0].Predicted != 0.87 || !pairs[0].Outcome || pairs[0].TaskID != "t-calib-integration" {
		t.Fatalf("resolved pair = %+v, want predicted=0.87 outcome=true task=t-calib-integration", pairs[0])
	}
}

func TestNilCalibrationStoreOnRunnerDoesNotPanicTaskCard(t *testing.T) {
	j := &judgment.Fake{
		Answers: map[string]judgment.Answer{"verdict": {Choice: "contradicted", Confidence: 0.9}},
	}
	r := &Runner{Judge: j} // Calibration left nil, the shipped default
	s := factHygieneTestState()
	// Must not panic: every calibration call in taskCard goes through
	// CalibrationStore's nil-receiver no-ops.
	_ = r.taskCard(context.Background(), &Task{ID: "t", Title: "fix checkout"}, s)
}
