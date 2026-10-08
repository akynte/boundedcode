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
	eps := []Endpoint{{Kind: TopicProduce, Repo: "r", File: "a.go", Line: 3, Topic: "t", Confidence: Exact},
		{Kind: GRPCCall, Repo: "r", File: "b.go", Line: 4, Service: "pkg.S", RPC: "M", Ref: "go:x/y", Confidence: Resolved},
		{Kind: ProtoDefine, Repo: "r", File: "a.proto", Line: 2, Proto: "pkg", Ref: "file:a", Confidence: Exact},
		{Kind: SQLSchema, Repo: "r", File: "m.sql", Line: 1, Table: "orders", Confidence: Exact}}
	for range 2 { // replace, not append
		if err := SaveRepo(ctx, s.DB, w.ID, repo.ID, eps); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadWorkspace(ctx, s.DB, w.ID)
	if err != nil || len(got) != len(eps) {
		t.Fatalf("load: %+v %v", got, err)
	}
	SortEndpoints(eps)
	for i := range eps {
		if got[i] != eps[i] {
			t.Fatalf("round trip:\n got %+v\nwant %+v", got[i], eps[i])
		}
	}
	links := []Link{{From: eps[0], To: Endpoint{Repo: "q", File: "b.go"}}}
	if len(Touching(links, []string{"r/a.go"})) != 1 || len(Touching(links, []string{"r/x.go"})) != 0 {
		t.Fatal("Touching wrong")
	}
}
