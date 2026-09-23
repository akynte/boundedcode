package eval_test

// PIPELINE_TEST_ONLY — design instructions §20/§21/§24 of the Evidence
// Suite v1 execution-integration task.
//
// This proves the wiring, never a benchmark result: it runs the REAL,
// unmodified internal/task.Runner (the same supervisor/verify/accept path
// every ordinary BoundedCode task goes through) against a REAL,
// already-sanitized Evidence Suite v1 workspace, with only the generator
// replaced by a deterministic fake that performs one known, trivial edit
// through the same interface a real engine uses (engine.Engine.Step). The
// sandbox is the REAL internal/sandbox/container.Runner, executing
// verification inside the REAL pinned official SWE-Bench Pro Verified
// image — internal/eval's own pre-existing seam
// (TaskOrigin.PinnedRuntime/Runtime, exercised identically in
// runtime_validation_test.go) is reused here without modification.
//
// What this is NOT: an agent solving the task, a new "benchmark engine", or
// a claim that BoundedCode resolved anything. No text produced by any
// model, real or fake, is graded here as a benchmark result — see the
// FAKE_ENGINE_STEP marker in the diff this test produces, and the naming of
// every artifact this test writes under a "pipelinetest-only" name.
//
// Skips (does not fail) if: Docker is unavailable, the pinned image has not
// been pulled, or the sanitized workspace this test depends on does not
// exist — see evals/evidence-v1/tooling/materialize_task.py, which must be
// run once (network + Docker, no credential) before this test can exercise
// anything.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

// pipelineTestVulsImage and pipelineTestVulsWorkspace are the exact,
// already-verified artifacts this test reuses — the same pinned image and
// sanitized workspace evals/evidence-v1/preflight.json and
// sanitization-results.json record, never re-derived here.
const (
	// Pinned to the digest, not the tag: internal/sandbox/container.Pinned
	// refuses a tag reference outright ("a measurement must name what it ran
	// against, and a tag does not") — confirmed by this test failing with
	// exactly that message before this constant was corrected to the
	// digest form.
	pipelineTestVulsImage  = "jefzda/sweap-images@sha256:e5310e43e886b49210c59ca5c79571235f6c38ccd50035e1bf4bd6bac02a6034"
	pipelineTestVulsTaskID = "instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-" +
		"v1151a6325649aaf997cd541ebe533b53fddf1b07"
	pipelineTestEditMarker = "// FAKE_ENGINE_STEP: PIPELINE_TEST_ONLY, not a real fix, never graded\n"
)

// evidenceSuiteSanitizedWorkspace returns a throwaway copy of the real
// materialized sanitized workspace, never the workspace itself: a linked
// git worktree created against it (task.Worktrees.Create) shares the same
// object database and refs, so running any task there — even a rejected
// one — permanently adds a branch to it (confirmed by this test's first
// run, which left a second commit reachable in "control" until this copy
// step was added). Reserved for real Evidence Suite v1 arm runs, this
// directory must stay exactly as sanitize_workspace.py left it; a pipeline
// test gets its own disposable copy under the guarded run root instead.
func evidenceSuiteSanitizedWorkspace(t *testing.T, taskID, arm string) string {
	t.Helper()
	root := findRepoRootForPipelineTest(t)
	src := filepath.Join(root, ".boundedcode-runs", "evidence-v1", "agent-workspaces", taskID, arm)
	if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
		t.Skipf("sanitized workspace not materialized at %s: %v — run "+
			"evals/evidence-v1/tooling/materialize_task.py first", src, err)
	}
	dst := filepath.Join(root, ".boundedcode-runs", "evidence-v1", "pipelinetest-copies", t.Name())
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("copying the sanitized workspace for a disposable pipeline-test run: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dst) })
	return dst
}

