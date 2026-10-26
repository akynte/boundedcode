package direct

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const archiveGo = `package txtar

type File struct{ Name string }

type Archive struct{ Files []File }

// Lookup returns the named file.
func (a *Archive) Lookup(name string) (*File, bool) {
	for _, f := range a.Files {
		if f.Name == name {
			return &f, true
		}
	}
	return nil, false
}
`

func TestGoPitfalls(t *testing.T) {
	ps := goPitfalls("txtar/archive.go", []byte(archiveGo))
	if len(ps) != 1 || ps[0].key != "Archive.Lookup:&f" || ps[0].line != 11 ||
		!strings.Contains(ps[0].note, "txtar/archive.go:11: Archive.Lookup returns &f") || !strings.Contains(ps[0].note, "return &a.Files[i]") {
		t.Fatalf("findings: %+v", ps)
	}
	quiet := map[string]string{
		"index":         "package p\nfunc F(xs []int) *int {\n\tfor i := range xs {\n\t\treturn &xs[i]\n\t}\n\treturn nil\n}\n",
		"local slice":   "package p\nfunc F() *int {\n\txs := []int{1}\n\tfor _, x := range xs {\n\t\treturn &x\n\t}\n\treturn nil\n}\n",
		"closure":       "package p\nfunc F(xs []int) {\n\tfor _, x := range xs {\n\t\t_ = func() *int { return &x }\n\t}\n}\n",
		"field address": "package p\ntype T struct{ N int }\nfunc F(ts []*T) *int {\n\tfor _, t := range ts {\n\t\treturn &t.N\n\t}\n\treturn nil\n}\n",
		"assignment":    "package p\nfunc F(xs []int) {\n\tfor _, x := range xs {\n\t\tx := x\n\t\t_ = &x\n\t}\n}\n",
		"no parse":      "package",
	}
	for name, src := range quiet {
		if ps := goPitfalls("p.go", []byte(src)); len(ps) != 0 {
			t.Errorf("%s: %+v", name, ps)
		}
	}
	// Nested loops report a return once.
	nested := "package p\nfunc F(xss [][]int) *[]int {\n\tfor _, xs := range xss {\n\t\tfor _, y := range xs {\n\t\t\tif y > 0 {\n\t\t\t\treturn &xs\n\t\t\t}\n\t\t}\n\t}\n\treturn nil\n}\n"
	if ps := goPitfalls("p.go", []byte(nested)); len(ps) != 1 {
		t.Fatalf("nested: %+v", ps)
	}
}

// The check notes a pitfall the change introduced, not one the file had.
func TestCheckNotesOnlyNewPitfalls(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "txtar"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(root, "txtar", "archive.go")
	if err := os.WriteFile(abs, []byte(archiveGo), 0o644); err != nil {
		t.Fatal(err)
	}
	diag := r.checkFile(context.Background(), abs, "txtar/archive.go", nil)
	if !strings.Contains(diag, "check note (compiles, but probably not what you meant):") || !strings.Contains(diag, "Archive.Lookup returns &f") {
		t.Fatalf("new file: %q", diag)
	}
	if diag := r.checkFile(context.Background(), abs, "txtar/archive.go", []byte(archiveGo)); strings.Contains(diag, "check note") {
		t.Fatalf("an existing finding was reported again: %q", diag)
	}
	// Through the edit tool: the note travels with the edit's result.
	if err := os.WriteFile(abs, []byte(strings.Replace(archiveGo, "return &f, true", "return nil, true", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := r.runTool(context.Background(), toolCallFor(t, "edit", map[string]any{"path": "txtar/archive.go", "old_string": "return nil, true", "new_string": "return &f, true"}))
	if !strings.Contains(out, "Archive.Lookup returns &f") {
		t.Fatalf("edit result: %q", out)
	}
}

func toolCallFor(t *testing.T, name string, args map[string]any) ToolCall {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var tc ToolCall
	tc.ID, tc.Type = "c1", "function"
	tc.Function.Name, tc.Function.Arguments = name, string(b)
	return tc
}
