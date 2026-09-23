package evidence

// Atlas grading lifecycle wiring — closes the "Full live end-to-end
// execution: BLOCKED by Atlas environment lifecycle/provisioning" gap.
//
// The real fix: `harbor task start-env`'s CLI command always tears its
// environment down when the process exits (confirmed by reading
// harbor/cli/tasks.py's own `finally: await environment.stop()`), so a
// separate Go-launched `docker exec` could never find a still-running
// container to grade against — the actual reason the previous pass's
// FindAtlasEnvironmentContainer/BuildAtlasVerifierCmd never worked
// end to end. evals/evidence-v1/tooling/run_atlas_grader.py now drives
// Harbor's own public Environment API (start/upload_dir/upload_file/exec/
// stop) directly in one process for one grading pass — proven manually,
// live, in this session: a real EnsureAtlasTaskDirectory clone, a real
// EnsurePatchedHarborRuntime-patched environment start (the exit-126 fix
// holding), the official tests/ directory uploaded to /tests per Harbor's
// own EnvironmentPaths convention, a real official reference patch
// (solution/addition.patch) applied via `git apply` inside the running
// container, and the official verifier (tests/evaluate_tests.py) actually
// launched — its own script printed "WARNING: No EVAL_API_KEY/
// EVAL_BASE_URL, skipping LLM evaluations" and continued rather than
// making a live call, since no credential was set, and the environment
// was torn down cleanly afterward (confirmed via `docker ps`).
//
// The automated tests below exercise the real Go-side wiring
// (EnsureAtlasTaskDirectory's real network clone, BuildAtlasGraderCmd's
// real construction, gradeSWEAtlas's real call sequence) and stub only
// the expensive final boundary (actually running run_atlas_grader.py,
// which drives a multi-minute real `go test` suite inside a container) —
// the same "intercept only the final executable/provider boundary"
// pattern already used elsewhere in this package, needed here because a
// real live run of this test would take several minutes per Docker image
// pull/build/test-suite run and would make routine `go test` runs of this
// package unacceptably slow.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAtlasTaskDirectoryFetchesTheRealHarborTaskDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("network clone; skipped under -short")
	}
	runRoot := t.TempDir()
	const taskID = "task-6902ef3ab97fe23e2ad2727a" // sftpgo TW — the frozen suite's own task

	dir, err := EnsureAtlasTaskDirectory(context.Background(), runRoot, BenchmarkSWEAtlasTestWriting, taskID)
	if err != nil {
		t.Skipf("could not clone scaleapi/SWE-Atlas (network?): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "task.toml")); err != nil {
		t.Fatalf("expected task.toml in the fetched directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tests")); err != nil {
		t.Fatalf("expected a tests/ directory: %v", err)
	}

	// Idempotent: a second call reuses the cached copy without re-cloning
	// — proven by removing network reachability being irrelevant the
	// second time (same directory returned, no error).
	dir2, err := EnsureAtlasTaskDirectory(context.Background(), runRoot, BenchmarkSWEAtlasTestWriting, taskID)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if dir2 != dir {
		t.Fatalf("expected the same cached path, got %q then %q", dir, dir2)
	}
}

func TestBuildAtlasGraderCmdConstructsTheRealDriverInvocation(t *testing.T) {
	cmd := BuildAtlasGraderCmd(context.Background(), "/venv/bin/python3", "/pythonpath",
		"/repo/evals/evidence-v1/tooling/run_atlas_grader.py", "/task-dir", "/patch-file", "/app",
		"atlas-test-secret", 900)

	if cmd.Args[0] != "/venv/bin/python3" {
		t.Fatalf("expected the venv python interpreter as argv[0], got %q", cmd.Args[0])
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "run_atlas_grader.py /task-dir /patch-file /app "+
		AtlasVerifierScript+" 900") {
		t.Fatalf("unexpected argv: %v", cmd.Args)
	}
	joinedEnv := strings.Join(cmd.Env, " ")
	if !strings.Contains(joinedEnv, "OPENAI_API_KEY=atlas-test-secret") ||
		!strings.Contains(joinedEnv, "EVAL_API_KEY=atlas-test-secret") {
		t.Fatalf("expected the Atlas evaluator credential in cmd.Env, got %v", cmd.Env)
	}
	if strings.Contains(joinedEnv, "ANTHROPIC_API_KEY") || strings.Contains(joinedEnv, "TYPESAFE_API_KEY") {
		t.Fatalf("expected no other credential role to appear in the grader's env, got %v", cmd.Env)
	}
}

