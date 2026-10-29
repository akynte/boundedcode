package direct

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/capability"
	"github.com/akynte/boundedcode/internal/repointel"
)

func toolResults(r *Runner) []string {
	var out []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			out = append(out, m.Content)
		}
	}
	return out
}

// A whole-file write over a file that changed since the model read it is
// refused; an edit whose text still matches goes ahead and says the file
// changed.
func TestStaleFileConflicts(t *testing.T) {
	up := &scripted{}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "cfg.txt", "port=8080\nhost=a\n")
	up.replies = []reply{{calls: []call{{"read", map[string]any{"path": "cfg.txt"}}}}, {text: "read"}}
	if _, err := r.Send(context.Background(), "read cfg.txt"); err != nil {
		t.Fatal(err)
	}
	// Someone else changes the file.
	writeFile(t, root, "cfg.txt", "port=8080\nhost=b\n")
	up.replies = append(up.replies,
		reply{calls: []call{{"write", map[string]any{"path": "cfg.txt", "content": "port=9090\nhost=a\n"}}}},
		reply{calls: []call{{"edit", map[string]any{"path": "cfg.txt", "old_string": "port=8080", "new_string": "port=9090"}}}},
		reply{text: "done"})
	if _, err := r.Send(context.Background(), "use port 9090"); err != nil {
		t.Fatal(err)
	}
	res := toolResults(r)
	if !strings.Contains(res[1], "cfg.txt changed since you last read it") {
		t.Fatalf("write: %q", res[1])
	}
	if !strings.Contains(res[2], "edited cfg.txt") || !strings.Contains(res[2], "note: cfg.txt changed since you last read it") {
		t.Fatalf("edit: %q", res[2])
	}
	b, _ := os.ReadFile(filepath.Join(root, "cfg.txt"))
	if string(b) != "port=9090\nhost=b\n" {
		t.Fatalf("file %q: the other change was lost", b)
	}
	var conflict bool
	for _, e := range *evs {
		conflict = conflict || e.Kind == EventToolEnd && e.Tool == "write" && e.Code == capability.CodeConflict
	}
	if !conflict {
		t.Fatal("no conflict code on the write's event")
	}
	// After the edit the model has seen its own change: a write goes on.
	up.replies = append(up.replies, reply{calls: []call{{"write", map[string]any{"path": "cfg.txt", "content": "port=9090\nhost=c\n"}}}}, reply{text: "done"})
	if _, err := r.Send(context.Background(), "host c"); err != nil {
		t.Fatal(err)
	}
	if res := toolResults(r); !strings.HasPrefix(res[len(res)-1], "overwrote cfg.txt") {
		t.Fatalf("write after edit: %q", res[len(res)-1])
	}
}

// multi_edit changes several files together; when one edit fails, none
// is written; a deny rule on any of its files refuses the call.
func TestMultiFileEdit(t *testing.T) {
	up := &scripted{}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "a.go", "package p\n\nconst A = 1\n")
	writeFile(t, root, "b.go", "package p\n\nconst B = 2\n")
	edits := func(bOld string) []any {
		return []any{
			map[string]any{"path": "a.go", "old_string": "A = 1", "new_string": "A = 10"},
			map[string]any{"path": "b.go", "old_string": bOld, "new_string": "B = 20"},
		}
	}
	up.replies = []reply{
		{calls: []call{{"multi_edit", map[string]any{"edits": edits("B = 3")}}}},
		{calls: []call{{"multi_edit", map[string]any{"edits": edits("B = 2")}}}},
		{text: "done"},
	}
	if _, err := r.Send(context.Background(), "change both"); err != nil {
		t.Fatal(err)
	}
	res := toolResults(r)
	if !strings.Contains(res[0], "edit 2: old_string not found") || !strings.Contains(res[0], "b.go (nothing was written)") {
		t.Fatalf("failed edit: %q", res[0])
	}
	if !strings.HasPrefix(res[1], "applied 2 edit(s) to a.go, b.go") {
		t.Fatalf("edit: %q", res[1])
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.go"))
	b, _ := os.ReadFile(filepath.Join(root, "b.go"))
	if !strings.Contains(string(a), "A = 10") || !strings.Contains(string(b), "B = 20") {
		t.Fatalf("a=%q b=%q", a, b)
	}
	// Both files are stamped, so a later write of either is checked.
	if r.seenBlob("a.go") == "" || r.seenBlob("b.go") == "" {
		t.Fatal("not stamped")
	}

	r.Opts.Permissions = Rules{Deny: []string{"multi_edit(b.go)"}}
	up.replies = append(up.replies, reply{calls: []call{{"multi_edit", map[string]any{"edits": []any{
		map[string]any{"path": "a.go", "old_string": "A = 10", "new_string": "A = 11"},
		map[string]any{"path": "b.go", "old_string": "B = 20", "new_string": "B = 21"},
	}}}}}, reply{text: "done"})
	if _, err := r.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	a, _ = os.ReadFile(filepath.Join(root, "a.go"))
	if strings.Contains(string(a), "A = 11") {
		t.Fatal("a deny rule on one file did not stop the call")
	}
}

