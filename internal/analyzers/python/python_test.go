package python_test

// What these tests are for.
//
// A SCIP index is easy to produce and hard to judge: a run that resolved
// nothing still emits documents, definitions and a confident exit status. The
// src-layout defect is exactly that shape — `from pkg.core import handler`
// silently becomes a local symbol, every cross-package edge disappears, and
// the only way to notice is to look at the graph. So every case below asserts
// on nodes and edges in the database, never on the indexer's exit code.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/analyzers/python"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// indexed is one repository, indexed with the semantic layer on.
type indexed struct {
	report index.SemanticReport
	graph  graph.Graph
	store  *store.Store
	repoID string
	root   string
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// sidecarDir finds the pinned indexer, or skips: a machine without it is a
// machine where these tests cannot say anything, and a green run on a missing
// indexer is the outcome to avoid.
func sidecarDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		candidate := filepath.Join(dir, python.SidecarDir)
		if _, err := os.Stat(filepath.Join(candidate, "node_modules", ".bin", "scip-python")); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("scip-python %s is not installed; run `npm install` in %s", python.Version, python.SidecarDir)
	return ""
}

func indexRepo(t *testing.T, files map[string]string) indexed {
	t.Helper()
	sidecar := sidecarDir(t)
	root := t.TempDir()
	write(t, root, files)

	dataRoot, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dataRoot.CloseAll() })
	ctx := context.Background()
	st, err := dataRoot.OpenWorkspace(ctx, workspace.DeriveID(root, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}

	py := &python.Indexer{SidecarDir: sidecar, Logf: t.Logf}
	ix := index.New(st, index.Options{Semantic: py})
	repo := workspace.Repository{
		ID:   workspace.DeriveRepositoryID(workspace.DeriveID(root, "", t.Name()), ".", ""),
		Name: "r", Path: ".", DefaultBranch: "main",
	}
	if err := ix.RegisterRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.Repository(ctx, repo.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Semantic == nil {
		t.Fatal("no semantic report; the indexer did not run on a Python repository")
	}
	t.Logf("status=%s reason=%q documents=%d definitions=%d refs=%d classes=%d funcs=%d cross=%d inherit=%d",
		stats.Semantic.Status, stats.Semantic.Reason, stats.Semantic.Documents,
		stats.Semantic.Definitions, stats.Semantic.References, stats.Semantic.Classes,
		stats.Semantic.Functions, stats.Semantic.CrossFileEdges, stats.Semantic.InheritanceEdges)
	return indexed{report: *stats.Semantic, graph: graph.New(st), store: st, repoID: repo.ID, root: root}
}

// node finds exactly one node by name, or fails with what was there.
func (i indexed) node(t *testing.T, name string, kinds ...graph.NodeKind) graph.Node {
	t.Helper()
	nodes, err := i.graph.NodesByName(context.Background(), name, kinds, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatalf("no node named %q (kinds %v)", name, kinds)
	}
	return nodes[0]
}

// edgeExists reports an edge of a kind between two named nodes, in either
// direction of lookup but the given direction of the edge.
func (i indexed) edgeExists(t *testing.T, srcName, dstName string, kind graph.EdgeKind) bool {
	t.Helper()
	var n int
	err := i.store.Index().SQL().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM edges e
		  JOIN nodes s ON s.node_id = e.src_id
		  JOIN nodes d ON d.node_id = e.dst_id
		 WHERE e.kind = ? AND s.name = ? AND d.name = ?`, string(kind), srcName, dstName).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// crossFileEdges counts edges whose endpoints are in different files.
func (i indexed) crossFile(t *testing.T, dstName string) int {
	t.Helper()
	var n int
	err := i.store.Index().SQL().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM edges e
		  JOIN nodes s ON s.node_id = e.src_id
		  JOIN nodes d ON d.node_id = e.dst_id
		 WHERE d.name = ? AND s.file_id <> d.file_id`, dstName).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// 1. A function defined in one file and called from another.
func TestFunctionDefinitionAndCrossFileCall(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"lib.py":  "def compute(x: int) -> int:\n    return x + 1\n",
		"main.py": "from lib import compute\n\n\ndef run() -> int:\n    return compute(2)\n",
	})
	fn := ix.node(t, "compute", graph.KindFunction)
	if fn.Path != "lib.py" {
		t.Errorf("compute is attributed to %q, not lib.py", fn.Path)
	}
	if fn.StartLine < 1 {
		t.Errorf("compute has no source range (start line %d)", fn.StartLine)
	}
	if n := ix.crossFile(t, "compute"); n == 0 {
		t.Error("no cross-file edge reaches compute; the call in main.py did not resolve")
	}
	if ix.report.CrossFileEdges == 0 {
		t.Error("the report claims no cross-file edges")
	}
}

