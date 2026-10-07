package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/stats"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

// fakeBackend serves canned data and records Exec calls.
type fakeBackend struct {
	mu       sync.Mutex
	execs    [][]string
	created  []CreateTaskRequest
	execOut  string
	execErr  error
	execWait chan struct{}
	prompter Prompter
	project  Project
	setup    []SetupStep
	status   map[string]task.Status
	gitInits []string
	nextID   int
	// Set-up wizard data and what it stored.
	hw        HardwareInfo
	providers []ProviderRow
	pmodels   []ProviderModel
	keys      map[string]string
	testErr   error
}

func (f *fakeBackend) Hardware(context.Context) HardwareInfo { return f.hw }
func (f *fakeBackend) Providers(context.Context) []ProviderRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.providers
}
func (f *fakeBackend) ProviderModels(_ context.Context, provider, _ string) ([]ProviderModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[provider] == "" {
		return nil, fmt.Errorf("no API key for %s", provider)
	}
	return f.pmodels, nil
}
func (f *fakeBackend) SetProviderKey(_ context.Context, provider, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys == nil {
		f.keys = map[string]string{}
	}
	f.keys[provider] = key
	return "OS credential store", nil
}
func (f *fakeBackend) DeleteProviderKey(_ context.Context, provider string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, provider)
	return nil
}
func (f *fakeBackend) TestProvider(context.Context) (string, error) {
	return "anthropic claude-x answered in 1s", f.testErr
}

func now(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339Nano) }

func sampleTask(id string, st task.Status) *task.Task {
	return &task.Task{ID: id, WorkspaceID: "w1", OriginalRequest: "Rename the order.created field total to amount_cents in producers and consumers",
		Goal: "Rename the field", AcceptanceCriteria: []string{"consumers compile", "contract tests pass"}, Status: st, Phase: task.PhaseImplementing,
		AttemptCount: 2, VerificationState: "failing", ChangedFiles: []string{"orders/event.go", "billing/consumer.go"},
		AgentRuntime: "openhands", AgentSessionID: "sess-1", ModelProfile: "qwen3.6",
		Decisions: []task.Decision{{Source: "policy", Text: "blocked: SPEC_AMBIGUOUS: unclear\n- which consumers?"}},
		Budget:    task.Budget{MaxAttempts: 5, MaxLocalTokens: 400000, UsedLocalTokens: 123456, MaxWallClockS: 3600, UsedWallClockS: 900, MaxEscalations: 2},
		CreatedAt: now(2 * time.Hour), UpdatedAt: now(time.Minute)}
}

func (f *fakeBackend) Info(context.Context) Info {
	return Info{Name: "boundedcode", Version: "0.1.0", Commit: "abc123", ConfigFile: "/cfg/config.yaml", ConfigExists: true, StateDB: "/state/state.db",
		DefaultModel: "qwen3.6", InferenceMode: "managed", SandboxKind: "docker", AgentImage: "bc-sandbox:dev", FrontierEnabled: true,
		FrontierProvider: "codex", FrontierApproval: true, FrontierMaxPacket: 24000, CurrentWorkspace: "payments", CrossService: true}
}

func (f *fakeBackend) Workspaces(context.Context) ([]Workspace, error) {
	return []Workspace{{Workspace: workspace.Workspace{ID: "w1", Name: "payments", CreatedAt: now(48 * time.Hour)}, Current: true,
		Repos: []workspace.Repository{
			{ID: "r1", Name: "orders", Path: "/src/orders", Languages: []string{"go"}, Enabled: true, IndexedAt: now(time.Hour)},
			{ID: "r2", Name: "billing", Path: "/src/billing", Languages: []string{"typescript"}, Enabled: true},
			{ID: "r3", Name: "legacy", Path: "/src/legacy", Enabled: false},
		}},
		{Workspace: workspace.Workspace{ID: "w2", Name: "infra"}}}, nil
}

func (f *fakeBackend) Tasks(context.Context, int) ([]*task.Task, error) {
	return []*task.Task{sampleTask("t-0001", task.StatusBlocked), sampleTask("t-0002", task.StatusCompleted), sampleTask("t-0003", task.StatusActive)}, nil
}

func (f *fakeBackend) Task(_ context.Context, id string) (TaskDetail, error) {
	if id == "missing" {
		return TaskDetail{}, errors.New("task not found")
	}
	f.mu.Lock()
	st, ok := f.status[id]
	f.mu.Unlock()
	if !ok {
		st = task.StatusBlocked
	}
	t := sampleTask(id, st)
	if st == task.StatusCompleted {
		t.Decisions = nil
	}
	return TaskDetail{Task: t,
		Strategies: []task.Strategy{{Attempt: 1, Outcome: "rejected", Summary: "renamed in orders only", Reason: "billing consumer broke"}, {Attempt: 2, Outcome: "active"}},
		Worktrees:  []task.Worktree{{RepoName: "orders", Path: "/work/orders", Branch: "agent/" + id, BaseCommit: "0123456789abcdef"}}}, nil
}

