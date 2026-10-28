package lang

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const fixture = "../ecosystem/testdata/polyglot"

func TestForPath(t *testing.T) {
	for name, want := range map[string]string{
		"services/api/price.go": "go", "web/src/cart.js": "javascript", "web/admin/src/user.ts": "typescript", "x.d.ts": "typescript",
		"App.tsx": "typescript", "ml/score.py": "python", "engine/src/lib.rs": "rust", "Order.java": "java", "Invoice.kt": "kotlin",
		"build.gradle.kts": "kotlin", "geom.c": "c", "geom.h": "c", "vec.cpp": "cpp", "vec.hpp": "cpp", "Account.cs": "csharp",
		"Price.php": "php", "discount.rb": "ruby", "Rakefile": "ruby", "Gemfile": "ruby", "Geo.swift": "swift", "money.dart": "dart",
		"deploy.sh": "shell", "001_init.sql": "sql", "main.tf": "terraform", "x.tftest.hcl": "terraform", "Dockerfile": "dockerfile",
		"Dockerfile.dev": "dockerfile", "api.dockerfile": "dockerfile", "deployment.yaml": "yaml", "package.json": "json",
		"tsconfig.json": "jsonc", "Cargo.toml": "toml", "pom.xml": "xml", "Ledger.csproj": "xml", "README.md": "markdown",
		"Makefile": "make", "CMakeLists.txt": "cmake", "service.proto": "protobuf", "schema.graphql": "graphql", "lib.ex": "elixir",
		"Main.hs": "haskell", "init.lua": "lua", "analysis.R": "r", "build.zig": "zig", "core.clj": "clojure",
	} {
		l := ForPath(name, "")
		if l == nil || l.ID != want {
			t.Errorf("%s: %v, want %s", name, l, want)
		}
	}
	for first, want := range map[string]string{"#!/usr/bin/env bash": "shell", "#!/bin/sh": "shell", "#!/usr/bin/env python3": "python",
		"#!/usr/bin/python3.12": "python", "#!/usr/bin/env -S node --no-warnings": "javascript", "#!/usr/bin/env ruby": "ruby", "#!/usr/bin/perl -w": "perl"} {
		if l := ForPath("scripts/tool", first); l == nil || l.ID != want {
			t.Errorf("%q: %v, want %s", first, l, want)
		}
	}
	if l := ForPath("notes.unknownext", ""); l != nil {
		t.Errorf("unknown: %v", l.ID)
	}
}