func findRepoRootForPipelineTest(t *testing.T) string {
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

func requirePipelineTestImage(t *testing.T, image string) {
	t.Helper()
	if ok, why := (&container.Runner{}).Available(context.Background()); !ok {
		t.Skipf("no usable container runtime: %s", why)
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("image not pulled: %s (run evals/evidence-v1/tooling/materialize_task.py or "+
			"docker pull it directly first)", image)
	}
}

// fakeEditEngine performs exactly one deterministic, harmless edit through
// the same interface a real engine.Engine implementation uses, then claims
// done. It never reasons, never calls a model, never reads Objective to
// decide what to do — the edit is fixed before Step is ever called, which
// is what makes this a pipeline test rather than a (fake) agent.
type fakeEditEngine struct {
	editPath string
	edited   bool
}

func (e *fakeEditEngine) Name() string                 { return "pipelinetest-fake-engine" }
func (e *fakeEditEngine) Health(context.Context) error { return nil }
func (e *fakeEditEngine) Close() error                 { return nil }
func (e *fakeEditEngine) Edits() bool                  { return true }

func (e *fakeEditEngine) Step(_ context.Context, req engine.Request) (*engine.Response, error) {
	full := filepath.Join(req.Worktree, e.editPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(body), pipelineTestEditMarker) {
		if err := os.WriteFile(full, append([]byte(pipelineTestEditMarker), body...), 0o644); err != nil {
			return nil, err
		}
		e.edited = true
	}
	return &engine.Response{
		Summary:    "PIPELINE_TEST_ONLY: one deterministic marker line prepended; not a real fix",
		ClaimsDone: true,
	}, nil
}

// TestEvidenceSuitePipelineWiring is the PIPELINE_TEST_ONLY proof design
// instructions §20/§24 ask for, run against a real SWE-Bench Pro Verified
// task: real internal/task.Runner, real internal/sandbox/container.Runner
// against the real pinned image, real internal/worktree linked-worktree
// creation from the real sanitized single-commit repository, real patch
// extraction, and a real application of that patch to a fresh pristine
// container of the same image — proving the whole chain design instruction
// §8 describes (edit -> command observes the edit -> patch -> pristine
// grading) without a single model call.
func TestEvidenceSuitePipelineWiring(t *testing.T) {
	requirePipelineTestImage(t, pipelineTestVulsImage)
	repo := evidenceSuiteSanitizedWorkspace(t, pipelineTestVulsTaskID, "control")

	// Verify the sanitized-git invariant one more time, immediately before
	// use — design instruction §9: reachable commits == 1, no remotes, no
	// tags.
	out, err := exec.Command("git", "-C", repo, "log", "--all", "--oneline").Output()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.TrimSpace(string(out))); n == 0 {
		t.Fatal("sanitized repo has no commits")
	}
	if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) != 1 {
		t.Fatalf("expected exactly 1 reachable commit in the sanitized repo, found %d: %v",
			len(lines), lines)
	}
	if remotes, _ := exec.Command("git", "-C", repo, "remote").Output(); len(strings.TrimSpace(string(remotes))) != 0 {
		t.Fatalf("sanitized repo unexpectedly has a remote: %s", remotes)
	}

	// store.OpenRoot needs a directory Docker Desktop's file-sharing
	// allowlist actually permits bind-mounting — t.TempDir() (under the
	// OS temp dir) is not shared by default on this host, and the container
	// runner's mount would fail with "mounts denied" before verification
	// ever ran (confirmed directly: this test failed exactly that way
	// before this was changed). Every path under the guarded run root is
	// already proven shared, so the store lives there instead, in its own
	// disposable subdirectory.
	storeRoot := filepath.Join(filepath.Dir(filepath.Dir(repo)), "pipelinetest-store", t.Name())
	if err := os.RemoveAll(storeRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(storeRoot) })
	root, err := store.OpenRoot(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	st, err := root.OpenWorkspace(context.Background(),
		workspace.DeriveID("/evidence-suite/pipelinetest", "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}

	fakeEngine := &fakeEditEngine{editPath: "README.md"}
	r, err := task.NewRunner(st, fakeEngine, &container.Runner{}, "evidence-pipeline-test")
	if err != nil {
		t.Fatal(err)
	}
	r.Logf = t.Logf
	// The real seam: internal/eval.SystemSolver.solveSupervised builds this
	// exact shape of sandbox.Spec from Task.Origin when PinnedRuntime() is
	// true (see internal/eval/solver.go). Reproduced directly here since
	// this test bypasses eval.Task's fixture-file loading (Evidence Suite
	// v1's tasks are not evals/tasks/*.task.yaml fixtures) in favor of
	// calling task.NewRunner directly — the narrower, lower-level seam.
	r.SandboxSpec = sandbox.Spec{
		Image: pipelineTestVulsImage, ImageDir: "/app",
		Network: sandbox.NetworkNone,
	}

	ctx := context.Background()
	id := task.NewID("evidence-pipelinetest")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID:           id,
		Title:        "PIPELINE_TEST_ONLY: prove Evidence Suite v1 execution wiring, not a real objective",
		Verification: recipe.Low,
		Budget: task.Budget{
			MaxAttempts: 1, MaxWallTime: 10 * time.Minute,
			Scope: []string{"README.md"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	outcome, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}
	if !fakeEngine.edited {
		t.Fatal("the fake engine's Step was never actually invoked to edit the worktree — the " +
			"pipeline did not reach the engine at all")
	}
	t.Logf("outcome: accepted=%v state=%s reasons=%v", outcome.Accepted, outcome.Task.State, outcome.Reasons)
	if len(outcome.Results) == 0 {
		t.Fatal("no verification results were recorded — the real container-runtime verification " +
			"path did not execute")
	}
	for _, res := range outcome.Results {
		head := res.Summary.Headline + " " + res.Err
		for _, broken := range []string{"executable file not found", "command not found",
			"no such file or directory", "No module named"} {
			if strings.Contains(strings.ToLower(head), strings.ToLower(broken)) {
				t.Errorf("verification result for %s looks like the runtime never started, not a "+
					"real check outcome: %s", res.Kind, strings.TrimSpace(head))
			}
		}
	}

	// The Runner already computed the change it produced — design
	// instruction §7/§10's "extract the patch" is this field, not a manual
	// git-diff of a worktree that Runner.cleanup may already have removed.
	diffOut := []byte(outcome.Diff)
	if !strings.Contains(outcome.Diff, "FAKE_ENGINE_STEP") {
		t.Fatalf("outcome.Diff does not contain the fake engine's marker edit:\n%s", outcome.Diff)
	}
	t.Logf("extracted patch (%d bytes), PIPELINE_TEST_ONLY, never to be graded as a benchmark result",
		len(diffOut))
	// outcome.Diff can end without a trailing newline on its final context
	// line (confirmed directly: a hex dump of the raw bytes ended ...0a20,
	// a newline then a bare space with nothing after it), which busybox's
	// `git apply` inside the grading container refuses as "corrupt patch".
	// A unified diff's lines must each be newline terminated; restoring one
	// is normalization of the format, not a change to its content, so it
	// is applied here, at the grading boundary, rather than to the
	// Runner's own diff computation, which is unrelated to Evidence Suite
	// v1's grading step specifically.
	if len(diffOut) > 0 && diffOut[len(diffOut)-1] != '\n' {
		diffOut = append(diffOut, '\n')
	}

	applyPipelineTestPatchToFreshPristineContainer(t, pipelineTestVulsImage, diffOut)

	// Record this run's provenance clearly as a pipeline test, never
	// benchmark evidence — design instruction §21.
	record := map[string]any{
		"kind":           "PIPELINE_TEST_ONLY",
		"task_id":        pipelineTestVulsTaskID,
		"fake_engine":    fakeEngine.Name(),
		"accepted":       outcome.Accepted,
		"state":          string(outcome.Task.State),
		"result_kinds":   len(outcome.Results),
		"patch_bytes":    len(diffOut),
		"do_not_use_for": "benchmark scoring, solve-rate claims, or any Evidence Suite v1 result table",
	}
	body, _ := json.MarshalIndent(record, "", "  ")
	t.Logf("%s", body)
}

// applyPipelineTestPatchToFreshPristineContainer proves the extracted patch
// is portable back into an untouched official image — a fresh container,
// never the one verification ran in, exactly matching the separation
// design instruction §11 requires between execution and grading.
func applyPipelineTestPatchToFreshPristineContainer(t *testing.T, image string, patch []byte) {
	t.Helper()
	tmp := t.TempDir()
	patchFile := filepath.Join(tmp, "pipelinetest.patch")
	if err := os.WriteFile(patchFile, patch, 0o600); err != nil {
		t.Fatal(err)
	}

	run := exec.Command("docker", "run", "-d", "--rm", "--entrypoint", "/bin/bash", image, "-c", "sleep 120")
	out, err := run.Output()
	if err != nil {
		t.Fatalf("starting the pristine grading container: %v", err)
	}
	cid := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "stop", "-t", "2", cid).Run() })

	if err := exec.Command("docker", "cp", patchFile, cid+":/tmp/pipelinetest.patch").Run(); err != nil {
		t.Fatalf("copying the patch into the grading container: %v", err)
	}
	checkOut, err := exec.Command("docker", "exec", cid, "bash", "-c",
		"cd /app && git apply --check /tmp/pipelinetest.patch").CombinedOutput()
	if err != nil {
		t.Fatalf("patch does not apply cleanly to the pristine image: %v\n%s", err, checkOut)
	}
	applyOut, err := exec.Command("docker", "exec", cid, "bash", "-c",
		"cd /app && git apply /tmp/pipelinetest.patch && head -1 README.md").CombinedOutput()
	if err != nil {
		t.Fatalf("applying the patch in the grading container: %v\n%s", err, applyOut)
	}
	if !strings.Contains(string(applyOut), "FAKE_ENGINE_STEP") {
		t.Fatalf("patch applied but the marker edit is not visible in the grading container: %s", applyOut)
	}
}