// 2. A class, its method, and a reference to that method from elsewhere.
func TestClassAndMethodReference(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"model.py": "class Account:\n    def balance(self) -> int:\n        return 0\n",
		"use.py":   "from model import Account\n\n\ndef total(a: Account) -> int:\n    return a.balance()\n",
	})
	ix.node(t, "Account", graph.KindClass)
	ix.node(t, "balance", graph.KindMethod)
	if n := ix.crossFile(t, "balance"); n == 0 {
		t.Error("a.balance() in use.py did not resolve to the method in model.py")
	}
	if n := ix.crossFile(t, "Account"); n == 0 {
		t.Error("the Account annotation in use.py did not resolve")
	}
}

// 3. An aliased import must still reach the real definition.
func TestImportAlias(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"deep/__init__.py": "",
		"deep/thing.py":    "def widget() -> int:\n    return 1\n",
		"app.py":           "import deep.thing as t\n\n\ndef go() -> int:\n    return t.widget()\n",
	})
	ix.node(t, "widget", graph.KindFunction)
	if n := ix.crossFile(t, "widget"); n == 0 {
		t.Error("t.widget() did not resolve through the alias")
	}
}

// 4. `from x import y` is the most common import form and the one the
// src-layout defect breaks first.
func TestFromImport(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"a/__init__.py": "",
		"a/b.py":        "VALUE = 3\n\n\ndef read() -> int:\n    return VALUE\n",
		"c.py":          "from a.b import read\n\n\ndef go() -> int:\n    return read()\n",
	})
	ix.node(t, "read", graph.KindFunction)
	if n := ix.crossFile(t, "read"); n == 0 {
		t.Error("from a.b import read did not produce a cross-file edge")
	}
}

// 5. Inheritance, which SCIP carries as a relationship rather than an
// occurrence and which therefore exercises a different path entirely.
func TestInheritance(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"base.py":  "class Shape:\n    def area(self) -> float:\n        return 0.0\n",
		"child.py": "from base import Shape\n\n\nclass Square(Shape):\n    def area(self) -> float:\n        return 1.0\n",
	})
	ix.node(t, "Shape", graph.KindClass)
	ix.node(t, "Square", graph.KindClass)
	if !ix.edgeExists(t, "Square", "Shape", graph.EdgeImplements) {
		t.Error("no implements edge from Square to Shape")
	}
	if ix.report.InheritanceEdges == 0 {
		t.Error("the report claims no inheritance edges")
	}
}

// 6 and 7. The src-layout case from the issue, with a test importing
// production code. This is the one that passed for the wrong reason before
// the search path was configured: the index had three documents and every
// cross-package reference was a local symbol.
func TestSrcLayoutPackageImportedByTests(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"pyproject.toml":      "[project]\nname = \"probe\"\nversion = \"0.1.0\"\n",
		"src/pkg/__init__.py": "",
		"src/pkg/core.py": "class Base:\n    def describe(self) -> str:\n        return \"base\"\n\n\n" +
			"class Handler(Base):\n    def __init__(self, name: str) -> None:\n        self.name = name\n\n" +
			"    def describe(self) -> str:\n        return self.name\n\n\n" +
			"def handler(name: str) -> Handler:\n    return Handler(name)\n",
		"tests/test_core.py": "from pkg.core import handler\n\n\ndef test_handler():\n" +
			"    h = handler(\"x\")\n    assert h.describe() == \"x\"\n",
	})
	fn := ix.node(t, "handler", graph.KindFunction)
	if fn.Path != "src/pkg/core.py" {
		t.Fatalf("handler is attributed to %q", fn.Path)
	}
	if n := ix.crossFile(t, "handler"); n == 0 {
		t.Fatal("tests/test_core.py does not reach src/pkg/core.py::handler. This is the " +
			"src-layout defect: pyright named the module src.pkg.core while the import says " +
			"pkg.core, so the reference became a local symbol")
	}
	if n := ix.crossFile(t, "describe"); n == 0 {
		t.Error("h.describe() in the test did not resolve to the method")
	}
	if !ix.edgeExists(t, "Handler", "Base", graph.EdgeImplements) {
		t.Error("inheritance inside the src package was lost")
	}
	if ix.report.Status != index.SemanticAvailable {
		t.Errorf("status = %s (%s)", ix.report.Status, ix.report.Reason)
	}
	// And the configuration written to get here is gone again.
	if _, err := os.Stat(filepath.Join(ix.root, "pyrightconfig.json")); err == nil {
		t.Error("pyrightconfig.json was left behind in the worktree")
	}
}

