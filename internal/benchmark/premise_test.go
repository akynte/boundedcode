package benchmark

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// premise materializes spec (with its reference applied when withRef),
// writes the hidden files and runs the checks with run; it returns
// whether every check passed, and the checks' output.
func premise(t *testing.T, spec TaskSpec, root string, withRef bool, run func(dir string, argv []string) (string, error)) (bool, string) {
	t.Helper()
	ctx := context.Background()
	spec, _ = plantCanaries(spec)
	fixture := ""
	if spec.Fixture != "" {
		fixture = "../../benchmarks/fixtures/" + spec.Fixture
	}
	repos, err := materialize(ctx, fixture, spec.Sources, root, spec.Setup)
	if err != nil {
		t.Fatal(err)
	}
	if withRef {
		if err := applyEdits(repos, spec.Reference); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range spec.Hidden {
		p := filepath.Join(repos[h.Repo], h.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(h.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ok := true
	var outs []string
	for _, c := range spec.Checks {
		out, err := run(repos[c.Repo], c.Run)
		outs = append(outs, strings.Join(c.Run, " ")+": "+tailLines(out, 8))
		if err != nil {
			ok = false
		}
	}
	return ok, strings.Join(outs, "\n")
}

// TestDirectTaskPremises: for every direct-mode task with a reference
// solution, the hidden checks fail on the base and pass with the reference,
// on this machine's toolchains (a task whose tools are missing is skipped).
// A budget task is graded on keeping the base working, so its checks pass
// on the base too.
func TestDirectTaskPremises(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the tasks' toolchains")
	}
	tasks, err := LoadTasks("../../benchmarks/direct")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range tasks {
		if len(spec.Reference) == 0 || len(spec.Checks) == 0 {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			for _, tool := range checkTools(spec) {
				if _, err := exec.LookPath(tool); err != nil {
					t.Skip(tool + " is not installed")
				}
			}
			run := func(dir string, argv []string) (string, error) {
				return hostCheck(context.Background(), spec, dir, argv)
			}
			if ok, out := premise(t, spec, filepath.Join(t.TempDir(), "base"), false, run); ok && spec.Capability != "budget" {
				t.Errorf("the hidden checks pass on the base:\n%s", out)
			}
			if ok, out := premise(t, spec, filepath.Join(t.TempDir(), "ref"), true, run); !ok {
				t.Errorf("the hidden checks fail with the reference:\n%s", out)
			}
		})
	}
}

// TestBoundedTaskReferences: as TestDirectTaskPremises, for the bounded
// suite's tasks with a reference, in the sandbox image.
func TestBoundedTaskReferences(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	tasks, err := LoadTasks("../../benchmarks/tasks")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	sb := &sandbox.Container{Engine: "docker", Image: image, Network: "none", PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()}
	for _, spec := range tasks {
		if len(spec.Reference) == 0 {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			if missing := imageMissing(t, image, spec.Requires); len(missing) > 0 {
				t.Skip("not in the image: " + strings.Join(missing, ", "))
			}
			run := func(dir string, argv []string) (string, error) {
				cmd, err := sb.Command(context.Background(), checkSpec(context.Background(), spec, dir, argv))
				if err != nil {
					return "", err
				}
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			for _, withRef := range []bool{false, true} {
				dir, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-premise-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				ok, out := premise(t, spec, dir, withRef, run)
				switch {
				case withRef && !ok:
					t.Errorf("the hidden checks fail with the reference:\n%s", out)
				case !withRef && ok:
					t.Errorf("the hidden checks pass on the base:\n%s", out)
				}
			}
		})
	}
}

// checkTools are the executables a task's checks start (their first word,
// and the shell's commands for sh -c), with its declared requirements.
func checkTools(spec TaskSpec) []string {
	tools := slices.Clone(spec.Requires)
	for _, c := range spec.Checks {
		tools = append(tools, c.Run[0])
		if c.Run[0] == "sh" && len(c.Run) > 2 {
			for _, w := range []string{"go", "node", "python3", "npm"} {
				if strings.Contains(" "+c.Run[2], " "+w+" ") {
					tools = append(tools, w)
				}
			}
		}
	}
	return tools
}

// imageMissing lists the tools of names the image lacks.
func imageMissing(t *testing.T, image string, names []string) []string {
	var missing []string
	for _, n := range names {
		if exec.Command("docker", "run", "--rm", "--entrypoint", "sh", image, "-c", "command -v "+n).Run() != nil {
			missing = append(missing, n)
		}
	}
	return missing
}
