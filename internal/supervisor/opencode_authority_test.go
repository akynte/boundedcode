package supervisor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
)

func TestEditorOperationIsAProposalUntilTheSupervisorAuthorizesIt(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "authorize this operation", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindSessionStart, map[string]any{
		"executor": "opencode", "phase": "EDITOR", "session_id": "ses_owner",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := supervisor.AuthorizeEditorOperation(ctx, s.Store, s.Workspace.Root, taskInfo.ID, "ses_other", supervisor.EditorEdit); err == nil {
		t.Fatal("an unbound session was authorized to edit")
	}
	if err := task.NewStore(s.Store).SetState(ctx, taskInfo.ID, task.StateReview); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = supervisor.AuthorizeEditorOperation(ctx, s.Store, s.Workspace.Root, taskInfo.ID, "ses_owner", supervisor.EditorEdit)
	if err == nil || !strings.Contains(err.Error(), "in review") {
		t.Fatalf("review-state edit was not rejected by the supervisor: %v", err)
	}
}

func TestSupervisorRejectsWorkerProviderAndModelRouteBypass(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "route task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindSessionStart, map[string]any{
		"executor": "opencode", "phase": "EDITOR", "session_id": "ses_route",
	}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.AuthorizeModelRequestOnRoute(ctx, s.Store, "ses_route", "other-provider", "other-model", "boundedcode-local", "expected-model"); err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("unauthorized provider was accepted: %v", err)
	}
	if err := supervisor.AuthorizeModelRequestOnRoute(ctx, s.Store, "ses_route", "boundedcode-local", "other-model", "boundedcode-local", "expected-model"); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("unauthorized model was accepted: %v", err)
	}
	if err := supervisor.AuthorizeModelRequestOnRoute(ctx, s.Store, "ses_route", "", "", "boundedcode-local", "expected-model"); err != nil {
		t.Fatalf("missing OpenCode route metadata was treated as an unauthorized route: %v", err)
	}
}

func TestEditorOperationRejectsTerminalTaskBeforeTouchingItsWorktree(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "terminal task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecordEvent(ctx, s.Store, taskInfo.ID, ledger.KindSessionStart, map[string]any{
		"executor": "opencode", "phase": "EDITOR", "session_id": "ses_owner",
	}); err != nil {
		t.Fatal(err)
	}
	if err := task.NewStore(s.Store).SetState(ctx, taskInfo.ID, task.StateAccepted); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := supervisor.AuthorizeEditorOperation(ctx, s.Store, s.Workspace.Root, taskInfo.ID, "ses_owner", supervisor.EditorFinish); err == nil {
		t.Fatal("a terminal task accepted another worker operation")
	}
}
