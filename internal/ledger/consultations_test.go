package ledger_test

// The end-to-end property: a clean consultation reaches SQL and is readable as
// a denominator, and a finding is readable as a numerator over it.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
)

func TestCleanAndFindingConsultationsBothReachTheDenominator(t *testing.T) {
	ctx := context.Background()
	_, st := newLedger(t)
	cs := ledger.NewConsultationStore(st)

	// Three consultations of one site: two clean, one with a finding. Before
	// this table only the third left any trace, so the site's finding rate
	// read as 1/1 rather than the true 1/3.
	for i, rec := range []ledger.Consultation{
		{ID: "c1", TaskID: "t-1", Site: "verification_integrity", Phase: "VERIFY",
			Reached: true, Requested: true, Status: "live", Questions: 4, Findings: 0,
			Model: "jev-1.13.0", Latency: 900 * time.Millisecond},
		{ID: "c2", TaskID: "t-1", Site: "verification_integrity", Phase: "VERIFY",
			Reached: true, Requested: true, Status: "live", Questions: 2, Findings: 0,
			Model: "jev-1.13.0", Latency: 1100 * time.Millisecond},
		{ID: "c3", TaskID: "t-2", Site: "verification_integrity", Phase: "VERIFY",
			Reached: true, Requested: true, Status: "live", Questions: 6, Findings: 2,
			Model: "jev-1.13.0", Latency: 1300 * time.Millisecond,
			PredictionIDs: []string{"p1", "p2"}},
	} {
		if err := cs.Write(ctx, rec); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// A site reached that declined before asking: a row, not an absence.
	if err := cs.Write(ctx, ledger.Consultation{
		ID: "c4", TaskID: "t-2", Site: "failure_triage", Phase: "VERIFY",
		Reached: true, Requested: false, Status: "skipped", SkipReason: "no_failures",
	}); err != nil {
		t.Fatalf("write skip: %v", err)
	}

	totals, err := cs.Totals(ctx)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}

	vi := totals["verification_integrity"]
	if vi == nil {
		t.Fatal("no totals for verification_integrity")
	}
	if vi.Reached != 3 {
		t.Errorf("denominator = %d, want 3", vi.Reached)
	}
	if vi.Requested != 3 {
		t.Errorf("requested = %d, want 3", vi.Requested)
	}
	if vi.WithFinding != 1 {
		t.Errorf("consultations with a finding = %d, want 1", vi.WithFinding)
	}
	if vi.Findings != 2 {
		t.Errorf("findings = %d, want 2", vi.Findings)
	}
	if vi.Questions != 12 {
		t.Errorf("questions = %d, want 12", vi.Questions)
	}
	if vi.ByStatus["live"] != 3 {
		t.Errorf("live = %d, want 3", vi.ByStatus["live"])
	}

	ft := totals["failure_triage"]
	if ft == nil {
		t.Fatal("a site that skipped before asking left no row")
	}
	if ft.Reached != 1 || ft.Requested != 0 {
		t.Errorf("reached=%d requested=%d, want 1/0", ft.Reached, ft.Requested)
	}
	if ft.BySkip["no_failures"] != 1 {
		t.Errorf("skip reason not recorded: %v", ft.BySkip)
	}

	// A site never reached has no row at all, which is the one meaning that
	// absence is allowed to carry.
	if _, ok := totals["review_rubric"]; ok {
		t.Error("a site that never ran has a row")
	}
}

func TestPredictionLinksToItsConsultation(t *testing.T) {
	ctx := context.Background()
	_, st := newLedger(t)
	cal := ledger.NewCalibrationStore(st)
	if err := cal.RecordPrediction(ctx, ledger.Prediction{
		TaskID: "t-1", Site: "diff_conformance", Subject: "hunk:a.go",
		Predicted: 0.8, ConsultationID: "c9",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	var got string
	if err := st.Ledger().ReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT consultation_id FROM judgment_predictions WHERE site = ?`,
			"diff_conformance").Scan(&got)
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "c9" {
		t.Errorf("consultation_id = %q, want c9", got)
	}
}

// Observation must never be able to stop a task: a store with no database
// accepts a record and reports nothing.
func TestObservationIsBestEffort(t *testing.T) {
	var cs *ledger.ConsultationStore
	cs.Consultation(context.Background(), ledger.Consultation{Site: "x"})
	if err := cs.Write(context.Background(), ledger.Consultation{Site: "x"}); err != nil {
		t.Errorf("nil store returned %v, want nil", err)
	}
}
