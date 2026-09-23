package evidence

// PIPELINE_TEST_ONLY — design instruction §32/§33 of the Evidence Suite v1
// strict-finalization task.
//
// Drives the REAL orchestrator (RunCampaign -> Decide -> Prepare ->
// ExecuteRun -> PersistResult -> ledger update) against a REAL,
// already-sanitized SWE-Bench Pro Verified workspace (vuls), the REAL
// internal/sandbox/container.Runner, and the REAL pinned official image —
// with only the generator replaced by a deterministic fake that performs
// one known, trivial edit through the same engine.Engine interface a real
// engine uses. It proves the full pipeline completes and persists
// correctly; it does not attempt to solve the task, and a real official
// FAILED (the fake edit does not pass the pinned test command) is the
// expected, acceptable outcome — what matters is that PersistResult wrote
// every artifact and status reflects it, not that the fake edit is
// correct.
//
// Skips (does not fail) if Docker is unavailable or the sanitized vuls
// workspace has not been materialized.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/sandbox/container"
)

const (
	campaignTestVulsImage  = "jefzda/sweap-images@sha256:e5310e43e886b49210c59ca5c79571235f6c38ccd50035e1bf4bd6bac02a6034"
	campaignTestVulsTaskID = "instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-" +
		"v1151a6325649aaf997cd541ebe533b53fddf1b07"
	campaignTestEditPath   = "scanner/redhatbase.go"
	campaignTestEditMarker = "// PIPELINE_TEST_ONLY marker: not a real fix.\n"
)

func requireCampaignTestImage(t *testing.T, image string) {
	t.Helper()
	if ok, why := (&container.Runner{}).Available(context.Background()); !ok {
		t.Skipf("no usable container runtime: %s", why)
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("image not pulled: %s", image)
	}
}

type campaignFakeEditEngine struct{ edited bool }

func (e *campaignFakeEditEngine) Name() string                 { return "campaigntest-fake-engine" }
func (e *campaignFakeEditEngine) Health(context.Context) error { return nil }
func (e *campaignFakeEditEngine) Close() error                 { return nil }
func (e *campaignFakeEditEngine) Edits() bool                  { return true }
func (e *campaignFakeEditEngine) Step(_ context.Context, req engine.Request) (*engine.Response, error) {
	full := filepath.Join(req.Worktree, campaignTestEditPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(body), campaignTestEditMarker) {
		if err := os.WriteFile(full, append([]byte(campaignTestEditMarker), body...), 0o644); err != nil {
			return nil, err
		}
		e.edited = true
	}
	return &engine.Response{Summary: "PIPELINE_TEST_ONLY", ClaimsDone: true}, nil
}

