package evidence_test

import (
	"testing"

	"github.com/akynte/boundedcode/internal/evidence"
)

func TestCompareAllArmsOnTheRealFrozenPlanFindsOnlyJevDifferences(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := evidence.CompareAllArms(plans)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 8 {
		t.Fatalf("got %d task diffs, want 8", len(diffs))
	}
	for _, d := range diffs {
		if !d.OnlyJevDiffers {
			t.Errorf("task %s: arm configs differ beyond Jev: %v", d.TaskID, d.Differences)
		}
		// Exactly "arm" and "jev_profile_sha256" should differ; nothing
		// else, and not zero differences either (an empty diff would mean
		// the comparison never actually looked at anything).
		if len(d.Differences) != 2 {
			t.Errorf("task %s: expected exactly 2 approved differences (arm, "+
				"jev_profile_sha256), got %v", d.TaskID, d.Differences)
		}
	}
}

func TestCompareArmsCatchesADisapprovedDifference(t *testing.T) {
	control := evidence.RunPlan{TaskID: "t1", Arm: evidence.ArmControl, ImageRef: "a@sha256:x"}
	treatment := evidence.RunPlan{TaskID: "t1", Arm: evidence.ArmExperimentalJevFull,
		ImageRef: "b@sha256:y", JevProfileSHA: "abc"}
	d := evidence.CompareArms(control, treatment)
	if d.OnlyJevDiffers {
		t.Fatal("expected OnlyJevDiffers=false when image_ref differs between arms")
	}
	found := false
	for _, f := range d.Differences {
		if f == "image_ref" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected image_ref to be reported as a difference: %v", d.Differences)
	}
}

func TestCompareArmsPassesForIdenticalNonJevFields(t *testing.T) {
	control := evidence.RunPlan{TaskID: "t1", Arm: evidence.ArmControl, ImageRef: "a@sha256:x",
		Benchmark: evidence.BenchmarkSWEBenchProVerified, BudgetRunsPerArm: 1, NetworkPolicy: "p"}
	treatment := evidence.RunPlan{TaskID: "t1", Arm: evidence.ArmExperimentalJevFull, ImageRef: "a@sha256:x",
		Benchmark: evidence.BenchmarkSWEBenchProVerified, BudgetRunsPerArm: 1, NetworkPolicy: "p",
		JevProfileSHA: "abc"}
	d := evidence.CompareArms(control, treatment)
	if !d.OnlyJevDiffers {
		t.Fatalf("expected OnlyJevDiffers=true, got differences: %v", d.Differences)
	}
}
