package supervisor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
)

func TestOpenCodeContextSurvivesSessionRecreation(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	first, err := supervisor.StartTask(ctx, s.Store, "Keep audit records for seven years", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordAnswer(ctx, s.Store, first.ID, "May we delete old records?", "No"); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindSessionStart,
		map[string]any{"requirements": []string{"Export a machine readable audit trail"},
			"constraints": []string{"Do not touch encryption keys"}}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindRecipeRun,
		supervisor.VerificationRecord{Accepted: false, Candidate: "abc", Reasons: []string{"audit test failed"}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"ses_original", "ses_after_restart"} {
		body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, session)
		if err != nil {
			t.Fatal(err)
		}
		for _, need := range []string{first.ID, first.Title, "May we delete old records?", "No", "audit test failed", "Export a machine readable audit trail", "Do not touch encryption keys"} {
			if !strings.Contains(body, need) {
				t.Errorf("session %s lost %q: %s", session, need, body)
			}
		}
	}
	second, err := supervisor.StartTask(ctx, s.Store, "Unrelated active work", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, "ses_unbound")
	if err != nil || !strings.Contains(body, "Multiple supervised tasks") {
		t.Fatalf("ambiguous task was silently selected: %q, %v", body, err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindSessionStart,
		map[string]any{"session_id": "ses_bound", "executor": "opencode"}); err != nil {
		t.Fatal(err)
	}
	body, err = supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, "ses_bound")
	if err != nil || !strings.Contains(body, first.Title) || strings.Contains(body, second.Title) {
		t.Fatalf("session binding did not select the task: %q, %v", body, err)
	}
}

func TestOpenCodeContextBoundsDecisionTail(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "Remember the original objective", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := supervisor.RecordAnswer(ctx, s.Store, task.ID, "Decision question", strings.Repeat("a", 80)); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := supervisor.DecisionPage(ctx, s.Store, task.ID, 95, 5)
	if err != nil || total != 100 || len(page) != 5 || page[0].Answer != strings.Repeat("a", 80) {
		t.Fatalf("decision page = %d of %d, err=%v", len(page), total, err)
	}
	body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, "ses_bounded_tail")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 5000 || !strings.Contains(body, "earlier user decisions remain") ||
		!strings.Contains(body, task.Title) || !strings.Contains(body, "bc_task_history") {
		t.Fatalf("context card did not retain a bounded, retrievable task history: %d bytes: %s", len(body), body)
	}
}
