package evidence

// GAP 4 §38 integration test: fixtures built through the ACTUAL
// PersistResult/LoadAndVerifyResult path (never a report-only fake
// schema), covering both solved, control-only, treatment-only, both
// failed, an infra-retry history, missing optional telemetry, and
// treatment judgment telemetry — then BuildCampaignReportFromResults
// aggregates them and the assertions check the real numbers.

import (
	"os"
	"path/filepath"
	"testing"
)

func persistFixture(t *testing.T, runRoot string, p RunPlan, runID string, status Status, patch []byte, judgments []JudgmentTelemetry) Result {
	t.Helper()
	res := Result{
		TaskID: p.TaskID, Benchmark: p.Benchmark, Arm: p.Arm, RunID: runID, SuiteHash: p.SuiteHash,
		OfficialBenchmarkStatus: status, BoundedCodeStatus: "done", Attempts: 1, TokensUsed: 100,
		WallTimeMS: 1000,
	}
	if _, err := PersistResult(runRoot, res, patch, nil, judgments); err != nil {
		t.Fatalf("PersistResult(%s): %v", p.Key(), err)
	}
	return res
}

func TestBuildCampaignReportFromResultsAggregatesRealPersistedFixtures(t *testing.T) {
	runRoot := t.TempDir()
	suiteHash := "report-test-suite"

	plans := []RunPlan{
		{SuiteHash: suiteHash, TaskID: "both-solved", Arm: ArmControl, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 0},
		{SuiteHash: suiteHash, TaskID: "both-solved", Arm: ArmExperimentalJevFull, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 1},
		{SuiteHash: suiteHash, TaskID: "control-only", Arm: ArmControl, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 2},
		{SuiteHash: suiteHash, TaskID: "control-only", Arm: ArmExperimentalJevFull, Benchmark: BenchmarkSWEBenchProVerified, RunOrder: 3},
		{SuiteHash: suiteHash, TaskID: "treatment-only", Arm: ArmControl, Benchmark: BenchmarkSWEAtlasTestWriting, RunOrder: 4},
		{SuiteHash: suiteHash, TaskID: "treatment-only", Arm: ArmExperimentalJevFull, Benchmark: BenchmarkSWEAtlasTestWriting, RunOrder: 5},
		{SuiteHash: suiteHash, TaskID: "both-failed", Arm: ArmControl, Benchmark: BenchmarkSWEAtlasRefactoring, RunOrder: 6},
		{SuiteHash: suiteHash, TaskID: "both-failed", Arm: ArmExperimentalJevFull, Benchmark: BenchmarkSWEAtlasRefactoring, RunOrder: 7},
	}

	current := map[string]Record{}
	record := func(p RunPlan, runID string, status Status, prev string) {
		current[p.Key()] = Record{SuiteHash: suiteHash, TaskID: p.TaskID, Arm: p.Arm, RunID: runID,
			Status: status, PreviousRunID: prev}
	}

	persistFixture(t, runRoot, plans[0], "run-both-solved-control", StatusSolved, []byte("diff\n"), nil)
	record(plans[0], "run-both-solved-control", StatusSolved, "")
	persistFixture(t, runRoot, plans[1], "run-both-solved-treatment", StatusSolved, []byte("diff\n"),
		[]JudgmentTelemetry{{Site: "intake_profile", Model: "jev-1.13.0", InputTokens: 200, LatencyMS: 50}})
	record(plans[1], "run-both-solved-treatment", StatusSolved, "")

	persistFixture(t, runRoot, plans[2], "run-control-only-control", StatusSolved, []byte("diff\n"), nil)
	record(plans[2], "run-control-only-control", StatusSolved, "")
	persistFixture(t, runRoot, plans[3], "run-control-only-treatment", StatusFailed, []byte("diff\n"), nil)
	record(plans[3], "run-control-only-treatment", StatusFailed, "")

	persistFixture(t, runRoot, plans[4], "run-treatment-only-control", StatusFailed, nil, nil)
	record(plans[4], "run-treatment-only-control", StatusFailed, "")
	persistFixture(t, runRoot, plans[5], "run-treatment-only-treatment", StatusSolved, []byte("diff\n"), nil)
	record(plans[5], "run-treatment-only-treatment", StatusSolved, "")

	persistFixture(t, runRoot, plans[6], "run-both-failed-control", StatusFailed, nil, nil)
	record(plans[6], "run-both-failed-control", StatusFailed, "")
	// Infra-retry history: an abandoned INFRA_ERROR attempt exists on
	// disk but is superseded in `current` by a later, real terminal run
	// — §21: aggregation must select the terminal attempt, not the
	// abandoned one.
	persistFixture(t, runRoot, RunPlan{SuiteHash: suiteHash, TaskID: "both-failed", Arm: ArmExperimentalJevFull,
		Benchmark: BenchmarkSWEAtlasRefactoring}, "run-both-failed-treatment-attempt1", StatusInfraErr, nil, nil)
	persistFixture(t, runRoot, plans[7], "run-both-failed-treatment-attempt2", StatusFailed, nil, nil)
	record(plans[7], "run-both-failed-treatment-attempt2", StatusFailed, "run-both-failed-treatment-attempt1")

	results, corrupted, err := LoadResultsForSuite(runRoot, suiteHash, plans, current)
	if err != nil {
		t.Fatalf("LoadResultsForSuite: %v", err)
	}
	if len(corrupted) != 0 {
		t.Fatalf("expected no corrupted results, got %v", corrupted)
	}
	if len(results) != 8 {
		t.Fatalf("expected all 8 fixtures to load, got %d: %+v", len(results), results)
	}
	// §21: the abandoned attempt-1 result must never surface.
	if got := results["both-failed|EXPERIMENTAL_JEV_FULL_AUTHORITY"].RunID; got != "run-both-failed-treatment-attempt2" {
		t.Fatalf("expected the terminal retry attempt to be selected, got run ID %q", got)
	}

	report := BuildCampaignReportFromResults(plans, results, corrupted)
	if report.Partial {
		t.Fatalf("expected a complete report (all 8 planned runs have terminal results), got partial: %+v", report)
	}
	if len(report.SolvedBothArms) != 1 || report.SolvedBothArms[0] != "both-solved" {
		t.Fatalf("expected exactly both-solved in SolvedBothArms, got %v", report.SolvedBothArms)
	}
	if len(report.SolvedControlOnly) != 1 || report.SolvedControlOnly[0] != "control-only" {
		t.Fatalf("expected exactly control-only in SolvedControlOnly, got %v", report.SolvedControlOnly)
	}
	if len(report.SolvedTreatmentOnly) != 1 || report.SolvedTreatmentOnly[0] != "treatment-only" {
		t.Fatalf("expected exactly treatment-only in SolvedTreatmentOnly, got %v", report.SolvedTreatmentOnly)
	}
	if len(report.SolvedNeither) != 1 || report.SolvedNeither[0] != "both-failed" {
		t.Fatalf("expected exactly both-failed in SolvedNeither, got %v", report.SolvedNeither)
	}
	if report.Aggregate.Attempts != 8 {
		t.Fatalf("expected 8 total attempts (1 per loaded result), got %d", report.Aggregate.Attempts)
	}
	if report.Aggregate.GeneratorTokens != 800 {
		t.Fatalf("expected 800 total generator tokens (100 x 8), got %d", report.Aggregate.GeneratorTokens)
	}

	md := report.RenderMarkdown()
	if md == "" {
		t.Fatal("expected non-empty rendered markdown")
	}
}

