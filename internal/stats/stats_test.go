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
