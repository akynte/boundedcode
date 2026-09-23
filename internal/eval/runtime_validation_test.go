package eval_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
)

// These prove the runtime, not the model. A task's own verification has to
// execute with the toolchain the task was written against, and it has to
// behave the same way on the untouched tree and on the tree with the known
// fixing change applied. Neither needs inference, and both were the thing the
// previous solve-rate batch could not establish.

// sharedTemp is a directory the container runtime can bind-mount. t.TempDir
// is under /tmp, which a VM-backed Docker does not share by default.
func sharedTemp(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(eval.RuntimeWorkDir(), "runtime-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func runtimeTask(t *testing.T, id string) eval.Task {
	t.Helper()
	for _, task := range loadSet(t) {
		if task.ID == id {
			return task
		}
	}
	t.Skipf("%s is not in the task set", id)
	return eval.Task{}
}

func requireRuntime(t *testing.T, task eval.Task) {
	t.Helper()
	if !task.Origin.PinnedRuntime() {
		t.Skipf("%s declares no pinned runtime", task.ID)
	}
	if _, err := os.Stat(task.FixturePath()); err != nil {
		t.Skipf("fixture not materialized: %v", err)
	}
	if ok, why := (&container.Runner{}).Available(context.Background()); !ok {
		t.Skipf("no container runtime: %s", why)
	}
	if err := exec.Command("docker", "image", "inspect", task.Origin.Runtime()).Run(); err != nil {
		t.Skipf("image not prefetched: %s", task.Origin.Runtime())
	}
}

// verifyIn runs every preset the repository declares, inside the task's
// pinned runtime, and returns the results by preset name.
func verifyIn(t *testing.T, task eval.Task, tree string) map[string]recipe.Result {
	t.Helper()
	presets, err := recipe.DiscoverPresets(tree, recipe.Level(task.Verification))
	if err != nil {
		t.Fatalf("discovering presets: %v", err)
	}
	if len(presets) == 0 {
		t.Fatalf("%s declares no verification preset", task.ID)
	}
	r := &recipe.Runner{
		Sandbox: &container.Runner{},
		Spec: sandbox.Spec{
			Image: task.Origin.Runtime(), ImageDir: task.Origin.RuntimeWorkdir,
			Dir: tree, Network: sandbox.NetworkNone, Env: task.Origin.RuntimeEnv,
		},
	}
	out := map[string]recipe.Result{}
	for _, p := range presets {
		rec := recipe.Recipe{Name: p.Name, Kind: p.Kind, Argv: p.Argv, Dir: p.Dir}
		out[p.Name] = r.Run(context.Background(), rec, tree, "baseline")
	}
	return out
}

// A preset that cannot start is the failure this whole change exists to
// remove: it is indistinguishable in a result table from a change that broke
// the build, and it was every Python task's outcome on the host.
func assertPresetsExecuted(t *testing.T, results map[string]recipe.Result) {
	t.Helper()
	for name, res := range results {
		head := res.Summary.Headline + " " + res.Err
		for _, broken := range []string{
			"No module named", "executable file not found",
			"command not found", "no such file or directory",
		} {
			if strings.Contains(strings.ToLower(head), strings.ToLower(broken)) {
				t.Errorf("preset %q did not execute: %s", name, strings.TrimSpace(head))
			}
		}
	}
}

func applyGold(t *testing.T, task eval.Task, tree string) {
	t.Helper()
	spec := swebenchSpecOf(t, task)
	patch := filepath.Join(tree, ".gold.patch")
	if err := os.WriteFile(patch, []byte(spec.Patch), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "apply", "-p1", ".gold.patch")
	cmd.Dir = tree
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("applying the known-good change: %s", out)
	}
	if err := os.Remove(patch); err != nil {
		t.Fatal(err)
	}
}

// The runtime has to be the task's, not this machine's. If the host's
// toolchain were being used the versions would match the host, and the whole
// point of the pinned image would be lost.
func TestVerificationRunsInTheTasksOwnRuntime(t *testing.T) {
	for _, id := range []string{"SWEBENCH-PYTEST-10051", "SWEBENCH-CADDY-4943"} {
		t.Run(id, func(t *testing.T) {
			task := runtimeTask(t, id)
			requireRuntime(t, task)

			dir := sharedTemp(t)
			copyFixture(t, task.FixturePath(), dir)

			run := &container.Runner{}
			spec := sandbox.Spec{
				Image: task.Origin.Runtime(), ImageDir: task.Origin.RuntimeWorkdir,
				Dir: dir, Network: sandbox.NetworkNone, Env: task.Origin.RuntimeEnv,
			}
			probe := []string{"python3", "--version"}
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				probe = []string{"go", "version"}
			}
			cmd, err := run.Command(context.Background(), spec, probe...)
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v in the pinned runtime: %v\n%s", probe, err, out)
			}
			t.Logf("%s toolchain inside %s: %s", id, task.Origin.Runtime(),
				strings.TrimSpace(string(out)))

			host, _ := exec.Command(probe[0], probe[1:]...).CombinedOutput()
			if strings.TrimSpace(string(out)) == strings.TrimSpace(string(host)) {
				t.Logf("note: the image and this host happen to carry the same %s", probe[0])
			}
		})
	}
}

