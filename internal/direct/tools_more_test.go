package direct

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMultiEditAtomic(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	p := filepath.Join(root, "a.go")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := r.findTool("multi_edit")
	_, err := tool.Run(context.Background(), r, map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "a", "new_string": "A"}, map[string]any{"old_string": "zzz", "new_string": "Z"}}})
	if err == nil || !strings.Contains(err.Error(), "edit 2") {
		t.Fatalf("partial failure: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a\nb\nc\n" {
		t.Fatal("a failed multi_edit wrote the file")
	}
	if _, err := tool.Run(context.Background(), r, map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "a", "new_string": "A"}, map[string]any{"old_string": "c", "new_string": "C"}}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "A\nb\nC\n" {
		t.Fatalf("%q", b)
	}
}

func TestReadMany(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "src/x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/x/a.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/b.ts"), []byte("let b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.findTool("read_many").Run(context.Background(), r, map[string]any{"paths": []any{"README.md", "src/**/*.go"}})
	if err != nil || !strings.Contains(out, "==> README.md <==\nreadme") || !strings.Contains(out, "==> src/x/a.go <==") || strings.Contains(out, "b.ts") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestPermissionRules(t *testing.T) {
	rules := Rules{Allow: []string{"bash(go test *)", "edit(src/**)", "web_fetch(docs.example.com)", "read"}, Deny: []string{"bash(rm *)", "read(.env*)"}}
	for spec, want := range map[string]bool{"bash(go test ./...)": true, "bash(go test)": true, "bash(go testx)": false, "edit(src/a/b.go)": true, "edit(lib/a.go)": false,
		"web_fetch(docs.example.com)": true, "web_fetch(evil.com)": false, "read(anything)": true, "write(x)": false} {
		if got := rules.Allowed(spec); got != want {
			t.Errorf("Allowed(%q) = %v", spec, got)
		}
	}
	if _, d := rules.Denied("bash(rm -rf x)"); !d {
		t.Error("rm not denied")
	}
	if _, d := rules.Denied("read(.env.local)"); !d {
		t.Error(".env not denied")
	}
	// Deny wins and needs no approval call; allow skips the approval.
	up := &scripted{replies: []reply{
		{calls: []call{{"bash", map[string]any{"command": "rm -rf build"}}, {"bash", map[string]any{"command": "touch made"}}}},
		{text: "ok"},
	}}
	r, root, _ := newRunner(t, up, ApprovalEdits)
	r.Opts.Permissions = Rules{Allow: []string{"bash(touch *)"}, Deny: []string{"bash(rm *)"}}
	asked := 0
	r.Approve = func(context.Context, ApprovalRequest) bool { asked++; return false }
	if _, err := r.Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatal("rules did not bypass approval")
	}
	if _, err := os.Stat(filepath.Join(root, "made")); err != nil && runtime.GOOS != "windows" {
		t.Fatal("allowed command did not run")
	}
	if !strings.Contains(r.Session.Messages[2].Content, "denied by the permission rule") {
		t.Fatalf("%q", r.Session.Messages[2].Content)
	}
}

