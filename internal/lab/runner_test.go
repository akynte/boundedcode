package lab

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

// fakeBench stands in for an arm's bcode: it records its environment and
// writes a report. A "tamper" build also edits the task definitions.
const fakeBench = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do [ "$1" = "--out" ] && out="$2"; shift; done
env > "$out.env"
pwd > "$out.pwd"
cat > "$out" <<'EOF'
{"id":"r","results":[{"id":"t1","success":true,"split":"heldout","languages":["go"],"wall_seconds":10}],"environment":{"model":{"sha256":"m1"},"tasks_sha256":"s"}}
EOF
`

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	for p, c := range map[string]string{
		"internal/direct/observe.go":       "package direct\n\nfunc observe() {}\n",
		"internal/direct/guard.go":         "package direct\n",
		"benchmarks/direct/README.txt":     "tasks\n",
		"benchmarks/fixtures/f/README.txt": "fixture\n",
	} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	return dir
}

func newRunner(t *testing.T, repo string) (*Runner, string) {
	t.Helper()
	user := t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, "models"), 0o700); err != nil {
		t.Fatal(err)
	}
	for p, c := range map[string]string{
		"config.yaml":   "version: 1\n# a comment the arm keeps\ndirect:\n  max_turns: 70\n",
		"models/m.yaml": "name: m\n",
		"jev.env":       "SECRET_TOKEN=do-not-copy\n",
	} {
		if err := os.WriteFile(filepath.Join(user, p), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &Runner{Repo: repo, LabDir: t.TempDir(), UserConfigDir: user, ModelsDir: "/models",
		Build: func(_ context.Context, src, out string) error {
			script := fakeBench + "# built as " + filepath.Base(out) + "\n"
			if filepath.Base(out) == "tamper" {
				script += "echo changed >> " + filepath.Join(repo, "benchmarks", "direct", "README.txt") + "\n"
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
				return err
			}
			return os.WriteFile(out, []byte(script), 0o755)
		},
		Test: func(context.Context, string) (string, error) { return "ok", nil },
	}
	return r, user
}

func TestRunnerIsolation(t *testing.T) {
	t.Setenv("FAKE_API_KEY", "sk-should-not-reach-an-arm")
	repo := gitRepo(t)
	r, _ := newRunner(t, repo)
	specDir := t.TempDir()
	patch := "diff --git a/internal/direct/observe.go b/internal/direct/observe.go\n--- a/internal/direct/observe.go\n+++ b/internal/direct/observe.go\n" +
		"@@ -1,3 +1,4 @@\n package direct\n \n+// observe less\n func observe() {}\n"
	if err := os.WriteFile(filepath.Join(specDir, "obs.patch"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	e := experiment(Arm{Name: "cand", Role: "candidate", Set: config.ExperimentSettings{Direct: config.Tunables{Retrieval: "basic"}}},
		Arm{Name: "code", Role: "candidate", Patch: "obs.patch"})
	e.Env = map[string]string{"GGML_OP_OFFLOAD_MIN_BATCH": "256"}
	spec := filepath.Join(specDir, "x.yaml")
	if err := os.WriteFile(spec, []byte("id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, rec, reports, err := r.Run(context.Background(), e, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Runs) != 6 || len(reports["cand"]) != 2 || rec.Repo.Revision == "" || rec.Inputs == "" || filepath.IsAbs(rec.Runs[0].Report.Path) || filepath.IsAbs(rec.Arms[0].Home) {
		t.Fatalf("record: %+v", rec)
	}
	// Order is rotated per trial.
	if rec.Runs[0].Arm != "base" || rec.Runs[3].Arm != "cand" {
		t.Fatalf("schedule: %s, %s", rec.Runs[0].Arm, rec.Runs[3].Arm)
	}
	fps := map[string]bool{}
	for _, a := range rec.Arms {
		fps[a.Fingerprint] = true
	}
	if len(fps) != 1 {
		t.Fatalf("fingerprints differ: %+v", rec.Arms)
	}
	code := rec.Arms[2]
	if code.Patch == nil || code.Patch.Files[0] != "internal/direct/observe.go" || code.SecurityTests != "ok" || code.Binary.SHA256 == rec.Arms[0].Binary.SHA256 {
		t.Fatalf("code arm: %+v", code)
	}
	home := filepath.Join(dir, "arms", "cand", "home")
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"retrieval: basic", "max_turns: 70", "a comment the arm keeps", "models_dir: /models"} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("arm config lacks %q:\n%s", want, cfg)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "config", "jev.env")); err == nil {
		t.Error("a credential file reached the arm's home")
	}
	if _, err := os.Stat(filepath.Join(home, "config", "models", "m.yaml")); err != nil {
		t.Error("model profiles not copied")
	}
	env, _ := os.ReadFile(reports["cand"][0] + ".env")
	if strings.Contains(string(env), "FAKE_API_KEY") || !strings.Contains(string(env), "BOUNDEDCODE_HOME="+home) || !strings.Contains(string(env), "GGML_OP_OFFLOAD_MIN_BATCH=256") {
		t.Errorf("arm environment:\n%s", env)
	}
	if out, _ := exec.Command("git", "-C", repo, "worktree", "list").Output(); strings.Count(string(out), "\n") != 1 {
		t.Errorf("worktrees left behind:\n%s", out)
	}
	if st, _ := exec.Command("git", "-C", repo, "status", "--porcelain").Output(); len(st) != 0 {
		t.Errorf("the repository changed:\n%s", st)
	}
	// A report changed after its run is caught.
	if err := os.WriteFile(reports["cand"][0], []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRuns(rec, dir); err == nil {
		t.Fatal("a changed report passed verification")
	}
}

func TestRunnerRefuses(t *testing.T) {
	repo := gitRepo(t)
	r, _ := newRunner(t, repo)
	specDir := t.TempDir()
	spec := filepath.Join(specDir, "x.yaml")
	_ = os.WriteFile(spec, []byte("id: x\n"), 0o644)
	// A patch to the guard is refused before anything is built from it.
	_ = os.WriteFile(filepath.Join(specDir, "guard.patch"), []byte(diff("internal/direct/guard.go", 1, false, "// allow all")), 0o644)
	e := experiment(Arm{Name: "code", Role: "candidate", Patch: "guard.patch"})
	if _, _, _, err := r.Run(context.Background(), e, spec); err == nil || !strings.Contains(err.Error(), "security or verification code") {
		t.Fatalf("guard patch: %v", err)
	}
	// A run that changes the task definitions voids the experiment.
	e = experiment(Arm{Name: "tamper", Role: "candidate", Set: config.ExperimentSettings{Direct: config.Tunables{Retrieval: "basic"}}})
	r.Build = func(_ context.Context, _, out string) error {
		_ = os.MkdirAll(filepath.Dir(out), 0o700)
		return os.WriteFile(out, []byte(fakeBench+"echo changed >> "+filepath.Join(repo, "benchmarks", "direct", "README.txt")+"\n"), 0o755)
	}
	if _, _, _, err := r.Run(context.Background(), e, spec); err == nil || !strings.Contains(err.Error(), "void") {
		t.Fatalf("tampering run: %v", err)
	}
}
