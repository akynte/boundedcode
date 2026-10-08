package xservice

import (
	"os"
	"path/filepath"
	"testing"
)

// Analyzers read arbitrary repository content: they must never panic.

func FuzzJSLex(f *testing.F) {
	for _, s := range []string{"fetch(`${a}/x`)", "/re[/]x/g; a/b", "`${`${x}`}`", "'unterminated", "@Get(", "process.env[", "a.b.c(", "`"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		a := &jsAnalyzer{repo: "r", rel: "f.ts", toks: jsLex(src), bindings: map[string][]jsTok{}, axios: map[string]string{}}
		a.collectBindings()
		a.scan()
	})
}

func FuzzGoSource(f *testing.F) {
	f.Add("package p\nfunc f(){ http.HandleFunc(\"POST /x\", h) }")
	f.Add("package p\nvar _ = fmt.Sprintf(\"%s%\", a)")
	f.Add("package p\nfunc f(){ x.Group(\"/a\").Group }")
	f.Fuzz(func(t *testing.T, src string) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		analyzeGo("r", dir, []string{"a.go"})
	})
}

func FuzzNormalizePath(f *testing.F) {
	f.Add("http://h/a/{b}/c?d")
	f.Fuzz(func(t *testing.T, s string) {
		p := NormalizePath(s)
		if p != "" && p[0] != '/' {
			t.Fatalf("normalized path %q does not start with /", p)
		}
	})
}

func FuzzSQL(f *testing.F) {
	for _, s := range []string{"SELECT * FROM a JOIN b ON", "WITH x AS (SELECT", "CREATE TABLE", "$$ unterminated", "'", `"`, "[x", "/* open",
		"INSERT INTO", "UPDATE x", "DROP TABLE a,", "DELETE FROM", "select ( ( (", ")))"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, r := range analyzeSQL(s) {
			if r.table == "" {
				t.Fatalf("empty table from %q", s)
			}
		}
		_ = looksLikeSQL(s)
	})
}

func FuzzProto(f *testing.F) {
	for _, s := range []string{"service S { rpc M(", "option (google.api.http) = { get:", "package", "message M { option", "service S { rpc M(stream", "/* x", `"x`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		pf := parseProto(s)
		for _, svc := range pf.services {
			for _, r := range svc.rpcs {
				for _, h := range r.http {
					_ = NormalizePath(h.path)
				}
			}
		}
		_ = grpcGatewayPath(s)
	})
}

func FuzzSource(f *testing.F) {
	for _, s := range []string{"'''x", `r#"x`, `@"x`, "<<~SQL\nx", "<<<SQL\n", "=begin", "/* x", "'a", `"""`, "x = Stub(", "this.x = new AClient(", "$c = new XClient("} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, l := range []srcLang{langPython, langJVM, langRust, langCSharp, langRuby, langPHP, langJS} {
			sf := lexSource(l, s)
			if len(sf.code) != len(s) || len(sf.bare) != len(s) {
				t.Fatalf("%s: views changed length", l)
			}
			analyzeSource("r", "f", l, s)
		}
	})
}
