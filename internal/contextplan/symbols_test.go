package contextplan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/task"
)

// fakeGraph is a codebase-memory stand-in: symbols per project.
type fakeGraph struct {
	repointel.Intelligence
	has   map[string][]string // project -> names
	calls []string
}

func (g *fakeGraph) Search(_ context.Context, project, query string, _ int) (string, error) {
	g.calls = append(g.calls, "search "+project+" "+query)
	for _, n := range g.has[project] {
		if n == query {
			return `{"results":[{"name":"` + n + `"}]}`, nil
		}
	}
	return `{"results":[]}`, nil
}

func (g *fakeGraph) Snippet(_ context.Context, project, name string) (string, error) {
	g.calls = append(g.calls, "snippet "+project+" "+name)
	for _, n := range g.has[project] {
		if n == name {
			return "func " + name + "() {} // from graph " + project, nil
		}
	}
	return "symbol not found", nil
}

func (g *fakeGraph) Trace(_ context.Context, project, name, _ string, _ int) (string, error) {
	g.calls = append(g.calls, "trace "+project+" "+name)
	return "caller: main", nil
}

// fakeNav is a Serena stand-in keyed by worktree root.
type fakeNav struct {
	syms  map[string][]repointel.Symbol // root -> symbols
	err   error
	calls []string
}

func (n *fakeNav) Name() string { return "fake" }

func (n *fakeNav) FindSymbol(_ context.Context, root, name string, _ repointel.FindOptions) ([]repointel.Symbol, error) {
	n.calls = append(n.calls, "find "+root+" "+name)
	if n.err != nil {
		return nil, n.err
	}
	var out []repointel.Symbol
	for _, s := range n.syms[root] {
		if s.NamePath == name {
			out = append(out, s)
		}
	}
	return out, nil
}

func (n *fakeNav) References(_ context.Context, root string, s repointel.Symbol) ([]repointel.Reference, error) {
	n.calls = append(n.calls, "refs "+root+" "+s.NamePath)
	return []repointel.Reference{{File: "internal/api/handler.go", Symbol: "Create", Kind: "Method", Line: 30, Snippet: "h.Payments." + s.NamePath + "(ctx, req)"}}, nil
}

func (n *fakeNav) Implementations(_ context.Context, root string, s repointel.Symbol) ([]repointel.Symbol, error) {
	n.calls = append(n.calls, "impls "+root+" "+s.NamePath)
	return []repointel.Symbol{{NamePath: "PostgresRepository", Kind: "Struct", File: "internal/payment/postgres.go", StartLine: 10, EndLine: 13},
		{NamePath: "MemoryRepository", Kind: "Struct", File: "internal/payment/memory.go", StartLine: 7, EndLine: 9}}, nil
}

func navInputs(request string, repos ...string) Inputs {
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: request}, WorkDir: "/w"}
	for _, r := range repos {
		in.Worktrees = append(in.Worktrees, task.Worktree{RepoName: r, Path: "/w/" + r, IndexProject: "ws." + r})
	}
	return in
}

