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

// casesTest reads table cases from testdata, as Prometheus's TestEvaluations
// reads promql/testdata/*.test.
const casesTest = "package calc\n\nimport (\n\t\"bufio\"\n\t\"fmt\"\n\t\"os\"\n\t\"testing\"\n)\n\n" +
	"func TestCases(t *testing.T) {\n\tf, err := os.Open(\"testdata/cases.txt\")\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tdefer f.Close()\n" +
	"\ts := bufio.NewScanner(f)\n\tfor s.Scan() {\n\t\tvar v, max, want int\n\t\tif _, err := fmt.Sscan(s.Text(), &v, &max, &want); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n" +
	"\t\tif got := Clamp(v, max); got != want {\n\t\t\tt.Errorf(\"Clamp(%d, %d) = %d, want %d\", v, max, got, want)\n\t\t}\n\t}\n}\n"

// TestBehaviourEvidenceGoCases covers the 2026-10-05 validation's
// Prometheus false negative (a data-driven test file read by a test
// elsewhere), tests that only fail to build on the base (new API, the caddy
// development false pass), and failures the base already had.
func TestBehaviourEvidenceGoCases(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	gomod := "module example.com/calc\n\ngo 1.22\n"
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	cases := []struct {
		name      string
		base      map[string]string
		change    map[string]string
		want      bool
		reason    string
		wantTests []string
	}{
		{"data-driven case fails on base",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": casesTest, "testdata/cases.txt": "9 5 5\n"},
			map[string]string{"calc.go": fixedGo, "testdata/cases.txt": "9 5 5\n-1 5 0\n"},
			true, "fail on the base", []string{"TestCases"}},
		{"data-driven case in a subpackage",
			map[string]string{"go.mod": gomod, "pkg/calc/calc.go": buggyGo, "pkg/calc/calc_test.go": casesTest, "pkg/calc/testdata/cases.txt": "9 5 5\n"},
			map[string]string{"pkg/calc/calc.go": fixedGo, "pkg/calc/testdata/cases.txt": "9 5 5\n-1 5 0\n"},
			true, "fail on the base", []string{"TestCases"}},
		{"data-driven case also passes on base",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": casesTest, "testdata/cases.txt": "9 5 5\n"},
			map[string]string{"calc.go": fixedGo, "testdata/cases.txt": "9 5 5\n3 5 3\n"},
			false, "also pass on the base", nil},
		{"test of new API does not build on base",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": oldTest},
			map[string]string{"calc.go": buggyGo + "\nfunc Abs(v int) int {\n\tif v < 0 {\n\t\treturn -v\n\t}\n\treturn v\n}\n",
				"calc_test.go": oldTest + "\nfunc TestAbs(t *testing.T) {\n\tif Abs(-2) != 2 {\n\t\tt.Fatal(\"abs\")\n\t}\n}\n"},
			false, "do not build or load", nil},
		{"failure the base already had",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": oldTest + "\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n"},
			map[string]string{"calc.go": fixedGo, "calc_test.go": oldTest + "\n// TestBroken is known to fail.\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n"},
			false, "also pass on the base", nil},
		{"unrelated test fails only with the change's tests",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": oldTest,
				"flaky_test.go": "package calc\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestFlaky(t *testing.T) {\n\tif _, err := os.Stat(\"calc_test.go\"); err == nil {\n\t\tif b, _ := os.ReadFile(\"calc_test.go\"); len(b) > 150 {\n\t\t\tt.Fatal(\"flake\")\n\t\t}\n\t}\n}\n"},
			map[string]string{"calc.go": fixedGo, "calc_test.go": oldTest + "\nfunc TestClampMid(t *testing.T) {\n\tif Clamp(3, 5) != 3 {\n\t\tt.Fatal(\"mid\")\n\t}\n}\n"},
			false, "also pass on the base", nil},
		{"new failing test next to an old failure",
			map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": oldTest + "\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n"},
			map[string]string{"calc.go": fixedGo, "calc_test.go": oldTest + "\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n" +
				"\nfunc TestClampNegative(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n"},
			true, "fail on the base", []string{"TestClampNegative"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, b := commitRepo(t, c.base)
			writeFiles(t, repo, c.change)
			ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "calc", Worktree: repo, Base: b, TaskID: "t"})
			if err != nil {
				t.Fatal(err)
			}
			if ev.Verified != c.want || !strings.Contains(ev.Reason, c.reason) {
				t.Fatalf("evidence = %+v", ev)
			}
			if c.want && strings.Join(ev.Tests, ",") != strings.Join(c.wantTests, ",") {
				t.Fatalf("tests = %v, want %v", ev.Tests, c.wantTests)
			}
		})
	}
}

