package stats

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

func TestCompute(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, _ := workspace.Store{DB: s.DB}.Create(ctx, "ws")
	l := task.Ledger{DB: s.DB}
	mk := func(status task.Status, attempts, tokens int, wallS float64) string {
		tk := &task.Task{WorkspaceID: w.ID, OriginalRequest: "x"}
		_ = l.Create(ctx, tk)
		tk.Status, tk.AttemptCount = status, attempts
		tk.Budget.UsedLocalTokens, tk.Budget.UsedWallClockS = tokens, wallS
		if err := l.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	mk(task.StatusCompleted, 2, 100000, 1800)
	esc := mk(task.StatusCompleted, 3, 200000, 1800)
	mk(task.StatusBlocked, 6, 300000, 3600)
	_, err = s.DB.Exec(`INSERT INTO escalations(task_id, trigger, reason, provider, status, packet_tokens, created_at, updated_at)
		VALUES(?, 'Z2', 'r', 'codex', 'answered', 6000, 'x', 'x')`, esc)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := Compute(ctx, s.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Tasks != 3 || sum.Completed != 2 || sum.CompletedLocalOnly != 1 || sum.LocalOnlyRate != 0.5 {
		t.Fatalf("%+v", sum)
	}
	if sum.EscalationRate < 0.33 || sum.EscalationRate > 0.34 || sum.FrontierPacketTok != 6000 || sum.LocalTokens != 600000 {
		t.Fatalf("%+v", sum)
	}
	if sum.WallHours != 2 || sum.VerifiedTasksPerHour != 1 || sum.AttemptsPerCompleted != 2.5 {
		t.Fatalf("%+v", sum)
	}
}

// TestProviders: a task with cloud model calls is not local-only, usage is
// grouped per provider, and cost is estimated only from configured prices.
func TestProviders(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, _ := workspace.Store{DB: s.DB}.Create(ctx, "ws")
	l := task.Ledger{DB: s.DB}
	mk := func(provider string, prompt, cached, completion int, status string) {
		tk := &task.Task{WorkspaceID: w.ID, OriginalRequest: "x"}
		_ = l.Create(ctx, tk)
		tk.Status = task.StatusCompleted
		if err := l.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`INSERT INTO model_calls(task_id, source, model, provider, prompt_tokens, cached_tokens, completion_tokens, status, created_at)
			VALUES(?, 'agent', 'm', ?, ?, ?, ?, ?, ?)`, tk.ID, provider, prompt, cached, completion, status, store.Now()); err != nil {
			t.Fatal(err)
		}
	}
	mk("local", 5000, 4000, 100, "ok")
	mk("anthropic", 2_000_000, 1_000_000, 100_000, "ok")
	mk("anthropic", 0, 0, 0, "http_429")
	sum, err := Compute(ctx, s.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Completed != 3 || sum.CompletedCloud != 2 || sum.CompletedLocalOnly != 1 {
		t.Fatalf("completed %d cloud %d local-only %d", sum.Completed, sum.CompletedCloud, sum.CompletedLocalOnly)
	}
	a := sum.ByProvider["anthropic"]
	if a.Calls != 2 || a.Failed != 1 || a.Prompt != 2_000_000 || a.Cached != 1_000_000 || a.Completion != 100_000 {
		t.Fatalf("anthropic usage %+v", a)
	}
	sum.ApplyPrices(map[string]Price{"anthropic": {Input: 4, CachedInput: 0.2, Output: 20}, "local": {}})
	// 1M uncached × $4 + 1M cached × $0.2 + 0.1M output × $20 = 4 + 0.2 + 2
	if got := sum.ByProvider["anthropic"].CostUSD; got < 6.199 || got > 6.201 {
		t.Fatalf("cost = %v", got)
	}
	if sum.ByProvider["local"].CostUSD != 0 {
		t.Fatal("cost estimated without prices")
	}
}
