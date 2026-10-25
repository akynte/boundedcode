package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/inference"
)

// scripted is an Upstream that answers from a queue; each reply is either
// text or tool calls. It records the requests it saw.
type scripted struct {
	mu       sync.Mutex
	replies  []reply
	requests []map[string]any
	status   int
	errBody  string
}

type reply struct {
	text  string
	calls []call
	// summary marks a reply to a compaction request.
}

type call struct {
	name string
	args map[string]any
}

func (s *scripted) Provider() string { return "local" }

func (s *scripted) Complete(_ context.Context, body map[string]any) (int, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, body)
	if s.status != 0 {
		st := s.status
		s.status = 0
		return st, []byte(`{"error":{"message":"` + s.errBody + `"}}`), nil
	}
	if len(s.replies) == 0 {
		return http.StatusOK, []byte(`{"choices":[{"message":{"role":"assistant","content":"(out of script)"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`), nil
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	msg := map[string]any{"role": "assistant", "content": r.text}
	if len(r.calls) > 0 {
		var tcs []map[string]any
		for i, c := range r.calls {
			args, _ := json.Marshal(c.args)
			tcs = append(tcs, map[string]any{"id": fmt.Sprintf("c%d", i), "type": "function", "function": map[string]any{"name": c.name, "arguments": string(args)}})
		}
		msg["tool_calls"] = tcs
	}
	out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20}})
	return http.StatusOK, out, nil
}

func (s *scripted) lastMessages(t *testing.T) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("no requests")
	}
	ms, _ := s.requests[len(s.requests)-1]["messages"].([]map[string]any)
	return ms
}

func newRunner(t *testing.T, up *scripted, approval string) (*Runner, string, *[]Event) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	var events []Event
	var mu sync.Mutex
	r := &Runner{
		Gateway: &inference.Gateway{Upstream: up, Model: "test"},
		Opts:    Options{Root: root, Approval: approval, Env: map[string]string{"platform": "test"}, CommandTimeout: 5 * time.Second},
		Sink: func(e Event) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
		Session: &Session{},
	}
	return r, root, &events
}

func kinds(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, string(e.Kind))
	}
	return out
}

func TestLoopReadsThenAnswers(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"read", map[string]any{"path": "main.go"}}}},
		{text: "main prints hello"},
	}}
	r, root, evs := newRunner(t, up, ApprovalEdits)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { println(\"hello\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.Send(context.Background(), "what does main do?")
	if err != nil {
		t.Fatal(err)
	}
	if out != "main prints hello" {
		t.Fatalf("final = %q", out)
	}
	ms := up.lastMessages(t)
	// system, user, assistant(tool call), tool, and the final request has those four.
	if len(ms) != 4 || ms[0]["role"] != "system" || ms[3]["role"] != "tool" {
		t.Fatalf("wire messages: %+v", ms)
	}
	if !strings.Contains(ms[3]["content"].(string), "     3\tfunc main()") {
		t.Fatalf("tool result: %q", ms[3]["content"])
	}
	if !strings.Contains(ms[0]["content"].(string), "platform: test") {
		t.Fatalf("system prompt lacks environment: %q", ms[0]["content"])
	}
	got := strings.Join(kinds(*evs), ",")
	if got != "tool_start,tool_end,text,done" {
		t.Fatalf("events: %s", got)
	}
	if len(r.Session.Messages) != 4 {
		t.Fatalf("session has %d messages", len(r.Session.Messages))
	}
}

func TestProjectInstructionsAndUserFile(t *testing.T) {
	up := &scripted{replies: []reply{{text: "ok"}}}
	r, root, _ := newRunner(t, up, ApprovalEdits)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Always run go vet."), 0o644); err != nil {
		t.Fatal(err)
	}
	uf := filepath.Join(t.TempDir(), "me.md")
	if err := os.WriteFile(uf, []byte("Prefer British spelling."), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Opts.UserInstructions = uf
	if _, err := r.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	sys := up.lastMessages(t)[0]["content"].(string)
	for _, want := range []string{"Always run go vet.", "Prefer British spelling.", "AGENTS.md"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}

func TestEditToolUniqueness(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalEdits)
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("x = 1\ny = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := r.findTool("edit")
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "a.txt", "old_string": "= 1", "new_string": "= 2"}); err == nil || !strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("ambiguous edit: %v", err)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "a.txt", "old_string": "= 1", "new_string": "= 2", "replace_all": true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "x = 2\ny = 2\n" {
		t.Fatalf("content %q", b)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "a.txt", "old_string": "nope", "new_string": ""}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}
}

