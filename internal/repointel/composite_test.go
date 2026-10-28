package repointel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLSP serves Go only, like gopls.
type fakeLSP struct {
	fail bool
	syms []Symbol
}

func (f fakeLSP) Name() string           { return "fake" }
func (f fakeLSP) Covers(string) []string { return []string{"go"} }
func (f fakeLSP) FindSymbol(context.Context, string, string, FindOptions) ([]Symbol, error) {
	if f.fail {
		return nil, ErrUnavailable
	}
	return f.syms, nil
}
func (f fakeLSP) References(context.Context, string, Symbol) ([]Reference, error) {
	return []Reference{{File: "svc/total.go", Line: 9, Symbol: "lsp"}}, nil
}
func (f fakeLSP) Implementations(context.Context, string, Symbol) ([]Symbol, error) { return nil, nil }

func polyglotRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for p, s := range map[string]string{
		"svc/total.go":        "package svc\n\nfunc Total(xs []int) int {\n\treturn 0\n}\n",
		"web/total.ts":        "export function Total(xs: number[]): number {\n  return 0\n}\n",
		"jobs/total.py":       "def Total(xs):\n    return 0\n",
		"db/001_total.sql":    "CREATE FUNCTION Total() RETURNS int AS $$ SELECT 0 $$ LANGUAGE sql;\n",
		"svc/total_caller.go": "package svc\n\nvar _ = Total(nil)\n",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func files(s []Symbol) string {
	var out []string
	for _, x := range s {
		out = append(out, x.File+":"+x.Kind)
	}
	return strings.Join(out, " ")
}

// TestCompositeRoutesByLanguage: the language server answers for Go, the
// text navigator for TypeScript, Python and SQL, in one result.
func TestCompositeRoutesByLanguage(t *testing.T) {
	root := polyglotRoot(t)
	lsp := fakeLSP{syms: []Symbol{{NamePath: "Total", Kind: "Function", File: "svc/total.go", StartLine: 3, EndLine: 5}}}
	got, err := WithFallback(lsp).FindSymbol(context.Background(), root, "Total", FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := files(got)
	if !strings.Contains(s, "svc/total.go:Function") || strings.Contains(s, "svc/total.go:Go definition") {
		t.Fatalf("Go must come from the language server only: %s", s)
	}
	for _, f := range []string{"web/total.ts:TypeScript definition", "jobs/total.py:Python definition"} {
		if !strings.Contains(s, f) {
			t.Fatalf("%s missing (no server serves it, the text navigator must): %s", f, s)
		}
	}
}

// TestCompositeFailedServerFallsBack: a server that is down does not hide
// its languages; the text navigator answers for them.
func TestCompositeFailedServerFallsBack(t *testing.T) {
	root := polyglotRoot(t)
	got, err := WithFallback(fakeLSP{fail: true}).FindSymbol(context.Background(), root, "Total", FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s := files(got); !strings.Contains(s, "svc/total.go:Go definition") || !strings.Contains(s, "web/total.ts") {
		t.Fatalf("text fallback missing: %s", s)
	}
}

// TestCompositeReferencesAndImplementations route by the symbol's file.
func TestCompositeReferencesAndImplementations(t *testing.T) {
	root := polyglotRoot(t)
	nav := WithFallback(fakeLSP{})
	refs, err := nav.References(context.Background(), root, Symbol{NamePath: "Total", File: "svc/total.go"})
	if err != nil || len(refs) != 1 || refs[0].Symbol != "lsp" {
		t.Fatalf("Go references must come from the server: %+v %v", refs, err)
	}
	refs, err = nav.References(context.Background(), root, Symbol{NamePath: "Total", File: "web/total.ts"})
	if err != nil || len(refs) == 0 || refs[0].Symbol == "lsp" {
		t.Fatalf("TypeScript references must come from the text navigator: %+v %v", refs, err)
	}
	if _, err := nav.Implementations(context.Background(), root, Symbol{NamePath: "Total", File: "web/total.ts"}); !errors.Is(err, ErrNeedsLanguageServer) {
		t.Fatalf("implementations without a server: %v", err)
	}
}

// TestCompositeNoText: only the servers answer.
func TestCompositeNoText(t *testing.T) {
	root := polyglotRoot(t)
	lsp := fakeLSP{syms: []Symbol{{NamePath: "Total", Kind: "Function", File: "svc/total.go", StartLine: 3}}}
	got, err := Composite{Providers: []Navigator{lsp}, NoText: true}.FindSymbol(context.Background(), root, "Total", FindOptions{})
	if err != nil || files(got) != "svc/total.go:Function" {
		t.Fatalf("%s %v", files(got), err)
	}
}
