package evidence_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/evidence"
)

func openTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	l, err := evidence.OpenLedger(filepath.Join(t.TempDir(), "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The single most important property in this whole package — design
// instruction §15: "this rule must be enforced in code, not merely
// documented." A task that failed because the generated solution was wrong
// must never be silently rerun to try for a better score.
func TestDecideNeverRestartsASolvedOrFailedRun(t *testing.T) {
	for _, status := range []evidence.Status{evidence.StatusSolved, evidence.StatusFailed} {
		rec := evidence.Record{Status: status}
		action := evidence.Decide(&rec)
		if action != evidence.ActionSkip {
			t.Errorf("status %s: Decide = %s, want %s — a terminal result must never restart",
				status, action, evidence.ActionSkip)
		}
	}
}

func TestDecideNeverSilentlyRestartsAnInvalidRun(t *testing.T) {
	rec := evidence.Record{Status: evidence.StatusInvalid, InvalidReason: evidence.InvalidReasonAntiLeakage}
	if action := evidence.Decide(&rec); action != evidence.ActionSkip {
		t.Errorf("Decide(INVALID) = %s, want %s — requires explicit operator action, never an "+
			"automatic rerun", action, evidence.ActionSkip)
	}
}

func TestDecideStartsAFreshPlanWithNoPriorRecord(t *testing.T) {
	if action := evidence.Decide(nil); action != evidence.ActionStart {
		t.Errorf("Decide(nil) = %s, want %s", action, evidence.ActionStart)
	}
}

func TestDecideRetriesOnlyInfrastructureErrors(t *testing.T) {
	rec := evidence.Record{Status: evidence.StatusInfraErr}
	if action := evidence.Decide(&rec); action != evidence.ActionRetryInfra {
		t.Errorf("Decide(INFRA_ERROR) = %s, want %s", action, evidence.ActionRetryInfra)
	}
}

func TestDecideRecoversAnInterruptedRunningRecordRatherThanIgnoringIt(t *testing.T) {
	rec := evidence.Record{Status: evidence.StatusRunning}
	if action := evidence.Decide(&rec); action != evidence.ActionRecoverInterrupted {
		t.Errorf("Decide(RUNNING) = %s, want %s — a live RUNNING record at startup means the "+
			"previous process died mid-run and must be recovered, not silently skipped or "+
			"silently restarted", action, evidence.ActionRecoverInterrupted)
	}
}

func TestLedgerIsAppendOnlyAndNeverLosesAnOlderRecord(t *testing.T) {
	l := openTestLedger(t)
	r1 := evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, RunID: "run-1",
		Status: evidence.StatusRunning, CreatedAt: time.Now()}
	r2 := evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, RunID: "run-1",
		Status: evidence.StatusSolved, CreatedAt: time.Now()}
	if err := l.Append(r1); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(r2); err != nil {
		t.Fatal(err)
	}
	all, err := l.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d records, want 2 — the first record must never be overwritten", len(all))
	}
	if all[0].Status != evidence.StatusRunning || all[1].Status != evidence.StatusSolved {
		t.Fatalf("records out of order or altered: %+v", all)
	}
}

func TestLedgerCurrentReturnsTheLatestRecordPerKey(t *testing.T) {
	l := openTestLedger(t)
	must(t, l.Append(evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, Status: evidence.StatusRunning}))
	must(t, l.Append(evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, Status: evidence.StatusSolved}))
	must(t, l.Append(evidence.Record{TaskID: "t2", Arm: evidence.ArmExperimentalJevFull, Status: evidence.StatusFailed}))

	cur, err := l.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur["t1|CONTROL"].Status != evidence.StatusSolved {
		t.Errorf("t1|CONTROL = %+v, want the latest (SOLVED) record", cur["t1|CONTROL"])
	}
	if cur["t2|EXPERIMENTAL_JEV_FULL_AUTHORITY"].Status != evidence.StatusFailed {
		t.Errorf("t2 key = %+v, want FAILED", cur["t2|EXPERIMENTAL_JEV_FULL_AUTHORITY"])
	}
}

// A retry must be linked to what it replaces, and the replaced record must
// still be in the ledger, unaltered — design instruction §13/§26.
func TestInfrastructureRetryLinksToThePreviousRunIDWithoutErasingIt(t *testing.T) {
	l := openTestLedger(t)
	original := evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, RunID: "run-1",
		Status: evidence.StatusInfraErr, Detail: "provider outage"}
	must(t, l.Append(original))

	retry := evidence.Record{TaskID: "t1", Arm: evidence.ArmControl, RunID: "run-2",
		Status: evidence.StatusRunning, PreviousRunID: "run-1", RetryReason: "infrastructure retry"}
	must(t, l.Append(retry))

	all, err := l.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d records, want 2", len(all))
	}
	if all[0].RunID != "run-1" || all[0].Status != evidence.StatusInfraErr {
		t.Fatalf("original record altered: %+v", all[0])
	}
	if all[1].PreviousRunID != "run-1" {
		t.Fatalf("retry record does not link to the original: %+v", all[1])
	}
}

func TestNewRunIDsAreUniqueAcrossCalls(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := evidence.NewRunID("some-task", evidence.ArmControl)
		if seen[id] {
			t.Fatalf("duplicate run ID: %s", id)
		}
		seen[id] = true
	}
}

func TestSummarizeCountsPendingForPlansWithNoRecord(t *testing.T) {
	plans := []evidence.RunPlan{
		{TaskID: "t1", Arm: evidence.ArmControl},
		{TaskID: "t2", Arm: evidence.ArmControl},
	}
	s := evidence.Summarize(plans, map[string]evidence.Record{
		"t1|CONTROL": {Status: evidence.StatusSolved},
	})
	if s.Planned != 2 {
		t.Errorf("Planned = %d, want 2", s.Planned)
	}
	if s.ByStatus[evidence.StatusSolved] != 1 {
		t.Errorf("SOLVED count = %d, want 1", s.ByStatus[evidence.StatusSolved])
	}
	if s.ByStatus[evidence.StatusPending] != 1 {
		t.Errorf("PENDING count = %d, want 1", s.ByStatus[evidence.StatusPending])
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
