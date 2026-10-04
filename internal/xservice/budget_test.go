package xservice

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// scanBounded scans dir and fails if the scan allocates more than limit
// bytes or takes longer than 10 s.
func scanBounded(t *testing.T, dir string, limit uint64) ([]Endpoint, []Diagnostic) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	eps, diags, err := Scan("r", dir, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("scan took %s", d)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > limit {
		t.Fatalf("scan allocated %d MiB (limit %d MiB)", alloc>>20, limit>>20)
	}
	return eps, diags
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestJSEvaluationIsBounded is the regression test for the 2026-10-04
// validation run that was killed at ~58 GiB RSS while scanning vuejs/core
// (packages/reactivity/src/collectionHandlers.ts): bindings that reference
// themselves and are referenced several times per expression were expanded
// exponentially. Ordinary endpoints in the same file must still resolve.
func TestJSEvaluationIsBounded(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	// The vue shape: names reused across functions, self-referencing
	// bindings, several references per expression.
	b.WriteString("const target = toRaw(target) + target + target + target\n")
	b.WriteString("const rawTarget = toRaw(target) + target + rawTarget + rawTarget\n")
	b.WriteString("const a = b + b + b + b\nconst b = a + a + a + a\n")
	// An acyclic chain that doubles four times per level.
	b.WriteString("const c0 = \"/segment\"\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "const c%d = c%d + c%d + c%d + c%d\n", i, i-1, i-1, i-1, i-1)
	}
	b.WriteString("fetch(`${rawTarget}/x`)\nfetch(a + '/y')\nfetch(c40)\n")
	// Ordinary, resolvable code in the same file.
	b.WriteString("const API = '/v1/payments'\nconst api = API + '/'\n")
	b.WriteString("app.post('/orders', handler)\nfetch(api + id, { method: 'DELETE' })\n")
	write(t, dir, "handlers.ts", b.String())

	eps, diags := scanBounded(t, dir, 256<<20)
	keys := map[string]bool{}
	for _, e := range eps {
		keys[e.Key()] = true
		if len(e.Path) > maxEvalLen+64 {
			t.Fatalf("unbounded path (%d bytes)", len(e.Path))
		}
	}
	if !keys["POST /orders"] || !keys["DELETE /v1/payments/{}"] {
		t.Fatalf("resolvable endpoints lost: %v", keys)
	}
	found := false
	for _, d := range diags {
		found = found || strings.Contains(d.Message, "budget")
	}
	if !found {
		t.Fatalf("expected a budget diagnostic, got %v", diags)
	}
}

// TestGoEvaluationIsBounded: the Go evaluator has the same shape (both sides
// of + and every constant reference are expanded).
func TestGoEvaluationIsBounded(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/svc\n\ngo 1.22\n")
	var b strings.Builder
	b.WriteString("package svc\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nconst c0 = \"/segment\"\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "const c%d = c%d + c%d + c%d + c%d\n", i, i-1, i-1, i-1, i-1)
	}
	b.WriteString("const prefix = \"/v1\"\n\nfunc routes(mux *http.ServeMux) {\n")
	b.WriteString("\tmux.HandleFunc(c40, nil)\n")
	b.WriteString("\tmux.HandleFunc(fmt.Sprintf(\"%s/%s\", c12, c12), nil)\n")
	b.WriteString("\tmux.HandleFunc(prefix+\"/payments\", nil)\n}\n")
	write(t, dir, "routes.go", b.String())

	eps, _ := scanBounded(t, dir, 256<<20)
	keys := map[string]bool{}
	for _, e := range eps {
		keys[e.Key()] = true
		if len(e.Path) > maxEvalLen+64 {
			t.Fatalf("unbounded path (%d bytes)", len(e.Path))
		}
	}
	if !keys["ANY /v1/payments"] {
		t.Fatalf("resolvable route lost: %v", keys)
	}
}

// TestJSBindingsWithoutSemicolons: in semicolon-free code (vuejs/core's
// style) a binding used to swallow the following statements, so bindings
// referenced each other and resolved to garbage paths tens of KiB long.
func TestJSBindingsWithoutSemicolons(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "client.ts", `const API = '/v1/payments'
const api = API + '/'
const refunds = API +
  '/refunds'
const orders = '/v2'
  + '/orders'
app.post('/orders', handler)
fetch(api + id, { method: 'DELETE' })
fetch(refunds, { method: 'POST' })
fetch(orders)
`)
	eps, diags := scanBounded(t, dir, 64<<20)
	got := map[string]bool{}
	for _, e := range eps {
		got[e.Key()] = true
	}
	for _, want := range []string{"POST /orders", "DELETE /v1/payments/{}", "POST /v1/payments/refunds", "GET /v2/orders"} {
		if !got[want] {
			t.Errorf("missing %s; got %v (diags %v)", want, got, diags)
		}
	}
	if len(got) != 4 {
		t.Errorf("unexpected endpoints: %v", got)
	}
}
