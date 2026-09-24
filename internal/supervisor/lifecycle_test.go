package supervisor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/session"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/workspace"
)

func bound(t *testing.T) *session.Session {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/x\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if _, err := workspace.Init(dir, workspace.InitOptions{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	s, err := session.Open(context.Background(), t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// A task finished without a verification must not read as fine. An executor's
// own account of its work is exactly what the completion contract exists not
// to trust, and a review that quietly said VERIFIED would launder it.
func TestFinishingWithoutVerifyingIsReportedUnverified(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "do a thing", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "UNVERIFIED" {
		t.Errorf("verdict = %q, want UNVERIFIED", rev.Verdict)
	}
	if !strings.Contains(rev.Format(), "nothing checked the work") {
		t.Errorf("the review does not say why it is unverified:\n%s", rev.Format())
	}
}

// A decision the user made is the part git cannot reconstruct, and the reason
// this record exists at all.
func TestUserDecisionsReachTheReview(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "add rate limiting", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordAnswer(ctx, s.Store, task.ID,
		"Per IP or per account?", "Per IP, 10 a minute"); err != nil {
		t.Fatal(err)
	}
	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Decisions) != 1 {
		t.Fatalf("got %d decisions, want 1", len(rev.Decisions))
	}
	if rev.Decisions[0].Answer != "Per IP, 10 a minute" {
		t.Errorf("answer = %q", rev.Decisions[0].Answer)
	}
	if !strings.Contains(rev.Format(), "Per IP or per account?") {
		t.Error("the question is missing from the rendered review")
	}
}

// A half-recorded decision is worse than none: it reads as though the user was
// consulted when the answer is unknown.
func TestAnIncompleteDecisionIsRefused(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "x", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordAnswer(ctx, s.Store, task.ID, "What limit?", ""); err == nil {
		t.Error("a decision with no answer was recorded")
	}
	if err := supervisor.RecordAnswer(ctx, s.Store, task.ID, "", "10"); err == nil {
		t.Error("a decision with no question was recorded")
	}
}

// A task needs an objective: the journal's whole value is saying what was
// being attempted.
func TestATaskNeedsAnObjective(t *testing.T) {
	s := bound(t)
	if _, err := supervisor.StartTask(context.Background(), s.Store, "   ", recipe.Standard); err == nil {
		t.Error("a task with no objective was created")
	}
}

func TestFinishingRejectsVerificationForAnOlderCandidate(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "change the implementation", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ledger.ContentManifest(s.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
		Accepted: true, Candidate: before, Level: "standard", Phase: "VERIFY",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Workspace.Root, "changed.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, taskInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "NOT VERIFIED" {
		t.Fatalf("verdict = %q, want NOT VERIFIED for a stale verification", rev.Verdict)
	}
	if !strings.Contains(strings.Join(rev.Warnings, "\n"), "reverify after every edit") {
		t.Fatalf("stale-verification warning is missing: %v", rev.Warnings)
	}
}

func TestFinishingRejectsVerificationBelowRequiredLevel(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "prove a concurrency invariant", recipe.High)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ledger.ContentManifest(s.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
		Accepted: true, Candidate: candidate, Level: "standard", Phase: "VERIFY",
	}); err != nil {
		t.Fatal(err)
	}

	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, taskInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "NOT VERIFIED" {
		t.Fatalf("verdict = %q, want NOT VERIFIED for insufficient verification", rev.Verdict)
	}
	if !strings.Contains(strings.Join(rev.Warnings, "\n"), "required level") {
		t.Fatalf("verification-level warning is missing: %v", rev.Warnings)
	}
}

func TestFinishingAcceptsCurrentCandidateAtRequiredLevel(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "prove a current candidate", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ledger.ContentManifest(s.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
		Accepted: true, Candidate: candidate, Level: "high", Phase: "VERIFY",
		Checks: []supervisor.CheckResult{
			{Name: "go build", Kind: recipe.KindBuild, Status: "pass", Headline: "ok", Candidate: candidate},
			{Name: "go vet", Kind: recipe.KindVet, Status: "pass", Headline: "ok", Candidate: candidate},
			{Name: "go test", Kind: recipe.KindTest, Status: "pass", Headline: "ok", Candidate: candidate},
			{Name: "gofmt", Kind: recipe.KindFormat, Status: "pass", Headline: "ok", Candidate: candidate},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, taskInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "VERIFIED" {
		t.Fatalf("verdict = %q, want VERIFIED for a current, sufficiently strong verification", rev.Verdict)
	}
}

func TestFinishingRejectsAnEditorNoOpEvenWithGreenVerification(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "change a file", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := ledger.ContentManifest(s.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEventCandidate(ctx, s.Store, taskInfo.ID, ledger.KindSessionStart, map[string]any{
		"objective": taskInfo.Title, "executor": "opencode", "phase": "EDITOR", "session_id": "ses_noop",
	}, initial); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
		Accepted: true, Candidate: initial, Level: "standard", Phase: "VERIFY",
	}); err != nil {
		t.Fatal(err)
	}

	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, taskInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "NOT VERIFIED" {
		t.Fatalf("verdict = %q, want NOT VERIFIED for an editor task that changed nothing", rev.Verdict)
	}
	if !strings.Contains(strings.Join(rev.Warnings, "\n"), "changed nothing") {
		t.Fatalf("no-op warning is missing: %v", rev.Warnings)
	}
}

func TestFinishingRejectsProtectedEditorChanges(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	policyDir := filepath.Join(s.Workspace.Root, "policies")
	if err := os.MkdirAll(policyDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "protected.yaml"), []byte("name: protected\nprotected:\n  - path: policies/**\n    reason: policy files are supervisor-owned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "change a policy file", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ledger.ContentManifest(s.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
		Accepted: true, Candidate: candidate, Level: "standard", Phase: "VERIFY",
	}); err != nil {
		t.Fatal(err)
	}

	rev, err := supervisor.FinishTask(ctx, s.Store, s.Workspace.Root, taskInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Verdict != "NOT VERIFIED" {
		t.Fatalf("verdict = %q, want NOT VERIFIED for a protected-path change", rev.Verdict)
	}
	if !strings.Contains(strings.Join(rev.Warnings, "\n"), "protects") {
		t.Fatalf("protected-path warning is missing: %v", rev.Warnings)
	}
}