// Baseline behaves, and the known-good change does not make it behave worse.
//
// The hidden tests are deliberately absent, so this does not say the change
// fixes the bug — the official grader says that, separately. What it says is
// that the repository's own checks run, and that they reach the same verdict
// on the tree with the real fix applied. A runtime where the fix breaks the
// build is a runtime that cannot measure anything.
func TestBaselineAndKnownGoodBehaveInTheRuntime(t *testing.T) {
	for _, id := range []string{"SWEBENCH-PYTEST-10051", "SWEBENCH-CADDY-4943"} {
		t.Run(id, func(t *testing.T) {
			task := runtimeTask(t, id)
			requireRuntime(t, task)

			base := sharedTemp(t)
			copyFixture(t, task.FixturePath(), base)
			baseline := verifyIn(t, task, base)
			assertPresetsExecuted(t, baseline)

			gold := sharedTemp(t)
			copyFixture(t, task.FixturePath(), gold)
			initGit(t, gold)
			applyGold(t, task, gold)
			fixed := verifyIn(t, task, gold)
			assertPresetsExecuted(t, fixed)

			for name, b := range baseline {
				f, ok := fixed[name]
				if !ok {
					t.Errorf("preset %q disappeared after the known-good change", name)
					continue
				}
				t.Logf("%-22s baseline=%-8s known-good=%-8s", name, b.Status, f.Status)
				// A preset that passes on the untouched tree and fails with
				// the real fix applied is reported, not failed. The fixing
				// change is half of a pair: the official flow replaces the
				// test files at the same time, and this deliberately does
				// not inject them, so an existing test asserting the old
				// behaviour is supposed to fail here. What would be a defect
				// is the build breaking, and that is checked above.
				if b.Passed() && !f.Passed() {
					t.Logf("   note: %q passes untouched and fails with the real fix, which is "+
						"expected where the fix is paired with a test change: %s",
						name, f.Summary.Headline)
				}
			}
		})
	}
}

// Nothing the evaluator knows may be readable from the tree the solver edits.
func TestRuntimeTreeHoldsNoOracleMaterial(t *testing.T) {
	for _, id := range []string{"SWEBENCH-PYTEST-10051", "SWEBENCH-CADDY-4943"} {
		t.Run(id, func(t *testing.T) {
			task := runtimeTask(t, id)
			if _, err := os.Stat(task.FixturePath()); err != nil {
				t.Skip("fixture not materialized")
			}
			spec := swebenchSpecOf(t, task)
			var found []string
			err := filepath.Walk(task.FixturePath(), func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err //nolint:wrapcheck // walk error
				}
				rel, _ := filepath.Rel(task.FixturePath(), p)
				if strings.Contains(rel, ".bc-swebench") || strings.HasSuffix(rel, "instance.json") {
					found = append(found, rel)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(found) > 0 {
				t.Errorf("acceptance material inside the fixture: %v", found)
			}
			// And the answer key must not be derivable from the tree: the
			// gold patch and the hidden test patch are non-empty and live
			// only in the acceptance key the solver never sees.
			if spec.Patch == "" || spec.TestPatch == "" {
				t.Fatal("the acceptance key carries no patch; this check would pass vacuously")
			}
			// A contiguous run of added lines, not a single one: one line of
			// a diff is often an import or a brace that the tree already
			// has, and asserting on it would fail for reasons that are not
			// leaks.
			for label, secret := range map[string]string{"test patch": spec.TestPatch, "gold patch": spec.Patch} {
				block := addedBlock(secret)
				if block == "" {
					t.Fatalf("%s yields no distinctive block; this check would pass vacuously", label)
				}
				if fixtureContains(t, task.FixturePath(), block) {
					t.Errorf("a distinctive block of the %s appears inside the fixture", label)
				}
			}
		})
	}
}

// addedBlock returns the longest run of consecutive added lines in a patch,
// which is the part of it least likely to exist in the tree already.
func addedBlock(patch string) string {
	var best, cur []string
	flush := func() {
		if len(cur) > len(best) {
			best = append([]string(nil), cur...)
		}
		cur = nil
	}
	for _, l := range strings.Split(patch, "\n") {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			if t := strings.TrimSpace(strings.TrimPrefix(l, "+")); t != "" {
				cur = append(cur, t)
				continue
			}
		}
		flush()
	}
	flush()
	if len(best) < 3 {
		return ""
	}
	return strings.Join(best, "\n")
}

func fixtureContains(t *testing.T, root, needle string) bool {
	t.Helper()
	if needle == "" {
		return false
	}
	var hit bool
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || hit || info.Size() > 4<<20 {
			return nil //nolint:nilerr // a file we cannot read is not a leak
		}
		body, err := os.ReadFile(p) //nolint:gosec // walking the fixture
		if err != nil {
			return nil //nolint:nilerr // likewise
		}
		if strings.Contains(string(body), needle) {
			hit = true
		}
		return nil
	})
	return hit
}

// swebenchSpecOf reads the official row out of a task's acceptance key. The
// key is the evaluator's; nothing from it reaches a worktree here.
func swebenchSpecOf(t *testing.T, task eval.Task) swebenchInstance {
	t.Helper()
	body, ok := task.Acceptance.Files[swebenchSpec]
	if !ok {
		t.Skipf("%s carries no %s", task.ID, swebenchSpec)
	}
	var spec swebenchInstance
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		t.Fatalf("%s: %v", task.ID, err)
	}
	return spec
}

// initGit makes a tree a repository so a patch can be applied to it.
func initGit(t *testing.T, dir string) {
	t.Helper()
	for _, argv := range [][]string{
		{"init", "-q", "."},
		{"-c", "user.email=eval@local", "-c", "user.name=eval", "add", "-A"},
		{"-c", "user.email=eval@local", "-c", "user.name=eval", "commit", "-qm", "base"},
	} {
		cmd := exec.Command("git", argv...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", argv, out)
		}
	}
}
