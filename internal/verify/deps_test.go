package verify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// isolatedRecorder is an isolated sandbox that records specs and runs
// nothing.
type isolatedRecorder struct{ specs []sandbox.Spec }

func (*isolatedRecorder) Name() string   { return "recorder" }
func (*isolatedRecorder) Isolated() bool { return true }
func (r *isolatedRecorder) Command(ctx context.Context, s sandbox.Spec) (*exec.Cmd, error) {
	r.specs = append(r.specs, s)
	return exec.CommandContext(ctx, "true"), nil
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		p = filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSpecMountsDependencies is the regression test for the 2026-10-04
// validation failure on axios: task worktrees had no node_modules, so the
// JavaScript stages never ran and an untested change was verified.
func TestSpecMountsDependencies(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	writeFiles(t, src, map[string]string{"node_modules/mocha/index.js": "", "packages/a/node_modules/x/i.js": ""})
	e := &Engine{Sandbox: &isolatedRecorder{}}
	spec, err := e.spec(RepoTarget{Name: "r", Worktree: wt, Source: src}, []string{"npm", "test"})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, m := range spec.Mounts {
		if strings.HasSuffix(m.Target, "node_modules") {
			if !m.ReadOnly {
				t.Fatalf("dependency mount must be read-only: %+v", m)
			}
			found[m.Target] = true
		}
	}
	if !found[filepath.Join(wt, "node_modules")] || !found[filepath.Join(wt, "packages/a/node_modules")] {
		t.Fatalf("mounts = %+v", spec.Mounts)
	}
	// Without a source (no ledger information) nothing is mounted.
	spec, _ = e.spec(RepoTarget{Name: "r", Worktree: wt}, []string{"npm", "test"})
	for _, m := range spec.Mounts {
		if strings.HasSuffix(m.Target, "node_modules") {
			t.Fatalf("unexpected mount %+v", m)
		}
	}
}

// TestDependencyMountFailsClosed: an agent-planted symlink at the mount point
// errors the stage instead of following the link or skipping the stage.
func TestDependencyMountFailsClosed(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	writeFiles(t, src, map[string]string{"node_modules/m/i.js": ""})
	writeFiles(t, wt, map[string]string{"package.json": `{"scripts":{"test":"true"}}`})
	if err := os.Symlink("/usr/local/bin", filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Sandbox: &isolatedRecorder{}}
	if _, err := e.spec(RepoTarget{Name: "r", Worktree: wt, Source: src}, []string{"true"}); !errors.Is(err, sandbox.ErrUnsafeDependencyTarget) {
		t.Fatalf("want ErrUnsafeDependencyTarget, got %v", err)
	}
	st := e.runStage(context.Background(), RepoTarget{Name: "r", Worktree: wt, Source: src}, Stage{Name: "npm-test", Run: []string{"true"}, Optional: true}, nil)
	if st.Status != "error" {
		t.Fatalf("stage must error, got %+v", st)
	}
}

func TestPresetJavaScriptWithoutTSConfig(t *testing.T) {
	got := map[string]bool{}
	for _, s := range presetFor(map[string]bool{"package.json": true}).Stages {
		got[s.Name] = true
	}
	if !got["npm-test"] || !got["npm-lint"] || !got["npm-build"] {
		t.Fatalf("a JavaScript project needs npm stages: %v", got)
	}
}

// TestNpmStagesWithoutDependencies: a declared script with no installed
// dependencies fails with an actionable message (it used to be skipped,
// passing the change with no tests run); an absent script is skipped.
func TestNpmStagesWithoutDependencies(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node required")
	}
	stage := func(name string) Stage {
		for _, s := range presetFor(map[string]bool{"package.json": true, "tsconfig.json": true}).Stages {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("no %s stage", name)
		return Stage{}
	}
	ctx := context.Background()
	e := &Engine{Sandbox: sandbox.None{}}
	run := func(files map[string]string, name string) StageResult {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		return e.runStage(ctx, RepoTarget{Name: "r", Worktree: dir}, stage(name), nil)
	}
	withTest := `{"scripts":{"test":"node -e 'process.exit(0)'"},"devDependencies":{"typescript":"5"}}`
	if r := run(map[string]string{"package.json": withTest, "tsconfig.json": "{}"}, "npm-test"); r.Status != "fail" || !strings.Contains(r.Output, "install the project's dependencies") {
		t.Fatalf("declared test, no deps: %+v", r)
	}
	if r := run(map[string]string{"package.json": withTest, "tsconfig.json": "{}"}, "tsc"); r.Status != "fail" || !strings.Contains(r.Output, "node_modules missing") {
		t.Fatalf("typescript declared, no deps: %+v", r)
	}
	if r := run(map[string]string{"package.json": withTest, "node_modules/.keep": ""}, "npm-test"); r.Status != "pass" {
		t.Fatalf("declared test with deps: %+v", r)
	}
	if r := run(map[string]string{"package.json": `{"scripts":{}}`}, "npm-test"); r.Status != "skipped" {
		t.Fatalf("no test script: %+v", r)
	}
	if r := run(map[string]string{"package.json": `{"scripts":{"test":"node -e 'process.exit(3)'"}}`, "node_modules/.keep": ""}, "npm-test"); r.Status != "fail" {
		t.Fatalf("failing test script: %+v", r)
	}
}

// TestDependencyMountInContainer runs a real container: the worktree's test
// imports a package that exists only in the checkout's node_modules.
func TestDependencyMountInContainer(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-deps-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	src, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	pkg := `{"scripts":{"test":"node test.js"}}`
	writeFiles(t, src, map[string]string{"package.json": pkg, "node_modules/answer/index.js": "module.exports = 42;\n"})
	writeFiles(t, wt, map[string]string{"package.json": pkg, "test.js": "process.exit(require('answer') === 42 ? 0 : 1);\n"})
	sb := &sandbox.Container{Engine: "docker", Image: image, Network: "none", UID: os.Getuid(), GID: os.Getgid()}
	e := &Engine{Sandbox: sb}
	var test Stage
	for _, s := range presetFor(map[string]bool{"package.json": true}).Stages {
		if s.Name == "npm-test" {
			test = s
		}
	}
	ctx := context.Background()
	if r := e.runStage(ctx, RepoTarget{Name: "r", Worktree: wt, Source: src}, test, nil); r.Status != "pass" {
		t.Fatalf("with the checkout's dependencies: %+v", r)
	}
	if r := e.runStage(ctx, RepoTarget{Name: "r", Worktree: wt}, test, nil); r.Status != "fail" {
		t.Fatalf("without dependencies the declared test must fail, got %+v", r)
	}
	// The mount is read-only: the stage cannot alter the checkout's deps.
	write := Stage{Name: "w", Run: []string{"sh", "-c", "echo x > node_modules/answer/index.js"}}
	if r := e.runStage(ctx, RepoTarget{Name: "r", Worktree: wt, Source: src}, write, nil); r.Status != "fail" {
		t.Fatalf("write to dependency mount: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(src, "node_modules/answer/index.js")); string(b) != "module.exports = 42;\n" {
		t.Fatalf("checkout dependency modified: %q", b)
	}
}