func ev(id int64, kind string, data map[string]any) telemetry.Event {
	b, _ := json.Marshal(data)
	return telemetry.Event{ID: id, TS: now(time.Duration(100-id) * time.Second), TaskID: "t-0001", Kind: kind, Data: b}
}

func (f *fakeBackend) Events(_ context.Context, _ string, after int64, _ int) ([]telemetry.Event, error) {
	all := []telemetry.Event{
		ev(1, "task.created", nil),
		ev(2, "attempt.started", map[string]any{"attempt": 1, "mode": "fresh"}),
		ev(3, "agent.event", map[string]any{"kind": "ActionEvent", "tool": "terminal", "action": `{"command":"go test ./...","kind":"TerminalAction"}`, "thought": "Run the tests first"}),
		ev(4, "agent.event", map[string]any{"kind": "ActionEvent", "tool": "file_editor", "action": `{"command":"str_replace","path":"/work/orders/event.go"`}),
		ev(5, "agent.event", map[string]any{"kind": "ObservationEvent", "tool": "terminal", "is_error": true}),
		ev(6, "verify.result", map[string]any{"repo": "orders", "scope": "targeted", "passed": false, "failures": []string{"go-test"}, "ms": 1234}),
		ev(7, "task.blocked", map[string]any{"reason": "SPEC_AMBIGUOUS"}),
		ev(8, "something.new", map[string]any{"x": 1}),
	}
	var out []telemetry.Event
	for _, e := range all {
		if e.ID > after {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeBackend) Verifications(context.Context, string, int) ([]verify.Result, error) {
	return []verify.Result{{Repository: "orders", Scope: verify.Targeted, Passed: false, Started: time.Now(), Duration: 3 * time.Second,
		Stages: []verify.StageResult{{Name: "gofmt", Status: "pass", DurationMS: 20, Command: "gofmt -l ."}, {Name: "go-test", Status: "fail", DurationMS: 2000, Command: "go test ./...", Output: "--- FAIL: TestX\nFAIL"}}}}, nil
}

func (f *fakeBackend) Diffs(context.Context, string) ([]RepoDiff, error) {
	return []RepoDiff{{Repo: "orders", Diff: "diff --git a/event.go b/event.go\n--- a/event.go\n+++ b/event.go\n@@ -1,3 +1,3 @@\n package orders\n-\tTotal int\n+\tAmountCents int\n"}}, nil
}

func (f *fakeBackend) Escalations(context.Context, string) ([]Escalation, error) {
	c := true
	return []Escalation{{ID: 1, Task: "t-0001", Trigger: "Z2", Provider: "codex", Model: "gpt", Status: "answered", PacketTokens: 3000, Created: now(time.Hour), Reason: "repeated failures", AdviceChangedCode: &c}}, nil
}

func (f *fakeBackend) EscalationSummary(context.Context) ([]EscalationGroup, error) {
	return []EscalationGroup{{Trigger: "Z2", Status: "answered", Outcome: "helped", Count: 1, PacketTokens: 3000}}, nil
}

func (f *fakeBackend) FrontierLogin(context.Context) (string, error) {
	return "Logged in using ChatGPT", nil
}

func (f *fakeBackend) Stats(context.Context, time.Duration) (stats.Summary, error) {
	return stats.Summary{Tasks: 3, ByStatus: map[string]int{"completed": 1, "blocked": 1, "active": 1}, Completed: 1, CompletedVerified: 1, CompletedLocalOnly: 1, LocalOnlyRate: 1}, nil
}

func (f *fakeBackend) Doctor(context.Context) []Check {
	return []Check{{Name: "git", Status: "ok", Detail: "git 2.47"}, {Name: "gitleaks", Status: "warn", Detail: "not found", Hint: "install it"}, {Name: "llama-server", Status: "fail", Detail: "missing"}}
}

func (f *fakeBackend) SerenaChecks(context.Context) []Check {
	return []Check{{Name: "serena", Status: "ok", Detail: "v1.7.0"}}
}

func (f *fakeBackend) Runtime(context.Context) (inference.Status, error) {
	return inference.Status{Running: true, Healthy: true, Managed: true, Profile: "qwen3.6", PID: 42, RSSMiB: 2048, CtxSize: 65536, Endpoint: inference.Endpoint{BaseURL: "http://127.0.0.1:8080"}}, nil
}

func (f *fakeBackend) RuntimeLog(context.Context, int) (string, []string, error) {
	return "/state/llama-server.log", []string{"srv  load_model: loading", "srv  ready"}, nil
}

func (f *fakeBackend) Models(context.Context) ([]ModelRow, error) {
	return []ModelRow{{Name: "qwen3.6", File: "/models/q.gguf", License: "Apache-2.0", Present: true, Default: true, Status: "validated",
		SizeBytes: 22134528992, Fit: "offload", FitDetail: "needs about 23 GB", Recommended: true},
		{Name: "small", File: "/models/s.gguf", License: "Apache-2.0", Status: "experimental", SizeBytes: 2740937888, Fit: "gpu"},
		{Name: "laguna", File: "/models/l.gguf", License: "MIT", Status: "review"}}, nil
}

func (f *fakeBackend) CreateTask(_ context.Context, req CreateTaskRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	f.nextID++
	return fmt.Sprintf("t-%04d", 8+f.nextID), nil
}

func (f *fakeBackend) Project(_ context.Context, dir string) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.project, nil
}

func (f *fakeBackend) GitInit(_ context.Context, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gitInits = append(f.gitInits, dir)
	f.project = Project{Dir: dir, Root: dir, Workspace: "fresh", Repo: "fresh", Branch: "main", Created: true}
	return nil
}

func (f *fakeBackend) Setup(context.Context) []SetupStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setup
}

