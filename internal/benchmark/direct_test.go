package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/inference"
)

// fakeModel answers like a model that reads the last message: a question
// gets an answer, a request to create a file gets a write call and then a
// confirmation.
type fakeModel struct{}

func (fakeModel) Provider() string { return "local" }

func (fakeModel) Complete(_ context.Context, body map[string]any) (int, []byte, error) {
	msgs, _ := body["messages"].([]map[string]any)
	last := msgs[len(msgs)-1]
	text := "The debit side goes to the settlement account."
	msg := map[string]any{"role": "assistant", "content": text}
	if last["role"] == "tool" {
		msg["content"] = "Created hello.txt."
	} else if c, _ := last["content"].(string); strings.Contains(c, "Create") {
		args, _ := json.Marshal(map[string]any{"path": "hello.txt", "content": "hi\n"})
		msg["content"] = ""
		msg["tool_calls"] = []map[string]any{{"id": "w1", "type": "function", "function": map[string]any{"name": "write", "arguments": string(args)}}}
	}
	out, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 10},
		"timings": map[string]any{"cache_n": 100, "prompt_n": 20, "predicted_n": 10},
	})
	return http.StatusOK, out, nil
}

func TestDirectSuiteScoresAnswersAndChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	fixtures := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixtures, "f", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "f", "app", "note.txt"), []byte("settlement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks := []TaskSpec{
		{ID: "ask", Category: "explain", Fixture: "f", Repos: []string{"app"}, Request: "What account gets the debit?", Expect: []string{"settlement"}},
		{ID: "make", Category: "small-fix", Fixture: "f", Repos: []string{"app"}, Request: "Create hello.txt with hi in it",
			Checks: []Check{{Repo: "app", Run: []string{"sh", "-c", "grep -q hi hello.txt"}}}},
		{ID: "wrong", Category: "explain", Fixture: "f", Repos: []string{"app"}, Request: "What is two plus two?", Expect: []string{"\\bfour\\b"}},
	}
	dr := &DirectRunner{FixturesDir: fixtures, WorkRoot: t.TempDir(), Settings: map[string]any{"approval": "auto"},
		NewRunner: func(_ context.Context, root, stateDir string) (*direct.Runner, func(), error) {
			sess, err := direct.NewSession(filepath.Join(stateDir, "sessions"), root, "local", "fake")
			if err != nil {
				return nil, nil, err
			}
			r := &direct.Runner{Gateway: &inference.Gateway{Upstream: fakeModel{}, Model: "fake"}, Session: sess,
				Opts: direct.Options{Root: root, Approval: direct.ApprovalAuto, Env: map[string]string{"platform": "test"}}}
			return r, func() { r.Close() }, nil
		}}
	out := filepath.Join(t.TempDir(), "report.json")
	rep, err := dr.Run(context.Background(), "fake", tasks, out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Tasks != 3 || rep.Summary.Passed != 2 || rep.Summary.Errors != 0 {
		t.Fatalf("summary: %+v", rep.Summary)
	}
	byID := map[string]DirectResult{}
	for _, r := range rep.Results {
		byID[r.ID] = r
	}
	ask := byID["ask"]
	if !ask.Success || ask.Mode != direct.ModeChat || ask.Calls != 1 || ask.ToolCalls != 0 || ask.PromptTokens != 120 || ask.CachedTokens != 100 || ask.GeneratedTokens != 10 {
		t.Fatalf("ask: %+v", ask)
	}
	mk := byID["make"]
	if !mk.Success || mk.Mode != direct.ModeCode || mk.Calls != 2 || mk.Tools["write"] != 1 || len(mk.FilesChanged) != 1 || mk.FilesChanged[0] != "hello.txt" || mk.AgentPatch == "" {
		t.Fatalf("make: %+v", mk)
	}
	if !strings.Contains(mk.AgentPatch, "+hi") {
		t.Fatalf("agent patch: %q", mk.AgentPatch)
	}
	home, _ := os.UserHomeDir()
	if b, _ := os.ReadFile(out); strings.Contains(string(b), home) || strings.Contains(string(b), dr.WorkRoot) {
		t.Fatal("the report must not contain machine-specific paths")
	}
	if w := byID["wrong"]; w.Success || len(w.FailedChecks) != 1 || !strings.Contains(w.FailedChecks[0], "does not match") {
		t.Fatalf("wrong: %+v", w)
	}
	if s := rep.Summary; s.CacheHitRate < 0.8 || s.MeanCalls < 1.3 {
		t.Fatalf("rates: %+v", s)
	}
	// Where the time and tokens went.
	if len(mk.CallLog) != 2 || mk.CallStats.Calls != 2 || mk.AgentSeconds <= 0 || mk.ToolTimes["write"].Calls != 1 || mk.ToolSeconds <= 0 {
		t.Fatalf("attribution: %+v", mk)
	}
	if c := mk.Context; c == nil || c.Counting != "estimate" || c.Sections["system"].Tokens == 0 || c.Sections["tool_schemas"].Bytes == 0 || c.PromptTokens != mk.CallLog[c.Call].Prompt {
		t.Fatalf("context breakdown: %+v", mk.Context)
	}
	if w := byID["wrong"]; w.FailureClass != "model" || mk.FailureClass != "" {
		t.Fatalf("failure classes: %q %q", w.FailureClass, mk.FailureClass)
	}
	if s := rep.Summary; s.Failures["model"] != 1 || s.MeanAgentSeconds <= 0 || s.FailedSeconds <= 0 || s.PassedSeconds <= 0 {
		t.Fatalf("summary attribution: %+v", s)
	}
	if rep.Environment.OS == "" || rep.Environment.CPUs == 0 || rep.Environment.Toolchains["go"] == "" {
		t.Fatalf("environment: %+v", rep.Environment)
	}
	if b, err := os.ReadFile(out); err != nil || !bytes.Contains(b, []byte(`"pass_rate"`)) {
		t.Fatalf("report file: %v", err)
	}
	var md bytes.Buffer
	if err := WriteDirectMarkdown(&md, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**2/3 passed (67%)**", "| ask | explain | chat | pass |", "| wrong | explain | chat | FAIL |", "does not match", "approval=auto"} {
		if !strings.Contains(md.String(), want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md.String())
		}
	}
	// Saved reports compare side by side.
	saved, err := LoadDirectReport(out)
	if err != nil {
		t.Fatal(err)
	}
	var cmp bytes.Buffer
	CompareDirect(&cmp, saved, rep)
	for _, want := range []string{"| passed | 2 | 2 |", "| ask | pass → pass |", "| wrong | FAIL → FAIL |"} {
		if !strings.Contains(cmp.String(), want) {
			t.Fatalf("comparison lacks %q:\n%s", want, cmp.String())
		}
	}
	// A task needs checks or expect; a bad expect pattern is refused.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("id: bad\nfixture: f\nrepos: [app]\nrequest: hi\nexpect: ['(']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(dir); err == nil || !strings.Contains(err.Error(), "expect") {
		t.Fatalf("bad expect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(fmt.Sprintf("id: ok\nfixture: f\nrepos: [app]\nrequest: hi\nexpect: ['%s']\n", "x")), 0o644); err != nil {
		t.Fatal(err)
	}
	if ts, err := LoadTasks(dir); err != nil || len(ts) != 1 || len(ts[0].Checks) != 0 {
		t.Fatalf("expect-only task: %v %+v", err, ts)
	}
}

