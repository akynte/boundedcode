package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/session"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/workspace"
)

func TestBrokerPinsWorkspaceAndPrompt(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Init(repo, workspace.InitOptions{Name: "one"}); err != nil {
		t.Fatal(err)
	}
	s, err := session.Open(ctx, t.TempDir(), repo)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task, err := supervisor.StartTask(ctx, s.Store, "Private task one", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	socket, stop, err := startOpenCodeBroker(ctx, s.Store, s.Root.Layout().Root(), repo, t.TempDir(), "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	t.Setenv(brokerEnv, socket)
	forged := socket[:len(socket)-1] + "0"
	if forged == socket {
		forged = socket[:len(socket)-1] + "1"
	}
	t.Setenv(brokerEnv, forged)
	if _, err := brokerCall(ctx, brokerRequest{Kind: "context", Session: "ses_one"}); err == nil {
		t.Fatal("forged broker capability accepted")
	}
	t.Setenv(brokerEnv, socket)
	if _, err := brokerCall(ctx, brokerRequest{Kind: "prompt", Session: "ses_one", Message: "msg_one", Body: "Do private work"}); err != nil {
		t.Fatal(err)
	}
	if hash, err := supervisor.OriginalOpenCodePrompt(ctx, s.Store, "ses_one"); err != nil || hash == "" {
		t.Fatalf("prompt not saved: %s %v", hash, err)
	}
	body, err := brokerCall(ctx, brokerRequest{Kind: "context", Session: "ses_one"})
	if err != nil || !strings.Contains(body, task.Title) {
		t.Fatalf("context: %q %v", body, err)
	}
	if _, err := brokerCall(ctx, brokerRequest{Kind: "other"}); err == nil {
		t.Fatal("arbitrary operation accepted")
	}
	// The socket owns exactly one Store. A second workspace never enters its
	// address space even when a client knows its task ID.
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(second, "go.mod"), []byte("module y\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Init(second, workspace.InitOptions{Name: "two"}); err != nil {
		t.Fatal(err)
	}
	other, err := session.Open(ctx, t.TempDir(), second)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherTask, err := supervisor.StartTask(ctx, other.Store, "Secret workspace two", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	body, err = brokerCall(ctx, brokerRequest{Kind: "context", Session: "ses_unbound"})
	if err != nil || strings.Contains(body, otherTask.Title) {
		t.Fatalf("cross-workspace state exposed: %q %v", body, err)
	}
}