func (f *fakeBackend) setStatus(id string, st task.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = st
}

func (f *fakeBackend) Exec(ctx context.Context, args []string, out io.Writer) error {
	f.mu.Lock()
	f.execs = append(f.execs, args)
	wait := f.execWait
	f.mu.Unlock()
	fmt.Fprint(out, f.execOut)
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.execErr
}

func (f *fakeBackend) Commands() []CommandInfo {
	return []CommandInfo{{Path: "task run", Use: "task run TASK", Short: "Run or resume a task"}, {Path: "bench infra", Use: "bench infra", Short: "Sweep"}}
}

func (f *fakeBackend) SetPrompter(p Prompter) { f.prompter = p }

func (f *fakeBackend) lastExec() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.execs) == 0 {
		return nil
	}
	return f.execs[len(f.execs)-1]
}

// harness drives a Model synchronously: commands run inline and their
// messages are fed back, like the Bubble Tea runtime does.
type harness struct {
	t  *testing.T
	m  *Model
	be *fakeBackend
	q  chan tea.Msg
}

// newHarness starts on the Tasks view; newChatHarness stays in the chat.
func newHarness(t *testing.T, w, h int) *harness {
	t.Helper()
	hn := newChatHarness(t, w, h, nil)
	hn.gotoView("Tasks")
	return hn
}

func defaultBackend() *fakeBackend {
	return &fakeBackend{
		project: Project{Dir: "/src/orders", Root: "/src/orders", Workspace: "payments", Repo: "orders", Branch: "main", Indexed: true},
		setup:   []SetupStep{{Name: "config", Title: "Configuration", OK: true}, {Name: "tools", Title: "Repository tools", OK: true}},
		status:  map[string]task.Status{},
	}
}

func newChatHarness(t *testing.T, w, h int, be *fakeBackend) *harness {
	t.Helper()
	if be == nil {
		be = defaultBackend()
	}
	m := newModel(context.Background(), be, Options{Dir: "/src/orders"})
	m.noTimers = true
	hn := &harness{t: t, m: m, be: be, q: make(chan tea.Msg, 1024)}
	m.send = func(msg tea.Msg) { hn.q <- msg }
	be.SetPrompter(m.prompt)
	hn.run(m.Init())
	hn.send(tea.WindowSizeMsg{Width: w, Height: h})
	return hn
}

// run executes a command tree with a time limit, skipping ticks (which
// would loop forever) and feeding results back into Update.
func (hn *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		hn.deliver(msg)
	case <-time.After(40 * time.Millisecond):
		// A timer or a job still running: deliver its result when it comes.
		go func() { hn.q <- <-done }()
	}
}

func (hn *harness) deliver(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			hn.run(c)
		}
	default:
		if isTimer(msg) {
			return
		}
		_, cmd := hn.m.Update(msg)
		hn.run(cmd)
	}
	hn.drain()
}

// drain feeds messages sent by background goroutines (job output).
func (hn *harness) drain() {
	for {
		select {
		case msg := <-hn.q:
			if isTimer(msg) {
				continue
			}
			_, cmd := hn.m.Update(msg)
			hn.run(cmd)
		default:
			return
		}
	}
}

func (hn *harness) send(msg tea.Msg) { hn.deliver(msg) }

func (hn *harness) gotoView(name string) {
	hn.t.Helper()
	for i, v := range hn.m.views {
		if v.name() == name {
			hn.run(hn.m.switchView(i))
			hn.drain()
			return
		}
	}
	hn.t.Fatalf("no view %q", name)
}