// A session task sends its follow-up turns in the same session, routes
// each one, checks each turn's answer and runs the checks after the last.
func TestDirectSuiteRunsSessionTurns(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	fixtures := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixtures, "f", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "f", "app", "note.txt"), []byte("settlement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sessions []*direct.Session
	dr := &DirectRunner{FixturesDir: fixtures, WorkRoot: t.TempDir(),
		NewRunner: func(_ context.Context, root, stateDir string) (*direct.Runner, func(), error) {
			sess, err := direct.NewSession(filepath.Join(stateDir, "sessions"), root, "local", "fake")
			if err != nil {
				return nil, nil, err
			}
			sessions = append(sessions, sess)
			r := &direct.Runner{Gateway: &inference.Gateway{Upstream: fakeModel{}, Model: "fake"}, Session: sess,
				Opts: direct.Options{Root: root, Approval: direct.ApprovalAuto, Env: map[string]string{"platform": "test"}}}
			return r, func() { r.Close() }, nil
		}}
	session := TaskSpec{ID: "session", Category: "session", Fixture: "f", Repos: []string{"app"}, Request: "What account gets the debit?",
		Turns: []Turn{
			{Request: "Create hello.txt with hi in it", Expect: []string{"created"}},
			{Request: "Which account was it again?", Expect: []string{"settlement"}},
		},
		Checks: []Check{{Repo: "app", Run: []string{"sh", "-c", "grep -q hi hello.txt"}}}}
	failing := session
	failing.ID = "failing"
	failing.Turns = []Turn{{Request: "Which account was it again?", Expect: []string{"\\bpayments\\b"}}}
	failing.Checks = nil
	failing.Expect = []string{"settlement"} // the first answer's
	rep, err := dr.Run(context.Background(), "fake", []TaskSpec{session, failing}, "")
	if err != nil {
		t.Fatal(err)
	}
	ok, bad := rep.Results[0], rep.Results[1]
	if !ok.Success || ok.Turns != 3 || strings.Join(ok.TurnModes, ",") != "chat,code,chat" || ok.Calls != 4 || ok.Tools["write"] != 1 || len(ok.TurnAnswers) != 3 || ok.PeakPrompt != 120 {
		t.Fatalf("session: %+v", ok)
	}
	if !strings.Contains(ok.Answer, "settlement") {
		t.Fatalf("the answer is the last turn's: %q", ok.Answer)
	}
	// All turns ran in one session.
	if len(sessions) != 2 {
		t.Fatalf("%d sessions", len(sessions))
	}
	users := 0
	for _, m := range sessions[0].Messages {
		if m.Role == "user" {
			users++
		}
	}
	if users != 3 {
		t.Fatalf("%d user messages in the session", users)
	}
	if bad.Success || len(bad.FailedChecks) != 1 || !strings.HasPrefix(bad.FailedChecks[0], "turn 2: answer does not match") {
		t.Fatalf("failing: %+v", bad)
	}
	var md bytes.Buffer
	if err := WriteDirectMarkdown(&md, rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "| session | session | chat, code, chat | pass |") {
		t.Fatalf("markdown:\n%s", md.String())
	}
	// A turn without a request is refused.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte("id: t\nfixture: f\nrepos: [app]\nrequest: hi\nexpect: [x]\nturns:\n  - expect: [y]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(dir); err == nil || !strings.Contains(err.Error(), "turn 2 has no request") {
		t.Fatalf("empty turn: %v", err)
	}
}

