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
