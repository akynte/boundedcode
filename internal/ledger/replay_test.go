package ledger_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
)

// fakeAuthority is a SiteAuthority a test controls, standing in for the
// judgment.Config + registry pair the real one reads.
type fakeAuthority map[string]ledger.ReplaySiteInfo

func (f fakeAuthority) Lookup(site string) (ledger.ReplaySiteInfo, bool) {
	info, ok := f[site]
	return info, ok
}

func seedPrediction(t *testing.T, c *ledger.CalibrationStore, p ledger.Prediction) {
	t.Helper()
	if err := c.RecordPrediction(context.Background(), p); err != nil {
		t.Fatalf("record: %v", err)
	}
}

func TestReplayIsDeterministicAndMakesNoCall(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-replay"

	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "hunk:a.go", Predicted: 0.9,
		Model: "jev-1.13.0", SiteVersion: "1", TierAtPrediction: "logged",
	})
	auth := fakeAuthority{"site_a": {Version: "1", EffectThreshold: 0.7, EffectPermitted: false,
		ConfiguredTier: "logged"}}

	first, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	second, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(first.Records) != 1 || len(second.Records) != 1 {
		t.Fatalf("expected one record each: %+v / %+v", first.Records, second.Records)
	}
	if first.Records[0] != second.Records[0] {
		t.Fatalf("replay is not deterministic:\n%+v\n%+v", first.Records[0], second.Records[0])
	}
	if len(first.Divergences) != 0 {
		t.Fatalf("logged tier recorded no effect and would apply none; no divergence expected: %+v",
			first.Divergences)
	}
}

func TestReplayReportsDivergenceWhenAuthorityWouldNowApply(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-replay-diverge"

	// Recorded at logged, so nothing was applied.
	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "hunk:a.go", Predicted: 0.9,
		Model: "jev-1.13.0", SiteVersion: "1", TierAtPrediction: "logged",
	})
	// A prediction below the site's floor: promoting the site still would
	// not act on it, so it must not be reported as a divergence.
	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "hunk:b.go", Predicted: 0.2,
		Model: "jev-1.13.0", SiteVersion: "1", TierAtPrediction: "logged",
	})

	auth := fakeAuthority{"site_a": {Version: "1", EffectThreshold: 0.7, EffectPermitted: true,
		ConfiguredTier: "routing"}}
	rep, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(rep.Divergences) != 1 {
		t.Fatalf("Divergences = %+v, want exactly the 0.9 row", rep.Divergences)
	}
	d := rep.Divergences[0]
	if d.Subject != "hunk:a.go" || d.Recorded != "not applied" || d.Replayed != "applied" {
		t.Fatalf("divergence = %+v", d)
	}
}

func TestReplayReportsAVersionMismatchRatherThanReplayingIt(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-replay-version"

	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "s", Predicted: 0.95,
		Model: "jev-1.13.0", SiteVersion: "1", TierAtPrediction: "logged",
	})
	auth := fakeAuthority{"site_a": {Version: "2", EffectThreshold: 0, EffectPermitted: true,
		ConfiguredTier: "routing"}}

	rep, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(rep.VersionMismatch) != 1 {
		t.Fatalf("VersionMismatch = %+v, want one entry", rep.VersionMismatch)
	}
	if len(rep.Divergences) != 0 {
		t.Fatalf("a record answering a superseded question must not be replayed: %+v", rep.Divergences)
	}
}

func TestReplayReportsAnUnknownSiteRatherThanGuessing(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-replay-unknown"

	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_from_another_build", Subject: "s", Predicted: 0.9,
		SiteVersion: "1", TierAtPrediction: "routing",
	})
	rep, err := c.ReplayTask(ctx, l, task, fakeAuthority{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(rep.UnknownSites) != 1 || rep.UnknownSites[0] != "site_from_another_build" {
		t.Fatalf("UnknownSites = %+v", rep.UnknownSites)
	}
	if len(rep.Divergences) != 0 {
		t.Fatalf("an unknown site must not be replayed against a guess: %+v", rep.Divergences)
	}
}

func TestReplayOfATaskWithNoRecordsSaysSoRatherThanClaimingSuccess(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	rep, err := c.ReplayTask(context.Background(), l, "never-ran", fakeAuthority{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Replayable() {
		t.Fatalf("a task with no records must not report as replayable: %+v", rep)
	}
	if rep.JudgmentOps != 0 {
		t.Fatalf("JudgmentOps = %d, want 0", rep.JudgmentOps)
	}
}

func TestReplayDoesNotMutateTheRecordsItReads(t *testing.T) {
	l, st := newLedger(t)
	c := ledger.NewCalibrationStore(st)
	ctx := context.Background()
	const task = "t-replay-readonly"

	seedPrediction(t, c, ledger.Prediction{
		TaskID: task, Site: "site_a", Subject: "s", Predicted: 0.9,
		Model: "m", SiteVersion: "1", TierAtPrediction: "logged",
	})
	auth := fakeAuthority{"site_a": {Version: "1", EffectPermitted: true, ConfiguredTier: "routing"}}

	before, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// A prediction replay reported as "would now apply" must still be
	// recorded as not intervened: replay reports, it does not act.
	pendingBefore, _ := c.PendingCount(ctx, "site_a")
	if _, err := c.ReplayTask(ctx, l, task, auth); err != nil {
		t.Fatalf("replay: %v", err)
	}
	after, err := c.ReplayTask(ctx, l, task, auth)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	pendingAfter, _ := c.PendingCount(ctx, "site_a")

	if before.Records[0].Intervened || after.Records[0].Intervened {
		t.Fatalf("replay marked a record as intervened; it must only read")
	}
	if pendingBefore != pendingAfter {
		t.Fatalf("replay changed the pending count: %d -> %d", pendingBefore, pendingAfter)
	}
	if len(before.Divergences) != len(after.Divergences) {
		t.Fatalf("replay is not idempotent: %d then %d divergences",
			len(before.Divergences), len(after.Divergences))
	}
}
