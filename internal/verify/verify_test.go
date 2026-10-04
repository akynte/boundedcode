package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
)

func TestImpacted(t *testing.T) {
	listing := "m/a\t/w/a\tfmt,\nm/b\t/w/b\tm/a,\nm/c\t/w/c\t,m/b\nm/d\t/w/d\tstrings,\n"
	got := impacted(listing, "/w", []string{"a/x.go"})
	if strings.Join(got, ",") != "m/a,m/b,m/c" {
		t.Fatalf("got %v", got)
	}
}

func TestDiffScope(t *testing.T) {
	r := diffScope([]string{"main.go", ".env"}, Config{})
	if r.Status != "fail" || !strings.Contains(r.Output, ".env") {
		t.Fatalf("%+v", r)
	}
	if r := diffScope([]string{"main.go"}, Config{}); r.Status != "pass" {
		t.Fatalf("%+v", r)
	}
}

func TestConfigRejectsDangerousStage(t *testing.T) {
	c := Config{Stages: []Stage{{Name: "deploy", Run: []string{"terraform", "apply"}}}}
	if c.validate() == nil {
		t.Fatal("expected policy rejection")
	}
}

// TestEngineGoRepo runs real gofmt/build/vet/test stages on the host
// (sandbox none) against a fixture service, before and after breaking it.
func TestEngineGoRepo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "ledger-service")
	if out, err := exec.Command("cp", "-r", "../../benchmarks/fixtures/payment-platform/ledger-service", repo).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	s, _ := store.Open(ctx, ":memory:")
	defer s.Close()
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), DB: s.DB, Gitleaks: fakeGitleaks(t)}
	tgt := RepoTarget{Name: "ledger-service", Worktree: repo, Base: base, TaskID: "t1"}

	res, err := e.Run(ctx, tgt, Full)
	if err != nil || !res.Passed {
		t.Fatalf("clean repo should pass: %v %+v", err, res.Failures())
	}
	// Break the ledger: unbalanced posting makes the consumer test fail.
	p := filepath.Join(repo, "internal/consumer/consumer.go")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "AmountCents: -ev.AmountCents", "AmountCents: ev.AmountCents", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = e.Run(ctx, tgt, Targeted)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || !slices.Contains(names(res.Failures()), "go-test") {
		t.Fatalf("expected go-test failure, got %+v", res.Stages)
	}
	if !slices.Contains(res.Packages, "example.com/ledger-service/internal/consumer") || slices.Contains(res.Packages, "./...") {
		t.Fatalf("targeted packages = %v", res.Packages)
	}
	if res.Signature() == "" {
		t.Fatal("empty failure signature")
	}
	runs, _ := LoadRuns(ctx, s.DB, "t1", 10)
	if len(runs) != 2 {
		t.Fatalf("persisted runs = %d", len(runs))
	}
}

func TestMissingTools(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	e := &Engine{Sandbox: sandbox.None{}}
	tgt := RepoTarget{Name: "r", Worktree: repo}
	opt := e.runStage(ctx, tgt, Stage{Name: "lint", Run: []string{"definitely-not-installed-tool-xyz"}, Optional: true}, nil)
	if opt.Status != "skipped" {
		t.Fatalf("optional missing tool: %+v", opt)
	}
	req := e.runStage(ctx, tgt, Stage{Name: "build", Run: []string{"definitely-not-installed-tool-xyz"}}, nil)
	if req.Status != "error" || !strings.Contains(req.Output, "not installed") {
		t.Fatalf("required missing tool: %+v", req)
	}
}

// TestAgentCannotWeakenVerification: the agent rewrites the verification
// config to a no-op and deletes go.mod to switch the Go stages off. The
// config is read from the base commit, the protected path fails diff scope,
// and the removed go.mod fails its stages.
func TestAgentCannotWeakenVerification(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	ctx := context.Background()
	repo := t.TempDir()
	files := map[string]string{
		"go.mod":    "module example.com/x\n\ngo 1.22\n",
		"x.go":      "package x\n\nfunc Add(a, b int) int { return a + b }\n",
		"x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		ConfigPath:  "version: 1\nstages:\n  - name: go-test\n    run: [go, test, ./...]\n    requires: [go.mod]\n",
	}
	for f, c := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(repo, f)), 0o755)
		_ = os.WriteFile(filepath.Join(repo, f), []byte(c), 0o644)
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	tgt := RepoTarget{Name: "x", Worktree: repo, Base: base, TaskID: "t1"}

	// Break the code and neuter the config in the worktree.
	_ = os.WriteFile(filepath.Join(repo, "x.go"), []byte("package x\n\nfunc Add(a, b int) int { return a - b }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, ConfigPath), []byte("version: 1\nstages:\n  - name: ok\n    run: [\"true\"]\n"), 0o644)
	res, err := e.Run(ctx, tgt, Targeted)
	if err != nil {
		t.Fatal(err)
	}
	failed := names(res.Failures())
	if res.Passed || !slices.Contains(failed, "go-test") || !slices.Contains(failed, "diff-scope") {
		t.Fatalf("weakened config was honoured: %+v", res.Stages)
	}
	// Deleting go.mod does not switch the Go stage off.
	_ = os.Remove(filepath.Join(repo, "go.mod"))
	res, _ = e.Run(ctx, tgt, Targeted)
	for _, s := range res.Stages {
		if s.Name == "go-test" && s.Status != "fail" {
			t.Fatalf("go-test with go.mod removed: %+v", s)
		}
	}
}

func TestPresetFromBaseFiles(t *testing.T) {
	c := presetFor(map[string]bool{"go.mod": true, "package.json": true, "tsconfig.json": true, "infra/main.tf": true, "deploy/chart/Chart.yaml": true})
	got := map[string]bool{}
	for _, s := range c.Stages {
		got[s.Name] = true
	}
	for _, want := range []string{"gofmt", "go-build", "go-vet", "go-test", "tsc", "npm-lint", "npm-test", "npm-build", "terraform-fmt", "helm-lint"} {
		if !got[want] {
			t.Errorf("missing preset stage %s", want)
		}
	}
}

// fakeGitleaks is a secret scanner that finds nothing, so tests don't depend
// on gitleaks being installed.
func fakeGitleaks(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "fake-gitleaks")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestFullGateRequiresSecretScanner: without a secret scanner the full gate
// fails; iteration (targeted) only skips the scan.
func TestFullGateRequiresSecretScanner(t *testing.T) {
	e := &Engine{Sandbox: sandbox.None{}, Gitleaks: filepath.Join(t.TempDir(), "missing-gitleaks")}
	tgt := RepoTarget{Name: "r", Worktree: t.TempDir()}
	if sr := e.secretScan(context.Background(), tgt, Full); sr.Status != "error" {
		t.Fatalf("full gate without scanner: %+v", sr)
	}
	if sr := e.secretScan(context.Background(), tgt, Targeted); sr.Status != "skipped" {
		t.Fatalf("targeted without scanner: %+v", sr)
	}
}
