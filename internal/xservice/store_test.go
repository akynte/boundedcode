package xservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

func TestSaveLoadTouching(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ws := workspace.Store{DB: s.DB}
	w, _ := ws.Create(ctx, "w")
	dir := filepath.Join(t.TempDir(), "r")
	_ = os.MkdirAll(dir, 0o755)
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	repo, err := ws.AddRepo(ctx, w, dir, "r")
	if err != nil {
		t.Fatal(err)
	}
	eps := []Endpoint{{Kind: TopicProduce, Repo: "r", File: "a.go", Line: 3, Topic: "t", Confidence: Exact}}
	for range 2 { // replace, not append
		if err := SaveRepo(ctx, s.DB, w.ID, repo.ID, eps); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadWorkspace(ctx, s.DB, w.ID)
	if err != nil || len(got) != 1 || got[0].Topic != "t" {
		t.Fatalf("load: %+v %v", got, err)
	}
	links := []Link{{From: got[0], To: Endpoint{Repo: "q", File: "b.go"}}}
	if len(Touching(links, []string{"r/a.go"})) != 1 || len(Touching(links, []string{"r/x.go"})) != 0 {
		t.Fatal("Touching wrong")
	}
}
