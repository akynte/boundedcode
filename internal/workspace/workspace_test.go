package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

func TestWorkspaceRepos(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ws := Store{DB: s.DB}
	w, err := ws.Create(ctx, "payment-platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Create(ctx, "payment-platform"); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err := ws.Create(ctx, "Bad Name"); err == nil {
		t.Fatal("invalid name accepted")
	}
	dir := filepath.Join(t.TempDir(), "svc")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	r, err := ws.AddRepo(ctx, w, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "svc" || len(r.Languages) != 2 {
		t.Fatalf("repo = %+v", r)
	}
	if _, err := ws.AddRepo(ctx, w, dir, ""); err == nil {
		t.Fatal("duplicate repo accepted")
	}
	if err := ws.MarkIndexed(ctx, r.ID, "proj"); err != nil {
		t.Fatal(err)
	}
	got, err := ws.Repo(ctx, w.ID, "svc")
	if err != nil || got.IndexProject != "proj" {
		t.Fatalf("repo lookup: %+v %v", got, err)
	}
}

// TestRepoLifecycle covers add, disable/enable and remove across a reopen of
// a file-backed state database, with the payment-platform fixture's layout.
func TestRepoLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ws := Store{DB: s.DB}
	w, err := ws.Create(ctx, "payment-platform")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"gateway", "auth-service", "payment-service", "ledger-service", "infrastructure"}
	base := t.TempDir()
	added := map[string]Repository{}
	for _, n := range names {
		dir := filepath.Join(base, n)
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		r, err := ws.AddRepo(ctx, w, dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if !r.Enabled {
			t.Fatalf("new repository disabled: %+v", r)
		}
		added[n] = r
	}
	if err := ws.MarkIndexed(ctx, added["gateway"].ID, "payment-platform.gateway"); err != nil {
		t.Fatal(err)
	}
	if err := ws.SetEnabled(ctx, added["infrastructure"].ID, false); err != nil {
		t.Fatal(err)
	}
	// A finished task with a worktree in payment-service.
	led := task.Ledger{DB: s.DB}
	tk := &task.Task{WorkspaceID: w.ID, OriginalRequest: "x"}
	if err := led.Create(ctx, tk); err != nil {
		t.Fatal(err)
	}
	if err := led.AddWorktree(ctx, task.Worktree{TaskID: tk.ID, RepositoryID: added["payment-service"].ID, Path: "/w", Branch: "b", BaseCommit: "c"}); err != nil {
		t.Fatal(err)
	}
	tk.Status = task.StatusCompleted
	if err := led.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ws = Store{DB: s.DB}
	all, err := ws.AllRepos(ctx, w.ID)
	if err != nil || len(all) != len(names) {
		t.Fatalf("all repos after reopen: %d %v", len(all), err)
	}
	for _, r := range all {
		want := added[r.Name]
		if r.ID != want.ID || r.Path != want.Path || r.Languages[0] != "go" {
			t.Fatalf("repository changed across reopen: %+v want %+v", r, want)
		}
		if r.Enabled != (r.Name != "infrastructure") {
			t.Fatalf("%s enabled=%v", r.Name, r.Enabled)
		}
	}
	if gw, _ := ws.Repo(ctx, w.ID, "gateway"); gw.IndexProject != "payment-platform.gateway" {
		t.Fatalf("index metadata lost: %+v", gw)
	}
	enabled, err := ws.Repos(ctx, w.ID)
	if err != nil || len(enabled) != len(names)-1 {
		t.Fatalf("enabled repos: %d %v", len(enabled), err)
	}
	for _, r := range enabled {
		if r.Name == "infrastructure" {
			t.Fatal("disabled repository listed by Repos")
		}
	}
	if r, err := ws.Repo(ctx, w.ID, "infrastructure"); err != nil || r.Enabled {
		t.Fatalf("disabled repository lookup: %+v %v", r, err)
	}

	if err := ws.RemoveRepo(ctx, added["payment-service"].ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("remove of a repository with task worktrees: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO xservice_endpoints(workspace_id, repository_id, repo, kind, file, line, env, confidence, scanned_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, w.ID, added["infrastructure"].ID, "infrastructure", "env_provide", "f", 1, "K", "high", store.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ws.RemoveRepo(ctx, added["infrastructure"].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Repo(ctx, w.ID, "infrastructure"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed repository still found: %v", err)
	}
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM xservice_endpoints WHERE repository_id = ?`, added["infrastructure"].ID).Scan(&n)
	if n != 0 {
		t.Fatalf("%d endpoints left for a removed repository", n)
	}
	if err := ws.RemoveRepo(ctx, added["infrastructure"].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
	if err := ws.SetEnabled(ctx, "r-missing", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("enable missing: %v", err)
	}
	// Re-adding a removed repository is allowed.
	if _, err := ws.AddRepo(ctx, w, filepath.Join(base, "infrastructure"), ""); err != nil {
		t.Fatal(err)
	}
}
