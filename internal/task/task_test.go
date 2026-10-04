package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

func TestLedgerRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err := workspace.Store{DB: s.DB}.Create(ctx, "ws")
	if err != nil {
		t.Fatal(err)
	}
	l := Ledger{DB: s.DB}
	tk := &Task{WorkspaceID: w.ID, OriginalRequest: "add idempotency", AcceptanceCriteria: []string{"tests pass"},
		RemainingSteps: []string{"implement", "verify"}}
	if err := l.Create(ctx, tk); err != nil {
		t.Fatal(err)
	}
	tk.MarkStep("implement")
	tk.Decide("agent", "use idempotency key header")
	tk.Budget.UsedLocalTokens = 1234
	tk.AgentSessionID = "conv-1"
	if err := l.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := l.Get(ctx, tk.ID[:len(tk.ID)-2]) // prefix lookup
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "add idempotency" || len(got.CompletedSteps) != 1 || got.RemainingSteps[0] != "verify" ||
		got.Budget.UsedLocalTokens != 1234 || got.AgentSessionID != "conv-1" || len(got.Decisions) != 1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	id, err := l.AddStrategy(ctx, tk.ID, 1, "patch handler")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.ResolveStrategy(ctx, id, "rejected", "tests failed"); err != nil {
		t.Fatal(err)
	}
	st, _ := l.Strategies(ctx, tk.ID)
	if len(st) != 1 || st[0].Outcome != "rejected" {
		t.Fatalf("strategies = %+v", st)
	}
	list, _ := l.List(ctx, w.ID, 10)
	if len(list) != 1 {
		t.Fatal("list")
	}
}

func TestLeaseAndCancelSafety(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, _ := workspace.Store{DB: s.DB}.Create(ctx, "ws")
	l := Ledger{DB: s.DB}
	tk := &Task{WorkspaceID: w.ID, OriginalRequest: "x"}
	if err := l.Create(ctx, tk); err != nil {
		t.Fatal(err)
	}

	// One live runner at a time.
	if _, err := l.AcquireLease(ctx, tk.ID, "a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcquireLease(ctx, tk.ID, "b", time.Minute); !errors.Is(err, ErrLeased) {
		t.Fatalf("second runner: %v", err)
	}
	// A dead runner's lease goes stale and is taken over; the old lease is reported.
	if _, err := s.DB.Exec(`UPDATE tasks SET lease_heartbeat = '2000-01-01T00:00:00Z' WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	prev, err := l.AcquireLease(ctx, tk.ID, "b", time.Minute)
	if err != nil || prev.Owner != "a" {
		t.Fatalf("takeover: prev=%+v err=%v", prev, err)
	}
	if err := l.Heartbeat(ctx, tk.ID, "a"); err == nil {
		t.Fatal("stale owner kept its lease")
	}
	id, _ := l.AddStrategy(ctx, tk.ID, 1, "(in progress)")
	if n, _ := l.RejectActiveStrategies(ctx, tk.ID, "process died"); n != 1 {
		t.Fatalf("reconciled %d strategies", n)
	}
	st, _ := l.Strategies(ctx, tk.ID)
	if st[0].ID != id || st[0].Outcome != "rejected" {
		t.Fatalf("strategy %+v", st[0])
	}

	// A cancel from another process is not overwritten by the runner's copy.
	other, _ := l.Get(ctx, tk.ID)
	other.Status = StatusCancelled
	if err := l.Save(ctx, other); err != nil {
		t.Fatal(err)
	}
	tk.AttemptCount = 3
	if err := l.Save(ctx, tk); !errors.Is(err, ErrCancelled) {
		t.Fatalf("runner save after cancel: %v", err)
	}
	if got, _ := l.Status(ctx, tk.ID); got != StatusCancelled {
		t.Fatalf("status = %s", got)
	}
	if err := l.ReleaseLease(ctx, tk.ID, "b"); err != nil {
		t.Fatal(err)
	}
	if ls, _ := l.LeaseOf(ctx, tk.ID); ls.Owner != "" {
		t.Fatalf("lease not released: %+v", ls)
	}
}