// isTimer reports messages of periodic commands (cursor blink, spinner),
// which would otherwise reschedule themselves forever.
func isTimer(msg tea.Msg) bool {
	switch msg.(type) {
	case tickMsg, toastExpiredMsg:
		return true
	}
	switch fmt.Sprintf("%T", msg) {
	case "spinner.TickMsg", "cursor.BlinkMsg", "cursor.initialBlinkMsg":
		return true
	}
	return false
}

func (hn *harness) keys(ks ...string) {
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		case "ctrl+k":
			msg = tea.KeyMsg{Type: tea.KeyCtrlK}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		hn.send(msg)
	}
}

func (hn *harness) typeText(s string) {
	for _, r := range s {
		if r == ' ' {
			hn.send(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
			continue
		}
		hn.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// screen renders the model and checks the frame geometry.
func (hn *harness) screen() string {
	hn.t.Helper()
	// Let late command results (slow loads, job output) arrive.
	for range 5 {
		time.Sleep(10 * time.Millisecond)
		hn.drain()
	}
	out := hn.m.View()
	lines := strings.Split(out, "\n")
	if len(lines) != hn.m.h {
		hn.t.Fatalf("frame has %d lines, want %d", len(lines), hn.m.h)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > hn.m.w {
			hn.t.Fatalf("line %d is %d cells wide (> %d): %q", i, w, hn.m.w, ansi.Strip(l))
		}
	}
	return ansi.Strip(out)
}

func (hn *harness) mustSee(parts ...string) {
	hn.t.Helper()
	s := hn.screen()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			hn.t.Fatalf("screen does not contain %q:\n%s", p, s)
		}
	}
}

func TestEveryViewRendersAtCommonSizes(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 60}} {
		hn := newHarness(t, sz[0], sz[1])
		for _, v := range hn.m.views {
			hn.gotoView(v.name())
			hn.screen()
		}
		hn.gotoView("Tasks")
		hn.keys("enter")
		for range tabNames {
			hn.screen()
			hn.keys("tab")
		}
		hn.keys("ctrl+k")
		hn.screen()
		hn.keys("esc", "?")
		hn.screen()
	}
}

func TestTinyTerminalShowsHint(t *testing.T) {
	hn := newHarness(t, 40, 10)
	if !strings.Contains(hn.screen(), "Terminal too small") {
		t.Fatal("expected size hint")
	}
}

func TestTaskListAndDetail(t *testing.T) {
	hn := newHarness(t, 140, 40)
	hn.mustSee("BoundedCode", "payments", "t-0001", "t-0002", "blocked", "Rename the order.created")
	hn.keys("f") // filter: active
	hn.mustSee("t-0003")
	if strings.Contains(hn.screen(), "t-0001  ") {
		t.Fatal("filter did not hide blocked task")
	}
	hn.keys("esc", "enter")
	hn.mustSee("t-0001", "Budget", "Attempts", "Worktrees", "agent/t-0001", "consumers compile")
	hn.keys("tab")
	hn.mustSee("Attempt 1", "$ go test ./...", "str_replace /work/orders/event.go", "verification orders (targeted) failed: go-test", "Run the tests first")
	hn.keys("tab")
	hn.mustSee("■ orders  +1 -1", "+    AmountCents int", "-    Total int")
	hn.keys("tab")
	hn.mustSee("go-test", "--- FAIL: TestX")
	hn.keys("tab")
	hn.mustSee("Z2", "repeated failures", "changed code: true")
	hn.keys("esc")
	hn.mustSee("TRIES")
}

func TestMissingTaskShowsError(t *testing.T) {
	hn := newHarness(t, 120, 30)
	hn.run(hn.m.openTask("missing"))
	hn.mustSee("task not found")
}

func TestRunFormStartsRunWithFlags(t *testing.T) {
	hn := newHarness(t, 140, 45)
	hn.keys("r") // t-0001 is blocked on SPEC_AMBIGUOUS: the form asks for a clarification
	hn.mustSee("Resume t-0001", "Clarification", "which consumers?")
	hn.typeText("only billing")
	hn.keys("tab", "right") // model: first profile
	hn.keys("tab", "right") // serena: on
	hn.keys("tab", "space") // pre-approve
	hn.keys("ctrl+s")
	got := strings.Join(hn.be.lastExec(), " ")
	want := "task run t-0001 --clarify only billing --model qwen3.6 --serena on --approve-frontier"
	if got != want {
		t.Fatalf("exec args = %q, want %q", got, want)
	}
	if hn.m.detail.tab != tabActivity || !hn.m.detail.open {
		t.Fatal("run should open the task's activity")
	}
}

