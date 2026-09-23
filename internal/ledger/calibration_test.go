package ledger_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
)

func TestRecordPredictionRequiresTaskSiteSubject(t *testing.T) {
	l, st := newLedger(t)
	_ = l
	c := ledger.NewCalibrationStore(st)
	if err := c.RecordPrediction(context.Background(), ledger.Prediction{}); err == nil {
		t.Fatalf("an empty prediction must be refused")
	}
}

func TestRecordOutcomeResolvesTheMostRecentUnresolvedPrediction(t *testing.T) {
	l, st := newLedger(t)
	_ = l
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-calib-1"

	if err := c.RecordPrediction(ctx, ledger.Prediction{
		TaskID: task, Site: "verification_integrity", Subject: "hunk:a_test.go", Predicted: 0.9,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	// A second, later judgment of the same subject — a re-verified attempt.
	if err := c.RecordPrediction(ctx, ledger.Prediction{
		TaskID: task, Site: "verification_integrity", Subject: "hunk:a_test.go", Predicted: 0.4,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := c.RecordOutcome(ctx, task, "verification_integrity", "hunk:a_test.go", true, "rejected at gate"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	pairs, err := c.Paired(ctx, "verification_integrity")
	if err != nil {
		t.Fatalf("paired: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("Paired = %+v, want exactly one resolved row", pairs)
	}
	// The most recent prediction (0.4) is the one that should have been
	// resolved, not the first (0.9): it describes the state the outcome
	// actually speaks to.
	if pairs[0].Predicted != 0.4 || !pairs[0].Outcome {
		t.Fatalf("resolved pair = %+v, want predicted=0.4 outcome=true", pairs[0])
	}

	pending, err := c.PendingCount(ctx, "verification_integrity")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("PendingCount = %d, want 1 (the first, still-unresolved prediction)", pending)
	}
}

func TestRecordOutcomeWithNoMatchingPredictionIsANoOp(t *testing.T) {
	l, st := newLedger(t)
	_ = l
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	if err := c.RecordOutcome(ctx, "no-such-task", "site", "subject", true, ""); err != nil {
		t.Fatalf("resolving with nothing to resolve must not error: %v", err)
	}
}

func TestResolveOpenForTaskResolvesEveryOpenPredictionForThatSite(t *testing.T) {
	l, st := newLedger(t)
	_ = l
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-calib-2"

	for _, subj := range []string{"hunk:a.go", "hunk:b.go", "hunk:c.go"} {
		if err := c.RecordPrediction(ctx, ledger.Prediction{
			TaskID: task, Site: "review_rubric", Subject: subj, Predicted: 0.6,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	// A prediction in a different task must not be touched.
	if err := c.RecordPrediction(ctx, ledger.Prediction{
		TaskID: "other-task", Site: "review_rubric", Subject: "hunk:a.go", Predicted: 0.6,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := c.ResolveOpenForTask(ctx, task, "review_rubric", false, "approved at gate"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	pairs, err := c.Paired(ctx, "review_rubric")
	if err != nil {
		t.Fatalf("paired: %v", err)
	}
	if len(pairs) != 3 {
		t.Fatalf("Paired = %+v, want the 3 predictions from the resolved task only", pairs)
	}
	for _, p := range pairs {
		if p.TaskID != task {
			t.Fatalf("a prediction from another task was resolved: %+v", p)
		}
		if p.Outcome {
			t.Fatalf("outcome should be false: %+v", p)
		}
	}

	pending, err := c.PendingCount(ctx, "review_rubric")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("PendingCount = %d, want 1 (the other task's prediction)", pending)
	}
}

func TestPairedIsScopedBySite(t *testing.T) {
	l, st := newLedger(t)
	_ = l
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()

	if err := c.RecordPrediction(ctx, ledger.Prediction{TaskID: "t", Site: "site_a", Subject: "s", Predicted: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := c.RecordOutcome(ctx, "t", "site_a", "s", true, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.RecordPrediction(ctx, ledger.Prediction{TaskID: "t", Site: "site_b", Subject: "s", Predicted: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := c.RecordOutcome(ctx, "t", "site_b", "s", false, ""); err != nil {
		t.Fatal(err)
	}

	pairsA, err := c.Paired(ctx, "site_a")
	if err != nil || len(pairsA) != 1 {
		t.Fatalf("Paired(site_a) = %+v, %v", pairsA, err)
	}
	pairsB, err := c.Paired(ctx, "site_b")
	if err != nil || len(pairsB) != 1 {
		t.Fatalf("Paired(site_b) = %+v, %v", pairsB, err)
	}
}

func TestNilCalibrationStoreIsInert(t *testing.T) {
	var c *ledger.CalibrationStore
	ctx := context.Background()
	if err := c.RecordPrediction(ctx, ledger.Prediction{TaskID: "t", Site: "s", Subject: "x", Predicted: 0.5}); err != nil {
		t.Fatalf("a nil store must not error on write: %v", err)
	}
	if err := c.RecordOutcome(ctx, "t", "s", "x", true, ""); err != nil {
		t.Fatalf("a nil store must not error on resolve: %v", err)
	}
	if err := c.ResolveOpenForTask(ctx, "t", "s", true, ""); err != nil {
		t.Fatalf("a nil store must not error on bulk resolve: %v", err)
	}
	pairs, err := c.Paired(ctx, "s")
	if err != nil || pairs != nil {
		t.Fatalf("a nil store must read back nothing: %+v, %v", pairs, err)
	}
}
