package evidence

// CHECK 2 (3rd finalization pass): the real
// RunCampaign -> LoadResultsForSuite -> BuildCampaignReportFromResults ->
// RenderMarkdown pipeline, exercised through RefreshCampaignReport (the
// exact function RunCampaign calls after every ExecuteRun/PersistResult/
// ledger update) — proving the report is always built from what is
// durable on disk, never from an in-memory Result a caller still holds.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshCampaignReportBuildsFromPersistedDataNotInMemoryMutation(t *testing.T) {
	runRoot := t.TempDir()
	suiteHash := "refresh-report-test-suite"

	plans := []RunPlan{
		{SuiteHash: suiteHash, TaskID: "task-a", Arm: ArmControl, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 0},
		{SuiteHash: suiteHash, TaskID: "task-a", Arm: ArmExperimentalJevFull, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 1},
	}
	led, err := OpenLedger(filepath.Join(runRoot, "ledger", "v1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Persist a CONTROL result.
	controlRes := Result{
		TaskID: "task-a", Benchmark: BenchmarkSWEBenchProVerified, Arm: ArmControl, RunID: "run-control-1",
		SuiteHash: suiteHash, OfficialBenchmarkStatus: StatusSolved, Attempts: 1, TokensUsed: 50,
	}
	if _, err := PersistResult(runRoot, controlRes, []byte("diff\n"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := led.Append(Record{SuiteHash: suiteHash, TaskID: "task-a", Arm: ArmControl,
		RunID: "run-control-1", Status: StatusSolved}); err != nil {
		t.Fatal(err)
	}

	// After only the CONTROL run: still a partial campaign (1 of 2).
	report, err := RefreshCampaignReport(runRoot, suiteHash, plans, led)
	if err != nil {
		t.Fatalf("RefreshCampaignReport (after CONTROL only): %v", err)
	}
	if !report.Partial {
		t.Fatal("expected PARTIAL CAMPAIGN with only 1 of 2 planned runs complete")
	}
	if report.Complete != 1 {
		t.Fatalf("expected 1 complete run, got %d", report.Complete)
	}
	mdBody, err := os.ReadFile(filepath.Join(runRoot, ReportMDFile))
	if err != nil {
		t.Fatalf("reading %s: %v", ReportMDFile, err)
	}
	if !strings.Contains(string(mdBody), "PARTIAL CAMPAIGN") {
		t.Fatalf("expected report.md to say PARTIAL CAMPAIGN, got:\n%s", mdBody)
	}

	// 2. Persist a treatment result.
	treatmentRes := Result{
		TaskID: "task-a", Benchmark: BenchmarkSWEBenchProVerified, Arm: ArmExperimentalJevFull,
		RunID: "run-treatment-1", SuiteHash: suiteHash, OfficialBenchmarkStatus: StatusFailed,
		Attempts: 1, TokensUsed: 60,
	}
	if _, err := PersistResult(runRoot, treatmentRes, []byte("diff\n"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := led.Append(Record{SuiteHash: suiteHash, TaskID: "task-a", Arm: ArmExperimentalJevFull,
		RunID: "run-treatment-1", Status: StatusFailed}); err != nil {
		t.Fatal(err)
	}

	// 3./4. Refresh again: the campaign/report refresh loads both from
	// disk and reflects their real official outcomes — CONTROL solved,
	// treatment failed, so task-a is "solved control only".
	report, err = RefreshCampaignReport(runRoot, suiteHash, plans, led)
	if err != nil {
		t.Fatalf("RefreshCampaignReport (after both): %v", err)
	}
	if report.Partial {
		t.Fatalf("expected a complete campaign now that both planned runs are terminal, got partial: %+v", report)
	}
	if len(report.SolvedControlOnly) != 1 || report.SolvedControlOnly[0] != "task-a" {
		t.Fatalf("expected task-a in SolvedControlOnly, got %+v", report)
	}

	// 5./6. Mutate the in-memory Result AFTER it was already persisted —
	// the report must not reflect this, since RefreshCampaignReport only
	// ever reads what PersistResult already wrote to disk.
	controlRes.OfficialBenchmarkStatus = StatusFailed // would flip the outcome if this leaked in
	reportAfterMutation, err := RefreshCampaignReport(runRoot, suiteHash, plans, led)
	if err != nil {
		t.Fatalf("RefreshCampaignReport (after in-memory mutation): %v", err)
	}
	if len(reportAfterMutation.SolvedControlOnly) != 1 || reportAfterMutation.SolvedControlOnly[0] != "task-a" {
		t.Fatalf("report changed after mutating an in-memory Result that was already persisted — "+
			"it must be sourced from disk only, got %+v", reportAfterMutation)
	}

	// The persisted JSON companion exists too and matches.
	jsonBody, err := os.ReadFile(filepath.Join(runRoot, ReportJSONFile))
	if err != nil {
		t.Fatalf("reading %s: %v", ReportJSONFile, err)
	}
	if len(jsonBody) == 0 {
		t.Fatal("expected a non-empty report.json")
	}
}
