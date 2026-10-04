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
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), DB: s.DB}
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