func TestPathsAreConfined(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	read := r.findTool("read")
	for _, p := range []string{"../x", filepath.Join(outside, "secret.txt"), "/etc/passwd"} {
		if _, err := read.Run(context.Background(), r, map[string]any{"path": p}); err == nil || !strings.Contains(err.Error(), "outside the project") {
			t.Errorf("%s: %v", p, err)
		}
	}
	if runtime.GOOS != "windows" {
		_ = os.Symlink(outside, filepath.Join(root, "link"))
		if _, err := read.Run(context.Background(), r, map[string]any{"path": "link/secret.txt"}); err == nil || !strings.Contains(err.Error(), "outside the project") {
			t.Errorf("symlink escape: %v", err)
		}
		if _, err := r.findTool("write").Run(context.Background(), r, map[string]any{"path": "link/new.txt", "content": "x"}); err == nil {
			t.Error("write through symlink escape succeeded")
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("KEY=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := read.Run(context.Background(), r, map[string]any{"path": ".env"}); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf(".env: %v", err)
	}
	if _, err := read.Run(context.Background(), r, map[string]any{"path": ".git/config"}); err == nil {
		t.Error(".git readable")
	}
}

func TestBashPolicyAndApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	up := &scripted{replies: []reply{
		{calls: []call{{"bash", map[string]any{"command": "git push origin main"}}}},
		{calls: []call{{"bash", map[string]any{"command": "rm -rf build"}}}},
		{calls: []call{{"bash", map[string]any{"command": "echo hello"}}}},
		{text: "done"},
	}}
	r, _, evs := newRunner(t, up, ApprovalEdits)
	var asked []ApprovalRequest
	r.Approve = func(_ context.Context, req ApprovalRequest) bool { asked = append(asked, req); return false }
	if _, err := r.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	// 1: policy denies the push before any approval; 2: rm asks and is denied; 3: echo is safe and runs.
	if len(asked) != 1 || !strings.Contains(asked[0].Detail, "rm -rf build") {
		t.Fatalf("asked: %+v", asked)
	}
	var toolResults []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			toolResults = append(toolResults, m.Content)
		}
	}
	if len(toolResults) != 3 || !strings.Contains(toolResults[0], "policy: command denied") || !strings.Contains(toolResults[1], "did not approve") || !strings.Contains(toolResults[2], "hello") {
		t.Fatalf("tool results: %q", toolResults)
	}
	got := strings.Join(kinds(*evs), ",")
	if got != "denied,denied,tool_start,tool_end,text,done" {
		t.Fatalf("events: %s", got)
	}
}

func TestBashTimeoutAndExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	r.Opts.CommandTimeout = 300 * time.Millisecond
	bash := r.findTool("bash")
	start := time.Now()
	if _, err := bash.Run(context.Background(), r, map[string]any{"command": "sleep 5"}); err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not kill the command")
	}
	out, err := bash.Run(context.Background(), r, map[string]any{"command": "echo oops >&2; exit 3"})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(out, "oops") {
		t.Fatalf("exit: %q %v", out, err)
	}
}

