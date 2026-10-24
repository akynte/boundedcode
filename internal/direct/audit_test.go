package direct

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Every tool call of an assistant message gets a result, even when the
// turn ends early (hand-off or interrupt): providers refuse a history with
// unanswered calls.
func TestHandoffAnswersEveryToolCall(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{
			{"edit", map[string]any{"path": "internal/auth/login.go", "old_string": "a", "new_string": "b"}},
			{"write", map[string]any{"path": "after.txt", "content": "x"}},
		}},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	os.MkdirAll(filepath.Join(root, "internal/auth"), 0o755)
	os.WriteFile(filepath.Join(root, "internal/auth/login.go"), []byte("a"), 0o644)
	r.Opts.Mode = ModeCode
	_, err := r.Send(context.Background(), "change login")
	var h *Handoff
	if !errors.As(err, &h) {
		t.Fatalf("err = %v", err)
	}
	ms := r.Session.Messages
	if len(ms) != 4 || ms[2].Role != "tool" || ms[2].ToolCallID != "c0" || ms[3].Role != "tool" || ms[3].ToolCallID != "c1" {
		t.Fatalf("messages: %+v", ms)
	}
	if !strings.Contains(ms[3].Content, "not run") {
		t.Fatalf("skipped call result: %q", ms[3].Content)
	}
	if _, err := os.Stat(filepath.Join(root, "after.txt")); err == nil {
		t.Fatal("a call after the hand-off ran")
	}
	denied := 0
	for _, e := range *evs {
		if e.Kind == EventDenied {
			denied++
		}
	}
	if denied != 2 {
		t.Fatalf("denied events: %d", denied)
	}
	// The session can continue: the next request pairs every call.
	up.replies = []reply{{text: "ok"}}
	r.Opts.AllowRisky = true
	if _, err := r.Send(context.Background(), "go on"); err != nil {
		t.Fatal(err)
	}
	calls, results := 0, 0
	for _, m := range up.lastMessages(t) {
		if tcs, ok := m["tool_calls"].([]ToolCall); ok {
			calls += len(tcs)
		}
		if m["role"] == "tool" {
			results++
		}
	}
	if calls != 2 || results != 2 {
		t.Fatalf("calls %d results %d", calls, results)
	}
}

func TestInterruptAnswersEveryToolCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	up := &scripted{replies: []reply{
		{calls: []call{{"bash", map[string]any{"command": "sleep 10"}}, {"bash", map[string]any{"command": "echo later"}}}},
	}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if _, err := r.Send(ctx, "go"); err != ErrInterrupted {
		t.Fatalf("err = %v", err)
	}
	ms := r.Session.Messages
	if len(ms) != 4 || ms[3].Role != "tool" || !strings.Contains(ms[3].Content, "interrupted") {
		t.Fatalf("messages: %+v", ms)
	}
}

// Plan mode still runs read-only, build and test commands.
func TestPlanModeAllowsSafeCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	ctx := context.Background()
	if _, err := r.findTool("enter_plan_mode").Run(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	tc := ToolCall{ID: "b"}
	tc.Function.Name = "bash"
	tc.Function.Arguments = `{"command":"echo planning"}`
	if res := r.runTool(ctx, tc); !strings.Contains(res, "planning") || strings.Contains(res, "Plan mode") {
		t.Fatalf("safe command in plan mode: %q", res)
	}
	tc.Function.Arguments = `{"command":"touch made.txt"}`
	if res := r.runTool(ctx, tc); !strings.Contains(res, "plan mode") {
		t.Fatalf("unsafe command in plan mode: %q", res)
	}
}

// A custom agent's tools: list restricts its tool set; other agents'
// names for the same tools are accepted.
func TestCustomAgentToolsAreHonoured(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	os.MkdirAll(filepath.Join(root, ".boundedcode/agents"), 0o755)
	os.WriteFile(filepath.Join(root, ".boundedcode/agents/reader.md"), []byte("---\nname: reader\ndescription: Reads\ntools: Read, grep, WebFetch\n---\nYou read.\n"), 0o644)
	child, err := r.subagent("reader")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, t := range child.Tools() {
		names = append(names, t.Name)
	}
	if got := strings.Join(names, ","); got != "read,grep,web_fetch" {
		t.Fatalf("tools = %s", got)
	}
	if !child.sharedMCP {
		t.Fatal("sub-agent should share the parent's MCP servers")
	}
}

