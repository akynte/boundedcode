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
	if err := supervisor.RecordAnswerWithRationale(ctx, s.Store, first.ID, "May we delete old records?", "No", "The audit retention contract forbids deletion."); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindSessionStart,
		map[string]any{"phase": "EDITOR", "requirements": []string{"Export a machine readable audit trail"},
			"acceptance_criteria": []string{"A fresh session can verify the audit trail without the old chat transcript"},
			"constraints":         []string{"Do not touch encryption keys"}}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindSessionStart,
		map[string]any{"acceptance_criteria": []string{"A resumed session contributes durable criteria"}}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, first.ID, ledger.KindRecipeRun,
		supervisor.VerificationRecord{Accepted: false, Candidate: "abc", Level: "standard", Phase: "VERIFY", Reasons: []string{"audit test failed"}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"ses_original", "ses_after_restart"} {
		body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, session)
		if err != nil {
			t.Fatal(err)
		}
		for _, need := range []string{first.ID, first.Title, "Current phase: EDITOR", "Verification level: standard", "May we delete old records?", "No", "The audit retention contract forbids deletion.", "audit test failed", "abc", "Export a machine readable audit trail", "A fresh session can verify the audit trail without the old chat transcript", "A resumed session contributes durable criteria", "Do not touch encryption keys"} {
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

// Non-goals are a distinct concept from a constraint: a constraint bounds how
// the work may be done, a non-goal says what the user explicitly ruled out of
// scope. A session that forgets a non-goal is prone to "helpfully" doing the
// very thing the user said not to.
func TestOpenCodeContextIsRebuiltFromLedgerNotCheckpointText(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "Keep authoritative state", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, task.ID, ledger.KindCheckpoint, map[string]any{
		"summary": "STALE_MODEL_SUMMARY: the user asked for something else",
	}); err != nil {
		t.Fatal(err)
	}
	body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, "ses_rebuilt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "STALE_MODEL_SUMMARY") || !strings.Contains(body, task.Title) {
		t.Fatalf("context used a prior checkpoint instead of the ledger: %s", body)
	}
}

func TestVerificationHistoryIsPagedAndCandidateBound(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "Keep verification history", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"candidate-a", "candidate-b", "candidate-c"} {
		if err := supervisor.RecordEvent(ctx, s.Store, task.ID, ledger.KindRecipeRun, supervisor.VerificationRecord{
			Accepted: false, Candidate: candidate, Level: "standard", Phase: "VERIFY", Reasons: []string{candidate + " failed"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := supervisor.VerificationPage(ctx, s.Store, task.ID, 0, 2)
	if err != nil || total != 3 || len(page) != 2 || page[0].Candidate != "candidate-c" || page[1].Candidate != "candidate-b" || page[0].ID == 0 || page[1].ID == 0 {
		t.Fatalf("verification page = %+v total=%d err=%v", page, total, err)
	}
	if page[0].Candidate == page[1].Candidate || page[0].Phase != "VERIFY" {
		t.Fatalf("verification provenance was not preserved: %+v", page)
	}
}

func TestOpenCodeContextRecoversNonGoals(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "Add rate limiting to the API", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, task.ID, ledger.KindSessionStart,
		map[string]any{"non_goals": []string{"Do not touch the authentication middleware"}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"ses_original", "ses_after_restart"} {
		body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, session)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "Do not touch the authentication middleware") {
			t.Errorf("session %s lost the recorded non-goal: %s", session, body)
		}
	}
}

func TestOpenCodeContextHasAWholeCardLimit(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "A task with a very large durable history", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	criteria := make([]string, 20)
	for i := range criteria {
		criteria[i] = strings.Repeat("criterion ", 180)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, task.ID, ledger.KindSessionStart, map[string]any{
		"acceptance_criteria": criteria,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := supervisor.RecordAnswerWithRationale(ctx, s.Store, task.ID, "Question", "Answer", strings.Repeat("why ", 100)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 200; i++ {
		if _, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{
			Type: "tool_observation", Text: strings.Repeat("observation ", 30),
		}, true); err != nil {
			t.Fatal(err)
		}
	}
	body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, "ses_bounded")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 12<<10 {
		t.Fatalf("active context grew to %d bytes; the whole-card limit is not enforced", len(body))
	}
	if !strings.Contains(body, task.Title) || !strings.Contains(body, "criterion") {
		t.Fatalf("bounded context dropped the task identity or a criterion: %s", body)
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
