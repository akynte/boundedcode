package verify

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

func stageIndex(c Config) map[string]Stage {
	m := map[string]Stage{}
	for _, st := range c.Stages {
		m[st.Name] = st
	}
	return m
}

func set(files ...string) map[string]bool {
	m := map[string]bool{}
	for _, f := range files {
		m[f] = true
	}
	return m
}

// A repository whose root is no project gets its subprojects' stages, run
// in their directories (the 2026-10-27 baseline's monorepo, which no test
// stage ever ran for).
func TestPresetSubprojects(t *testing.T) {
	c := presetFor(set("README.md", "contracts/summary.schema.json",
		"api/go.mod", "api/order.go", "api/order_test.go",
		"web/package.json", "web/src/summary.ts", "web/packages/ui/package.json",
		"services/billing/Cargo.toml", "services/billing/src/lib.rs",
		"vendor/x/go.mod", "web/node_modules/y/package.json", "test/fixtures/z/go.mod", ".tools/go.mod",
		"a/b/c/d/go.mod", "infra/main.tf"))
	st := stageIndex(c)
	for name, dir := range map[string]string{"api/go-test": "api", "api/go-build": "api", "web/npm-test": "web", "services/billing/cargo-test": "services/billing"} {
		if st[name].Dir != dir {
			t.Errorf("%s: dir %q (stages %v)", name, st[name].Dir, slices.Collect(func(yield func(string) bool) {
				for k := range st {
					if !yield(k) {
						return
					}
				}
			}))
		}
	}
	if s := st["api/go-test"]; !s.Tests || s.Requires[0] != "go.mod" || !slices.Contains(s.Run, "{packages}") {
		t.Errorf("go-test keeps its preset form: %+v", s)
	}
	for name := range st {
		for _, bad := range []string{"vendor/", "node_modules", "fixtures", ".tools", "a/b/c/d", "web/packages/ui", "infra/terraform"} {
			if strings.HasPrefix(name, bad) {
				t.Errorf("unexpected stage %s", name)
			}
		}
	}
	if _, ok := st["terraform-fmt"]; !ok {
		t.Error("the root's tree-wide checks stay")
	}
	if !c.runsTests() {
		t.Error("the monorepo now runs tests")
	}

	// A root Makefile orchestrates its subprojects: the root's stages only.
	for _, s := range presetFor(set("Makefile", "api/go.mod", "web/package.json")).Stages {
		if s.Dir != "" {
			t.Errorf("root Makefile: subproject stage %s", s.Name)
		}
	}
	// A root Go module still gets a nested web package's stages (another
	// ecosystem) and a nested Go module's (./... stops at it); the root's
	// gofmt already covers the nested module's changed files.
	st = stageIndex(presetFor(set("go.mod", "main.go", "main_test.go", "api/go.mod", "api/x_test.go", "web/package.json")))
	for _, name := range []string{"go-test", "api/go-test", "api/go-build", "web/npm-test"} {
		if _, ok := st[name]; !ok {
			t.Errorf("polyglot root: no %s stage", name)
		}
	}
	if _, ok := st["api/gofmt"]; ok {
		t.Error("gofmt twice")
	}
	// A nested package of the root's own ecosystem is the root's (a
	// workspace): no second stage.
	st = stageIndex(presetFor(set("package.json", "test/a.test.js", "packages/ui/package.json")))
	if _, ok := st["packages/ui/npm-test"]; ok {
		t.Error("a nested package of the root's ecosystem got its own stage")
	}
}

func TestStagePackages(t *testing.T) {
	for _, c := range []struct {
		dir  string
		in   []string
		want string
	}{
		{"", []string{"./x", "./y"}, "./x ./y"},
		{"api", []string{"./..."}, "./..."},
		{"api", []string{"./api/internal/x", "./web/z", "./api"}, "./internal/x ."},
		{"api", []string{"./web/z"}, "./..."},
		{"api", []string{"-run", "^(TestA)$", "-v", "./api/internal/x"}, "-run ^(TestA)$ -v ./internal/x"},
		{"api", []string{"-run", "^(TestA)$", "-v", "./..."}, "-run ^(TestA)$ -v ./..."},
		{"api", []string{"./api/..."}, "./..."},
	} {
		if got := strings.Join(stagePackages(c.dir, c.in), " "); got != c.want {
			t.Errorf("stagePackages(%q, %v) = %q, want %q", c.dir, c.in, got, c.want)
		}
	}
}

