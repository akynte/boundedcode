package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

func TestTaskSpecsLoad(t *testing.T) {
	tasks, err := LoadTasks("../../benchmarks/tasks")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 10 {
		t.Fatalf("only %d tasks", len(tasks))
	}
	cats := map[string]bool{}
	for _, tk := range tasks {
		cats[tk.Category] = true
	}
	if len(cats) < 8 {
		t.Fatalf("categories: %v", cats)
	}
}

// TestHiddenChecksFailOnBase proves every task is non-trivial: its hidden
// acceptance checks must fail on the untouched base. Runs checks in the
// sandbox image when BC_TEST_DOCKER_IMAGE is set (needs python3/node/go).
func TestHiddenChecksFailOnBase(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	tasks, err := LoadTasks("../../benchmarks/tasks")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-bench-spec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	sb := &sandbox.Container{Engine: "docker", Image: image, Network: "none", UID: os.Getuid(), GID: os.Getgid()}
	for _, spec := range tasks {
		t.Run(spec.ID, func(t *testing.T) {
			ctx := context.Background()
			repos, err := materialize(ctx, "../../benchmarks/fixtures/"+spec.Fixture, filepath.Join(root, spec.ID), spec.Setup)
			if err != nil {
				t.Fatal(err)
			}
			for _, h := range spec.Hidden {
				p := filepath.Join(repos[h.Repo], h.Path)
				_ = os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte(h.Content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			failed := 0
			var outputs []string
			for _, c := range spec.Checks {
				dir := repos[c.Repo]
				cmd, err := sb.Command(ctx, sandbox.Spec{Argv: c.Run, Workdir: dir, Mounts: []sandbox.Mount{{Host: dir, Target: dir}},
					Env: map[string]string{"GOFLAGS": "-buildvcs=false"}})
				if err != nil {
					t.Fatal(err)
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					failed++
				}
				outputs = append(outputs, string(out))
				// Infrastructure errors (missing tools) would make every task "fail" for the wrong reason.
				for _, bad := range []string{"executable file not found", "command not found", "No such file or directory: 'python3'", "Unable to find image"} {
					if strings.Contains(string(out), bad) {
						t.Fatalf("check infrastructure problem: %s", out)
					}
				}
			}
			if failed == 0 {
				t.Fatalf("hidden checks pass on the untouched base: task is trivial\n%s", strings.Join(outputs, "\n"))
			}
		})
	}
}
