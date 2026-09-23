package retrieval_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Qualified symbol references, the way a model writes them.
//
// The recorded failure: EDIT's packet is built from the plan's symbols, and a
// plan wrote `Reconciler.RunOnce`, `worker.Report.ReservationsFreed` and
// `internal/service/user.go::UserService.Email`. The graph stores short names
// and the lookup was exact, so none matched, the packet held no code, and the
// model spent its whole EDIT budget reading the repository from scratch.

func qualifiedRepo(t *testing.T) *retrieval.Retriever {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod": "module example.test/wh\n\ngo 1.26\n",
		"internal/worker/reconcile.go": "package worker\n\n// Reconciler frees orphaned reservations.\ntype Reconciler struct{}\n\n" +
			"// RunOnce runs one pass.\nfunc (r *Reconciler) RunOnce() int { return 1 }\n",
		"internal/worker/restock.go": "package worker\n\n// Restocker reorders stock.\ntype Restocker struct{}\n\n" +
			"// RunOnce runs one pass.\nfunc (r *Restocker) RunOnce() int { return 2 }\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	ctx := context.Background()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{ID: "r", Name: "r", Path: dir, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		t.Fatal(err)
	}
	return retrieval.New(st)
}

// runOnceFiles reports which files the packet's RunOnce slices came from.
func runOnceFiles(t *testing.T, r *retrieval.Retriever, symbols ...string) []string {
	t.Helper()
	pkt, err := r.Build(context.Background(), retrieval.Request{Symbols: symbols, TokenBudget: 20000})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, s := range pkt.Slices {
		if s.Symbol == "RunOnce" && s.Origin == retrieval.OriginExplicit {
			files = append(files, s.Path)
		}
	}
	sort.Strings(files)
	return files
}

func TestAQualifiedSymbolRetrievesItsDeclaration(t *testing.T) {
	r := qualifiedRepo(t)
	for _, ref := range []string{
		"Reconciler.RunOnce",
		"worker.Reconciler.RunOnce",
		"internal/worker/reconcile.go::Reconciler.RunOnce",
		"internal/worker/reconcile.go::RunOnce",
	} {
		got := runOnceFiles(t, r, ref)
		if len(got) != 1 || got[0] != "internal/worker/reconcile.go" {
			t.Errorf("%s retrieved RunOnce from %v, want only internal/worker/reconcile.go", ref, got)
		}
	}
}

// The qualifier narrows; it is not ignored. Without that, `Reconciler.RunOnce`
// would drag in every RunOnce in the repository.
func TestAQualifierThatMatchesNothingRetrievesNothing(t *testing.T) {
	r := qualifiedRepo(t)
	if got := runOnceFiles(t, r, "Invoicer.RunOnce"); len(got) != 0 {
		t.Errorf("Invoicer.RunOnce retrieved %v; no Invoicer exists", got)
	}
}

// A short name behaves exactly as before: every declaration of it.
func TestAShortNameStillRetrievesEveryDeclaration(t *testing.T) {
	r := qualifiedRepo(t)
	got := runOnceFiles(t, r, "RunOnce")
	if len(got) != 2 {
		t.Errorf("RunOnce retrieved %v, want both declarations", got)
	}
}
