package repointel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const polyglot = "../ecosystem/testdata/polyglot"

// Definitions are found by name in any language, without a language
// server.
func TestTextNavigatorFindSymbol(t *testing.T) {
	ctx := context.Background()
	n := TextNavigator{}
	for name, want := range map[string]string{
		"Total": "services/api/price.go:4", "Basket": "web/src/cart.js:7", "isAdmin": "web/admin/src/user.ts:13",
		"predict": "ml/score.py:11", "restock": "engine/src/lib.rs:15", "withTax": "jvm/orders/src/main/java/shop/Order.java:10",
		"manhattan": "native/geom/geom.c:5", "Account": "dotnet/Ledger/Account.cs:3", "discounted": "php/src/Price.php:11",
		"apply": "ruby/lib/discount.rb:7", "distance": "swift/Sources/Geo/Geo.swift:11", "centsOf": "dart/lib/money.dart:8",
		"build_all": "scripts/deploy.sh:4", "big_orders": "db/migrations/001_init.sql:8", "app_version": "infra/variables.tf:1",
		"Vec/dot": "native/vec/vec.cpp:7",
	} {
		syms, err := n.FindSymbol(ctx, polyglot, name, FindOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range syms {
			got = append(got, s.File+":"+itoa(s.StartLine))
		}
		if !contains(got, want) {
			t.Errorf("%s: %v, want %s", name, got, want)
		}
	}
	syms, _ := n.FindSymbol(ctx, polyglot, "Total", FindOptions{IncludeBody: true, Within: "services"})
	if len(syms) != 1 || !strings.Contains(syms[0].Body, "for _, c := range cents") || syms[0].EndLine < syms[0].StartLine+5 {
		t.Fatalf("body: %+v", syms)
	}
	if _, err := n.Implementations(ctx, polyglot, Symbol{}); !errors.Is(err, ErrNeedsLanguageServer) {
		t.Fatal(err)
	}
	if _, err := n.FindSymbol(ctx, polyglot, "x", FindOptions{Within: "../.."}); err == nil {
		t.Fatal("within escaped the project")
	}
}

func TestTextNavigatorReferences(t *testing.T) {
	refs, err := TextNavigator{}.References(context.Background(), polyglot, Symbol{NamePath: "Total", File: "services/api/price.go"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, r.File+":"+itoa(r.Line)+" "+r.Symbol)
	}
	if !contains(got, "services/api/price.go:16 Sum") || !contains(got, "services/api/price_test.go:6 TestTotal") {
		t.Fatalf("%v", got)
	}
	for _, g := range got {
		if strings.HasPrefix(g, "services/api/price.go:4 ") {
			t.Fatal("the definition is listed as a reference")
		}
	}
}

type emptyNav struct{ TextNavigator }

func (emptyNav) Name() string { return "lsp" }
func (emptyNav) FindSymbol(context.Context, string, string, FindOptions) ([]Symbol, error) {
	return nil, ErrUnavailable
}

func TestFallback(t *testing.T) {
	nav := WithFallback(emptyNav{})
	syms, err := nav.FindSymbol(context.Background(), polyglot, "centsOf", FindOptions{})
	if err != nil || len(syms) != 1 || nav.Name() != "lsp+text" {
		t.Fatalf("%v %v", syms, err)
	}
	if WithFallback(nil).Name() != "text" {
		t.Fatal("no language server: text")
	}
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

// Secret files are never read: not for definitions, not for references.
func TestTextNavigatorSkipsSecrets(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{".env": "API_KEY=s3cret\n", "secrets/keys.py": "API_KEY = 's3cret'\n", "app.py": "import os\nkey = os.environ['API_KEY']\n"} {
		p := dir + "/" + name
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := TextNavigator{}.References(context.Background(), dir, Symbol{NamePath: "API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if strings.Contains(r.Snippet, "s3cret") {
			t.Fatalf("a secret file was read: %+v", r)
		}
	}
	if len(refs) != 1 || refs[0].File != "app.py" {
		t.Fatalf("%+v", refs)
	}
	syms, _ := TextNavigator{}.FindSymbol(context.Background(), dir, "API_KEY", FindOptions{IncludeBody: true})
	if len(syms) != 0 {
		t.Fatalf("%+v", syms)
	}
}
