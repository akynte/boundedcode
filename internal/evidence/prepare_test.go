package evidence_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/evidence"
)

func runRootForTest(t *testing.T) string {
	return filepath.Join(repoRoot(t), ".boundedcode-runs", "evidence-v1")
}

func TestPrepareOnAnAlreadyMaterializedSWEBenchProTask(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	var vulsControl evidence.RunPlan
	found := false
	for _, p := range plans {
		if p.Benchmark == evidence.BenchmarkSWEBenchProVerified && p.Arm == evidence.ArmControl &&
			p.Repository == "future-architect/vuls" {
			vulsControl = p
			found = true
			break
		}
	}
	if !found {
		t.Fatal("vuls control plan not found")
	}

	prepared, err := evidence.Prepare(context.Background(), runRootForTest(t), vulsControl)
	if err != nil {
		t.Skipf("workspace not materialized: %v", err)
	}
	if prepared.WorkspacePath == "" {
		t.Fatal("WorkspacePath not populated")
	}
	if prepared.SourceFingerprint == "" {
		t.Fatal("SourceFingerprint not populated")
	}
}

// The real proof of design instruction §23, against the real materialized
// workspaces: both arms' sanitized copies of the same task must
// fingerprint identically, confirming the two arms genuinely start from
// byte-identical source state.
func TestPrepareBothArmsOfEverySWEBenchProTaskFingerprintIdentically(t *testing.T) {
	plans, err := evidence.BuildPlan(manifestPath(t), profilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	byTask := map[string]map[evidence.Arm]evidence.RunPlan{}
	for _, p := range plans {
		if p.Benchmark != evidence.BenchmarkSWEBenchProVerified {
			continue
		}
		if byTask[p.TaskID] == nil {
			byTask[p.TaskID] = map[evidence.Arm]evidence.RunPlan{}
		}
		byTask[p.TaskID][p.Arm] = p
	}
	if len(byTask) != 4 {
		t.Fatalf("expected 4 SWE-Bench Pro Verified tasks, got %d", len(byTask))
	}
	root := runRootForTest(t)
	prepared := 0
	for taskID, arms := range byTask {
		control, err := evidence.Prepare(context.Background(), root, arms[evidence.ArmControl])
		if err != nil {
			t.Skipf("%s: control workspace not materialized: %v", taskID, err)
		}
		treatment, err := evidence.Prepare(context.Background(), root, arms[evidence.ArmExperimentalJevFull])
		if err != nil {
			t.Skipf("%s: treatment workspace not materialized: %v", taskID, err)
		}
		if control.SourceFingerprint != treatment.SourceFingerprint {
			t.Errorf("%s: control/treatment fingerprints differ:\n  control:   %s\n  treatment: %s",
				taskID, control.SourceFingerprint, treatment.SourceFingerprint)
		}
		diff := evidence.CompareArms(control, treatment)
		if !diff.OnlyJevDiffers {
			t.Errorf("%s: prepared arms differ beyond Jev: %v", taskID, diff.Differences)
		}
		prepared++
	}
	if prepared != 4 {
		t.Fatalf("only %d of 4 tasks were actually prepared and compared", prepared)
	}
}

func TestPrepareRefusesSWEAtlasExplicitlyRatherThanGuessing(t *testing.T) {
	p := evidence.RunPlan{TaskID: "task-x", Benchmark: evidence.BenchmarkSWEAtlasTestWriting}
	_, err := evidence.Prepare(context.Background(), runRootForTest(t), p)
	if err == nil {
		t.Fatal("expected Prepare to refuse a SWE Atlas task explicitly")
	}
}

func TestFingerprintIsDeterministicAcrossTwoIndependentCalls(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "world")

	fp1, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %s vs %s", fp1, fp2)
	}
}

func TestFingerprintChangesWhenContentChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	fp1, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "goodbye")
	fp2, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp2 {
		t.Fatal("fingerprint did not change when file content changed")
	}
}

func TestFingerprintExcludesGit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	fpBefore, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/master\n")
	fpAfter, err := evidence.Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fpBefore != fpAfter {
		t.Fatal(".git content changed the fingerprint; it must be excluded")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