func TestSafeCommands(t *testing.T) {
	cases := map[string]bool{
		"go test ./...":              true,
		"git status && go vet ./...": true,
		"npm test":                   true,
		"git push":                   false,
		"rm -rf x":                   false,
		"go test ./... > out.txt":    false,
		"cat $(find . -name x)":      false,
		"pytest -q tests/":           true,
		"ls | grep foo":              true,
		"curl http://x":              false,
		"git commit -m x":            false,
		"make test":                  true,
		"make install":               false,
		"":                           false,
		"go run ./cmd/tool":          true,
		"python3 -c 'print(1)'":      true,
		"echo hi; rm -rf /tmp/x":     false,
	}
	for cmd, want := range cases {
		if got := isSafeCommand(cmd); got != want {
			t.Errorf("isSafeCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestMutationConsentModes(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalEdits)
	if err := os.MkdirAll(filepath.Join(root, ".github/workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := r.findTool("write")
	if _, need := w.Consent(r, map[string]any{"path": "x.go", "content": "x"}); need {
		t.Error("edits mode asked for a plain file")
	}
	if _, need := w.Consent(r, map[string]any{"path": ".github/workflows/ci.yml", "content": "x"}); !need {
		t.Error("edits mode did not ask for a protected path")
	}
	r.Opts.Approval = ApprovalAsk
	if _, need := w.Consent(r, map[string]any{"path": "x.go", "content": "x"}); !need {
		t.Error("ask mode did not ask")
	}
	r.Opts.Approval = ApprovalAuto
	if _, need := w.Consent(r, map[string]any{"path": ".github/workflows/ci.yml", "content": "x"}); need {
		t.Error("auto mode asked")
	}
}

func TestGrepGlobLs(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "src/pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules/dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/pkg/a.go"), []byte("package pkg\nfunc Needle() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/b.ts"), []byte("const needle = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules/dep/c.go"), []byte("Needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out, err := r.goGrep(ctx, root, "Needle", "", false, "content", 0)
	if err != nil || out != "src/pkg/a.go:2:func Needle() {}" {
		t.Fatalf("goGrep: %q %v", out, err)
	}
	out, _ = r.goGrep(ctx, root, "needle", "*.ts", true, "content", 0)
	if out != "src/b.ts:1:const needle = 1" {
		t.Fatalf("goGrep glob+icase: %q", out)
	}
	out, err = r.findTool("grep").Run(ctx, r, map[string]any{"pattern": "Needle", "glob": "*.go"})
	if err != nil || !strings.Contains(out, "src/pkg/a.go:2:") || strings.Contains(out, "node_modules") {
		t.Fatalf("grep tool: %q %v", out, err)
	}
	out, _ = r.findTool("glob").Run(ctx, r, map[string]any{"pattern": "**/*.go"})
	if out != "src/pkg/a.go" {
		t.Fatalf("glob: %q", out)
	}
	out, _ = r.findTool("glob").Run(ctx, r, map[string]any{"pattern": "src/**/*.go"})
	if out != "src/pkg/a.go" {
		t.Fatalf("glob prefix: %q", out)
	}
	out, _ = r.findTool("ls").Run(ctx, r, map[string]any{"path": "src"})
	if out != "b.ts\npkg/" {
		t.Fatalf("ls: %q", out)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		glob, rel string
		want      bool
	}{
		{"*.go", "a/b/c.go", true},
		{"**/*.go", "c.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"src/**/*.ts", "src/x/y.ts", true},
		{"src/**/*.ts", "lib/x/y.ts", false},
		{"src/*.ts", "src/y.ts", true},
		{"src/*.ts", "src/x/y.ts", false},
		{"Makefile", "Makefile", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.glob, c.rel); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v", c.glob, c.rel, got)
		}
	}
}

func TestSessionPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSession(dir, "/some/project", "local", "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Title = "t"
	_ = s.Append(Message{Role: "user", Content: "one"})
	tc := ToolCall{ID: "c1", Type: "function"}
	tc.Function.Name = "read"
	tc.Function.Arguments = `{"path":"x"}`
	_ = s.Append(Message{Role: "assistant", ToolCalls: []ToolCall{tc}})
	_ = s.Append(Message{Role: "tool", ToolCallID: "c1", Content: "r"})
	_ = s.Append(Message{Role: "assistant", Content: "a"})
	got, err := LoadSession(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID || len(got.Messages) != 4 || got.Messages[1].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("reloaded: %+v", got)
	}
	// Compaction keeps order: summary first, then the tail.
	_ = s.replace(Message{Role: "user", Content: "SUMMARY"}, s.Messages[3:])
	got, err = LoadSession(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "SUMMARY" || got.Messages[1].Content != "a" || got.Compactions != 1 {
		t.Fatalf("after compaction: %+v", got.Messages)
	}
	list, err := ListSessions(dir, "/some/project")
	if err != nil || len(list) != 1 || list[0].ID != s.ID {
		t.Fatalf("list: %+v %v", list, err)
	}
	if p, err := FindSession(dir, "/some/project", s.ID[:6]); err != nil || p != s.Path {
		t.Fatalf("find: %q %v", p, err)
	}
	if _, err := ListSessions(dir, "/other"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactionWhenContextIsSmall(t *testing.T) {
	up := &scripted{replies: []reply{
		{text: "SUMMARY TEXT"}, // the compaction request comes first
		{text: "answer"},
		{text: "final"},
	}}
	r, _, evs := newRunner(t, up, ApprovalAuto)
	r.Opts.ContextTokens = 4000
	for i := 0; i < 12; i++ {
		_ = r.Session.Append(Message{Role: "user", Content: "q" + fmt.Sprint(i)})
		_ = r.Session.Append(Message{Role: "assistant", Content: strings.Repeat("blah ", 60)})
	}
	if _, err := r.Send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Send(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range *evs {
		if e.Kind == EventCompact {
			found = true
		}
	}
	if !found {
		t.Fatalf("no compaction event: %v", kinds(*evs))
	}
	if r.Session.Messages[0].Role != "user" || !strings.Contains(r.Session.Messages[0].Content, "SUMMARY TEXT") {
		t.Fatalf("history head: %+v", r.Session.Messages[0])
	}
	// The compaction request itself did not carry tools, and the call after
	// it saw the shortened history.
	up.mu.Lock()
	defer up.mu.Unlock()
	if _, has := up.requests[0]["tools"]; has {
		t.Fatal("compaction request carried tools")
	}
	if ms := up.requests[1]["messages"].([]map[string]any); len(ms) > 10 {
		t.Fatalf("history not shortened: %d messages", len(ms))
	}
}

func TestContextOverflowErrorCompactsAndRetries(t *testing.T) {
	up := &scripted{replies: []reply{
		{text: "SUMMARY"},
		{text: "recovered"},
	}, status: http.StatusBadRequest, errBody: "This model's maximum context length is 8192 tokens"}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	for i := 0; i < 8; i++ {
		_ = r.Session.Append(Message{Role: "user", Content: "q"})
		_ = r.Session.Append(Message{Role: "assistant", Content: "a"})
	}
	out, err := r.Send(context.Background(), "x")
	if err != nil || out != "recovered" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestInterruptKeepsHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	up := &scripted{replies: []reply{
		{calls: []call{{"bash", map[string]any{"command": "sleep 10"}}}},
		{text: "never"},
	}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err := r.Send(ctx, "go")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v", err)
	}
	if n := len(r.Session.Messages); n != 3 { // user, assistant call, tool result
		t.Fatalf("messages kept: %d", n)
	}
}

func TestParseCompletionShapes(t *testing.T) {
	m, err := parseCompletion([]byte(`{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"reasoning_content":"thinking","tool_calls":[{"function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Content != "ab" || m.Reasoning != "thinking" || len(m.ToolCalls) != 1 || m.ToolCalls[0].ID == "" || m.ToolCalls[0].Type != "function" {
		t.Fatalf("%+v", m)
	}
	if _, err := parseCompletion([]byte(`{"choices":[]}`)); err == nil {
		t.Fatal("no choices accepted")
	}
}

func TestUnknownToolAndBadArguments(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"teleport", nil}}},
		{text: "ok"},
	}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	if _, err := r.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Session.Messages[2].Content, "unknown tool") {
		t.Fatalf("%q", r.Session.Messages[2].Content)
	}
	tc := ToolCall{ID: "z"}
	tc.Function.Name = "read"
	tc.Function.Arguments = "{not json"
	if out := r.runTool(context.Background(), tc); !strings.Contains(out, "not a JSON object") {
		t.Fatalf("%q", out)
	}
}

func TestMaxTurns(t *testing.T) {
	var replies []reply
	for i := 0; i < 5; i++ {
		replies = append(replies, reply{calls: []call{{"ls", map[string]any{}}}})
	}
	up := &scripted{replies: replies}
	r, _, evs := newRunner(t, up, ApprovalAuto)
	r.Opts.MaxTurns = 3
	if _, err := r.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	last := (*evs)[len(*evs)-2]
	if last.Kind != EventNote || !strings.Contains(last.Text, "Stopped after 3") {
		t.Fatalf("%+v", last)
	}
}

func TestIntentOnlyAnswerIsNudgedOnce(t *testing.T) {
	up := &scripted{replies: []reply{
		{text: "I need to look at the code first. Let me start by reading the files."},
		{calls: []call{{"ls", map[string]any{}}}},
		{text: "It is a shop. Let me know if you want more."},
		{text: "unused"},
	}}
	r, _, evs := newRunner(t, up, ApprovalAuto)
	out, err := r.Send(context.Background(), "what is this?")
	if err != nil || out != "It is a shop. Let me know if you want more." {
		t.Fatalf("%q %v", out, err)
	}
	if got := strings.Join(kinds(*evs), ","); got != "text,note,tool_start,tool_end,text,done" {
		t.Fatalf("events: %s", got)
	}
	cases := map[string]bool{"Let me check the file.": true, "The function returns 404. I'll add a test if you want.": false, "I'm going to read main.go": true, "Done.": false}
	for in, want := range cases {
		if looksLikeIntent(in) != want {
			t.Errorf("looksLikeIntent(%q) != %v", in, want)
		}
	}
}

func TestEditToleratesIndentation(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	p := filepath.Join(root, "cart.go")
	src := "func Total() int {\n\tif count > 10 {\n\t\tsum = sum * 90 / 100\n\t}\n\treturn sum\n}\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := r.findTool("edit")
	// Spaces for tabs, as models often send.
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "cart.go",
		"old_string": "    if count > 10 {\n        sum = sum * 90 / 100\n    }\n",
		"new_string": "    if count >= 10 {\n        // bulk\n        sum = sum * 90 / 100\n    }\n"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "func Total() int {\n\tif count >= 10 {\n\t\t// bulk\n\t\tsum = sum * 90 / 100\n\t}\n\treturn sum\n}\n"
	if string(b) != want {
		t.Fatalf("got:\n%q\nwant:\n%q", b, want)
	}
	// Ambiguous loose matches are refused; other character differences too.
	if err := os.WriteFile(p, []byte("\tx := 1\n\ty := 2\n\tx := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "cart.go", "old_string": "x := 1", "new_string": "x := 3"}); err == nil || !strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("exact ambiguity: %v", err)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "cart.go", "old_string": "    x := 1\n", "new_string": "x := 3"}); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("loose ambiguity: %v", err)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "cart.go", "old_string": "y := 3", "new_string": "y := 4"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("not found: %v", err)
	}
	// Spaces file, tab-indented old_string.
	if err := os.WriteFile(p, []byte("fn main() {\n    if a {\n        b()\n    }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "cart.go", "old_string": "\tif a {\n\t\tb()\n\t}", "new_string": "\tif a {\n\t\tc()\n\t}"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "fn main() {\n    if a {\n        c()\n    }\n}\n" {
		t.Fatalf("spaces file: %q", b)
	}
}

func TestRepeatedFailingCallIsFlagged(t *testing.T) {
	var replies []reply
	for i := 0; i < 3; i++ {
		replies = append(replies, reply{calls: []call{{"read", map[string]any{"path": "missing.go"}}}})
	}
	replies = append(replies, reply{text: "giving up"})
	r, _, _ := newRunner(t, &scripted{replies: replies}, ApprovalAuto)
	if _, err := r.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	if len(results) != 3 || strings.Contains(results[1], "failed 3 times") || !strings.Contains(results[2], "failed 3 times") {
		t.Fatalf("results: %q", results)
	}
}

func TestWorkingDirectoryInsideProject(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "svc/pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "svc/pkg/a.go"), []byte("package pkg // needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("top\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("S=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Opts.Dir = filepath.Join(root, "svc")
	ctx := context.Background()
	if out, err := r.findTool("read").Run(ctx, r, map[string]any{"path": "pkg/a.go"}); err != nil || !strings.Contains(out, "needle") {
		t.Fatalf("read relative to dir: %q %v", out, err)
	}
	if out, err := r.findTool("read").Run(ctx, r, map[string]any{"path": "../top.txt"}); err != nil || !strings.Contains(out, "top") {
		t.Fatalf("read parent inside root: %q %v", out, err)
	}
	if _, err := r.findTool("read").Run(ctx, r, map[string]any{"path": "../.env"}); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret via parent: %v", err)
	}
	if _, err := r.findTool("read").Run(ctx, r, map[string]any{"path": "../../x"}); err == nil {
		t.Fatal("escaped the root")
	}
	if out, _ := r.findTool("glob").Run(ctx, r, map[string]any{"pattern": "**/*.go"}); out != "pkg/a.go" {
		t.Fatalf("glob relative to dir: %q", out)
	}
	if out, _ := r.goGrep(ctx, r.Opts.Dir, "needle", "", false, "content", 0); out != "pkg/a.go:1:package pkg // needle" {
		t.Fatalf("grep relative to dir: %q", out)
	}
	if runtime.GOOS != "windows" {
		if out, err := r.findTool("bash").Run(ctx, r, map[string]any{"command": "basename \"$PWD\""}); err != nil || strings.TrimSpace(out) != "svc" {
			t.Fatalf("bash cwd: %q %v", out, err)
		}
	}
	r.Opts.Env = nil
	if env := r.environment(); !strings.Contains(env["project root"], root) {
		t.Fatalf("env: %+v", env)
	}
}

func TestDoneEventListsChangedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	up := &scripted{replies: []reply{
		{calls: []call{{"write", map[string]any{"path": "new.txt", "content": "n"}}, {"edit", map[string]any{"path": "a.txt", "old_string": "1", "new_string": "2"}}}},
		{text: "done"},
		{text: "nothing"},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "i"}} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if _, err := r.Send(context.Background(), "change"); err != nil {
		t.Fatal(err)
	}
	done := (*evs)[len(*evs)-1]
	if strings.Join(done.Changed, ",") != "a.txt,new.txt" {
		t.Fatalf("changed = %v", done.Changed)
	}
	if _, err := r.Send(context.Background(), "nothing"); err != nil {
		t.Fatal(err)
	}
	done = (*evs)[len(*evs)-1]
	if done.Changed == nil || len(done.Changed) != 0 {
		t.Fatalf("second turn changed = %#v", done.Changed)
	}
}