// TestBehaviourEvidenceNonGoLoadFailure: a JavaScript test that fails on the
// base only because it imports a module the change adds is not evidence.
func TestBehaviourEvidenceNonGoLoadFailure(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil || testing.Short() {
		t.Skip("node required")
	}
	ctx := context.Background()
	repo, b := commitRepo(t, map[string]string{
		"package.json":    `{"scripts":{"test":"node test/run.js"}}`,
		"node_modules/.k": "",
		"lib.js":          "exports.clamp = (v, max) => v > max ? max : v;\n",
		"test/run.js":     "const {clamp} = require('../lib');\nif (clamp(9, 5) !== 5) process.exit(1);\n",
		".gitignore":      "node_modules/\n",
	})
	writeFiles(t, repo, map[string]string{
		"abs.js":      "exports.abs = (v) => v < 0 ? -v : v;\n",
		"test/run.js": "const {clamp} = require('../lib');\nconst {abs} = require('../abs');\nif (clamp(9, 5) !== 5 || abs(-2) !== 2) process.exit(1);\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "js", Worktree: repo, Base: b, TaskID: "t", Source: repo})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Verified || !strings.Contains(ev.Reason, "do not build or load") {
		t.Fatalf("evidence = %+v", ev)
	}
}

func TestIsTestFile(t *testing.T) {
	for p, want := range map[string]bool{
		"calc_test.go":                    true,
		"src/a.test.ts":                   true,
		"src/a.spec.mjs":                  true,
		"promql/testdata/functions.test":  true,
		"test/run.js":                     true,
		"tests/fixtures/x.json":           true,
		"caddytest/integration/caddyfile": true,
		"internal/test_utils/x.go":        true,
		"e2e-tests/login.ts":              true,
		"src/__tests__/a.js":              true,
		"spec/models/user_spec.rb":        true,
		"testing/helpers.py":              true,
		"calc.go":                         false,
		"api/latest/handler.go":           false,
		"pkg/contest/score.go":            false,
		"attest/sign.go":                  false,
		"docs/protest.md":                 false,
		"src/testament.js":                false,
		"Makefile":                        false,
	} {
		if got := IsTestFile(p); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestBehaviourEvidenceReviewCases covers the code review of the evidence
// change: a control run that does not produce test results must not turn
// the base's own failures into evidence; Go inputs under testdata (for
// example analysistest sources) are test data; a stage that cannot run is
// reported as such.
func TestBehaviourEvidenceReviewCases(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	gomod := "module example.com/calc\n\ngo 1.22\n"
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	run := func(t *testing.T, base, change map[string]string) Evidence {
		t.Helper()
		repo, b := commitRepo(t, base)
		writeFiles(t, repo, change)
		ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "calc", Worktree: repo, Base: b, TaskID: "t"})
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	t.Run("control run skipped", func(t *testing.T) {
		// The test stage is skipped (exit 127, optional) in the control tree
		// only, so its failures are unknown; TestBroken fails on the base
		// anyway and must not count.
		cfg := "version: 1\nstages:\n  - name: go-test\n    optional: true\n    tests: true\n    requires: [go.mod]\n" +
			"    run: [sh, -c, 'case \"$PWD\" in */control*) exit 127;; esac; go test -count=1 ./...']\n"
		ev := run(t, map[string]string{"go.mod": gomod, ".boundedcode/verification.yaml": cfg, "calc.go": buggyGo, "calc_test.go": casesTest,
			"broken_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n", "testdata/cases.txt": "9 5 5\n"},
			map[string]string{"calc.go": fixedGo, "testdata/cases.txt": "9 5 5\n3 5 3\n"})
		if ev.Verified {
			t.Fatalf("base failures counted without a control result: %+v", ev)
		}
	})
	t.Run("go input under testdata", func(t *testing.T) {
		readsGo := strings.Replace(casesTest, `"testdata/cases.txt"`, `"testdata/src/cases.go"`, 1)
		readsGo = strings.Replace(readsGo, "\tfor s.Scan() {\n", "\tfor s.Scan() {\n\t\tif len(s.Text()) == 0 || s.Text()[0] == '/' {\n\t\t\tcontinue\n\t\t}\n", 1)
		ev := run(t, map[string]string{"go.mod": gomod, "calc.go": buggyGo, "calc_test.go": readsGo, "testdata/src/cases.go": "// cases\n9 5 5\n"},
			map[string]string{"calc.go": fixedGo, "testdata/src/cases.go": "// cases\n9 5 5\n-1 5 0\n"})
		if !ev.Verified || strings.Join(ev.Tests, ",") != "TestCases" {
			t.Fatalf("evidence = %+v", ev)
		}
	})
	t.Run("stage cannot run", func(t *testing.T) {
		cfg := "version: 1\nstages:\n  - name: go-test\n    tests: true\n    requires: [go.mod]\n    run: [bc-no-such-test-runner, ./...]\n"
		ev := run(t, map[string]string{"go.mod": gomod, ".boundedcode/verification.yaml": cfg, "calc.go": buggyGo, "calc_test.go": oldTest},
			map[string]string{"calc.go": fixedGo, "calc_test.go": oldTest + "\nfunc TestClampNegative(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n"})
		if ev.Verified || !strings.Contains(ev.Reason, "could not run on the base commit") || !strings.Contains(ev.Reason, "not installed") {
			t.Fatalf("evidence = %+v", ev)
		}
	})
}

func TestNewLoadFailure(t *testing.T) {
	if newLoadFailure("PASS a\nFAIL b: expected 2", "PASS a") {
		t.Error("an assertion failure is not a load failure")
	}
	if !newLoadFailure("Error: Cannot find module '../abs'", "") {
		t.Error("a missing module is a load failure")
	}
	if newLoadFailure("warn: Cannot find module 'optional-dep'\nFAIL b", "warn: Cannot find module 'optional-dep'") {
		t.Error("a load message the base already prints does not count")
	}
}
