package evidence_test

import (
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/evidence"
)

func TestBuildCampaignReportOnTheRealFrozenPlanWithNoRunsIsPartial(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	r := evidence.BuildCampaignReport(plans, map[string]evidence.Record{})
	if !r.Partial {
		t.Fatal("a report with zero completed runs must be Partial — design instruction §40")
	}
	if r.Complete != 0 {
		t.Fatalf("Complete = %d, want 0", r.Complete)
	}
	if r.Planned != 16 {
		t.Fatalf("Planned = %d, want 16", r.Planned)
	}
	if !strings.Contains(r.RenderMarkdown(), "PARTIAL CAMPAIGN") {
		t.Fatal("expected the rendered markdown to say PARTIAL CAMPAIGN")
	}
}

func TestBuildCampaignReportCategorizesPairedOutcomesCorrectly(t *testing.T) {
	plans := []evidence.RunPlan{
		{TaskID: "solved-both", Arm: evidence.ArmControl},
		{TaskID: "solved-both", Arm: evidence.ArmExperimentalJevFull},
		{TaskID: "control-only", Arm: evidence.ArmControl},
		{TaskID: "control-only", Arm: evidence.ArmExperimentalJevFull},
		{TaskID: "treatment-only", Arm: evidence.ArmControl},
		{TaskID: "treatment-only", Arm: evidence.ArmExperimentalJevFull},
		{TaskID: "neither", Arm: evidence.ArmControl},
		{TaskID: "neither", Arm: evidence.ArmExperimentalJevFull},
	}
	current := map[string]evidence.Record{
		"solved-both|CONTROL":                            {Status: evidence.StatusSolved},
		"solved-both|EXPERIMENTAL_JEV_FULL_AUTHORITY":    {Status: evidence.StatusSolved},
		"control-only|CONTROL":                           {Status: evidence.StatusSolved},
		"control-only|EXPERIMENTAL_JEV_FULL_AUTHORITY":   {Status: evidence.StatusFailed},
		"treatment-only|CONTROL":                         {Status: evidence.StatusFailed},
		"treatment-only|EXPERIMENTAL_JEV_FULL_AUTHORITY": {Status: evidence.StatusSolved},
		"neither|CONTROL":                                {Status: evidence.StatusFailed},
		"neither|EXPERIMENTAL_JEV_FULL_AUTHORITY":        {Status: evidence.StatusFailed},
	}
	r := evidence.BuildCampaignReport(plans, current)
	if r.Partial {
		t.Error("all 8 runs are complete (solved/failed); expected Partial=false")
	}
	if len(r.SolvedBothArms) != 1 || r.SolvedBothArms[0] != "solved-both" {
		t.Errorf("SolvedBothArms = %v", r.SolvedBothArms)
	}
	if len(r.SolvedControlOnly) != 1 || r.SolvedControlOnly[0] != "control-only" {
		t.Errorf("SolvedControlOnly = %v", r.SolvedControlOnly)
	}
	if len(r.SolvedTreatmentOnly) != 1 || r.SolvedTreatmentOnly[0] != "treatment-only" {
		t.Errorf("SolvedTreatmentOnly = %v", r.SolvedTreatmentOnly)
	}
	if len(r.SolvedNeither) != 1 || r.SolvedNeither[0] != "neither" {
		t.Errorf("SolvedNeither = %v", r.SolvedNeither)
	}
}

func TestBuildCampaignReportIsNotPartialWhenEveryNonInvalidRunIsComplete(t *testing.T) {
	plans := []evidence.RunPlan{
		{TaskID: "t1", Arm: evidence.ArmControl},
		{TaskID: "t1", Arm: evidence.ArmExperimentalJevFull},
	}
	current := map[string]evidence.Record{
		"t1|CONTROL":                         {Status: evidence.StatusSolved},
		"t1|EXPERIMENTAL_JEV_FULL_AUTHORITY": {Status: evidence.StatusFailed},
	}
	r := evidence.BuildCampaignReport(plans, current)
	if r.Partial {
		t.Fatal("expected Partial=false when every non-invalid run is complete")
	}
}
