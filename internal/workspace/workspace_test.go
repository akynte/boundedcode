package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
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