func TestNewTaskFormCreatesAndRuns(t *testing.T) {
	hn := newHarness(t, 140, 50)
	hn.keys("n")
	hn.mustSee("New task", "Workspace", "orders", "billing")
	hn.keys("tab")
	hn.typeText("Add retries")
	hn.keys("tab", "space") // select orders
	hn.keys("ctrl+s")
	if len(hn.be.created) != 1 {
		t.Fatalf("created %d tasks", len(hn.be.created))
	}
	req := hn.be.created[0]
	if req.Workspace != "payments" || req.Request != "Add retries" || len(req.Repos) != 1 || req.Repos[0] != "orders" {
		t.Fatalf("request = %+v", req)
	}
	if got := strings.Join(hn.be.lastExec(), " "); got != "task run t-0009" {
		t.Fatalf("exec = %q", got)
	}
}

func TestFormRequiresRequest(t *testing.T) {
	hn := newHarness(t, 140, 50)
	hn.keys("n", "ctrl+s")
	hn.mustSee("Request is required")
	if len(hn.be.created) != 0 {
		t.Fatal("created a task without a request")
	}
}

func TestCancelAsksFirst(t *testing.T) {
	hn := newHarness(t, 120, 40)
	hn.keys("c")
	hn.mustSee("Cancel task t-0001?")
	hn.keys("n")
	if hn.be.lastExec() != nil {
		t.Fatal("cancel ran without confirmation")
	}
	hn.keys("c", "y")
	if got := strings.Join(hn.be.lastExec(), " "); got != "task cancel t-0001" {
		t.Fatalf("exec = %q", got)
	}
}

func TestPaletteRunsCommands(t *testing.T) {
	hn := newHarness(t, 120, 40)
	hn.keys("ctrl+k")
	hn.typeText("go to stats")
	hn.keys("enter")
	if hn.m.current().name() != "Stats" {
		t.Fatalf("view = %s", hn.m.current().name())
	}
	hn.mustSee("Local-only rate", "100%")
}

func TestConsoleRunsCommandAndShowsOutput(t *testing.T) {
	hn := newHarness(t, 120, 40)
	hn.be.execOut = "hello from the cli\n"
	hn.gotoView("Console")
	hn.typeText(`boundedcode intel search "order created"`)
	hn.keys("enter")
	if got := hn.be.lastExec(); len(got) != 3 || got[2] != "order created" {
		t.Fatalf("exec = %q", got)
	}
	hn.mustSee("hello from the cli", "intel search")
}

