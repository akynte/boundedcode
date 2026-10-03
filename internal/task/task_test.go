package task

import (
	"context"
	"testing"

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