func TestSerenaPreferredForSymbols(t *testing.T) {
	in := navInputs("Add Refund to `PaymentRepository` and use it in CreatePayment", "billing")
	g := &fakeGraph{has: map[string][]string{"ws.billing": {"PaymentRepository", "CreatePayment"}}}
	nav := &fakeNav{syms: map[string][]repointel.Symbol{"/w/billing": {
		{NamePath: "PaymentRepository", Kind: "Interface", File: "internal/payment/repository.go", StartLine: 22, EndLine: 27, Body: "type PaymentRepository interface {\n\tInsert(...)\n}"},
		{NamePath: "CreatePayment", Kind: "Method", File: "internal/payment/service.go", StartLine: 31, EndLine: 40, Body: "func (s *Service) CreatePayment() {}"},
	}}}
	in.Intel, in.Nav = g, nav
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	for _, want := range []string{
		"### billing: PaymentRepository (interface) ./billing/internal/payment/repository.go:22-27",
		"implementations:\n- MemoryRepository (struct) ./billing/internal/payment/memory.go:7-9\n- PostgresRepository",
		"referenced from:\n- ./billing/internal/api/handler.go:30 in Create:",
		"### billing: CreatePayment (method) ./billing/internal/payment/service.go:31-40",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "snippet") || strings.HasPrefix(c, "trace") {
			t.Errorf("graph snippet/trace used although Serena answered: %v", g.calls)
		}
	}
	if st.NavSymbols != 2 || st.GraphSymbols != 0 || st.Fallbacks != 0 || st.NavCalls != 5 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestFallbackToGraphWhenSerenaUnavailable(t *testing.T) {
	in := navInputs("Fix CreatePayment", "billing")
	g := &fakeGraph{has: map[string][]string{"ws.billing": {"CreatePayment"}}}
	in.Intel = g
	in.Nav = &fakeNav{err: fmt.Errorf("%w: serena crashed", repointel.ErrUnavailable)}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if !strings.Contains(out, "from graph ws.billing") || !strings.Contains(out, "callers:") {
		t.Fatalf("no graph fallback:\n%s", out)
	}
	if st.Fallbacks != 1 || st.NavErrors != 1 || st.GraphSymbols != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

// TestGraphSelectsRepositories: in a multi-repository task the graph decides
// which repositories mention a name; Serena is only asked there.
func TestGraphSelectsRepositories(t *testing.T) {
	in := navInputs("Rename the PaymentCharged event field", "gateway", "ledger-service", "payment-service")
	g := &fakeGraph{has: map[string][]string{
		"ws.payment-service": {"PaymentCharged"},
		"ws.ledger-service":  {"PaymentCharged"},
	}}
	nav := &fakeNav{syms: map[string][]repointel.Symbol{
		"/w/payment-service": {{NamePath: "PaymentCharged", Kind: "Struct", File: "internal/events/kafka.go", StartLine: 5, EndLine: 9}},
		"/w/ledger-service":  {{NamePath: "PaymentCharged", Kind: "Struct", File: "internal/consumer/consumer.go", StartLine: 8, EndLine: 12}},
	}}
	in.Intel, in.Nav = g, nav
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	for _, c := range nav.calls {
		if strings.Contains(c, "/w/gateway") {
			t.Fatalf("serena queried a repository the graph excluded: %v", nav.calls)
		}
	}
	if !strings.Contains(out, "./payment-service/internal/events/kafka.go:5-9") || !strings.Contains(out, "./ledger-service/internal/consumer/consumer.go:8-12") {
		t.Fatalf("producer and consumer symbols expected:\n%s", out)
	}
	if st.ReposSkipped != 1 || st.NavSymbols != 2 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestGraphOnlyBehaviourUnchanged(t *testing.T) {
	in := navInputs("Fix CreatePayment", "a", "b")
	g := &fakeGraph{has: map[string][]string{"ws.a": {"CreatePayment"}, "ws.b": {"CreatePayment"}}}
	in.Intel = g
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if strings.Count(out, "### ") != 1 || !strings.Contains(out, "from graph ws.a") {
		t.Fatalf("graph-only path must show the first repository only:\n%s", out)
	}
}

func TestSymbolContextDeterministic(t *testing.T) {
	mk := func() string {
		in := navInputs("Fix `ledger.Post` and PaymentRepository", "billing")
		in.Intel = &fakeGraph{has: map[string][]string{"ws.billing": {"Post", "PaymentRepository"}}}
		in.Nav = &fakeNav{syms: map[string][]repointel.Symbol{"/w/billing": {
			{NamePath: "Post", Kind: "Function", File: "internal/other/post.go", StartLine: 1, EndLine: 2},
			{NamePath: "Post", Kind: "Function", File: "internal/ledger/ledger.go", StartLine: 3, EndLine: 9},
			{NamePath: "PaymentRepository", Kind: "Interface", File: "r.go", StartLine: 1, EndLine: 4},
		}}}
		var st IntelStats
		return requestSymbols(context.Background(), in, &st)
	}
	a := mk()
	if a != mk() {
		t.Fatal("symbol context not deterministic")
	}
	// "ledger.Post": the definition under internal/ledger ranks first.
	if i, j := strings.Index(a, "internal/ledger/ledger.go"), strings.Index(a, "internal/other/post.go"); i < 0 || j < 0 || i > j {
		t.Fatalf("qualified match not ranked first:\n%s", a)
	}
}

func TestNamePatterns(t *testing.T) {
	if got := namePatterns("Store.Get"); strings.Join(got, ",") != "Store/Get,Get" {
		t.Fatal(got)
	}
	if got := namePatterns("CreatePayment"); strings.Join(got, ",") != "CreatePayment" {
		t.Fatal(got)
	}
	if got := clipLines("a\nb\nc\nd", 2); got != "a\nb\n… (2 more lines)" {
		t.Fatalf("%q", got)
	}
}

// lexicalWorktree writes a small checkout for lexical-search tests.
func lexicalWorktree(t *testing.T, files map[string]string) task.Worktree {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return task.Worktree{RepoName: "billing", Path: root, IndexProject: "ws.billing"}
}

func needRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
}

// TestLexicalFallback: names neither Serena nor the graph knows are found by
// ripgrep in the worktree, with context, and never in secret paths.
func TestLexicalFallback(t *testing.T) {
	needRipgrep(t)
	w := lexicalWorktree(t, map[string]string{
		"internal/refund/refund.go": "package refund\n\n// RefundWindow is the refund period.\nconst RefundWindow = 30\n\nfunc a() int { return RefundWindow }\n",
		"secrets/prod.go":           "package secrets\n\nconst RefundWindow = 1 // secret\n",
		"config/credentials.json":   `{"RefundWindow": "hunter2"}`,
		"vendor/x/x.go":             "package x\n\nconst RefundWindow = 2\n",
		"node_modules/y/y.js":       "const RefundWindow = 3\n",
		"internal/other.go":         "package internal\n\n// RefundWindowExtra must not match a whole-word search.\n",
	})
	if err := os.Symlink(filepath.Join(w.Path, "internal"), filepath.Join(w.Path, "linked")); err != nil {
		t.Fatal(err)
	}
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "Extend `RefundWindow` to 60 days"}, Worktrees: []task.Worktree{w}}
	in.Intel = &fakeGraph{has: map[string][]string{}}
	in.Nav = &fakeNav{}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if !strings.Contains(out, "### billing: RefundWindow (lexical) ./billing/internal/refund/refund.go:4") ||
		!strings.Contains(out, ">    4 const RefundWindow = 30") || !strings.Contains(out, "     1 package refund") {
		t.Fatalf("lexical hit with context expected:\n%s", out)
	}
	for _, bad := range []string{"secrets/", "credentials", "hunter2", "vendor/", "node_modules", "RefundWindowExtra", "linked/"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked into lexical context:\n%s", bad, out)
		}
	}
	if st.LexicalSymbols != 1 || st.LexicalCalls != 1 || st.GraphSymbols != 0 || st.NavSymbols != 0 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestLexicalNotUsedWhenGraphAnswers(t *testing.T) {
	needRipgrep(t)
	w := lexicalWorktree(t, map[string]string{"a.go": "package a\n\nfunc CreatePayment() {}\n"})
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "Fix CreatePayment"}, Worktrees: []task.Worktree{w}}
	in.Intel = &fakeGraph{has: map[string][]string{"ws.billing": {"CreatePayment"}}}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if strings.Contains(out, "(lexical)") || st.LexicalCalls != 0 || st.GraphSymbols != 1 {
		t.Fatalf("lexical stage ran although the graph answered: %+v\n%s", st, out)
	}
}

