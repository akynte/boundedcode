package bench

import "testing"

func scheduleCoverageFixture(t *testing.T, modes []Mode) (Schedule, []Result) {
	t.Helper()
	_, suite := testTaskAndSuite(t)
	schedule, err := BuildSchedule(suite, modes, 1, 17, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]Result, 0, len(schedule.Entries))
	for _, spec := range schedule.Entries {
		result := NewResult(suite, suite.Tasks[0], spec, EnvironmentSnapshot{OS: "test", Architecture: "test", CPUCount: 1})
		result.Official = true
		result.ManifestHash = "manifest-test"
		result.Model.IdentityVerified = true
		result.Evaluator = EvaluatorResult{
			Status:         EvaluatorPass,
			Independent:    true,
			BaselineStatus: EvaluatorFail,
		}
		result.System.ReportedSuccess = true
		result.Status = RunCompleted
		result.Derive()
		results = append(results, result)
	}
	return schedule, results
}

func TestAddScheduleCoverageRestoresEligibleReport(t *testing.T) {
	schedule, results := scheduleCoverageFixture(t, []Mode{Raw, Bounded})
	report := AddScheduleCoverage(BuildReport(results), schedule)
	if !report.ScheduleVerified {
		t.Fatalf("complete schedule was not verified: %+v", report)
	}
	if !report.PerformanceEvidence {
		t.Fatalf("complete eligible schedule did not restore performance evidence: %+v", report)
	}
}

func TestAddScheduleCoverageIsIndependentOfEvidenceEligibility(t *testing.T) {
	schedule, results := scheduleCoverageFixture(t, []Mode{Raw})
	for i := range results {
		results[i].Official = false
		results[i].Smoke = true
	}
	report := AddScheduleCoverage(BuildReport(results), schedule)
	if !report.ScheduleVerified {
		t.Fatalf("exact single-mode schedule was not verified: %+v", report)
	}
	if report.PerformanceEvidence {
		t.Fatal("smoke results became performance evidence")
	}
}

func TestAddScheduleCoverageRejectsMissingExtraAndDuplicateCells(t *testing.T) {
	schedule, results := scheduleCoverageFixture(t, []Mode{Raw, Bounded})

	missing := AddScheduleCoverage(BuildReport(results[:1]), schedule)
	if missing.ScheduleVerified || missing.PerformanceEvidence || missing.IncompletePairs != 1 {
		t.Fatalf("missing cell was treated as complete: %+v", missing)
	}

	duplicateResults := append([]Result(nil), results...)
	duplicateResults = append(duplicateResults, results[0])
	duplicate := AddScheduleCoverage(BuildReport(duplicateResults), schedule)
	if duplicate.ScheduleVerified || duplicate.PerformanceEvidence {
		t.Fatalf("duplicate cell was treated as complete: %+v", duplicate)
	}
	if duplicate.IncompletePairs != 0 {
		t.Fatalf("duplicate exact cell was counted as incomplete: %+v", duplicate)
	}

	extraResults := append([]Result(nil), results...)
	extra := results[0]
	extra.PairID = "not-in-schedule"
	extraResults = append(extraResults, extra)
	extraReport := AddScheduleCoverage(BuildReport(extraResults), schedule)
	if extraReport.ScheduleVerified || extraReport.PerformanceEvidence {
		t.Fatalf("extra cell was treated as complete: %+v", extraReport)
	}
}

func TestAddScheduleCoverageRejectsMismatchedIdentity(t *testing.T) {
	schedule, results := scheduleCoverageFixture(t, []Mode{Raw, Bounded})
	mutations := []struct {
		name string
		edit func(*Result)
	}{
		{name: "run index", edit: func(r *Result) { r.RunIndex++ }},
		{name: "run id", edit: func(r *Result) { r.RunID += "-stale" }},
		{name: "task", edit: func(r *Result) { r.TaskID += "-stale" }},
		{name: "suite", edit: func(r *Result) { r.BenchmarkSuite += "-stale" }},
		{name: "suite version", edit: func(r *Result) { r.SuiteVersion += "-stale" }},
		{name: "suite hash", edit: func(r *Result) { r.SuiteHash += "-stale" }},
		{name: "repetition", edit: func(r *Result) { r.Repetition++ }},
		{name: "seed", edit: func(r *Result) { r.Seed++ }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			copyResults := append([]Result(nil), results...)
			mutation.edit(&copyResults[0])
			report := AddScheduleCoverage(BuildReport(copyResults), schedule)
			if report.ScheduleVerified || report.PerformanceEvidence {
				t.Fatalf("mismatched identity was accepted: %+v", report)
			}
		})
	}
}

func TestAddScheduleCoverageClearsStaleFlagsForInvalidSchedule(t *testing.T) {
	schedule, results := scheduleCoverageFixture(t, []Mode{Raw, Bounded})
	report := BuildReport(results)
	report.ScheduleVerified = true
	report.PerformanceEvidence = true
	invalid := schedule
	invalid.SchemaVersion = 99
	report = AddScheduleCoverage(report, invalid)
	if report.ScheduleVerified || report.PerformanceEvidence {
		t.Fatalf("invalid schedule retained evidence flags: %+v", report)
	}
}