func TestBuildCampaignReportFromResultsFlagsCorruptedResultsAndExcludesThem(t *testing.T) {
	runRoot := t.TempDir()
	suiteHash := "corrupt-test-suite"
	p := RunPlan{SuiteHash: suiteHash, TaskID: "corrupted-task", Arm: ArmControl, Benchmark: BenchmarkSWEBenchProVerified}

	persistFixture(t, runRoot, p, "run-1", StatusSolved, []byte("original diff\n"), nil)

	// Tamper with the persisted patch after the fact — the recorded hash
	// in artifact-hashes.json no longer matches.
	dir := ResultDir(runRoot, suiteHash, p.TaskID, p.Arm, "run-1")
	if err := os.WriteFile(filepath.Join(dir, PatchFile), []byte("tampered diff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	current := map[string]Record{p.Key(): {SuiteHash: suiteHash, TaskID: p.TaskID, Arm: p.Arm, RunID: "run-1", Status: StatusSolved}}
	results, corrupted, err := LoadResultsForSuite(runRoot, suiteHash, []RunPlan{p}, current)
	if err != nil {
		t.Fatalf("LoadResultsForSuite: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected the tampered result to be excluded, got %+v", results)
	}
	if len(corrupted) != 1 {
		t.Fatalf("expected exactly 1 corrupted entry, got %v", corrupted)
	}

	report := BuildCampaignReportFromResults([]RunPlan{p}, results, corrupted)
	if !report.Partial {
		t.Fatal("a report with a corrupted, excluded result must never be marked complete")
	}
	if len(report.Corrupted) != 1 {
		t.Fatalf("expected the report to carry the corruption note, got %v", report.Corrupted)
	}
}
