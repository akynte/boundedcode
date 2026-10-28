package verify

import (
	"context"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

func all(n int) map[int]bool {
	m := map[int]bool{}
	for i := 1; i <= n; i++ {
		m[i] = true
	}
	return m
}

func summary(ms []Mutant) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Orig+">"+m.Repl)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestGoMutants(t *testing.T) {
	src := "package p\n\nfunc Clamp(v, max int) int {\n\tif v > max {\n\t\treturn max\n\t}\n\treturn v\n}\n\nfunc Ok(s string) bool {\n\treturn s != \"a < b\" && len(s) < 10\n}\n"
	ms := goMutants("p.go", []byte(src), all(20))
	got := summary(ms)
	for _, want := range []string{">><=", ">>>=", "!=>==", "&&>||", "<>>=", "<><=", "10>11", "v > max>!(v > max)", "max>(max)+1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, m := range ms {
		mut := src[:m.Start] + m.Repl + src[m.End:]
		if !goParses("p.go", []byte(mut)) {
			t.Errorf("%s does not parse", m)
		}
		if !strings.Contains(mut, `"a < b"`) {
			t.Errorf("mutated inside a string: %s", m)
		}
	}
	// Only the lines the change touched.
	if ms := goMutants("p.go", []byte(src), map[int]bool{4: true}); summary(ms) != ">><= >>>= v > max>!(v > max)" {
		t.Errorf("line 4 only: %s", summary(ms))
	}
}

func TestPythonMutants(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3")
	}
	src := "def clamp(v, mx):\n    if v > mx and mx >= 0:\n        return mx\n    s = 'a < b'  # x < y\n    return -v + 1 if False else v\n"
	ms, err := pythonMutants(context.Background(), "p.py", []byte(src), all(10))
	if err != nil {
		t.Fatal(err)
	}
	got := summary(ms)
	for _, want := range []string{">><=", ">>>=", ">=><", ">=>>", "and>or", "+>-", "False>True", "0>1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	ret, err := pythonMutants(context.Background(), "r.py", []byte("def f(a):\n    return 5\n"), all(5))
	if err != nil || summary(ret) != "5>6" {
		t.Errorf("return value: %s %v", summary(ret), err)
	}
	for _, m := range ms {
		if m.Line == 4 {
			t.Errorf("mutated a string or comment: %s", m)
		}
		if m.Orig == "-" && m.Line == 5 && m.Start == strings.Index(src, "-v") {
			t.Errorf("mutated a unary minus: %s", m)
		}
	}
}

func TestLexMutants(t *testing.T) {
	for name, c := range map[string]struct {
		src          string
		hash, rust   bool
		want, absent string
	}{
		"javascript": {src: "function f(a, b) {\n  const s = \"a < b\"; // x && y\n  return a < b && b !== 3 ? true : `${a > b}`;\n}\n",
			want: "!==>=== &&>|| 3>4 <><= <>>= true>false", absent: ">>"},
		"typescript": {src: "export function f(xs: Array<number>, n: number): boolean {\n  return xs.length >= n || n == 0;\n}\n",
			want: "0>1 ==>!= >=>< >=>> ||>&&", absent: "<number"},
		"java": {src: "class A {\n  List<String> xs; /* a < b */\n  boolean f(int v) { return v > 10; }\n}\n", want: "10>11 >><= >>>="},
		"rust": {src: "fn f<'a>(s: &'a str, n: usize) -> bool {\n    let c = 'x';\n    s.len() <= n && c != 'y'\n}\n", rust: true,
			want: "!=>== &&>|| <=>< <=>>"},
		"cpp":          {src: "template <typename T>\nbool lt(const T& a, const T& b) { return a < b; }\n// a > b\n", want: "<><= <>>="},
		"php":          {src: "<?php\nfunction f($a) {\n  # $a > 1\n  return $a >= 2 && true;\n}\n", hash: true, want: "&&>|| 2>3 >=>< >=>> true>false"},
		"return value": {src: "int f(int a) {\n  if (a >= 50) {\n    return 5;\n  }\n  return 10;\n}\n", want: "10>11 50>51 5>6 >=>< >=>>"},
		"unterminated": {src: "const s = \"open\nconst b = a < b;\n", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			ms := lexMutants("f", []byte(c.src), all(20), c.hash, c.rust)
			if got := summary(ms); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSpread(t *testing.T) {
	ms := []Mutant{{Line: 1, Start: 1, Operator: "relational"}, {Line: 1, Start: 2, Operator: "logical"}, {Line: 2, Start: 3, Operator: "relational"},
		{Line: 3, Start: 4, Operator: "constant"}}
	got := spread(ms, 3)
	if len(got) != 3 || got[0].Line != 1 || got[1].Line != 3 || got[2].Line != 1 && got[2].Line != 2 {
		t.Fatalf("%+v", got)
	}
	// Boundary mutants come after the scored ones.
	got = spread([]Mutant{{Line: 1, Start: 1, Operator: "boundary"}, {Line: 2, Start: 2, Operator: "relational"}}, 2)
	if got[0].Operator != "relational" || got[1].Operator != "boundary" {
		t.Fatalf("%+v", got)
	}
}