// A session started in a subdirectory stamps files relative to the root,
// so their changes are detected too.
func TestStampsFromSubdirectory(t *testing.T) {
	up := &scripted{}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "svc/main.go", "package main\n")
	r.Opts.Dir = filepath.Join(root, "svc")
	up.replies = []reply{{calls: []call{{"read", map[string]any{"path": "main.go"}}}}, {text: "ok"}}
	if _, err := r.Send(context.Background(), "read main.go"); err != nil {
		t.Fatal(err)
	}
	if r.seenBlob("svc/main.go") == "" {
		t.Fatalf("stamps %+v", r.Session.Messages[2].Stamps)
	}
}

type fakeNav struct{ syms []repointel.Symbol }

func (fakeNav) Name() string { return "fake" }
func (n fakeNav) FindSymbol(_ context.Context, _, name string, opt repointel.FindOptions) ([]repointel.Symbol, error) {
	var out []repointel.Symbol
	for _, s := range n.syms {
		if (s.NamePath == name || strings.HasSuffix(s.NamePath, "/"+name)) && (opt.Within == "" || s.File == opt.Within) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (fakeNav) References(context.Context, string, repointel.Symbol) ([]repointel.Reference, error) {
	return nil, nil
}
func (fakeNav) Implementations(context.Context, string, repointel.Symbol) ([]repointel.Symbol, error) {
	return nil, nil
}

// replace_symbol replaces exactly the range the language server gives,
// and is offered only when there is one.
func TestReplaceSymbol(t *testing.T) {
	up := &scripted{}
	if r, _, _ := newRunner(t, up, ApprovalAuto); r.findTool("replace_symbol") != nil {
		t.Fatal("offered without a language server")
	}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	src := "package p\n\n// Add adds.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n\n// Sub subtracts.\nfunc Sub(a, b int) int { return a - b }\n"
	writeFile(t, root, "m.go", src)
	r.Nav = fakeNav{syms: []repointel.Symbol{{NamePath: "Add", File: "m.go", StartLine: 4, EndLine: 6}, {NamePath: "Sub", File: "m.go", StartLine: 9, EndLine: 9}}}
	r.Opts.NoChecks = true
	up.replies = []reply{
		{calls: []call{{"replace_symbol", map[string]any{"path": "m.go", "name_path": "Add", "new_source": "func Add(a, b int) int {\n\treturn a + b\n}"}}}},
		{calls: []call{{"replace_symbol", map[string]any{"path": "m.go", "name_path": "Mul", "new_source": "func Mul() {}"}}}},
		{text: "done"},
	}
	if _, err := r.Send(context.Background(), "fix Add"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "m.go"))
	want := strings.Replace(src, "return a - b\n}", "return a + b\n}", 1)
	if string(b) != want {
		t.Fatalf("file:\n%s", b)
	}
	res := toolResults(r)
	if !strings.HasPrefix(res[0], "replaced Add in m.go (lines 4-6, now 4-6)") || !strings.Contains(res[1], "finds no symbol Mul") {
		t.Fatalf("results %q", res)
	}
}
