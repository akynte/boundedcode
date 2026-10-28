package direct

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/retrieve"
)

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@e"}, args...)...)
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// The adaptive retrieval block travels with the message, and the message
// records what it listed with each item's provenance.
func TestAdaptiveRetrievalTravelsWithMessage(t *testing.T) {
	up := &scripted{replies: []reply{{text: "ok"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "svc/total.go", "package svc\n\nfunc Total() int {\n\treturn Sum()\n}\n")
	writeFile(t, root, "svc/other.go", "package svc\n\nfunc Other() {}\n")
	if _, err := r.Send(context.Background(), "the build fails:\n```\nsvc/total.go:4:9: undefined: Sum\n```"); err != nil {
		t.Fatal(err)
	}
	um := r.Session.Messages[0]
	if !strings.Contains(um.Context, "Likely relevant places, ranked") || !strings.Contains(um.Context, "svc/total.go:4 (Total)") {
		t.Fatalf("context:\n%s", um.Context)
	}
	rec := um.Retrieval
	if rec == nil || len(rec.Items) == 0 || rec.Items[0].File != "svc/total.go" || rec.Items[0].Blob == "" || !strings.Contains(strings.Join(rec.Items[0].Methods, ","), "build_error") {
		t.Fatalf("record: %+v", rec)
	}
	// The record survives the session file.
	dir := t.TempDir()
	s, err := NewSession(dir, root, "local", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(um); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSession(s.Path)
	if err != nil || back.Messages[0].Retrieval == nil || back.Messages[0].Retrieval.Items[0].Blob != rec.Items[0].Blob {
		t.Fatalf("reloaded: %+v %v", back, err)
	}
}

// A file read earlier and changed outside the conversation is named once
// in the next message's context; a file read and unchanged is listed by
// retrieval as already read instead of again.
func TestFreshnessNotesExternalChanges(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{name: "read", args: map[string]any{"path": "a.go"}}}},
		{text: "read it"},
		{text: "second"},
		{text: "third"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "a.go", "package a\n\nfunc Apply() {}\n")
	if _, err := r.Send(context.Background(), "look at a.go"); err != nil {
		t.Fatal(err)
	}
	if st := r.Session.Messages[2].Stamps; len(st) != 1 || st[0].Path != "a.go" || st[0].Op != "read" {
		t.Fatalf("stamps: %+v", st)
	}
	if _, err := r.Send(context.Background(), "where is `Apply` defined?"); err != nil {
		t.Fatal(err)
	}
	second := r.Session.Messages[len(r.Session.Messages)-2]
	if strings.Contains(second.Context, "changed since") || !strings.Contains(second.Context, "already read in this conversation, unchanged: a.go") {
		t.Fatalf("unchanged file:\n%s", second.Context)
	}
	writeFile(t, root, "a.go", "package a\n\nfunc Apply() { panic(1) }\n")
	if _, err := r.Send(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	third := r.Session.Messages[len(r.Session.Messages)-2]
	if !strings.Contains(third.Context, "These files changed since this conversation last read or wrote them") || !strings.Contains(third.Context, "a.go") {
		t.Fatalf("changed file not noted:\n%s", third.Context)
	}
	up.replies = append(up.replies, reply{text: "fourth"})
	if _, err := r.Send(context.Background(), "ok"); err != nil {
		t.Fatal(err)
	}
	if fourth := r.Session.Messages[len(r.Session.Messages)-2]; strings.Contains(fourth.Context, "changed since") {
		t.Fatalf("noted twice:\n%s", fourth.Context)
	}
}

// A branch switch between messages is named.
func TestFreshnessNotesCheckoutChange(t *testing.T) {
	up := &scripted{replies: []reply{{text: "one"}, {text: "two"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "a.go", "package a\n")
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")
	if _, err := r.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "-b", "feature")
	writeFile(t, root, "a.go", "package a\n\n// changed\n")
	git(t, root, "commit", "-qam", "change")
	if _, err := r.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	c := r.Session.Messages[len(r.Session.Messages)-2].Context
	if !strings.Contains(c, "The checkout changed since the previous message (main@") || !strings.Contains(c, "now feature@") {
		t.Fatalf("context:\n%s", c)
	}
}

// After a compaction, recall still brings back a summarised result (from
// the archive, in memory and after reloading the session file), noting
// when the file it shows changed since; the summary lists the ids.
func TestRecallAfterCompaction(t *testing.T) {
	up := &scripted{}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "big.go", "package big\n")
	s, err := NewSession(t.TempDir(), root, "local", "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Session = s
	long := strings.Repeat("line of big.go\n", 100)
	head := []Message{
		{Role: "user", Content: "read big.go"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{"read", `{"path":"big.go"}`}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read", Content: long, Stamps: []FileStamp{{Path: "big.go", Blob: retrieve.FileBlob(root, "big.go"), Op: "read"}}},
	}
	tail := []Message{{Role: "user", Content: "next"}}
	for _, m := range append(append([]Message{}, head...), tail...) {
		if err := s.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	idx := recallIndex(r.toolList(), head)
	if !strings.Contains(idx, "- c1: read big.go (") {
		t.Fatalf("index: %q", idx)
	}
	if err := s.replace(Message{Role: "user", Content: "[summary]" + idx}, tail); err != nil {
		t.Fatal(err)
	}
	recall := recallTool()
	for _, sess := range []func() *Session{func() *Session { return s }, func() *Session {
		back, err := LoadSession(s.Path)
		if err != nil {
			t.Fatal(err)
		}
		return back
	}} {
		r.Session = sess()
		out, err := recall.Run(context.Background(), r, map[string]any{"id": "c1"})
		if err != nil || out != long {
			t.Fatalf("recall after compaction: %v %q", err, out[:min(len(out), 60)])
		}
	}
	writeFile(t, root, "big.go", "package big\n\nvar X = 1\n")
	out, err := recall.Run(context.Background(), r, map[string]any{"id": "c1"})
	if err != nil || !strings.HasPrefix(out, "[note: big.go changed since this result was produced") {
		t.Fatalf("stale recall: %v %q", err, out[:min(len(out), 120)])
	}
}

// A second read of an unchanged region whose first result is still in the
// history counts as redundant; a read after the file changed does not.
func TestRedundantReads(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{name: "read", args: map[string]any{"path": "a.go"}}}},
		{calls: []call{{name: "read", args: map[string]any{"path": "a.go"}}}},
		{calls: []call{{name: "edit", args: map[string]any{"path": "a.go", "old_string": "Apply", "new_string": "Apply2"}}}},
		{calls: []call{{name: "read", args: map[string]any{"path": "a.go"}}}},
		{text: "done"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.NoChecks = true
	writeFile(t, root, "a.go", "package a\n\nfunc Apply() {}\n")
	if _, err := r.Send(context.Background(), "rename Apply"); err != nil {
		t.Fatal(err)
	}
	if n := r.RedundantReads(); n != 1 {
		t.Fatalf("redundant reads %d, want 1", n)
	}
	var ops []string
	for _, m := range r.Session.Messages {
		for _, s := range m.Stamps {
			ops = append(ops, m.Name+":"+s.Op)
		}
	}
	if strings.Join(ops, " ") != "read:read read:read edit:write read:read" {
		t.Fatalf("stamps %v", ops)
	}
}

// A session resumed from its file after the checkout changed notes the
// files it showed that differ now (context restoration).
func TestFreshnessAfterResume(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{name: "read", args: map[string]any{"path": "a.go"}}}},
		{text: "read it"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "a.go", "package a\n\nfunc Apply() {}\n")
	s, err := NewSession(t.TempDir(), root, "local", "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Session = s
	if _, err := r.Send(context.Background(), "look at a.go"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "a.go", "package a\n\nfunc Apply() { _ = 1 }\n")
	back, err := LoadSession(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	up2 := &scripted{replies: []reply{{text: "ok"}}}
	r2, _, _ := newRunner(t, up2, ApprovalAuto)
	r2.Opts.Root, r2.Session = root, back
	if _, err := r2.Send(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if c := back.Messages[len(back.Messages)-2].Context; !strings.Contains(c, "These files changed since this conversation last read or wrote them") || !strings.Contains(c, "a.go") {
		t.Fatalf("resumed context:\n%s", c)
	}
}
