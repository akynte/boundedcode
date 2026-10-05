package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// TestKilledRunnerHelper is the child process of TestSIGKILLResume: it runs
// the task with an agent that edits the repository and then hangs, until the
// parent SIGKILLs it. Not a test on its own.
func TestKilledRunnerHelper(t *testing.T) {
	if os.Getenv("BC_KILL_CHILD_DB") == "" {
		t.Skip("helper process")
	}
	ctx := context.Background()
	s, err := store.Open(ctx, os.Getenv("BC_KILL_CHILD_DB"))
	if err != nil {
		t.Fatal(err)
	}
	ready := os.Getenv("BC_KILL_CHILD_READY")
	hang := func(ws, _ string) (string, error) {
		// Half the work: a new test file, then the process dies mid-turn.
		_ = os.WriteFile(filepath.Join(ws, "ledger-service/internal/consumer/balance_test.go"),
			[]byte("package consumer\n"), 0o644)
		_ = os.WriteFile(ready, nil, 0o644)
		time.Sleep(time.Minute)
		return "", nil
	}
	r := newRunner(s, os.Getenv("BC_KILL_CHILD_ROOT"), &scripted.Runtime{Steps: []scripted.Step{hang}}, nil)
	r.LeaseOwner = "child"
	_, _ = r.Run(ctx, os.Getenv("BC_KILL_CHILD_TASK"), RunOptions{})
}

// TestSIGKILLResume kills the runner process (no signal handler, no
// cleanup) in the middle of an agent turn, then resumes the task from a new
// runner: the stale lease is taken over, the orphaned attempt is reconciled,
// the agent's partial edits are kept, and the task completes.
func TestSIGKILLResume(t *testing.T) {
	_, w, s, root := setup(t)
	ctx := context.Background()
	r := newRunner(s, root, &scripted.Runtime{}, nil)
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting in HandlePaymentCharged", nil, []string{"go test ./... passes"})
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledRunnerHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "BC_KILL_CHILD_DB="+filepath.Join(root, "state.db"), "BC_KILL_CHILD_ROOT="+root,
		"BC_KILL_CHILD_TASK="+tk.ID, "BC_KILL_CHILD_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("child never reached the agent turn")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// Dead runner state: lease held, attempt active, phase implementing.
	l := task.Ledger{DB: s.DB}
	if ls, _ := l.LeaseOf(ctx, tk.ID); ls.Owner != "child" {
		t.Fatalf("lease after kill: %+v", ls)
	}
	// A second runner is refused while the lease is fresh...
	r2 := newRunner(s, root, &scripted.Runtime{}, nil)
	if _, err := r2.Run(ctx, tk.ID, RunOptions{}); err == nil || !strings.Contains(err.Error(), "another process") {
		t.Fatalf("concurrent run allowed: %v", err)
	}
	// ...and takes over once it is stale.
	old := leaseStale
	leaseStale = 50 * time.Millisecond
	defer func() { leaseStale = old }()
	time.Sleep(100 * time.Millisecond)

	var resumeMsg string
	fix := func(ws, msg string) (string, error) {
		resumeMsg = msg
		if _, err := os.Stat(filepath.Join(ws, "ledger-service/internal/consumer/balance_test.go")); err != nil {
			return "", err // the killed attempt's edits must survive
		}
		return "negated the settlement amount", replaceIn(filepath.Join(ws, consumerFile),
			"AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	rt3 := &scripted.Runtime{Steps: []scripted.Step{fix}}
	r3 := newRunner(s, root, rt3, nil)
	got, err := r3.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted {
		t.Fatalf("status=%s\n%s", got.Status, resumeMsg)
	}
	if !strings.Contains(resumeMsg, "resuming a task") {
		t.Fatalf("resume pack not used:\n%s", resumeMsg)
	}
	strats, _ := l.Strategies(ctx, tk.ID)
	if strats[0].Outcome != "rejected" || !strings.Contains(strats[0].Reason, "previous run stopped") {
		t.Fatalf("orphaned attempt not reconciled: %+v", strats[0])
	}
	evs, _ := telemetry.Events(ctx, s.DB, tk.ID, 0, 1000)
	recovered := false
	for _, e := range evs {
		recovered = recovered || e.Kind == "task.recovered"
	}
	if !recovered {
		t.Fatal("no task.recovered event")
	}
	if ls, _ := l.LeaseOf(ctx, tk.ID); ls.Owner != "" {
		t.Fatalf("lease not released after completion: %+v", ls)
	}
	wts, _ := l.Worktrees(ctx, tk.ID)
	if out, _ := gitops.Run(ctx, wts[0].Path, "status", "--porcelain"); out != "" {
		t.Fatalf("uncommitted work left: %s", out)
	}
	_ = s.Close()
}

// TestModelServerOutageDoesNotBurnAttempts: the model server dies during an
// agent turn (transport errors recorded by the gateway, the turn ends with an
// error). The runner repairs it via EnsureModel and retries without counting
// the turn as an attempt or as a failed strategy for Z2.
func TestModelServerOutageDoesNotBurnAttempts(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()
	var taskID string
	outage := func(string, string) (string, error) {
		_, err := s.DB.Exec(`INSERT INTO model_calls(task_id, source, model, status, created_at) VALUES(?, 'agent', 'm', 'transport_error', ?)`,
			taskID, store.Now())
		if err != nil {
			return "", err
		}
		return "", os.ErrDeadlineExceeded // the turn ends with an error
	}
	fix := func(ws, _ string) (string, error) {
		if err := addReproTest(ws); err != nil {
			return "", err
		}
		return "negated", replaceIn(filepath.Join(ws, consumerFile),
			"AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	r := newRunner(s, root, &scripted.Runtime{Steps: []scripted.Step{outage, fix}}, nil)
	repairs := 0
	r.EnsureModel = func(context.Context) error { repairs++; return nil }
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	taskID = tk.ID
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.AttemptCount != 1 || repairs != 1 || got.Budget.FailedVerifyRuns != 0 {
		t.Fatalf("status=%s attempts=%d repairs=%d failed_verify=%d", got.Status, got.AttemptCount, repairs, got.Budget.FailedVerifyRuns)
	}
}