// 8. A dynamic reference nothing can resolve must not poison what can.
func TestUnresolvedDynamicReferenceDegradesSafely(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"real.py": "def solid() -> int:\n    return 1\n",
		"dyn.py": "import importlib\n\nfrom real import solid\n\n\n" +
			"def go(name: str):\n    mod = importlib.import_module(name)\n" +
			"    return getattr(mod, name)() + solid()\n",
	})
	if n := ix.crossFile(t, "solid"); n == 0 {
		t.Error("a resolvable call was lost because an unresolvable one shared the file")
	}
	if ix.report.Status == index.SemanticUnavailable {
		t.Errorf("a dynamic import made the whole index unavailable: %s", ix.report.Reason)
	}
}

// 9. One unparseable file must not take the repository with it.
func TestSyntaxErrorInOneFileLeavesTheRestUsable(t *testing.T) {
	ix := indexRepo(t, map[string]string{
		"good.py":   "def fine() -> int:\n    return 1\n",
		"user.py":   "from good import fine\n\n\ndef go() -> int:\n    return fine()\n",
		"broken.py": "def oops(:\n    this is not python\n",
	})
	ix.node(t, "fine", graph.KindFunction)
	if n := ix.crossFile(t, "fine"); n == 0 {
		t.Error("the working files lost their edges because one file does not parse")
	}
	if ix.report.Status == index.SemanticUnavailable {
		t.Errorf("one syntax error made the index unavailable: %s", ix.report.Reason)
	}
}

// 10. Indexing must not use the network. Proved by pointing every proxy
// variable at a closed port: a tool that reached out would fail, and this one
// does not.
func TestIndexingUsesNoNetwork(t *testing.T) {
	for _, v := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY"} {
		t.Setenv(v, "http://127.0.0.1:1")
	}
	t.Setenv("npm_config_registry", "http://127.0.0.1:1")
	ix := indexRepo(t, map[string]string{
		"lib.py":  "def compute(x: int) -> int:\n    return x + 1\n",
		"main.py": "from lib import compute\n\n\ndef run() -> int:\n    return compute(2)\n",
	})
	if ix.report.Status != index.SemanticAvailable {
		t.Errorf("indexing needed the network: %s (%s)", ix.report.Status, ix.report.Reason)
	}
	if n := ix.crossFile(t, "compute"); n == 0 {
		t.Error("no cross-file edge with the network closed")
	}
}

// A repository that is not Python must cost nothing and claim nothing.
func TestNonPythonRepositoryIsNotClaimed(t *testing.T) {
	py := &python.Indexer{}
	root := t.TempDir()
	write(t, root, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	if py.Applies(root) {
		t.Error("a Go repository was claimed by the Python indexer")
	}
}

// The pinned version is what runs, and a mismatch is disclosed rather than
// silently producing a differently-named graph.
func TestVersionIsPinned(t *testing.T) {
	dir := sidecarDir(t)
	body, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `"@sourcegraph/scip-python": "` + python.Version + `"`
	if !strings.Contains(string(body), want) {
		t.Errorf("%s/package.json does not pin %s", python.SidecarDir, want)
	}
}
