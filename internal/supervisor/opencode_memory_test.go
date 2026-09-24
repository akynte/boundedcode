package supervisor_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
)

func TestTypedMemorySurvivesFreshContextWithoutPromotingHypothesis(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	task, err := supervisor.StartTask(ctx, s.Store, "Keep the original criterion", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := supervisor.RecordOpenCodePrompt(ctx, s.Store, "ses_original", "msg_early", "Implement audit retention; never remove encryption checks")
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, task.ID, ledger.KindSessionStart, map[string]any{"session_id": "ses_original", "original_prompt_hash": hash}); err != nil {
		t.Fatal(err)
	}
	evidence, err := artifacts.New(s.Store).Put([]byte("func retainAudit() {}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "repository_fact", Text: "invented confirmation", Evidence: evidence}, true); err == nil {
		t.Fatal("editor promoted its claim to fact")
	}
	fact, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "repository_fact", Text: "retention code is in audit.go", Evidence: evidence, Path: "audit.go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "model_hypothesis", Text: "the old key path is unused"}, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "contradicted_hypothesis", Text: "the old key path is used by archive export", Supersedes: hypothesis}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "repository_fact", Text: "unsupported fact"}, true); err == nil {
		t.Fatal("fact without evidence accepted")
	}
	if _, err := supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "supervisor_decision", Text: "model says accepted"}, true); err == nil {
		t.Fatal("model promoted to supervisor")
	}
	for i := 0; i < 200; i++ {
		_, err = supervisor.RecordMemory(ctx, s.Store, task.ID, supervisor.MemoryRecord{Type: "tool_observation", Text: strings.Repeat("old detail ", 20)}, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []string{"ses_original", "ses_fresh"} {
		body, err := supervisor.OpenCodeContext(ctx, s.Store, s.Workspace.Root, session)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, task.Title) || !strings.Contains(body, "Original raw user request") || !strings.Contains(body, "retention code is in audit.go") || !strings.Contains(body, "source=repository") || !strings.Contains(body, "the old key path is used by archive export") || strings.Contains(body, "Task memory #"+fmt.Sprint(hypothesis)+" [model_hypothesis]") || len(body) > 9000 {
			t.Fatalf("bad context (%d): %s", len(body), body)
		}
	}
	page, err := supervisor.MemoryPage(ctx, s.Store, task.ID, fact+1, 1)
	if err != nil || len(page) != 1 || page[0].ID != fact || page[0].Evidence != evidence {
		t.Fatalf("evidence lost: %+v %v", page, err)
	}
}
