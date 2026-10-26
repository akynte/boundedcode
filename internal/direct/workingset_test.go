package direct

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The working set: older tool results are set aside before the history
// outgrows the budget, stay in the session, and come back through recall.
func TestWorkingSetElidesAndRecalls(t *testing.T) {
	big := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 70) // ~3 KB
	up := &scripted{replies: []reply{
		{calls: []call{{name: "read", args: map[string]any{"path": "big.txt"}}}},
		{text: "one"},
		{calls: []call{{name: "read", args: map[string]any{"path": "big.txt"}}}},
		{text: "two"},
		{calls: []call{{name: "read", args: map[string]any{"path": "big.txt"}}}},
		{text: "three"},
		{calls: []call{{name: "recall", args: map[string]any{"id": "c0"}}}},
		{text: "four"},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := NewSession(t.TempDir(), root, "local", "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Session = sess
	r.Opts.ContextTokens, r.Opts.WorkingSetTokens = 16000, 5000
	for _, m := range []string{"first", "second", "third"} {
		if _, err := r.Send(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	// The first two reads fit; the third pushed the history over the
	// budget, so the oldest result (seen by the model, not among the two
	// newest) was set aside before the model was called again.
	if r.Session.Messages[2].Elided == "" || r.Session.Messages[6].Elided != "" {
		t.Fatalf("after three reads: %v", roles(r.Session.Messages))
	}
	if _, err := r.Send(context.Background(), "fourth"); err != nil {
		t.Fatal(err)
	}
	if r.Session.Compactions != 0 {
		t.Fatal("setting results aside must come before summarising")
	}
	first := r.Session.Messages[2]
	if first.Role != "tool" || !strings.Contains(first.Elided, "lazy dog") || !strings.Contains(first.Content, `call recall with id "c0"`) {
		t.Fatalf("oldest tool result not set aside: %+v", first)
	}
	// The recall result pushed the history over again: the second read is
	// set aside too, and the two newest results stay verbatim.
	if r.Session.Messages[6].Elided == "" {
		t.Fatalf("second read: %v", roles(r.Session.Messages))
	}
	for _, i := range []int{10, 14} {
		if recent := r.Session.Messages[i]; recent.Role != "tool" || recent.Elided != "" {
			t.Fatalf("the newest results must stay verbatim: %v", roles(r.Session.Messages))
		}
	}
	// The model saw the placeholder, and recall gave it the text back.
	ms := up.lastMessages(t)
	if c := ms[3]["content"].(string); !strings.Contains(c, "set aside to save context") {
		t.Fatalf("wire message: %q", c)
	}
	if res := r.Session.Messages[len(r.Session.Messages)-2]; res.Name != "recall" || !strings.Contains(res.Content, "lazy dog") {
		t.Fatalf("recall result: %+v", res)
	}
	noted := false
	for _, e := range *evs {
		if e.Kind == EventCompact && strings.Contains(e.Text, "Set aside 1 older tool result") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("no note about the elision")
	}
	// Reloading the session reproduces the elision.
	got, err := LoadSession(sess.Path)
	if err != nil {
		t.Fatal(err)
	}
	if m := got.Messages[2]; !strings.Contains(m.Elided, "lazy dog") || !strings.Contains(m.Content, "set aside") {
		t.Fatalf("reloaded: %+v", m)
	}
	if md := got.Markdown(); !strings.Contains(md, "lazy dog") {
		t.Fatal("the export must show the full result")
	}
}

func TestWorkingSetDefaults(t *testing.T) {
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	r.Opts.ContextTokens = 131072
	if got := r.workingSetTokens(); got != defaultWorkingSetLocal {
		t.Fatalf("local budget %d", got)
	}
	if got := r.maxToolOutput(); got != defaultToolOutputLocal {
		t.Fatalf("local cap %d", got)
	}
	r.Opts.WorkingSetTokens = 200000
	if got := r.workingSetTokens(); got != 131072*3/4 {
		t.Fatalf("budget above the window: %d", got)
	}
	r.Opts.ContextTokens, r.Opts.WorkingSetTokens = 8192, 0
	if got := r.workingSetTokens(); got != 6144 {
		t.Fatalf("small window budget %d", got)
	}
	if got := r.clip(strings.Repeat("x", 20000)); len(got) > defaultToolOutputLocal+64 || !strings.Contains(got, "bytes cut") {
		t.Fatalf("clip: %d bytes", len(got))
	}
}

// A long file reads as an outline unless a range is asked for.
func TestReadOutlineForLongFiles(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	var b strings.Builder
	b.WriteString("package p\n\nimport \"fmt\"\n\ntype Thing struct{ N int }\n\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "// F%d does a thing.\nfunc F%d(x int) int {\n\tfmt.Println(x)\n\tfor j := 0; j < x; j++ {\n\t\tx++\n\t}\n\treturn x\n}\n\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(root, "long.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	read := readTool()
	out, err := read.Run(context.Background(), r, map[string]any{"path": "long.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "this is its outline") || !strings.Contains(out, "func F59(x int) int {") || !strings.Contains(out, "type Thing struct") || strings.Contains(out, "fmt.Println") {
		t.Fatalf("outline:\n%s", out)
	}
	out, err = read.Run(context.Background(), r, map[string]any{"path": "long.go", "offset": 8, "limit": 3})
	if err != nil || !strings.Contains(out, "     8\tfunc F0(x int) int {") || strings.Contains(out, "outline") {
		t.Fatalf("ranged read: %v\n%s", err, out)
	}
	// Prose: headings. Flat text: no outline, the file itself.
	md := strings.Repeat("# Title\n\ntext\n\n## Section\n\nmore text\n\n", 60)
	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ = read.Run(context.Background(), r, map[string]any{"path": "doc.md"}); !strings.Contains(out, "outline") || !strings.Contains(out, "## Section") || strings.Contains(out, "more text") {
		t.Fatalf("prose outline:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(root, "flat.txt"), []byte(strings.Repeat("line\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ = read.Run(context.Background(), r, map[string]any{"path": "flat.txt"}); strings.Contains(out, "outline") || !strings.Contains(out, "   500\tline") {
		t.Fatalf("flat file:\n%s", out)
	}
}

// Too many grep matches collapse to a count per file instead of a
// truncated dump.
func TestGrepCollapsesWhenTooManyMatches(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	for i := 0; i < 30; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d.txt", i)), []byte(strings.Repeat("a needle here\n", 10)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rg := range []bool{true, false} {
		var out string
		var err error
		if rg {
			if _, lerr := exec.LookPath("rg"); lerr != nil {
				continue
			}
			out, err = grepTool().Run(context.Background(), r, map[string]any{"pattern": "needle", "path": "."})
		} else {
			out, err = r.goGrep(context.Background(), root, "needle", "", false, "content", 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "300 matches in 30 files") || !strings.Contains(out, "f00.txt: 10") || strings.Contains(out, "a needle here") {
			t.Fatalf("rg=%v:\n%s", rg, out)
		}
	}
	out, err := r.goGrep(context.Background(), root, "needle", "f0*", false, "content", 0)
	if err != nil || strings.Count(out, "\n") != 99 || !strings.HasPrefix(out, "f00.txt:1:a needle here") {
		t.Fatalf("under the limit:\n%s", out)
	}
}

// Every file change is followed by the cheapest check for its language,
// and the model is told the result.
func TestChangesAreCheckedAfterWriting(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{name: "write", args: map[string]any{"path": "x.go", "content": "package p\n\nfunc f() {\n\treturn 1\n"}}}},
		{calls: []call{{name: "edit", args: map[string]any{"path": "x.go", "old_string": "\treturn 1\n", "new_string": "}\n"}}}},
		{calls: []call{{name: "write", args: map[string]any{"path": "c.json", "content": "{\"a\": }"}}}},
		{text: "done"},
	}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	if _, err := r.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	if len(results) != 3 {
		t.Fatalf("%d tool results", len(results))
	}
	if !strings.Contains(results[0], "check: Go syntax error") || !strings.Contains(results[0], "Fix this before going on") {
		t.Fatalf("broken write: %q", results[0])
	}
	if !strings.Contains(results[1], "check: ok (Go parse)") {
		t.Fatalf("fixed edit: %q", results[1])
	}
	if !strings.Contains(results[2], "not valid JSON") {
		t.Fatalf("broken json: %q", results[2])
	}
}

func TestGoBuildCheckInAModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go")
	}
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := writeTool()
	if _, err := w.Run(context.Background(), r, map[string]any{"path": "lib/a.go", "content": "package lib\n\nfunc A() int { return undefinedName }\n"}); err != nil {
		t.Fatal(err)
	}
	if diag := r.checkFile(context.Background(), filepath.Join(root, "lib/a.go"), "lib/a.go"); !strings.Contains(diag, "go build ./lib failed") || !strings.Contains(diag, "undefinedName") {
		t.Fatalf("type error: %q", diag)
	}
	if err := os.WriteFile(filepath.Join(root, "lib/a.go"), []byte("package lib\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if diag := r.checkFile(context.Background(), filepath.Join(root, "lib/a.go"), "lib/a.go"); diag != "check: ok (go build ./lib)" {
		t.Fatalf("clean: %q", diag)
	}
	r.Opts.NoChecks = true
	// NoChecks is honoured by runTool, not by checkFile itself.
	if _, err := exec.LookPath("python3"); err == nil {
		if err := os.WriteFile(filepath.Join(root, "s.py"), []byte("def f(:\n  pass\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if diag := r.checkFile(context.Background(), filepath.Join(root, "s.py"), "s.py"); !strings.Contains(diag, "check: python3 reports") || !strings.Contains(diag, "SyntaxError") {
			t.Fatalf("python: %q", diag)
		}
	}
}

func roles(ms []Message) []string {
	var out []string
	for i, m := range ms {
		out = append(out, fmt.Sprintf("%d:%s/%d/%d", i, m.Role, len(m.Content), len(m.Elided)))
	}
	return out
}

// The names a request mentions are looked up before the first model call
// and the places found travel with the message.
func TestSeedPackFindsTheNamedCode(t *testing.T) {
	up := &scripted{replies: []reply{{text: "ok"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "internal/ledger"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package ledger\n\n// Post records entries.\nfunc (l *Ledger) Post(entries ...Entry) error {\n\treturn nil\n}\n\nfunc (l *Ledger) Balance(id string) int64 { return 0 }\n"
	if err := os.WriteFile(filepath.Join(root, "internal/ledger/ledger.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/ledger/ledger_test.go"), []byte("package ledger\n\nfunc TestPost(t *testing.T) { _ = (&Ledger{}).Post() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Send(context.Background(), "Why does `Post` return nil, and where is internal/ledger/ledger.go used by Balance()?"); err != nil {
		t.Fatal(err)
	}
	um := up.lastMessages(t)[1]["content"].(string)
	for _, want := range []string{"Likely relevant places", "- Post: internal/ledger/ledger.go:4 (func (l *Ledger) Post(entries ...Entry) error {)", "internal/ledger/ledger_test.go:3", "- internal/ledger/ledger.go: ", "- Balance: internal/ledger/ledger.go:8 (func (l *Ledger) Balance"} {
		if !strings.Contains(um, want) {
			t.Fatalf("missing %q in:\n%s", want, um)
		}
	}
	if !strings.Contains(r.Session.Messages[0].Context, "Likely relevant places") {
		t.Fatal("the places must be stored with the message (stable prefix)")
	}
	// A name nobody uses, or that is everywhere, adds nothing.
	r.Opts.NoSeedPack = false
	if sp := r.seedPack(context.Background(), "What about `Nonexistent`?"); sp != "" {
		t.Fatalf("unknown name: %q", sp)
	}
	r.Opts.NoSeedPack = true
	if sp := r.seedPack(context.Background(), "Where is `Post`?"); sp != "" {
		t.Fatalf("disabled: %q", sp)
	}
	// A mode switch keeps the places and swaps only the framing.
	r.Opts.NoSeedPack = false
	r.Opts.Mode = ModeChat
	r.Session.Messages = nil
	up.replies = []reply{{text: "ok"}}
	if _, err := r.Send(context.Background(), "Where is `Post` defined?"); err != nil {
		t.Fatal(err)
	}
	r.Opts.Mode = ModeCode
	r.reframeTurn()
	c := r.Session.Messages[0].Context
	framing, places, _ := strings.Cut(c, "\n\nLikely relevant places")
	if !strings.Contains(framing, "classified as a request for a direct change") || strings.Contains(framing, "question or discussion") || !strings.Contains(places, "- Post: internal/ledger/ledger.go:4") {
		t.Fatalf("reframed context:\n%s", c)
	}
}

// The answer streams as it is generated: text deltas precede the whole
// text, and they add up to it. Sub-agents report whole.
func TestAnswerStreamsAsDeltas(t *testing.T) {
	up := &scripted{replies: []reply{{text: "Hello there, world"}}}
	r, _, evs := newRunner(t, up, ApprovalAuto)
	if _, err := r.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	var streamed string
	seenText := false
	for _, e := range *evs {
		switch e.Kind {
		case EventTextDelta:
			if seenText {
				t.Fatal("a delta after the whole text")
			}
			streamed += e.Text
		case EventText:
			seenText = true
			if e.Text != "Hello there, world" {
				t.Fatalf("text: %q", e.Text)
			}
		}
	}
	if streamed != "Hello there, world" {
		t.Fatalf("deltas add up to %q", streamed)
	}
	r.Opts.NoStream = true
	up.replies = []reply{{text: "again"}}
	*evs = nil
	if _, err := r.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	for _, e := range *evs {
		if e.Kind == EventTextDelta {
			t.Fatal("deltas while streaming is off")
		}
	}
}

// The first message of a session carries a map of the project.
func TestProjectMapOnFirstMessage(t *testing.T) {
	up := &scripted{replies: []reply{{text: "ok"}, {text: "ok"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	for f, content := range map[string]string{
		"go.mod":                     "module example.com/shop\n\ngo 1.22\n",
		"README.md":                  "# shop\n",
		"Makefile":                   "build:\n\tgo build ./...\n.PHONY: test\ntest:\n\tgo test ./...\n",
		"internal/cart/cart.go":      "package cart\n",
		"internal/cart/cart_test.go": "package cart\n",
		"cmd/shop/main.go":           "package main\n",
		"web/app.ts":                 "export {}\n",
		"node_modules/x/index.js":    "",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	um := up.lastMessages(t)[1]["content"].(string)
	for _, want := range []string{"Project map", "- Go module example.com/shop (build: go build ./..., test: go test ./...)", "- Makefile targets: build, test", "- internal/: 2 source files (Go 2)", "- cmd/: 1 source files (Go 1)", "- web/: 1 source files (TypeScript 1)", "- top-level files: Makefile, README.md, go.mod"} {
		if !strings.Contains(um, want) {
			t.Fatalf("missing %q in:\n%s", want, um)
		}
	}
	if strings.Contains(um, "node_modules") {
		t.Fatal("dependency directories are not mapped")
	}
	if _, err := r.Send(context.Background(), "and again"); err != nil {
		t.Fatal(err)
	}
	if um := up.lastMessages(t)[3]["content"].(string); strings.Contains(um, "Project map") {
		t.Fatal("the map is sent once per session")
	}
}
