package verify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// fakeEngine is a stand-in docker CLI. FAKE_ENGINE selects the state: down
// (daemon unreachable), noimage, or stage125 (engine fine, the stage itself
// exits 125).
const fakeEngine = `#!/bin/sh
[ -n "$FAKE_ENGINE_LOG" ] && echo "$*" >> "$FAKE_ENGINE_LOG"
case "$1:$FAKE_ENGINE" in
info:down) echo "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?" >&2; exit 1 ;;
info:*) exit 0 ;;
image:stage125) exit 0 ;;
image:*) exit 1 ;;
run:down) echo "docker: Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?" >&2; exit 125 ;;
run:noimage) echo "Unable to find image 'bc-agent:test' locally" >&2; echo "docker: Error response from daemon: pull access denied" >&2; exit 125 ;;
run:stage125) echo "the stage's own failure"; exit 125 ;;
esac
exit 0
`

func fakeDockerOnPath(t *testing.T, state string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fakeEngine), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ENGINE", state)
}

// TestBrokenEngineIsAnError: a container engine that cannot run a stage is
// an environment error naming the engine and its fix, never the stage's
// tool missing, never skipped (even for optional stages) and never a pass.
func TestBrokenEngineIsAnError(t *testing.T) {
	ctx := context.Background()
	tgt := RepoTarget{Name: "r", Worktree: t.TempDir()}
	stages := []Stage{{Name: "build", Run: []string{"go", "build", "./..."}}, {Name: "lint", Run: []string{"golangci-lint", "run"}, Optional: true}}
	run := func(t *testing.T, engine string) []StageResult {
		t.Helper()
		e := &Engine{Sandbox: &sandbox.Container{Engine: engine, Image: "bc-agent:test"}}
		var out []StageResult
		for _, st := range stages {
			sr := e.runStage(ctx, tgt, st, nil)
			if sr.Status != "error" {
				t.Fatalf("%s: status %q, want error: %+v", st.Name, sr.Status, sr)
			}
			if strings.Contains(sr.Output, "not installed (required by stage") || strings.Contains(sr.Output, "golangci-lint not installed") {
				t.Fatalf("%s: engine problem reported as the stage tool: %s", st.Name, sr.Output)
			}
			out = append(out, sr)
		}
		if res := (Result{Stages: out}); len(res.Failures()) != len(stages) {
			t.Fatalf("engine errors must count as failures: %+v", res.Failures())
		}
		return out
	}
	t.Run("engine not installed", func(t *testing.T) {
		for _, sr := range run(t, "bc-missing-engine-xyz") {
			if !strings.Contains(sr.Output, "bc-missing-engine-xyz is not installed") || !strings.Contains(sr.Output, "setup --only sandbox") {
				t.Fatalf("%s: %s", sr.Name, sr.Output)
			}
		}
	})
	t.Run("daemon down", func(t *testing.T) {
		fakeDockerOnPath(t, "down")
		for _, sr := range run(t, "docker") {
			if sr.ExitCode != 125 || !strings.Contains(sr.Output, "daemon running") || !strings.Contains(sr.Output, "docker group") {
				t.Fatalf("%s: %+v", sr.Name, sr)
			}
		}
	})
	t.Run("image missing", func(t *testing.T) {
		fakeDockerOnPath(t, "noimage")
		for _, sr := range run(t, "docker") {
			if !strings.Contains(sr.Output, "bc-agent:test is not built") || !strings.Contains(sr.Output, "setup --only sandbox") {
				t.Fatalf("%s: %s", sr.Name, sr.Output)
			}
		}
	})
}

// TestStageExit125IsAFailure: with a working engine, a stage that itself
// exits 125 is the change's failure, not an engine error.
func TestStageExit125IsAFailure(t *testing.T) {
	fakeDockerOnPath(t, "stage125")
	e := &Engine{Sandbox: &sandbox.Container{Engine: "docker", Image: "bc-agent:test"}}
	sr := e.runStage(context.Background(), RepoTarget{Name: "r", Worktree: t.TempDir()}, Stage{Name: "test", Run: []string{"go", "test"}}, nil)
	if sr.Status != "fail" || sr.ExitCode != 125 || !strings.Contains(sr.Output, "the stage's own failure") {
		t.Fatalf("%+v", sr)
	}
}

func TestSecretScannerHint(t *testing.T) {
	e := &Engine{Sandbox: sandbox.None{}, Gitleaks: filepath.Join(t.TempDir(), "missing-gitleaks")}
	sr := e.secretScan(context.Background(), RepoTarget{Name: "r", Worktree: t.TempDir()}, Full)
	if sr.Status != "error" || !strings.Contains(sr.Output, "setup --only tools") || strings.Contains(sr.Output, "install-deps.sh") {
		t.Fatalf("%+v", sr)
	}
}

// TestEngineDiagnosisIsCached: a broken engine is diagnosed once for a run
// of stages, not once per stage (a hung daemon costs each probe its full
// timeout).
func TestEngineDiagnosisIsCached(t *testing.T) {
	fakeDockerOnPath(t, "down")
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("FAKE_ENGINE_LOG", log)
	ctx := context.Background()
	tgt := RepoTarget{Name: "r", Worktree: t.TempDir()}
	e := &Engine{Sandbox: &sandbox.Container{Engine: "docker", Image: "bc-agent:test"}}
	for _, name := range []string{"a", "b", "c"} {
		if sr := e.runStage(ctx, tgt, Stage{Name: name, Run: []string{"true"}}, nil); sr.Status != "error" {
			t.Fatalf("%s: %+v", name, sr)
		}
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count("\n"+string(b), "\ninfo\n"); n != 1 { // the diagnosis; the resource probe runs `info --format`
		t.Fatalf("engine probed %d times for three stages, want 1:\n%s", n, b)
	}
}

func TestForeignDependencyHint(t *testing.T) {
	esbuild := "Error: You installed esbuild for another platform than the one you're currently using."
	if got := foreignDependencyHint(esbuild, &sandbox.Container{}); (got != "") != (runtime.GOOS != "linux") {
		t.Fatalf("hint on %s = %q", runtime.GOOS, got)
	}
	if foreignDependencyHint(esbuild, sandbox.None{}) != "" {
		t.Fatal("hint without a container")
	}
	if !foreignDeps.MatchString(esbuild) || !foreignDeps.MatchString("Cannot find module '@rollup/rollup-linux-x64-gnu'") ||
		foreignDeps.MatchString("expected 2, got 3") {
		t.Fatal("pattern")
	}
}
