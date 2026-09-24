package supervisor_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
)

func TestTaskMutationRequiresTheBoundOpenCodeSession(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "bind this task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindSessionStart, map[string]any{
		"executor": "opencode", "phase": "EDITOR", "session_id": "ses_owner",
	}); err != nil {
		t.Fatal(err)
	}

	owned, err := supervisor.TaskBoundToSession(ctx, s.Store, taskInfo.ID, "ses_owner")
	if err != nil || !owned {
		t.Fatalf("owner session was not bound: owned=%t err=%v", owned, err)
	}
	owned, err = supervisor.TaskBoundToSession(ctx, s.Store, taskInfo.ID, "ses_other")
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatal("a different OpenCode session was accepted as the task owner")
	}
}