func TestHooksBlockAndAnnotate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh hooks")
	}
	up := &scripted{replies: []reply{
		{calls: []call{{"write", map[string]any{"path": "x.txt", "content": "1"}}, {"ls", map[string]any{}}}},
		{text: "done"},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	r.Opts.Hooks = []Hook{
		{Event: HookPreTool, Matcher: "^write$", Command: `read -r in; echo "no writes today" >&2; exit 2`},
		{Event: HookPostTool, Matcher: "ls", Command: `echo "post-hook saw $BOUNDEDCODE_HOOK_EVENT"`},
		{Event: HookUserPrompt, Command: `echo "remember the style guide"`},
	}
	if _, err := r.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); err == nil {
		t.Fatal("blocked write happened")
	}
	if !strings.Contains(r.Session.Messages[0].Content, "remember the style guide") {
		t.Fatalf("prompt hook: %q", r.Session.Messages[0].Content)
	}
	var results []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "blocked by a hook: no writes today") || !strings.Contains(results[1], "[hook] post-hook saw post_tool") {
		t.Fatalf("%q", results)
	}
	found := false
	for _, e := range *evs {
		if e.Kind == EventDenied && strings.Contains(e.Err, "hook") {
			// The refused call's arguments come with it (the security grader reads them).
			found = e.Args["path"] == "x.txt"
		}
	}
	if !found {
		t.Fatal("no denied event for the hook")
	}
	// A blocking user_prompt hook stops the message.
	r.Opts.Hooks = []Hook{{Event: HookUserPrompt, Command: `exit 2`}}
	if _, err := r.Send(context.Background(), "again"); err == nil || !strings.Contains(err.Error(), "blocked by a hook") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckpointsAndUndo(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"edit", map[string]any{"path": "a.txt", "old_string": "one", "new_string": "two"}}, {"write", map[string]any{"path": "new.txt", "content": "n"}}}},
		{text: "done"},
		{calls: []call{{"edit", map[string]any{"path": "a.txt", "old_string": "two", "new_string": "three"}}}},
		{text: "done again"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.CheckpointDir = t.TempDir()
	r.Session.ID = "s1"
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Send(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	restored, err := r.Undo()
	if err != nil || strings.Join(restored, ",") != "a.txt" {
		t.Fatalf("undo 1: %v %v", restored, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "two\n" {
		t.Fatalf("after undo 1: %q", b)
	}
	restored, err = r.Undo()
	if err != nil || strings.Join(restored, ",") != "a.txt,new.txt (removed)" {
		t.Fatalf("undo 2: %v %v", restored, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "one\n" {
		t.Fatalf("after undo 2: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err == nil {
		t.Fatal("created file not removed")
	}
	if _, err := r.Undo(); err == nil {
		t.Fatal("third undo succeeded")
	}
}

func TestBackgroundShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash")
	}
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	ctx := context.Background()
	out, err := r.findTool("bash").Run(ctx, r, map[string]any{"command": "echo start; sleep 30", "run_in_background": true})
	if err != nil || !strings.Contains(out, "started shell-1") {
		t.Fatalf("%q %v", out, err)
	}
	time.Sleep(150 * time.Millisecond)
	out, err = r.findTool("bash_output").Run(ctx, r, map[string]any{"shell_id": "shell-1"})
	if err != nil || !strings.Contains(out, "running") || !strings.Contains(out, "start") {
		t.Fatalf("%q %v", out, err)
	}
	if out, err = r.findTool("kill_shell").Run(ctx, r, map[string]any{"shell_id": "shell-1"}); err != nil || out != "killed shell-1" {
		t.Fatalf("%q %v", out, err)
	}
	time.Sleep(100 * time.Millisecond)
	out, _ = r.findTool("bash_output").Run(ctx, r, map[string]any{"shell_id": "shell-1"})
	if strings.Contains(out, "running") {
		t.Fatalf("still running: %q", out)
	}
	if _, err := r.findTool("bash_output").Run(ctx, r, map[string]any{"shell_id": "shell-9"}); err == nil {
		t.Fatal("unknown shell accepted")
	}
	r.Close()
}

func TestTodoPlanAskMemory(t *testing.T) {
	r, root, evs := newRunner(t, &scripted{}, ApprovalEdits)
	ctx := context.Background()
	out, err := r.findTool("todo_write").Run(ctx, r, map[string]any{"todos": []any{map[string]any{"content": "read", "status": "completed"}, map[string]any{"content": "fix", "status": "in_progress"}, map[string]any{"content": "test", "status": "pending"}}})
	if err != nil || !strings.Contains(out, "[x] read") || !strings.Contains(out, "[>] fix") || len(r.Todos) != 3 {
		t.Fatalf("%q %v", out, err)
	}
	// Plan mode blocks edits until the plan is approved.
	if _, err := r.findTool("enter_plan_mode").Run(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	tc := ToolCall{ID: "w"}
	tc.Function.Name = "write"
	tc.Function.Arguments = `{"path":"p.txt","content":"x"}`
	if res := r.runTool(ctx, tc); !strings.Contains(res, "Plan mode is on") {
		t.Fatalf("%q", res)
	}
	if _, err := os.Stat(filepath.Join(root, "p.txt")); err == nil {
		t.Fatal("plan mode wrote a file")
	}
	approvals := []bool{false, true}
	r.Approve = func(_ context.Context, req ApprovalRequest) bool {
		a := approvals[0]
		approvals = approvals[1:]
		return a
	}
	if out, _ := r.findTool("exit_plan_mode").Run(ctx, r, map[string]any{"plan": "1. do it"}); !strings.Contains(out, "did not approve") || !r.plan {
		t.Fatalf("rejected plan: %q plan=%v", out, r.plan)
	}
	if out, _ := r.findTool("exit_plan_mode").Run(ctx, r, map[string]any{"plan": "1. do it"}); !strings.Contains(out, "approved") || r.plan {
		t.Fatalf("approved plan: %q", out)
	}
	// ask_user: no asker in headless mode; an asker answers.
	r.tools = nil
	if r.findTool("ask_user") != nil {
		t.Fatal("ask_user offered without an asker")
	}
	r.Asker = func(_ context.Context, q string, opts []string) (string, error) {
		return "option B (" + strings.Join(opts, "/") + ")", nil
	}
	r.tools = nil
	out, err = r.findTool("ask_user").Run(ctx, r, map[string]any{"question": "A or B?", "options": []any{"A", "B"}})
	if err != nil || out != "The user answered: option B (A/B)" {
		t.Fatalf("%q %v", out, err)
	}
	// save_memory appends to the configured file.
	mem := filepath.Join(t.TempDir(), "AGENTS.md")
	r.Opts.MemoryFile = mem
	r.tools = nil
	if _, err := r.findTool("save_memory").Run(ctx, r, map[string]any{"fact": "prefers tabs"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mem); !strings.Contains(string(b), "- prefers tabs (") {
		t.Fatalf("%q", b)
	}
	kinds := map[EventKind]bool{}
	for _, e := range *evs {
		kinds[e.Kind] = true
	}
	if !kinds[EventTodos] || !kinds[EventQuestion] || !kinds[EventMode] {
		t.Fatalf("events: %v", kinds)
	}
}

func TestNotebookTools(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	nb := `{"cells":[{"cell_type":"markdown","id":"m1","metadata":{},"source":["# Title\n"]},{"cell_type":"code","id":"c1","metadata":{},"execution_count":1,"outputs":[{"output_type":"stream","name":"stdout","text":["hi\n"]}],"source":["print('hi')\n"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	if err := os.WriteFile(filepath.Join(root, "n.ipynb"), []byte(nb), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out, err := r.findTool("notebook_read").Run(ctx, r, map[string]any{"path": "n.ipynb"})
	if err != nil || !strings.Contains(out, "cell 1 (code, id c1)") || !strings.Contains(out, "[output]\nhi") {
		t.Fatalf("%q %v", out, err)
	}
	if out, err = r.findTool("read").Run(ctx, r, map[string]any{"path": "n.ipynb"}); err != nil || !strings.Contains(out, "2 cells") {
		t.Fatalf("read notebook: %q %v", out, err)
	}
	edit := r.findTool("notebook_edit")
	if _, err := edit.Run(ctx, r, map[string]any{"path": "n.ipynb", "cell": "c1", "source": "print('bye')"}); err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Run(ctx, r, map[string]any{"path": "n.ipynb", "cell": "0", "mode": "insert", "cell_type": "markdown", "source": "note"}); err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Run(ctx, r, map[string]any{"path": "n.ipynb", "cell": "0", "mode": "delete"}); err != nil {
		t.Fatal(err)
	}
	out, _ = r.findTool("notebook_read").Run(ctx, r, map[string]any{"path": "n.ipynb"})
	if !strings.Contains(out, "2 cells") || !strings.Contains(out, "note") || !strings.Contains(out, "print('bye')") || strings.Contains(out, "[output]") {
		t.Fatalf("%q", out)
	}
	if _, err := edit.Run(ctx, r, map[string]any{"path": "n.ipynb", "cell": "9", "source": "x"}); err == nil {
		t.Fatal("bad cell accepted")
	}
}

func TestWebFetchAndSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>T</title><style>x{}</style></head><body><h1>Hello</h1><script>bad()</script><p>World &amp; <b>friends</b></p></body></html>`))
		case "/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"title":"One","url":"http://a","content":"first"},{"title":"Two","url":"http://b","content":"second"}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	r, _, _ := newRunner(t, &scripted{replies: []reply{{text: "The page greets the world."}}}, ApprovalEdits)
	r.Opts.Web.SearchURL = srv.URL + "/search?q={query}"
	ctx := context.Background()
	out, err := r.findTool("web_fetch").Run(ctx, r, map[string]any{"url": srv.URL + "/page"})
	if err != nil || out != "Hello\nWorld & friends" {
		t.Fatalf("%q %v", out, err)
	}
	if out, err = r.findTool("web_fetch").Run(ctx, r, map[string]any{"url": srv.URL + "/page", "prompt": "what does it say?"}); err != nil || out != "The page greets the world." {
		t.Fatalf("prompted fetch: %q %v", out, err)
	}
	if _, err := r.findTool("web_fetch").Run(ctx, r, map[string]any{"url": srv.URL + "/missing"}); err == nil || !strings.Contains(err.Error(), "http 404") {
		t.Fatalf("404: %v", err)
	}
	if _, err := r.findTool("web_fetch").Run(ctx, r, map[string]any{"url": "file:///etc/passwd"}); err == nil {
		t.Fatal("file URL accepted")
	}
	out, err = r.findTool("web_search").Run(ctx, r, map[string]any{"query": "x y"})
	if err != nil || !strings.Contains(out, "1. One\n   http://a\n   first") || !strings.Contains(out, "2. Two") {
		t.Fatalf("%q %v", out, err)
	}
	// Consent: edits mode asks unless the host is allowed; auto never.
	if _, need := r.findTool("web_fetch").Consent(r, map[string]any{"url": srv.URL + "/page"}); !need {
		t.Fatal("edits mode did not ask for an unknown host")
	}
	r.Opts.Web.AllowHosts = []string{"127.0.0.1"}
	if _, need := r.findTool("web_fetch").Consent(r, map[string]any{"url": srv.URL + "/page"}); need {
		t.Fatal("allowed host asked")
	}
}

func TestSkillsCommandsAndAgents(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, ".boundedcode/skills/review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".boundedcode/skills/review/SKILL.md"), []byte("---\nname: review\ndescription: Review a diff\n---\nReview $ARGUMENTS carefully. Files live in ${SKILL_DIR}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".boundedcode/commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".boundedcode/commands/fix-issue.md"), []byte("---\ndescription: Fix an issue\n---\nFix issue $1 (all: $ARGUMENTS). Branch: !`echo main`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".boundedcode/agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".boundedcode/agents/tester.md"), []byte("---\nname: tester\ndescription: Writes tests\nread_only: true\n---\nYou write tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.tools = nil
	if sk := r.Skills(); len(sk) != 1 || sk[0].Name != "review" {
		t.Fatalf("skills: %+v", sk)
	}
	if ag := r.Agents(); len(ag) != 1 || ag[0].Name != "tester" || !ag[0].ReadOnly || ag[0].Prompt != "You write tests." {
		t.Fatalf("agents: %+v", ag)
	}
	if !strings.Contains(r.systemPrompt(), "- review: Review a diff") {
		t.Fatal("skills not in the system prompt")
	}
	out, err := r.findTool("skill").Run(context.Background(), r, map[string]any{"name": "review", "args": "PR 12"})
	if err != nil || !strings.Contains(out, "Review PR 12 carefully") || !strings.Contains(out, filepath.Join(root, ".boundedcode/skills/review")) {
		t.Fatalf("%q %v", out, err)
	}
	cmds := r.Commands("")
	if len(cmds) != 2 || cmds[0].Name != "fix-issue" || cmds[1].Name != "review" {
		t.Fatalf("commands: %+v", cmds)
	}
	prompt, ok, err := r.ExpandCommand(context.Background(), "", "fix-issue", `42 "with care"`)
	if err != nil || !ok || prompt != "Fix issue 42 (all: 42 \"with care\"). Branch: main" {
		t.Fatalf("%q %v %v", prompt, ok, err)
	}
	if _, ok, _ := r.ExpandCommand(context.Background(), "", "nope", ""); ok {
		t.Fatal("unknown command expanded")
	}
}

func TestSubagentTask(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"task", map[string]any{"description": "find it", "prompt": "where is Needle?", "subagent_type": "explore"}}}},
		// the sub-agent's turns:
		{calls: []call{{"grep", map[string]any{"pattern": "Needle"}}}},
		{text: "Needle is in a.go line 1."},
		// back to the parent:
		{text: "Found: a.go."},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("Needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.Send(context.Background(), "find Needle")
	if err != nil || out != "Found: a.go." {
		t.Fatalf("%q %v", out, err)
	}
	if !strings.Contains(r.Session.Messages[2].Content, "<subagent type=\"explore\">\nNeedle is in a.go line 1.") {
		t.Fatalf("%q", r.Session.Messages[2].Content)
	}
	n := 0
	for _, e := range *evs {
		if e.Kind == EventSubagent {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no sub-agent events")
	}
	// An explore sub-agent has no write tool and cannot nest.
	child, _ := r.subagent("explore")
	if child.findTool("write") != nil || child.findTool("task") != nil || child.findTool("read") == nil {
		t.Fatal("explore tool set wrong")
	}
	if _, err := r.subagent("nope"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestAttachReferencesAndImages(t *testing.T) {
	up := &scripted{replies: []reply{{text: "ok"}}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hello notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pic.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("S=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Send(context.Background(), "look at @notes.md and @pic.png and @.env and @missing.txt"); err != nil {
		t.Fatal(err)
	}
	m := r.Session.Messages[0]
	if !strings.Contains(m.Content, "<file path=\"notes.md\">\nhello notes\n</file>") || strings.Contains(m.Content, "S=1") {
		t.Fatalf("%q", m.Content)
	}
	if len(m.Parts) != 2 || m.Parts[1]["type"] != "image_url" {
		t.Fatalf("parts: %+v", m.Parts)
	}
	wire := up.lastMessages(t)[1]
	if _, ok := wire["content"].([]map[string]any); !ok {
		t.Fatalf("wire content: %T", wire["content"])
	}
	// read on an image attaches it after the tool result.
	up.replies = []reply{{calls: []call{{"read", map[string]any{"path": "pic.png"}}}}, {text: "seen"}}
	if _, err := r.Send(context.Background(), "describe pic.png"); err != nil {
		t.Fatal(err)
	}
	msgs := r.Session.Messages
	last := msgs[len(msgs)-2]
	if last.Role != "user" || len(last.Parts) != 2 {
		t.Fatalf("image not attached after the tool result: %+v", last)
	}
}

func TestMCPServerTools(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("python3")
	}
	script := filepath.Join(t.TempDir(), "mcp.py")
	if err := os.WriteFile(script, []byte(`
import sys, json
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fake"}}}
    elif m.get("method") == "tools/list":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"tools":[{"name":"add","description":"Add numbers","inputSchema":{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}}]}}
    elif m.get("method") == "tools/call":
        a = m["params"]["arguments"]
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"content":[{"type":"text","text":str(a["a"]+a["b"])}]}}
    else:
        continue
    sys.stdout.write(json.dumps(out)+"\n"); sys.stdout.flush()
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	r.Opts.MCP = []MCPServer{{Name: "calc", Command: "/usr/bin/python3", Args: []string{"-I", script}}, {Name: "broken", Command: "/nonexistent"}}
	tool := r.findTool("mcp__calc__add")
	if tool == nil {
		t.Fatalf("MCP tool missing: %v", r.MCPStatus())
	}
	out, err := tool.Run(context.Background(), r, map[string]any{"a": 2, "b": 3})
	if err != nil || out != "5" {
		t.Fatalf("%q %v", out, err)
	}
	st := r.MCPStatus()
	if len(st) != 2 || !strings.Contains(st[0], "calc: 1 tool(s): add") || !strings.Contains(st[1], "broken: unavailable") {
		t.Fatalf("%v", st)
	}
	r.Close()
}
