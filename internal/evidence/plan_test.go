package evidence_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/evidence"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func manifestPath(t *testing.T) string {
	return filepath.Join(repoRoot(t), "evals", "evidence-v1", "manifest.json")
}

func profilePath(t *testing.T) string {
	return filepath.Join(repoRoot(t), "evals", "evidence-v1", "jev-profile.yaml")
}

// The single most important correctness property of this package: its
// hardcoded Registry must name exactly the 8 task IDs the frozen manifest
// names, no more, no fewer, no substitutions — design instruction §1.
func TestRegistryMatchesTheFrozenManifestExactly(t *testing.T) {
	entries, err := evidence.LoadRegistry(manifestPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 8 {
		t.Fatalf("got %d entries, want 8", len(entries))
	}
	for _, e := range entries {
		if e.TaskID == "" || e.ImageRef == "" || e.ImageDir == "" {
			t.Errorf("incomplete registry entry: %+v", e)
		}
	}
}

func TestBuildPlanProducesExactlySixteenRuns(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 16 {
		t.Fatalf("got %d plans, want 16", len(plans))
	}
	counts := evidence.CountByArmAndBenchmark(plans)
	for _, arm := range []evidence.Arm{evidence.ArmControl, evidence.ArmExperimentalJevFull} {
		total := 0
		for _, n := range counts[arm] {
			total += n
		}
		if total != 8 {
			t.Errorf("arm %s has %d planned runs, want 8", arm, total)
		}
	}
}

func TestBuildPlanFrozenRunOrderIsAllControlThenAllTreatment(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	ordered := evidence.Ordered(plans)
	for i, p := range ordered {
		wantArm := evidence.ArmControl
		if i >= 8 {
			wantArm = evidence.ArmExperimentalJevFull
		}
		if p.Arm != wantArm {
			t.Fatalf("run %d (order %d): arm = %s, want %s — design instruction §9's frozen "+
				"order (all CONTROL, then all treatment) is violated", i, p.RunOrder, p.Arm, wantArm)
		}
	}
}

func TestBuildPlanEveryImageIsPinnedToADigest(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if !containsAt(p.ImageRef) {
			t.Errorf("task %s: image %q is not pinned to a digest", p.TaskID, p.ImageRef)
		}
	}
}

func containsAt(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			return true
		}
	}
	return false
}

func TestBuildPlanOnlyTreatmentCarriesTheJevProfileHash(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if p.Arm == evidence.ArmControl && p.JevProfileSHA != "" {
			t.Errorf("task %s CONTROL run unexpectedly carries a Jev profile hash", p.TaskID)
		}
		if p.Arm == evidence.ArmExperimentalJevFull && p.JevProfileSHA == "" {
			t.Errorf("task %s treatment run is missing its Jev profile hash", p.TaskID)
		}
	}
}

// The frozen suite hash must be the manifest's own hash, computed fresh —
// never a value this package could drift from the actual file.
func TestBuildPlanSuiteHashMatchesTheActualManifestFile(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	const expected = "758b95266f0eed0552bc84b6f4b13c5fbee2fa0095456bcacd0e7b0e8d490f56"
	for _, p := range plans {
		if p.SuiteHash != expected {
			t.Fatalf("suite hash = %s, want %s", p.SuiteHash, expected)
		}
	}
}