// A task whose check commands are not installed is skipped and reported
// as an environment gap; a run where no task can run is refused.
func TestDirectSuiteSkipsTasksWithoutTheirToolchain(t *testing.T) {
	dr := &DirectRunner{NewRunner: func(context.Context, string, string) (*direct.Runner, func(), error) {
		t.Fatal("no task may run")
		return nil, nil, nil
	}}
	tasks := []TaskSpec{{ID: "a", Checks: []Check{{Run: []string{"no-such-command-xyz", "test"}}}}, {ID: "b", Checks: []Check{{Run: []string{"no-such-command-xyz"}}}}}
	if _, err := dr.Run(context.Background(), "m", tasks, ""); err == nil || err.Error() != "no task can run here: no-such-command-xyz not on PATH" {
		t.Fatalf("err = %v", err)
	}

	dr.NewRunner = func(context.Context, string, string) (*direct.Runner, func(), error) {
		return nil, nil, errors.New("no model here")
	}
	tasks = append(tasks, TaskSpec{ID: "c", Language: "go", Repos: []string{"app"}, Checks: []Check{{Run: []string{"go", "version"}}}})
	rep, err := dr.Run(context.Background(), "m", tasks, "")
	if err != nil {
		t.Fatal(err)
	}
	a, c := rep.Results[0], rep.Results[2]
	if a.Skipped != "no-such-command-xyz not on PATH" || a.FailureClass != "environment" {
		t.Fatalf("skipped task: %+v", a)
	}
	if c.Skipped != "" || c.Success || c.FailureClass == "" {
		t.Fatalf("runnable task: %+v", c)
	}
	if s := rep.Summary; s.Tasks != 1 || s.Skipped != 2 || s.Failures["environment (skipped)"] != 2 {
		t.Fatalf("summary: %+v", s)
	}
	if len(rep.ByLanguage) != 1 || rep.ByLanguage[0].Language != "go" {
		t.Fatalf("by language: %+v", rep.ByLanguage)
	}
}

// The shipped suite loads and every task fits the direct runner.
func TestDirectSuiteDefinitions(t *testing.T) {
	tasks, err := LoadTasks(filepath.Join("..", "..", "benchmarks", "direct"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 12 {
		t.Fatalf("%d tasks", len(tasks))
	}
	cats, langs := map[string]int{}, map[string]int{}
	for _, task := range tasks {
		if len(task.Repos) != 1 {
			t.Errorf("%s: %d repos", task.ID, len(task.Repos))
		}
		if task.Category == "" || task.Timeout <= 0 {
			t.Errorf("%s: category and timeout are required", task.ID)
		}
		cats[task.Category]++
		langs[task.Language]++
	}
	// Every language with a fixture has a change and a question.
	for _, l := range []string{"go", "python", "typescript", "rust", "java", "multi"} {
		if langs[l] < 2 {
			t.Errorf("%s: %d tasks", l, langs[l])
		}
	}
	if langs[""] > 0 {
		t.Errorf("%d tasks without a language", langs[""])
	}
	for _, c := range []string{"explain", "locate", "diagnose", "small-fix", "add-test", "refactor", "feature", "large-locate", "large-change", "session"} {
		if cats[c] == 0 {
			t.Errorf("no %s task", c)
		}
	}
}
