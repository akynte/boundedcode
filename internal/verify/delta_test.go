package verify

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

func commitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	writeFiles(t, repo, files)
	ctx := context.Background()
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	return repo, base
}

const buggyGo = "package calc\n\n// Clamp limits v to [0, max].\nfunc Clamp(v, max int) int {\n\tif v > max {\n\t\treturn max\n\t}\n\treturn v\n}\n"
const fixedGo = "package calc\n\n// Clamp limits v to [0, max].\nfunc Clamp(v, max int) int {\n\tif v > max {\n\t\treturn max\n\t}\n\tif v < 0 {\n\t\treturn 0\n\t}\n\treturn v\n}\n"
const oldTest = "package calc\n\nimport \"testing\"\n\nfunc TestClampHigh(t *testing.T) {\n\tif Clamp(9, 5) != 5 {\n\t\tt.Fatal(\"high\")\n\t}\n}\n"

// TestBehaviourEvidence is the regression test for the 2026-10-04/05
// false passes: changes that passed every configured check without any test
// showing the requested behaviour (two of them did nothing at all).
func TestBehaviourEvidence(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	base := map[string]string{"go.mod": "module example.com/calc\n\ngo 1.22\n", "calc.go": buggyGo, "calc_test.go": oldTest}
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	cases := []struct {
		name   string
		change map[string]string
		want   bool
		reason string
	}{
		{"code only, no test", map[string]string{"calc.go": fixedGo}, false, "no tests"},
		{"new test also passes on base", map[string]string{"calc.go": fixedGo,
			"calc_test.go": oldTest + "\nfunc TestClampMid(t *testing.T) {\n\tif Clamp(3, 5) != 3 {\n\t\tt.Fatal(\"mid\")\n\t}\n}\n"}, false, "also pass on the base"},
		{"reproduction test", map[string]string{"calc.go": fixedGo,
			"calc_test.go": oldTest + "\nfunc TestClampNegative(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n"}, true, "fail on the base"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, b := commitRepo(t, base)
			writeFiles(t, repo, c.change)
			ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "calc", Worktree: repo, Base: b, TaskID: "t"})
			if err != nil {
				t.Fatal(err)
			}
			if ev.Verified != c.want || !strings.Contains(ev.Reason, c.reason) {
				t.Fatalf("evidence = %+v", ev)
			}
			if c.want && (len(ev.Tests) != 1 || ev.Tests[0] != "TestClampNegative") {
				t.Fatalf("tests = %v (only the new reproduction test may count)", ev.Tests)
			}
		})
	}
}

// TestBehaviourEvidenceNonGo: without per-test names, the same test stage
// must pass on the untouched base and fail with the change's tests.
func TestBehaviourEvidenceNonGo(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil || testing.Short() {
		t.Skip("node required")
	}
	ctx := context.Background()
	files := map[string]string{
		"package.json":    `{"scripts":{"test":"node test/run.js"}}`,
		"node_modules/.k": "",
		"lib.js":          "exports.clamp = (v, max) => v > max ? max : v;\n",
		"test/run.js":     "const {clamp} = require('../lib');\nif (clamp(9, 5) !== 5) process.exit(1);\n",
		".gitignore":      "node_modules/\n",
	}
	repo, b := commitRepo(t, files)
	writeFiles(t, repo, map[string]string{
		"lib.js":      "exports.clamp = (v, max) => v > max ? max : v < 0 ? 0 : v;\n",
		"test/run.js": "const {clamp} = require('../lib');\nif (clamp(9, 5) !== 5 || clamp(-1, 5) !== 0) process.exit(1);\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "js", Worktree: repo, Base: b, TaskID: "t", Source: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Verified {
		t.Fatalf("evidence = %+v", ev)
	}
}