func TestLexicalBounded(t *testing.T) {
	needRipgrep(t)
	files := map[string]string{}
	for i := range 8 {
		files[fmt.Sprintf("f%d.go", i)] = strings.Repeat("var _ = ledgerPost\n", 5)
	}
	w := lexicalWorktree(t, files)
	// Qualified names fall back to their last segment.
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "Fix `ledger.ledgerPost`"}, Worktrees: []task.Worktree{w}}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if n := strings.Count(out, "(lexical)"); n != maxLexicalHits {
		t.Fatalf("%d lexical hits, want %d:\n%s", n, maxLexicalHits, out)
	}
	if strings.Count(out, "./billing/f0.go:") != 3 { // --max-count 3 per file
		t.Fatalf("per-file cap not applied:\n%s", out)
	}
	if st.LexicalCalls != 2 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestLexicalSkippedWithoutRipgrep(t *testing.T) {
	old := rgBinary
	rgBinary = "boundedcode-no-such-rg"
	t.Cleanup(func() { rgBinary = old })
	w := lexicalWorktree(t, map[string]string{"a.go": "package a\n\nfunc CreatePayment() {}\n"})
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "Fix CreatePayment"}, Worktrees: []task.Worktree{w}}
	var st IntelStats
	if out := requestSymbols(context.Background(), in, &st); out != "" || st.LexicalCalls != 0 {
		t.Fatalf("%+v %q", st, out)
	}
}
