package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

type fakeFrontier struct{ asked []string }

func (f *fakeFrontier) Name() string { return "fake" }
func (f *fakeFrontier) Ask(_ context.Context, packet, _ string) (string, error) {
	f.asked = append(f.asked, packet)
	return "Root cause: the settlement entry must be the negated amount so postings balance.", nil
}

func replaceIn(path, old, new string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), old) {
		return os.ErrNotExist
	}
	return os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
}

func setup(t *testing.T) (*Runner, workspace.Workspace, *store.Store, string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("needs go toolchain")
	}
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repos", "ledger-service")
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-r", "../../benchmarks/fixtures/payment-platform/ledger-service", repo).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	// Plant the bug the task must fix: an unbalanced settlement posting.
	if err := replaceIn(filepath.Join(repo, "internal/consumer/consumer.go"), "AmountCents: -ev.AmountCents", "AmountCents: ev.AmountCents"); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(root, "state.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ws := workspace.Store{DB: s.DB}
	w, _ := ws.Create(ctx, "pp")
	if _, err := ws.AddRepo(ctx, w, repo, ""); err != nil {
		t.Fatal(err)
	}
	return nil, w, s, root
}

func newRunner(s *store.Store, root string, rt *scripted.Runtime, fr frontier.Provider) *Runner {
	cfg := config.Defaults()
	cfg.Frontier.RequireApproval = true
	cfg.Escalation.FailedAttempts = 2
	log := slog.New(slog.DiscardHandler)
	rec := telemetry.New(s.DB, log)
	return &Runner{DB: s.DB, Ledger: task.Ledger{DB: s.DB}, WS: workspace.Store{DB: s.DB}, Rec: rec,
		Agent: rt, Verify: &verify.Engine{Sandbox: sandbox.None{}, CacheDir: filepath.Join(root, "cache"), DB: s.DB, Rec: rec},
		Frontier: fr, Approve: func(context.Context, frontier.Trigger, string, int) bool { return true },
		NewGateway: func(string, int) *inference.Gateway { return &inference.Gateway{Model: "m"} },
		Cfg:        cfg, Paths: config.Paths{Data: filepath.Join(root, "data")}, Model: "m", CtxSize: 65536, Log: log, Out: io.Discard}
}

const consumerFile = "ledger-service/internal/consumer/consumer.go"

func TestRunFailEscalateCrashResumeAndComplete(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()

	wrongFix := func(ws, _ string) (string, error) {
		return "renamed variable", replaceIn(filepath.Join(ws, consumerFile), "var ev PaymentCharged", "var ev PaymentCharged // decoded event")
	}
	rt := &scripted.Runtime{Steps: []scripted.Step{wrongFix, wrongFix}}
	fr := &fakeFrontier{}
	r := newRunner(s, root, rt, fr)
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting in HandlePaymentCharged", nil, []string{"go test ./... passes"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after two attempts: the budget stops the first run.
	tk.Budget.MaxAttempts = 2
	if err := r.Ledger.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusBlocked || got.AttemptCount != 2 {
		t.Fatalf("after first run: status=%s attempts=%d", got.Status, got.AttemptCount)
	}
	if len(fr.asked) != 1 || !strings.Contains(fr.asked[0], "ESCALATION Z2") || !strings.Contains(fr.asked[0], "go-test") {
		t.Fatalf("expected one Z2 escalation with verification evidence, got %d", len(fr.asked))
	}

	// "Restart": brand-new runner and runtime; only the DB, git and files survive.
	rightFix := func(ws, msg string) (string, error) {
		if !strings.Contains(msg, "resuming a task") || !strings.Contains(msg, "STRATEGIES ALREADY TRIED") {
			return "", os.ErrInvalid // resume pack must be rebuilt from the ledger
		}
		return "negated the settlement amount", replaceIn(filepath.Join(ws, consumerFile), "AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	rt2 := &scripted.Runtime{Steps: []scripted.Step{rightFix}}
	r2 := newRunner(s, root, rt2, fr)
	tk2, _ := r2.Ledger.Get(ctx, tk.ID)
	tk2.Budget.MaxAttempts = 5
	_ = r2.Ledger.Save(ctx, tk2)
	got, err = r2.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != "full_pass" || got.Phase != task.PhaseReview {
		t.Fatalf("expected completed merge candidate, got status=%s phase=%s verify=%s; messages:\n%s", got.Status, got.Phase, got.VerificationState, strings.Join(rt2.Messages, "\n----\n"))
	}
	if rt2.Requests[0].Gateway == nil {
		t.Fatal("runner did not pass its metering gateway to the runtime")
	}
	strats, _ := r2.Ledger.Strategies(ctx, tk.ID)
	for _, st := range strats {
		if st.Outcome == "active" {
			t.Errorf("strategy %d left active: %+v", st.ID, st)
		}
	}
	if rt2.Resumes != 1 || got.Budget.SessionsResumed < 1 {
		t.Fatalf("session was not resumed: resumes=%d", rt2.Resumes)
	}
	// The fix is committed on the task branch; main is untouched.
	wts, _ := r2.Ledger.Worktrees(ctx, tk.ID)
	log, _ := gitops.Run(ctx, wts[0].Path, "log", "--oneline", "main..HEAD")
	if !strings.Contains(log, "attempt 3") {
		t.Fatalf("checkpoint commits missing: %s", log)
	}
	mainSrc, _ := gitops.Run(ctx, wts[0].RepoPath, "show", "main:internal/consumer/consumer.go")
	if strings.Contains(mainSrc, "-ev.AmountCents") {
		t.Fatal("main branch was modified")
	}
	// Z2 (repeated failure) in the first run, then Z3 (pre-merge review: the
	// change touches ledger code) in the second.
	rows, _ := s.DB.QueryContext(ctx, `SELECT trigger, outcome FROM escalations WHERE task_id = ? AND status = 'answered' ORDER BY id`, tk.ID)
	var got2 []string
	for rows.Next() {
		var trig, outcome string
		_ = rows.Scan(&trig, &outcome)
		got2 = append(got2, trig+"="+outcome)
	}
	rows.Close()
	if strings.Join(got2, ",") != "Z2=helped,Z3=helped" {
		t.Fatalf("escalations = %v", got2)
	}
	evs, _ := telemetry.Events(ctx, s.DB, tk.ID, 0, 1000)
	kinds := map[string]int{}
	for _, e := range evs {
		kinds[e.Kind]++
	}
	for _, k := range []string{"task.created", "context.pack", "agent.turn", "verify.result", "frontier.answered", "task.blocked", "task.completed", "agent.session"} {
		if kinds[k] == 0 {
			t.Errorf("audit log missing %s (have %v)", k, kinds)
		}
	}
}
