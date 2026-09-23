package ledger_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
)

// Shadow and operational evidence, kept apart.
//
// The reason this is a test rather than a comment: once a site is promoted,
// its own effect can move the outcome it is later scored against. A hunk
// tainted because a judgment called it a weakening draws the attention that
// gets it rejected, and the site is then scored as having predicted a
// rejection it helped cause. A calibration report that pooled the two would
// read as evidence for a promotion it partly caused.

func TestShadowAndOperationalRowsAreScoredApart(t *testing.T) {
	ctx := context.Background()
	var pairs []ledger.Pair
	// Shadow: a site that is confidently wrong most of the time, observed
	// without intervening. Not all-false: a population whose outcomes never
	// vary has no skill figure to beat, which Population.Degenerate reports.
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		pairs = append(pairs, ledger.Pair{Predicted: 0.9, Outcome: i%5 == 0})
	}
	// Operational: the same site, apparently much better, on rows where its
	// own effect was applied — the self-confirming shape this separation
	// exists to keep out of the promotion decision.
	for i := 0; i < ledger.MinCalibrationSample; i++ {
		pairs = append(pairs, ledger.Pair{Predicted: 0.9, Outcome: i%5 != 0, Intervened: true})
	}
	_ = ctx

	rep := ledger.Calibrate("site", pairs, 0, 0)
	if rep.Shadow.N != ledger.MinCalibrationSample || rep.Operational.N != ledger.MinCalibrationSample {
		t.Fatalf("populations = shadow %d, operational %d; want %d each",
			rep.Shadow.N, rep.Operational.N, ledger.MinCalibrationSample)
	}
	if rep.Shadow.Skill >= 0 {
		t.Fatalf("shadow skill = %v; a site that is confidently wrong must not score positive",
			rep.Shadow.Skill)
	}
	if rep.Operational.Skill == rep.Shadow.Skill {
		t.Fatalf("the two populations produced the same figure; they are not being scored apart")
	}
}

func TestPredictionsRecordTheAuthorityAndInterventionTheyWereMadeUnder(t *testing.T) {
	_, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-contamination"

	if err := c.RecordPrediction(ctx, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "s", Predicted: 0.8,
		Model: "jev-1.13.0", SiteVersion: "2", TierAtPrediction: "routing",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := c.MarkIntervened(ctx, task, "site_a", "s"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := c.RecordOutcome(ctx, task, "site_a", "s", true, "gate"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	pairs, err := c.Paired(ctx, "site_a")
	if err != nil || len(pairs) != 1 {
		t.Fatalf("Paired = %+v, %v", pairs, err)
	}
	p := pairs[0]
	if p.Model != "jev-1.13.0" || p.SiteVersion != "2" || p.TierAtPrediction != "routing" {
		t.Fatalf("provenance not round-tripped: %+v", p)
	}
	if !p.Intervened {
		t.Fatalf("a row whose effect was applied must come back marked intervened: %+v", p)
	}

	rep := ledger.Calibrate("site_a", pairs, 0, 0)
	if rep.Shadow.N != 0 || rep.Operational.N != 1 {
		t.Fatalf("an intervened row must be scored as operational, not shadow: %+v", rep)
	}
}

func TestCalibrateReportsWhenItIsPoolingModelVersions(t *testing.T) {
	pairs := []ledger.Pair{
		{Predicted: 0.5, Outcome: true, Model: "jev-1.13.0", SiteVersion: "1"},
		{Predicted: 0.5, Outcome: false, Model: "jev-1.14.0", SiteVersion: "1"},
	}
	rep := ledger.Calibrate("site", pairs, 0, 0)
	if len(rep.Versions) != 2 {
		t.Fatalf("Versions = %+v; a pooled report must say which combinations it pooled", rep.Versions)
	}
}

func TestCalibrateGroupedPartitionsByModelAndQuestionVersion(t *testing.T) {
	pairs := []ledger.Pair{
		{Predicted: 0.9, Outcome: true, Model: "jev-1.13.0", SiteVersion: "1"},
		{Predicted: 0.9, Outcome: true, Model: "jev-1.13.0", SiteVersion: "1"},
		{Predicted: 0.1, Outcome: false, Model: "jev-1.14.0", SiteVersion: "2"},
	}
	reports := ledger.CalibrateGrouped("site", pairs, 0, 0)
	if len(reports) != 2 {
		t.Fatalf("got %d groups, want one per (model, version)", len(reports))
	}
	for _, rep := range reports {
		if len(rep.Versions) != 1 {
			t.Fatalf("a grouped report must describe exactly one combination: %+v", rep.Versions)
		}
	}
	if reports[0].Versions[0].N+reports[1].Versions[0].N != len(pairs) {
		t.Fatalf("grouping lost or duplicated rows")
	}
}

// Equal timestamps must still resolve deterministically. This is the
// regression the rowid tiebreak was introduced for and that seq now owns:
// several predictions for one subject recorded inside a single millisecond
// is the ordinary case when a call site loops over findings.
func TestEqualTimestampPredictionsResolveTheLatestDeterministically(t *testing.T) {
	_, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task, site, subject = "t-equal-ts", "site_a", "hunk:a.go"

	for i, p := range []float64{0.1, 0.2, 0.3, 0.4, 0.5} {
		if err := c.RecordPrediction(ctx, ledger.Prediction{
			TaskID: task, Site: site, Subject: subject, Predicted: p,
			Detail: string(rune('a' + i)),
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := c.RecordOutcome(ctx, task, site, subject, true, "note"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	pairs, err := c.Paired(ctx, site)
	if err != nil {
		t.Fatalf("paired: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("Paired = %+v, want one resolved row", pairs)
	}
	if pairs[0].Predicted != 0.5 {
		t.Fatalf("resolved predicted = %v, want 0.5 — the most recently inserted prediction, "+
			"regardless of whether the five landed in the same millisecond", pairs[0].Predicted)
	}
}