func TestPromptFromBackgroundIsAnswered(t *testing.T) {
	hn := newHarness(t, 120, 40)
	res := make(chan bool, 1)
	go func() { res <- hn.be.prompter(context.Background(), "Frontier escalation Z2", "Send it?") }()
	msg := <-hn.q
	hn.send(msg)
	hn.mustSee("Frontier escalation Z2", "Send it?")
	hn.keys("y")
	select {
	case ok := <-res:
		if !ok {
			t.Fatal("answer lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt not answered")
	}
}

func TestQuitWithRunningJobAsks(t *testing.T) {
	hn := newHarness(t, 120, 40)
	hn.be.execWait = make(chan struct{})
	defer close(hn.be.execWait)
	hn.gotoView("Console")
	hn.typeText("doctor")
	hn.keys("enter")
	if hn.m.runningJobs() != 1 {
		t.Fatalf("running = %d", hn.m.runningJobs())
	}
	hn.keys("ctrl+c")
	hn.mustSee("Quit BoundedCode?", "1 operation(s)")
}

func TestJobOutputLines(t *testing.T) {
	j := &job{}
	j.append("a\nb")
	j.append("c\rprogress 50%\rprogress 100%\n")
	got := strings.Join(j.output(), "|")
	if got != "a|bcprogress 50%|progress 100%"[:0]+"a|progress 100%" {
		t.Fatalf("output = %q", got)
	}
}

func TestSplitArgs(t *testing.T) {
	for in, want := range map[string]string{
		`task create "add retries" -c 'a b'`: `task|create|add retries|-c|a b`,
		`a\ b c`:                             `a b|c`,
		`  x   y  `:                          `x|y`,
	} {
		got, err := splitArgs(in)
		if err != nil || strings.Join(got, "|") != want {
			t.Errorf("splitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := splitArgs(`"open`); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestFuzzyScore(t *testing.T) {
	if fuzzyScore("gts", "Go to Stats") < 0 {
		t.Fatal("subsequence not matched")
	}
	if fuzzyScore("xyz", "Go to Stats") >= 0 {
		t.Fatal("non-match matched")
	}
	if fuzzyScore("stats", "Go to Stats") <= fuzzyScore("stats", "Stats for 7 days and some other words") {
		t.Fatal("shorter match should rank higher")
	}
}

func TestJobRenderedIsIncrementalAndWidthAware(t *testing.T) {
	j := &job{}
	j.append("first line that is fairly long\nsecond\n")
	a := j.rendered(10)
	j.append("third\npart")
	b := j.rendered(10)
	if len(b) <= len(a) || ansi.Strip(b[len(b)-1]) != "part" || ansi.Strip(b[len(b)-2]) != "third" {
		t.Fatalf("incremental render = %q", b)
	}
	if !slicesEqual(b[:len(a)], a) {
		t.Fatal("cached prefix changed")
	}
	wide := j.rendered(80)
	if ansi.Strip(wide[0]) != "first line that is fairly long" {
		t.Fatalf("width change not re-wrapped: %q", wide[0])
	}
	for range maxJobLines + 10 {
		j.append("x\n")
	}
	if got := j.rendered(80); len(got) > maxJobLines+2 {
		t.Fatalf("rendered %d lines after trimming", len(got))
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestArrowKeysNavigateSidebarAndViews(t *testing.T) {
	hn := newHarness(t, 140, 40)
	hn.keys("down", "down")
	if hn.m.tasks.cur.pos != 2 {
		t.Fatalf("list cursor = %d, want 2", hn.m.tasks.cur.pos)
	}
	hn.keys("up")
	if hn.m.tasks.cur.pos != 1 {
		t.Fatalf("list cursor = %d, want 1", hn.m.tasks.cur.pos)
	}
	hn.keys("left") // into the sidebar
	hn.mustSee("switch view")
	hn.keys("down", "down") // Workspaces, Intel
	if hn.m.current().name() != "Intel" {
		t.Fatalf("view = %s, want Intel", hn.m.current().name())
	}
	hn.keys("up", "up", "up", "up") // Workspaces, Tasks, Chat, wraps to Console
	if hn.m.current().name() != "Console" {
		t.Fatalf("view = %s, want Console", hn.m.current().name())
	}
	hn.keys("down", "down", "down", "right") // Chat, Tasks, Workspaces, back into the view
	if hn.m.sideFocus || hn.m.current().name() != "Workspaces" {
		t.Fatalf("sideFocus=%v view=%s", hn.m.sideFocus, hn.m.current().name())
	}
	hn.keys("right") // repositories pane: ← now belongs to the view
	hn.keys("left")
	if hn.m.sideFocus || hn.m.workspace.focus != 0 {
		t.Fatal("← in the repositories pane should return to workspaces, not the sidebar")
	}
	// In a task, ←/→ switch tabs.
	hn.gotoView("Tasks")
	hn.keys("enter", "right")
	if hn.m.detail.tab != tabActivity || hn.m.sideFocus {
		t.Fatalf("tab = %d sideFocus = %v", hn.m.detail.tab, hn.m.sideFocus)
	}
}

func (hn *harness) say(text string) {
	hn.typeText(text)
	hn.keys("enter")
}

func (hn *harness) execs() []string {
	hn.be.mu.Lock()
	defer hn.be.mu.Unlock()
	var out []string
	for _, e := range hn.be.execs {
		out = append(out, strings.Join(e, " "))
	}
	return out
}

func TestChatMessageCreatesAndRunsTask(t *testing.T) {
	hn := newChatHarness(t, 140, 40, nil)
	hn.mustSee("coding agent", "/src/orders ⎇ main", "Describe a change")
	hn.say("add retries to the client")
	if len(hn.be.created) != 1 || hn.be.created[0].Workspace != "payments" || hn.be.created[0].FromTask != "" {
		t.Fatalf("created = %+v", hn.be.created)
	}
	if got := hn.be.lastExec(); strings.Join(got, " ") != "task run t-0009" {
		t.Fatalf("exec = %q", got)
	}
	hn.mustSee("❯ add retries to the client", "Task t-0009", "$ go test ./...", "str_replace /work/orders/event.go")
}

func TestChatFollowUpStartsFromCompletedTask(t *testing.T) {
	hn := newChatHarness(t, 140, 40, nil)
	hn.say("add retries")
	hn.be.setStatus("t-0009", task.StatusCompleted)
	hn.run(hn.m.chat.loadTask(hn.m, "t-0009"))
	hn.run(hn.m.chat.loadDiff(hn.m, "t-0009", false))
	hn.mustSee("✔ Done", "1 file(s) changed", "/apply")
	hn.say("also log each retry")
	if len(hn.be.created) != 2 || hn.be.created[1].FromTask != "t-0009" {
		t.Fatalf("created = %+v", hn.be.created)
	}
	if got := strings.Join(hn.be.lastExec(), " "); got != "task run t-0010" {
		t.Fatalf("exec = %q", got)
	}
}

func TestChatAnswersAnAmbiguousTask(t *testing.T) {
	hn := newChatHarness(t, 140, 40, nil)
	hn.say("rename the field")
	hn.mustSee("The request is ambiguous", "which consumers?")
	hn.say("only billing")
	if got := strings.Join(hn.be.lastExec(), " "); got != "task run t-0009 --clarify only billing" {
		t.Fatalf("exec = %q", got)
	}
	if len(hn.be.created) != 1 {
		t.Fatal("an answer must not create a new task")
	}
}

func TestChatQueuesWhileRunningAndEscInterrupts(t *testing.T) {
	be := defaultBackend()
	be.execWait = make(chan struct{})
	hn := newChatHarness(t, 140, 40, be)
	hn.say("add retries")
	hn.mustSee("Working", "esc to interrupt")
	hn.say("and a test")
	hn.mustSee("Queued")
	if len(hn.execs()) != 1 {
		t.Fatalf("execs = %q", hn.execs())
	}
	hn.keys("esc") // interrupts the run; the queued message is sent next
	for range 20 {
		if len(hn.execs()) == 2 {
			break
		}
		hn.screen()
	}
	ex := hn.execs()
	if len(ex) != 2 || ex[1] != "task run t-0009 --clarify and a test" {
		t.Fatalf("execs = %q", ex)
	}
	close(be.execWait)
}

func TestChatRequiresSetupAndRunsIt(t *testing.T) {
	be := defaultBackend()
	be.setup = append(be.setup, SetupStep{Name: "model", Title: "Default model weights", Detail: "qwen not downloaded"})
	hn := newChatHarness(t, 140, 40, be)
	hn.mustSee("Setup needed", "Default model weights", "/setup")
	hn.say("do something")
	hn.mustSee("setup is not complete")
	if len(be.created) != 0 {
		t.Fatal("task created before setup")
	}
	hn.say("/setup")
	hn.mustSee("Where should the agent's model run?")
}

func TestChatOutsideGitOffersInit(t *testing.T) {
	be := defaultBackend()
	be.project = Project{Dir: "/tmp/plain"}
	hn := newChatHarness(t, 140, 40, be)
	hn.mustSee("not a git repository", "/git-init")
	hn.say("hello")
	hn.mustSee("Can't start a task")
	hn.say("/git-init")
	hn.mustSee("Make /src/orders a git repository?")
	hn.keys("y")
	if len(be.gitInits) != 1 {
		t.Fatal("git init not run")
	}
	hn.mustSee("Registered /src/orders as workspace fresh")
}

func TestChatSlashCommands(t *testing.T) {
	hn := newChatHarness(t, 140, 40, nil)
	hn.typeText("/ap")
	hn.mustSee("/apply", "bring the current task's changes")
	hn.keys("esc")
	hn.say("/apply")
	hn.mustSee("No task yet")
	hn.say("add retries")
	hn.say("/diff")
	hn.mustSee("■ orders", "+    AmountCents int")
	hn.say("/apply")
	hn.mustSee("Apply t-0009 to your checkout?", "NOT") // the fake task is blocked
	hn.keys("y")
	if got := strings.Join(hn.be.lastExec(), " "); got != "task apply t-0009 --force" {
		t.Fatalf("exec = %q", got)
	}
	hn.say("/help")
	hn.mustSee("Talk to BoundedCode", "/git-init")
	hn.say("/nope")
	hn.mustSee("Unknown command /nope")
}

func wizardBackend() *fakeBackend {
	be := defaultBackend()
	be.hw = HardwareInfo{Summary: "linux/amd64, 64 GB RAM, RTX 4060 (8 GB, CUDA)", Recommended: "qwen3.6"}
	be.providers = []ProviderRow{{Name: "local", Model: "qwen3.6", Selected: true}, {Name: "openai"}, {Name: "anthropic"}, {Name: "gemini"}, {Name: "openai-compatible"}}
	be.pmodels = []ProviderModel{{ID: "claude-x", Display: "Claude X", ContextWindow: 1000000}, {ID: "claude-y", Display: "Claude Y"}}
	return be
}

// TestWizardLocal: /setup → local → the suggested model → confirm → `model
// use` → confirm → `setup --yes`.
func TestWizardLocal(t *testing.T) {
	be := wizardBackend()
	hn := newChatHarness(t, 120, 40, be)
	hn.say("/setup")
	hn.mustSee("Where should the agent's model run?", "This machine: linux/amd64", "Suggested here:", "qwen3.6.")
	hn.keys("ctrl+s")
	hn.mustSee("Local model", "validated", "GPU + RAM", "suggested", "fits the GPU")
	if strings.Contains(hn.screen(), "laguna") {
		t.Fatal("a profile under license review is offered")
	}
	hn.keys("ctrl+s")
	hn.mustSee("Set up the local model")
	hn.keys("y")
	if got := strings.Join(be.lastExec(), " "); !strings.HasPrefix(got, "model use ") {
		t.Fatalf("exec = %q", got)
	}
	hn.mustSee("Install what is missing?")
	hn.keys("y")
	if got := strings.Join(be.lastExec(), " "); got != "setup --yes" {
		t.Fatalf("exec = %q", got)
	}
}

// TestWizardCloud: cloud → Anthropic → key (masked, never in an Exec) →
// the provider's models → `provider use` → test → `setup --yes`.
func TestWizardCloud(t *testing.T) {
	be := wizardBackend()
	hn := newChatHarness(t, 120, 40, be)
	hn.say("/setup")
	hn.keys("down", "ctrl+s") // "A cloud model API"
	hn.mustSee("Cloud provider", "Anthropic", "billed per token")
	hn.keys("down") // openai → anthropic
	hn.keys("tab", "tab")
	hn.typeText("sk-ant-secret-value-123456")
	if strings.Contains(hn.screen(), "sk-ant-secret") {
		t.Fatal("API key shown on screen")
	}
	hn.keys("ctrl+s")
	if be.keys["anthropic"] != "sk-ant-secret-value-123456" {
		t.Fatalf("stored keys = %v", be.keys)
	}
	hn.mustSee("Cloud model", "claude-x", "context 1000000 tokens")
	hn.keys("ctrl+s")
	exec := strings.Join(be.lastExec(), " ")
	if exec != "provider use anthropic --model claude-x" {
		t.Fatalf("exec = %q", exec)
	}
	hn.mustSee("Install what is missing?")
	hn.keys("y")
	if got := strings.Join(be.lastExec(), " "); got != "setup --yes" {
		t.Fatalf("exec = %q", got)
	}
	for _, e := range be.execs {
		if strings.Contains(strings.Join(e, " "), "secret") {
			t.Fatalf("key passed to a command: %v", e)
		}
	}
}

// TestWizardValidation: an OpenAI-compatible service needs its URL, and
// models of providers that do not report a context window need one.
func TestWizardValidation(t *testing.T) {
	be := wizardBackend()
	hn := newChatHarness(t, 120, 40, be)
	hn.say("/setup")
	hn.keys("down", "ctrl+s")
	hn.keys("down", "down", "down") // openai-compatible
	hn.keys("tab", "tab")
	hn.typeText("key-0123456789")
	hn.keys("ctrl+s")
	hn.mustSee("needs its API URL")
	hn.keys("shift+tab")
	hn.typeText("https://api.example.com/v1")
	hn.keys("ctrl+s")
	hn.mustSee("Cloud model")
	hn.keys("down") // claude-y: no reported context window
	hn.keys("ctrl+s")
	hn.mustSee("enter the model's context window")
	hn.keys("tab", "tab")
	hn.typeText("131072")
	hn.keys("ctrl+s")
	if got := strings.Join(be.lastExec(), " "); got != "provider use openai-compatible --model claude-y --base-url https://api.example.com/v1 --context-window 131072" {
		t.Fatalf("exec = %q", got)
	}
}

// TestWizardFitsSmallTerminals renders every wizard step at the smallest
// supported common size (screen() checks the frame's exact size).
func TestWizardFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {200, 60}} {
		be := wizardBackend()
		hn := newChatHarness(t, size[0], size[1], be)
		hn.say("/setup")
		hn.screen()
		hn.keys("ctrl+s")
		hn.screen() // local models
		hn.keys("esc")
		hn.say("/setup")
		hn.keys("down", "ctrl+s")
		hn.screen() // providers
		hn.keys("down", "tab", "tab")
		hn.typeText("k-0123456789")
		hn.keys("ctrl+s")
		hn.screen() // cloud models
	}
}

// TestRuntimeModelActions: use, download (after confirmation) and delete
// the selected model from the Runtime view.
func TestRuntimeModelActions(t *testing.T) {
	be := wizardBackend()
	hn := newChatHarness(t, 140, 40, be)
	hn.gotoView("Runtime")
	hn.keys("down") // "small": not downloaded
	hn.keys("u")
	if got := strings.Join(be.lastExec(), " "); got != "model use small" {
		t.Fatalf("exec = %q", got)
	}
	hn.keys("d")
	hn.mustSee("Download model", "small")
	hn.keys("y")
	if got := strings.Join(be.lastExec(), " "); got != "model fetch small --yes" {
		t.Fatalf("exec = %q", got)
	}
	hn.keys("up", "x") // qwen3.6: downloaded
	hn.mustSee("Delete model weights?")
	hn.keys("y")
	if got := strings.Join(be.lastExec(), " "); got != "model remove qwen3.6 --yes" {
		t.Fatalf("exec = %q", got)
	}
}
