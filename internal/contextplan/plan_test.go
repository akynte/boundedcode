package contextplan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

func repoWithChange(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "svc")
	_ = os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n\nfunc A() int {\n\treturn 1\n}\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "docs", "adr"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "docs", "adr", "0001-idempotency.md"), []byte("# Idempotency keys\nAll payment endpoints require idempotency keys.\n"), 0o644)
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
		if _, err := gitops.Run(ctx, dir, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, dir, "rev-parse", "HEAD")
	_ = os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n\nfunc A() int {\n\treturn 2\n}\n"), 0o644)
	return dir, base
}

func inputs(t *testing.T, mode Mode, budget int) Inputs {
	dir, base := repoWithChange(t)
	tk := &task.Task{ID: "t1", OriginalRequest: "Add idempotency to payment `CreatePayment`", AcceptanceCriteria: []string{"go test passes"},
		Phase: task.PhaseImplementing, AttemptCount: 2, RemainingSteps: []string{"fix test"}}
	vr := verify.Result{Repository: "svc", Stages: []verify.StageResult{{Name: "go-test", Status: "fail", ExitCode: 1, Command: "go test ./pkg",
		Output: "--- FAIL: TestA\n    pkg/a.go:4: want 1 got 2\nFAIL"}}}
	return Inputs{Task: tk, Worktrees: []task.Worktree{{RepoName: "svc", Path: dir, Branch: "agent/t1", BaseCommit: base}},
		WorkDir: filepath.Dir(dir), Verification: []verify.Result{vr}, Mode: mode, BudgetTokens: budget, MaxAttempts: 5,
		Strategies: []task.Strategy{{Attempt: 1, Summary: "changed return value", Outcome: "rejected", Reason: "go-test failed"}}}
}

func TestResumePackRebuildsFromAuthoritativeSources(t *testing.T) {
	in := inputs(t, ModeResume, 8000)
	p, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	out := p.Render()
	for _, want := range []string{"resuming a task", "Add idempotency", "go test passes", "attempt 2 of 5", "fix test",
		"go-test failed", "-\treturn 1", "+\treturn 2", "changed return value", "> ", "Idempotency keys", "Never push"} {
		if !strings.Contains(out, want) {
			t.Errorf("pack missing %q", want)
		}
	}
	if p.Tokens > p.Budget {
		t.Fatalf("tokens %d > budget %d", p.Tokens, p.Budget)
	}
	p2, _ := Build(context.Background(), in)
	if p2.Render() != out {
		t.Fatal("pack not deterministic")
	}
}

func TestBudgetEnforced(t *testing.T) {
	in := inputs(t, ModeRetry, 600)
	in.Verification[0].Stages[0].Output = strings.Repeat("very long failure output line\n", 2000)
	p, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tokens > 600 {
		t.Fatalf("tokens %d exceed budget", p.Tokens)
	}
	truncated := false
	for _, s := range p.Sections {
		truncated = truncated || s.Truncated
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
}

func TestIdentifiers(t *testing.T) {
	got := identifiers("Fix `ledger.Post` and CreatePayment; also handlePaymentCharged in the HTTP layer")
	want := []string{"ledger.Post", "CreatePayment", "handlePaymentCharged"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
}
