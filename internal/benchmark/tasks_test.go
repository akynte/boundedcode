package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/telemetry"
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
	// Same limits as `bench tasks` (the 64-PID default makes Go compiles fail
	// with "resource temporarily unavailable", a false "fails on base").
	sb := &sandbox.Container{Engine: "docker", Image: image, Network: "none", PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()}
	for _, spec := range tasks {
		t.Run(spec.ID, func(t *testing.T) {
			ctx := context.Background()
			fixture := ""
			if spec.Fixture != "" {
				fixture = "../../benchmarks/fixtures/" + spec.Fixture
			}
			repos, err := materialize(ctx, fixture, spec.Sources, filepath.Join(root, spec.ID), spec.Setup)
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
				cmd, err := sb.Command(ctx, checkSpec(ctx, spec, dir, c.Run))
				if err != nil {
					t.Fatal(err)
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					failed++
				}
				if os.Getenv("BC_TEST_VERBOSE") != "" {
					t.Logf("%s: %v\n%s", strings.Join(c.Run, " "), err, tailLines(string(out), 6))
				}
				outputs = append(outputs, string(out))
				// Infrastructure errors (missing tools) would make every task "fail" for the wrong reason.
				for _, bad := range []string{"executable file not found", "command not found", "No such file or directory: 'python3'", "Unable to find image",
					"resource temporarily unavailable", "module lookup disabled"} {
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

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestCollectIntel(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec := telemetry.New(st.DB, nil)
	rec.Emit(ctx, "t1", "context.pack", map[string]any{"tokens": 900, "sections": []map[string]any{{"key": "code", "tokens": 400}},
		"intel": contextplan.IntelStats{NavCalls: 6, NavMillis: 12.5, GraphCalls: 1, NavSymbols: 2}})
	rec.Emit(ctx, "t1", "agent.event", map[string]any{"kind": "ActionEvent", "tool": "terminal"})
	m := collectIntel(ctx, st.DB, "t1")
	if m.Packs != 1 || m.CodeTokens != 400 || m.NavCalls != 6 || m.NavSymbols != 2 || m.GraphCalls != 1 || m.AgentToolCalls != 1 {
		t.Fatalf("%+v", m)
	}
}