func TestRunCampaignFullFakeGeneratorPipelineForSWEBenchPro(t *testing.T) {
	requireCampaignTestImage(t, campaignTestVulsImage)

	realRoot := "../../.boundedcode-runs/evidence-v1"
	realControl := filepath.Join(realRoot, "agent-workspaces", campaignTestVulsTaskID, "control")
	if _, err := os.Stat(filepath.Join(realControl, ".git")); err != nil {
		t.Skipf("sanitized vuls workspace not materialized: %v — run "+
			"evals/evidence-v1/tooling/materialize_task.py first", err)
	}

	runRoot := filepath.Join(realRoot, "pipelinetest-copies", "campaign-"+t.Name())
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runRoot) })

	dst := filepath.Join(runRoot, "agent-workspaces", campaignTestVulsTaskID, "control")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-a", realControl, dst).CombinedOutput(); err != nil {
		t.Fatalf("copying sanitized workspace for a disposable campaign run: %v\n%s", err, out)
	}

	led, err := OpenLedger(filepath.Join(runRoot, "ledger", "v1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	plan := RunPlan{
		SuiteHash: "pipelinetest-suite-hash", TaskID: campaignTestVulsTaskID, Arm: ArmControl,
		Benchmark: BenchmarkSWEBenchProVerified, Repository: "future-architect/vuls", Language: "go",
		ImageRef: campaignTestVulsImage, ImageDir: "/app", RunOrder: 0,
	}

	fakeEngine := &campaignFakeEditEngine{}
	rf := &RunnerFactory{
		Provider:          fakeProvider{},
		EngineFactory:     func() (engine.Engine, error) { return fakeEngine, nil },
		BoundedCodeCommit: "pipelinetest",
	}

	steps, err := RunCampaign(context.Background(), runRoot, led, rf, []RunPlan{plan}, nil)
	if err != nil {
		t.Fatalf("RunCampaign: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	step := steps[0]
	if step.Action != ActionStart {
		t.Fatalf("expected ActionStart for a fresh PENDING run, got %v", step.Action)
	}
	if !step.Ran {
		t.Fatal("expected the run to actually execute")
	}
	if !fakeEngine.edited {
		t.Fatal("expected the fake engine's Step to have run and made its marker edit")
	}
	if step.Result.BoundedCodeStatus == "" {
		t.Fatal("expected a non-empty BoundedCodeStatus from the real task.Outcome")
	}
	if step.Result.PatchBytes == 0 {
		t.Fatal("expected a non-empty patch from the fake engine's edit")
	}
	// §32: "the fake solution does not have to pass the benchmark" — a
	// one-line comment prepended ahead of the target function does not
	// change the pinned test command's outcome, so either a real SOLVED or
	// a real FAILED is an acceptable, honest result here. What matters is
	// that the official grader actually ran and reported a real verdict,
	// not INFRA_ERROR/INVALID.
	if step.Result.OfficialBenchmarkStatus != StatusSolved && step.Result.OfficialBenchmarkStatus != StatusFailed {
		t.Fatalf("expected a real official SOLVED or FAILED verdict, got %s (detail: %s)",
			step.Result.OfficialBenchmarkStatus, step.Result.Detail)
	}

	// §33: verify every persisted artifact exists, hashes match, and
	// status/report can see it purely by reading back from disk.
	dir := ResultDir(runRoot, plan.SuiteHash, plan.TaskID, plan.Arm, step.Result.RunID)
	for _, f := range []string{ResultJSONFile, PatchFile, EvaluatorRawFile, TelemetryFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected persisted artifact %s: %v", f, err)
		}
	}
	loaded, err := LoadResult(runRoot, plan.SuiteHash, plan.TaskID, plan.Arm, step.Result.RunID)
	if err != nil {
		t.Fatalf("LoadResult: %v", err)
	}
	if loaded.OfficialBenchmarkStatus != step.Result.OfficialBenchmarkStatus {
		t.Fatalf("persisted result.json does not match what RunCampaign observed: got %s, want %s",
			loaded.OfficialBenchmarkStatus, step.Result.OfficialBenchmarkStatus)
	}
	if loaded.PatchSHA256 == "" {
		t.Fatal("expected result.json to carry the patch's SHA-256")
	}

	current, err := led.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current[plan.Key()].Status != step.Result.OfficialBenchmarkStatus {
		t.Fatalf("expected the ledger's current status to match the official result, got %s want %s",
			current[plan.Key()].Status, step.Result.OfficialBenchmarkStatus)
	}

	// §34: attempting to persist the same completed run again must fail,
	// and running the campaign again must not rerun it (no-hidden-retry
	// through the real orchestrator, not just Decide() in isolation).
	if _, err := PersistResult(runRoot, step.Result, []byte("x"), nil, nil); err == nil {
		t.Fatal("expected a second PersistResult for the same run ID to be refused")
	}
	fakeEngine.edited = false
	secondSteps, err := RunCampaign(context.Background(), runRoot, led, rf, []RunPlan{plan}, nil)
	if err != nil {
		t.Fatalf("second RunCampaign: %v", err)
	}
	if secondSteps[0].Action != ActionSkip {
		t.Fatalf("expected the second campaign invocation to skip the now-terminal run, got %v",
			secondSteps[0].Action)
	}
	if fakeEngine.edited {
		t.Fatal("the generator must not be invoked again for an already-terminal run")
	}
}