// Command templates: arguments a template does not mention are appended,
// and the model can run a template through the skill tool.
func TestCommandArgumentsAndModelInvocation(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	os.MkdirAll(filepath.Join(root, ".boundedcode/commands"), 0o755)
	os.WriteFile(filepath.Join(root, ".boundedcode/commands/triage.md"), []byte("---\ndescription: Triage\n---\nTriage the issue.\n"), 0o644)
	ctx := context.Background()
	prompt, ok, err := r.ExpandCommand(ctx, "", "triage", "#12 urgent")
	if err != nil || !ok || prompt != "Triage the issue.\n\nARGUMENTS: #12 urgent" {
		t.Fatalf("%q %v %v", prompt, ok, err)
	}
	if p, _, _ := r.ExpandCommand(ctx, "", "triage", ""); p != "Triage the issue." {
		t.Fatalf("no args: %q", p)
	}
	sk := r.findTool("skill")
	if sk == nil {
		t.Fatal("skill tool absent although a command template exists")
	}
	out, err := sk.Run(ctx, r, map[string]any{"name": "triage", "args": "#12"})
	if err != nil || !strings.Contains(out, "<command name=\"triage\">") || !strings.Contains(out, "ARGUMENTS: #12") {
		t.Fatalf("%q %v", out, err)
	}
	if !strings.Contains(r.systemPrompt(), "- triage (command template): Triage") {
		t.Fatal("command template not listed in the system prompt")
	}
}

func TestGrepOutputModesAndContext(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo needle\nthree\nfour\nfive needle\nsix\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("needle\n"), 0o644)
	ctx := context.Background()
	out, err := r.goGrep(ctx, root, "needle", "", false, "files", 0)
	if err != nil || out != "a.txt\nb.txt" {
		t.Fatalf("files: %q %v", out, err)
	}
	out, _ = r.goGrep(ctx, root, "needle", "", false, "count", 0)
	if out != "a.txt:2\nb.txt:1" {
		t.Fatalf("count: %q", out)
	}
	out, _ = r.goGrep(ctx, root, "needle", "a.txt", false, "content", 1)
	want := "a.txt-1-one\na.txt:2:two needle\na.txt-3-three\na.txt-4-four\na.txt:5:five needle\na.txt-6-six"
	if out != want {
		t.Fatalf("context:\n%s\nwant:\n%s", out, want)
	}
	// Through the tool (ripgrep when present): files mode lists paths only.
	out, err = r.findTool("grep").Run(ctx, r, map[string]any{"pattern": "needle", "output_mode": "files"})
	if err != nil || !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") || strings.Contains(out, "two needle") {
		t.Fatalf("grep files: %q %v", out, err)
	}
	out, err = r.findTool("grep").Run(ctx, r, map[string]any{"pattern": "needle", "output_mode": "count"})
	if err != nil || !strings.Contains(out, "a.txt:2") {
		t.Fatalf("grep count: %q %v", out, err)
	}
}

func TestSessionMarkdown(t *testing.T) {
	s := &Session{ID: "s1", Title: "fix it", Model: "m", Provider: "local"}
	tc := ToolCall{ID: "c0"}
	tc.Function.Name, tc.Function.Arguments = "read", `{"path":"a.go"}`
	s.Messages = []Message{{Role: "user", Content: "fix it"}, {Role: "assistant", ToolCalls: []ToolCall{tc}},
		{Role: "tool", Name: "read", ToolCallID: "c0", Content: "1\tpackage a"}, {Role: "assistant", Content: "Done."}}
	md := s.Markdown()
	for _, want := range []string{"# fix it", "Session s1 · local/m", "## User\n\nfix it", "- 🛠 `read` {\"path\":\"a.go\"}", "<details><summary>result of read</summary>", "## Assistant\n\nDone."} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// The environment in the system prompt follows the working tree.
func TestEnvironmentRefreshesPerTurn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	up := &scripted{replies: []reply{{text: "a"}, {text: "b"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.Env = nil
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "i"}} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if _, err := r.Send(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if sys := up.lastMessages(t)[0]["content"].(string); !strings.Contains(sys, "git status: clean") {
		t.Fatalf("first prompt: %q", sys)
	}
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o644)
	if _, err := r.Send(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if sys := up.lastMessages(t)[0]["content"].(string); !strings.Contains(sys, "git status: 1 changed path(s)") {
		t.Fatalf("second prompt: %q", sys)
	}
}