// The definitions of every fixture source file are found, in every
// language; control-flow lines are not definitions.
func TestDefinitions(t *testing.T) {
	for file, want := range map[string]string{
		"services/api/price.go":                    "Total Cart Sum",
		"web/src/cart.js":                          "applyDiscount Basket constructor add totalOf",
		"web/admin/src/user.ts":                    "User Role Level isAdmin UserStore find",
		"ml/score.py":                              "THRESHOLD score Model predict",
		"engine/src/lib.rs":                        "Inventory Inventory take restock tests take_and_restock",
		"jvm/orders/src/main/java/shop/Order.java": "Order withTax",
		"jvm/billing/src/main/kotlin/Invoice.kt":   "Invoice total Currency CODE",
		"native/geom/geom.c":                       "iabs manhattan",
		"native/geom/geom.h":                       "GEOM_H point",
		"native/vec/vec.hpp":                       "shop Vec",
		"native/vec/vec.cpp":                       "shop Vec::Vec Vec::dot",
		"dotnet/Ledger/Account.cs":                 "Shop.Ledger Account Post Entry",
		"php/src/Price.php":                        "Price __construct discounted format_price",
		"ruby/lib/discount.rb":                     "Shop Discount initialize apply none",
		"swift/Sources/Geo/Geo.swift":              "Point distance",
		"dart/lib/money.dart":                      "Money centsOf",
		"scripts/deploy.sh":                        "build_all release",
		"db/migrations/001_init.sql":               "orders orders_total_idx big_orders",
		"infra/main.tf":                            "deploy version",
		"Dockerfile":                               "build run",
	} {
		b, err := os.ReadFile(filepath.Join(fixture, file))
		if err != nil {
			t.Fatal(err)
		}
		l := ForPath(file, "")
		var got []string
		for _, line := range strings.Split(string(b), "\n") {
			if name, ok := l.Definition(line); ok && name != "" {
				got = append(got, name)
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s: %q, want %q", file, strings.Join(got, " "), want)
		}
	}
	js := ByID("javascript")
	for _, line := range []string{"  if (x) {", "  for (const a of b) {", "  while (true) {", "  switch (k) {", "  return f(x);"} {
		if name, ok := js.Definition(line); ok {
			t.Errorf("%q is a definition (%q)", line, name)
		}
	}
	c := ByID("c")
	for _, line := range []string{"    return manhattan(a, b) == 7 ? 0 : 1;", "int x = f(1);", "  foo(bar);"} {
		if name, ok := c.Definition(line); ok {
			t.Errorf("C %q is a definition (%q)", line, name)
		}
	}
}

func TestTests(t *testing.T) {
	for file, want := range map[string]bool{
		"services/api/price_test.go": true, "services/api/price.go": false, "web/src/cart.test.js": true, "web/src/cart.js": false,
		"ml/tests/test_score.py": true, "ml/score.py": false, "jvm/orders/src/test/java/shop/OrderTest.java": true,
		"jvm/orders/src/main/java/shop/Order.java": false, "ruby/test/discount_test.rb": true, "php/tests/PriceTest.php": true,
		"dart/test/money_test.dart": true, "swift/Tests/GeoTests/GeoTests.swift": true, "native/geom/test_geom.c": true,
		"native/vec/vec_test.cpp": true, "dotnet/Ledger.Tests/AccountTests.cs": true, "engine/tests/it.rs": true,
	} {
		if got := ForPath(file, "").IsTest(file); got != want {
			t.Errorf("%s: %v", file, got)
		}
	}
}

// A polyglot repository is counted language by language, none dominant.
func TestCensus(t *testing.T) {
	counts, _, truncated := Census(fixture, CensusOptions{})
	if truncated {
		t.Fatal("truncated")
	}
	got := map[string]int{}
	for _, c := range counts {
		got[c.Language.ID] = c.Files
	}
	for _, id := range []string{"go", "javascript", "typescript", "python", "rust", "java", "kotlin", "c", "cpp", "csharp", "php", "ruby",
		"swift", "dart", "shell", "sql", "terraform", "yaml", "dockerfile", "markdown", "json", "jsonc", "toml", "xml", "make", "cmake"} {
		if got[id] == 0 {
			t.Errorf("%s not counted (%v)", id, got)
		}
	}
	if got["shell"] != 2 {
		t.Errorf("the extensionless script is recognised by its #! line: shell=%d", got["shell"])
	}
}

func TestParseDiagnostics(t *testing.T) {
	out := strings.Join([]string{
		"./price.go:12:5: undefined: Totl",                                                  // go
		"src/user.ts(4,3): error TS2322: Type 'number' is not assignable to type 'string'.", // tsc
		"error[E0308]: mismatched types",                                                    // rustc (message above)
		"  --> src/lib.rs:9:16",
		"geom.c:7:12: error: expected ';' before '}' token",                             // gcc
		"/w/src/main/java/shop/Order.java:9: error: cannot find symbol",                 // javac
		"[ERROR] /w/src/main/java/shop/Order.java:[10,5] ';' expected",                  // maven
		"Account.cs(8,9): error CS0103: The name 'x' does not exist [/w/Ledger.csproj]", // msbuild
		`  File "/w/ml/score.py", line 7, in score`,                                     // python
		"PHP Parse error:  syntax error, unexpected '}' in src/Price.php on line 12",    // php
		"lib/discount.rb:8: syntax error, unexpected end-of-input",                      // ruby
		"scripts/deploy.sh: line 4: syntax error near unexpected token `}'",             // bash
		"e: file:///w/src/main/kotlin/Invoice.kt:3:5 Unresolved reference: x",           // kotlinc
		"see https://example.com/x.html:80 for details",                                 // a URL, not a file
	}, "\n")
	ds := ParseDiagnostics(out, nil, 0)
	var got []string
	for _, d := range ds {
		got = append(got, filepath.Base(d.File)+":"+itoa(d.Line))
	}
	want := "price.go:12 user.ts:4 lib.rs:9 geom.c:7 Order.java:9 Order.java:10 Account.cs:8 score.py:7 Price.php:12 discount.rb:8 deploy.sh:4 Invoice.kt:3"
	if strings.Join(got, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
	if ds[2].Message != "error[E0308]: mismatched types" {
		t.Errorf("rust message %q", ds[2].Message)
	}
	keep := ParseDiagnostics(out, func(f string) bool { return strings.HasSuffix(f, ".java") }, 1)
	if len(keep) != 1 || !strings.HasSuffix(keep[0].File, "Order.java") {
		t.Errorf("filtered %v", keep)
	}
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(i)) }

func fmtInt(i int) string {
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if s == "" {
		s = "0"
	}
	return s
}

func TestInProcessChecks(t *testing.T) {
	for _, c := range []struct {
		name, src string
		ok        bool
	}{
		{"a.json", `{"a": 1}`, true}, {"a.json", `{"a": 1,}`, false}, {"a.json", "  ", false},
		{"a.jsonl", "{\"a\":1}\n{\"b\":2}\n", true}, {"a.jsonl", "{\"a\":1}\n{oops}\n", false},
		{"k.yaml", "a: 1\n---\nb: [1, 2]\n", true}, {"k.yaml", "a: 1\n---\nb: [1, 2\n", false},
		{"p.xml", "<a><b/></a>", true}, {"p.xml", "<a><b></a>", false},
		{"x.go", "package x\nfunc F() {}\n", true}, {"x.go", "package x\nfunc F( {}\n", false},
	} {
		l := ForPath(c.name, "")
		err := l.Check.InProcess(c.name, []byte(c.src))
		if (err == nil) != c.ok {
			t.Errorf("%s %q: %v", c.name, c.src, err)
		}
	}
	if ByID("jsonc").Check != nil {
		t.Error("JSON with comments (tsconfig.json) must not be checked as strict JSON")
	}
}

// A language is added without touching anything else: recognised, counted
// and searchable.
func TestRegisterLanguage(t *testing.T) {
	Register(Language{ID: "nimtest", Name: "Nim", Kind: KindProgramming, Extensions: []string{".nimtest"},
		Definitions: []*regexp.Regexp{regexp.MustCompile(`^proc\s+(?P<name>\w+)`)}})
	t.Cleanup(func() { unregister("nimtest") })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.nimtest"), []byte("proc greet() =\n  echo 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := ForPath("a.nimtest", "")
	if l == nil || l.Name != "Nim" {
		t.Fatal("not recognised")
	}
	if name, _ := l.Definition("proc greet() ="); name != "greet" {
		t.Fatalf("definition %q", name)
	}
	counts, _, _ := Census(dir, CensusOptions{})
	if len(counts) != 1 || counts[0].Language.ID != "nimtest" {
		t.Fatalf("census %+v", counts)
	}
	f := &Finder{LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	var kinds []string
	for _, c := range f.Capabilities(l) {
		kinds = append(kinds, c.Kind+"="+c.Status)
	}
	sort.Strings(kinds)
	if strings.Join(kinds, " ") != "check=unsupported format=unsupported lint=unsupported lsp=unsupported" {
		t.Fatalf("capabilities %v", kinds)
	}
}

func unregister(id string) {
	mu.Lock()
	defer mu.Unlock()
	for i, l := range registry {
		if l.ID == id {
			registry = append(registry[:i], registry[i+1:]...)
			break
		}
	}
	reindex()
}

// Tools are host tools: never a binary inside the project.
func TestFinderExcludesProject(t *testing.T) {
	proj := t.TempDir()
	f := &Finder{Exclude: proj, LookPath: func(bin string) (string, error) {
		if bin == "eslint" {
			return filepath.Join(proj, "node_modules/.bin/eslint"), nil
		}
		if bin == "node" {
			return "/usr/bin/node", nil
		}
		return "", os.ErrNotExist
	}}
	if f.Find("eslint") != "" || f.Find("node") != "/usr/bin/node" {
		t.Fatal("finder")
	}
	caps := map[string]Capability{}
	for _, c := range f.Capabilities(ByID("javascript")) {
		caps[c.Kind] = c
	}
	if caps[CapCheck].Status != Available || caps[CapLint].Status != MissingTool || caps[CapLSP].Status != MissingTool {
		t.Fatalf("%+v", caps)
	}
	if c, _, st := f.FindCheck(ByID("javascript"), "App.jsx"); c != nil || st != Unsupported {
		t.Fatal("JSX is not checked by node")
	}
}
