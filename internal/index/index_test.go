package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Excluded directories must actually be pruned. This is cheap to get wrong and
// expensive when it is: indexing .git turns a small repository into thousands
// of meaningless files, and node_modules into hundreds of thousands.
func TestExcludedDirectoriesArePruned(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {}\n")
	write("README.md", "# demo\n")
	// Noise that must not be indexed.
	write(".git/config", "[core]\n")
	write(".env", "PRIVATE=value")
	write("config/service.key", "private key")
	write("config/credentials.json", "private value")
	write(".agent/secrets/service.txt", "private value")
	write(".git/objects/ab/cdef", "binary-ish")
	write("node_modules/left-pad/index.js", "module.exports = 1\n")
	write("vendor/example.com/dep/dep.go", "package dep\n")

	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID("/index/test", "", "excludes"))
	if err != nil {
		t.Fatal(err)
	}

	ix := index.New(st, index.Options{})
	repoRec := workspace.Repository{ID: "r1", Name: "demo", Path: ".", DefaultBranch: "main"}
	if err := ix.RegisterRepository(ctx, repoRec); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.Repository(ctx, "r1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Fatalf("expected exactly main.go and README.md, got %d files", stats.Files)
	}

	rows, err := st.Index().SQL().QueryContext(ctx, `SELECT path FROM files ORDER BY path`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		for _, bad := range []string{".git/", "node_modules/", "vendor/"} {
			if len(p) >= len(bad) && p[:len(bad)] == bad {
				t.Errorf("indexed an excluded path: %s", p)
			}
		}
	}
}

func TestIndexingOneRepositoryPreservesAnotherRepositorysChunks(t *testing.T) {
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "first.go"), []byte("package first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.go"), []byte("package second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID("/index/test", "", "multi-repository"))
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(st, index.Options{})
	for _, repo := range []workspace.Repository{
		{ID: "r1", Name: "first", Path: ".", DefaultBranch: "main"},
		{ID: "r2", Name: "second", Path: "second", DefaultBranch: "main"},
	} {
		if err := ix.RegisterRepository(ctx, repo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ix.Repository(ctx, "r1", first); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r2", second); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.Index().SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("indexing the second repository removed the first repository's chunks: got %d, want 2", count)
	}
}

// An empty exclude list is an explicit operator choice and must be honoured,
// not silently replaced with the defaults.
func TestAnExplicitlyEmptyExcludeListIsHonoured(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", ".git/config"} {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(f)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID("/index/test", "", "noexclude"))
	if err != nil {
		t.Fatal(err)
	}

	ix := index.New(st, index.Options{Excludes: []string{}})
	if err := ix.RegisterRepository(ctx,
		workspace.Repository{ID: "r1", Name: "demo", Path: ".", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.Repository(ctx, "r1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Fatalf("an explicitly empty exclude list must index everything, got %d files", stats.Files)
	}
}
