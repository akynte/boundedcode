package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var updateReplay = flag.Bool("update-replay", false, "rewrite testdata/crash-replay.json from this run of TestSIGKILLResume")

// crashReplay is what TestSIGKILLResume observes, in order. The test compares
// it with testdata/crash-replay.json and fails on any difference, so a passing
// run proves the file is what a real kill and resume produced. Readers (such as
// a site that replays it) can rely on that; -update-replay rewrites the file
// when the behaviour changes on purpose.
type crashReplay struct {
	Test  string       `json:"test"`
	About string       `json:"about"`
	Steps []replayStep `json:"steps"`
}

// rfc3339 matches the wall-clock times in observed messages, which differ on every run.
var rfc3339 = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

type replayStep struct {
	Actor    string `json:"actor"`
	Action   string `json:"action"`
	Observed string `json:"observed"`
}

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
	l := task.Ledger{DB: s.DB}
	rec := crashReplay{Test: "TestSIGKILLResume", About: "A runner process is killed with SIGKILL in the middle of an agent turn; " +
		"a second runner is refused, a third takes over the stale lease and finishes the task. Each step is what the test observed."}
	see := func(actor, action, observed string) {
		observed = rfc3339.ReplaceAllString(strings.ReplaceAll(observed, tk.ID, "<task>"), "<time>")
		rec.Steps = append(rec.Steps, replayStep{actor, action, observed})
	}
	if t0, err := l.Get(ctx, tk.ID); err == nil {
		see("test", "creates the task", fmt.Sprintf("status %s, phase %s", t0.Status, t0.Phase))
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
	partial := "ledger-service/internal/consumer/balance_test.go"
	_, statErr := os.Stat(filepath.Join(r.WorkDir(tk.ID), partial))
	see("runner A", "runs as a separate process (lease owner \"child\"); its agent writes a file and stays mid-turn",
		fmt.Sprintf("%s in the worktree: %t", partial, statErr == nil))
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitErr := cmd.Wait()
	see("test", "kills runner A with SIGKILL: no signal handler, no cleanup", fmt.Sprint(waitErr))

	// Dead runner state: lease held, attempt active, phase implementing.
	ls, _ := l.LeaseOf(ctx, tk.ID)
	if ls.Owner != "child" {
		t.Fatalf("lease after kill: %+v", ls)
	}
	if t1, err := l.Get(ctx, tk.ID); err == nil {
		st, _ := l.Strategies(ctx, tk.ID)
		attempt := "no attempt"
		if len(st) > 0 {
			attempt = fmt.Sprintf("attempt %d %s", st[0].Attempt, st[0].Outcome)
		}
		see("ledger", "what the dead runner left behind", fmt.Sprintf("lease held by %q; task %s in phase %s; %s", ls.Owner, t1.Status, t1.Phase, attempt))
	}
	// A second runner is refused while the lease is fresh...
	r2 := newRunner(s, root, &scripted.Runtime{}, nil)
	_, err2 := r2.Run(ctx, tk.ID, RunOptions{})
	if err2 == nil || !strings.Contains(err2.Error(), "another process") {
		t.Fatalf("concurrent run allowed: %v", err2)
	}
	see("runner B", "tries to run the task while the lease is fresh", "refused: "+err2.Error())
	// ...and takes over once it is stale.
	old := leaseStale
	leaseStale = 50 * time.Millisecond
	defer func() { leaseStale = old }()
	time.Sleep(100 * time.Millisecond)
	see("test", "waits for the lease to go stale",
		fmt.Sprintf("no heartbeat for longer than %s (%s in production, shortened for the test)", leaseStale, old))

	var resumeMsg string
	survived := false
	fix := func(ws, msg string) (string, error) {
		resumeMsg = msg
		if _, err := os.Stat(filepath.Join(ws, partial)); err != nil {
			return "", err // the killed attempt's edits must survive
		}
		survived = true
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
	for _, line := range strings.Split(resumeMsg, "\n") {
		if strings.Contains(line, "resuming a task") {
			see("runner C", "takes over the stale lease; its agent is told", strings.TrimSpace(line))
			break
		}
	}
	see("runner C", "its agent looks for the killed attempt's file", fmt.Sprintf("%s still in the worktree: %t", partial, survived))
	strats, _ := l.Strategies(ctx, tk.ID)
	if strats[0].Outcome != "rejected" || !strings.Contains(strats[0].Reason, "previous run stopped") {
		t.Fatalf("orphaned attempt not reconciled: %+v", strats[0])
	}
	see("ledger", "reconciles the orphaned attempt", fmt.Sprintf("attempt %d %s: %s", strats[0].Attempt, strats[0].Outcome, strats[0].Reason))
	evs, _ := telemetry.Events(ctx, s.DB, tk.ID, 0, 1000)
	recovered := false
	kinds := make([]string, 0, len(evs))
	for _, e := range evs {
		recovered = recovered || e.Kind == "task.recovered"
		kinds = append(kinds, e.Kind)
	}
	if !recovered {
		t.Fatal("no task.recovered event")
	}
	see("ledger", "the task's event log, in order", strings.Join(kinds, " → "))
	ls, _ = l.LeaseOf(ctx, tk.ID)
	if ls.Owner != "" {
		t.Fatalf("lease not released after completion: %+v", ls)
	}
	wts, _ := l.Worktrees(ctx, tk.ID)
	if out, _ := gitops.Run(ctx, wts[0].Path, "status", "--porcelain"); out != "" {
		t.Fatalf("uncommitted work left: %s", out)
	}
	see("runner C", "finishes the task", fmt.Sprintf("status %s; lease released; worktree clean", got.Status))
	_ = s.Close()
	checkReplay(t, rec)
}

// checkReplay compares what the run observed with testdata/crash-replay.json.
func checkReplay(t *testing.T, rec crashReplay) {
	t.Helper()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	golden := filepath.Join("testdata", "crash-replay.json")
	if *updateReplay {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (create it with -update-replay)", err)
	}
	if !bytes.Equal(want, b) {
		t.Fatalf("this run observed something other than %s (rerun with -update-replay if the change is intended):\n%s", golden, b)
	}
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

// TestCloudFailures: with a cloud provider, a rate-limited or unavailable
// API is retried without burning attempts (there is no server to restart),
// and a rejected API key blocks the task with the fix instead of burning
// attempts.
func TestCloudFailures(t *testing.T) {
	failing := func(s *store.Store, taskID *string, status string) scripted.Step {
		return func(string, string) (string, error) {
			_, err := s.DB.Exec(`INSERT INTO model_calls(task_id, source, model, provider, status, created_at) VALUES(?, 'agent', 'm', 'anthropic', ?, ?)`,
				*taskID, status, store.Now())
			if err != nil {
				return "", err
			}
			return "", os.ErrDeadlineExceeded
		}
	}
	fix := func(ws, _ string) (string, error) {
		if err := addReproTest(ws); err != nil {
			return "", err
		}
		return "negated", replaceIn(filepath.Join(ws, consumerFile),
			"AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	defer func(f func(int) time.Duration) { cloudBackoff = f }(cloudBackoff)
	cloudBackoff = func(int) time.Duration { return time.Millisecond }

	t.Run("rate limited", func(t *testing.T) {
		_, w, s, root := setup(t)
		defer s.Close()
		var taskID string
		r := newRunner(s, root, &scripted.Runtime{Steps: []scripted.Step{failing(s, &taskID, "http_429"), fix}}, nil)
		r.Cfg.Inference.Provider = "anthropic"
		tk, err := r.Create(context.Background(), w, "Fix the unbalanced ledger posting", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		taskID = tk.ID
		got, err := r.Run(context.Background(), tk.ID, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != task.StatusCompleted || got.AttemptCount != 1 {
			t.Fatalf("status=%s attempts=%d", got.Status, got.AttemptCount)
		}
	})
	t.Run("key rejected", func(t *testing.T) {
		_, w, s, root := setup(t)
		defer s.Close()
		var taskID string
		r := newRunner(s, root, &scripted.Runtime{Steps: []scripted.Step{failing(s, &taskID, "http_401"), fix}}, nil)
		r.Cfg.Inference.Provider = "anthropic"
		tk, err := r.Create(context.Background(), w, "Fix the unbalanced ledger posting", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		taskID = tk.ID
		got, _ := r.Run(context.Background(), tk.ID, RunOptions{})
		if got == nil || got.Status != task.StatusBlocked || got.AttemptCount != 0 {
			t.Fatalf("task = %+v", got)
		}
		if last := got.Decisions[len(got.Decisions)-1].Text; !strings.Contains(last, "API key was rejected") {
			t.Fatalf("block reason = %q", last)
		}
	})
}