func TestStageDirValidation(t *testing.T) {
	for dir, ok := range map[string]bool{"": true, "api": true, "services/billing": true,
		"/etc": false, "../x": false, "..": false, ".": false, "api/../..": false, "api/": false, `a\b`: false} {
		if err := validStageDir(dir); (err == nil) != ok {
			t.Errorf("validStageDir(%q) = %v", dir, err)
		}
	}
	c := Config{Stages: []Stage{{Name: "x", Run: []string{"true"}, Dir: "../out"}}}
	if err := c.validate(); err == nil {
		t.Error("a stage directory outside the repository is accepted")
	}
	// A directory that is a symbolic link out of the worktree is refused.
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks:", err)
	}
	if _, err := stageDir(root, "link"); err == nil {
		t.Error("a stage directory that leaves the worktree through a symlink is accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if d, err := stageDir(root, "api"); err != nil || d != filepath.Join(root, "api") {
		t.Errorf("stageDir(api) = %q, %v", d, err)
	}
}

// End to end on a Go + Node monorepo: the stages run in their projects,
// and a change with a reproduction test in each language is verified.
func TestSubprojectVerificationAndEvidence(t *testing.T) {
	for _, bin := range []string{"go", "node", "npm"} {
		if _, err := exec.LookPath(bin); err != nil || testing.Short() {
			t.Skip(bin + " required")
		}
	}
	ctx := context.Background()
	repo, base := commitRepo(t, map[string]string{
		"README.md":         "# shop\n",
		"api/go.mod":        "module example.com/api\n\ngo 1.22\n",
		"api/calc.go":       buggyGo,
		"api/calc_test.go":  oldTest,
		"web/package.json":  `{"scripts":{"test":"node test/run.js"}}`,
		"web/lib.js":        "exports.clamp = (v, max) => v > max ? max : v;\n",
		"web/test/run.js":   "const {clamp} = require('../lib');\nif (clamp(9, 5) !== 5) process.exit(1);\n",
		"web/.gitignore":    "node_modules/\n",
		"contracts/a.json":  "{}\n",
		"api/internal/x.go": "package internal\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	target := RepoTarget{Name: "shop", Worktree: repo, Base: base, TaskID: "t", Source: repo}

	writeFiles(t, repo, map[string]string{
		"api/calc.go":      fixedGo,
		"api/calc_test.go": oldTest + "\nfunc TestClampNegative(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n",
		"web/lib.js":       "exports.clamp = (v, max) => v > max ? max : v < 0 ? 0 : v;\n",
		"web/test/run.js":  "const {clamp} = require('../lib');\nif (clamp(9, 5) !== 5 || clamp(-1, 5) !== 0) process.exit(1);\n",
	})
	res, err := e.Run(ctx, target, Full)
	if err != nil {
		t.Fatal(err)
	}
	ran := map[string]string{}
	for _, s := range res.Stages {
		ran[s.Name] = s.Status
	}
	if !res.Passed || ran["api/go-test"] != "pass" || ran["web/npm-test"] != "pass" {
		t.Fatalf("stages %v", ran)
	}
	ev, err := e.BehaviourEvidence(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Verified || !slices.Contains(ev.Tests, "TestClampNegative") || !slices.ContainsFunc(ev.Tests, func(s string) bool { return strings.HasPrefix(s, "web/npm-test") }) {
		t.Fatalf("evidence = %+v", ev)
	}

	// The buggy code with only the new tests fails in the project's stage.
	writeFiles(t, repo, map[string]string{"api/calc.go": buggyGo})
	res, err = e.Run(ctx, target, Full)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatal("the subproject's failing test did not fail verification")
	}
}

// Formatting is judged on the files the change touched: a file already
// unformatted at the base (Temporal's SDK ships one) does not fail every
// change, and an unformatted file the change touches does.
func TestGofmtJudgesChangedFiles(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil || testing.Short() {
		t.Skip("gofmt required")
	}
	ctx := context.Background()
	repo, base := commitRepo(t, map[string]string{
		"go.mod":       "module example.com/calc\n\ngo 1.22\n",
		"calc.go":      buggyGo,
		"calc_test.go": oldTest,
		"upstream.go":  "package calc\n\nfunc upstream() {}", // no final newline: gofmt would change it
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	target := RepoTarget{Name: "calc", Worktree: repo, Base: base, TaskID: "t", Source: repo}
	gofmtStatus := func() (string, string) {
		t.Helper()
		res, err := e.Run(ctx, target, Full)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range res.Stages {
			if s.Name == "gofmt" {
				return s.Status, s.Output
			}
		}
		t.Fatal("no gofmt stage")
		return "", ""
	}
	writeFiles(t, repo, map[string]string{"calc.go": fixedGo})
	if st, out := gofmtStatus(); st != "pass" {
		t.Fatalf("a formatted change fails gofmt because of an untouched file: %s %s", st, out)
	}
	writeFiles(t, repo, map[string]string{"calc.go": strings.Replace(fixedGo, "\tif v > max {", "\tif v>max {", 1)})
	if st, out := gofmtStatus(); st != "fail" || !strings.Contains(out, "calc.go") || strings.Contains(out, "upstream.go") {
		t.Fatalf("an unformatted change: %s %s", st, out)
	}
	if got := strings.Join(stageFiles("api", []string{"api/a.go", "web/b.ts", "api/x/c.go"}), " "); got != "a.go x/c.go" {
		t.Fatalf("stageFiles = %q", got)
	}
}

// A repository's own go test stage, with fixed packages and no requires
// (as go-large-iface-method's), still gives per-test evidence.
func TestEvidenceCustomGoTestStage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	repo, base := commitRepo(t, map[string]string{
		"go.mod":       "module example.com/calc\n\ngo 1.22\n",
		"calc.go":      buggyGo,
		"calc_test.go": oldTest,
		ConfigPath:     "version: 1\nstages:\n  - {name: go-test, run: [go, test, -count=1, ./...]}\n",
	})
	writeFiles(t, repo, map[string]string{
		"calc.go":      fixedGo,
		"calc_test.go": oldTest + "\nfunc TestClampNegative(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal(\"negative\")\n\t}\n}\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	ev, err := e.BehaviourEvidence(ctx, RepoTarget{Name: "calc", Worktree: repo, Base: base, TaskID: "t", Source: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Verified || len(ev.Tests) != 1 || ev.Tests[0] != "TestClampNegative" {
		t.Fatalf("evidence = %+v", ev)
	}
}

// A stage that did not pass says why: the code, a missing tool, or a
// timeout (not verified); a missing tool is never reported as failing code.
func TestStageCauses(t *testing.T) {
	ctx := context.Background()
	repo, base := commitRepo(t, map[string]string{
		"README.md": "x\n",
		ConfigPath: "version: 1\nstages:\n" +
			"  - {name: tool, run: [sh, -c, 'exit 127']}\n" +
			"  - {name: optional-tool, run: [sh, -c, 'exit 127'], optional: true}\n" +
			"  - {name: code, run: [sh, -c, 'echo FAIL; exit 1']}\n" +
			"  - {name: slow, run: [sh, -c, 'sleep 5'], timeout: 1s}\n" +
			"  - {name: ok, run: [sh, -c, 'true']}\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	res, err := e.Run(ctx, RepoTarget{Name: "r", Worktree: repo, Base: base, TaskID: "t", Source: repo}, Full)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range res.Stages {
		got[s.Name] = s.Status + "/" + s.Cause
	}
	for name, want := range map[string]string{"tool": "error/missing tool", "optional-tool": "skipped/missing tool", "code": "fail/code",
		"slow": "fail/timeout", "ok": "pass/"} {
		if got[name] != want {
			t.Errorf("%s: %s, want %s", name, got[name], want)
		}
	}
	if res.Passed {
		t.Fatal("passed")
	}
}