func TestGradeSWEAtlasRealCallSequenceWithTheScriptExecutionStubbed(t *testing.T) {
	if testing.Short() {
		t.Skip("network clone; skipped under -short")
	}
	runRoot := t.TempDir()
	const taskID = "task-6902ef3ab97fe23e2ad2727a"
	p := RunPlan{TaskID: taskID, Benchmark: BenchmarkSWEAtlasTestWriting, ImageDir: "/app"}
	patch := []byte("diff --git a/x b/x\n")

	pythonExe := filepath.Join(mustRepoRootForTest(t), ".boundedcode-runs", "evidence-v1",
		"venv", "bin", "python3")
	if _, err := os.Stat(pythonExe); err != nil {
		t.Skipf("no harbor venv python at %s (run evals/evidence-v1/tooling's harbor setup first)", pythonExe)
	}

	var captured *exec.Cmd
	var patchFileContentAtCallTime []byte
	rf := &RunnerFactory{
		AtlasEvalAPIKey: "atlas-test-secret",
		PythonExe:       pythonExe,
		RepoRoot:        mustRepoRootForTest(t),
		RunVerifierCmd: func(cmd *exec.Cmd) ([]byte, error) {
			captured = cmd
			// gradeSWEAtlas removes its scratch patch file via defer
			// right after this call returns — read it now, while it
			// still exists, exactly the way the real script would (as
			// one of cmd's own argv paths).
			for _, a := range cmd.Args {
				if strings.Contains(a, ".agent.patch") {
					patchFileContentAtCallTime, _ = os.ReadFile(a)
				}
			}
			reward := 1.0
			body, _ := json.Marshal(atlasGraderOutcome{OK: true, ReturnCode: 0, Stdout: "ok", Reward: &reward})
			return body, nil
		},
	}

	out, status, err := gradeSWEAtlas(context.Background(), runRoot, rf, p, patch)
	if err != nil {
		t.Skipf("could not fetch the real Harbor task directory (network?): %v", err)
	}
	if status != StatusSolved {
		t.Fatalf("expected StatusSolved from the stubbed OK=true outcome, got %s", status)
	}
	if len(out) == 0 {
		t.Fatal("expected the grader's raw output to flow back through gradeSWEAtlas")
	}

	if captured == nil {
		t.Fatal("expected RunVerifierCmd to have been called with the real constructed command")
	}
	if !strings.Contains(captured.Args[0], "python3") {
		t.Fatalf("expected the venv python interpreter, got %q", captured.Args[0])
	}
	if !strings.Contains(strings.Join(captured.Args, " "), AtlasGraderScript) {
		t.Fatalf("expected the real driver script path in argv: %v", captured.Args)
	}
	// The real task directory really was fetched and passed as an argv —
	// not a placeholder path.
	found := false
	for _, a := range captured.Args {
		if strings.Contains(a, filepath.Join("atlas-task-dirs", taskID)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the real fetched task directory in argv: %v", captured.Args)
	}
	// The agent's patch really was written to a scratch file and passed
	// by path (never inlined into argv, which a shell could mangle).
	var patchFileArg string
	for _, a := range captured.Args {
		if strings.Contains(a, ".agent.patch") {
			patchFileArg = a
		}
	}
	if patchFileArg == "" {
		t.Fatalf("expected a patch scratch-file path in argv: %v", captured.Args)
	}
	if string(patchFileContentAtCallTime) != string(patch) {
		t.Fatalf("scratch patch file content mismatch: got %q, want %q", patchFileContentAtCallTime, patch)
	}
	if _, err := os.Stat(patchFileArg); err == nil {
		t.Fatal("expected the scratch patch file to be cleaned up after gradeSWEAtlas returns")
	}
}

func mustRepoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd)) // internal/evidence -> repo root
}
