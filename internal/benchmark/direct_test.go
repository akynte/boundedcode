package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
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
	if b, err := os.ReadFile(mk.AgentPatch); err != nil || !strings.Contains(string(b), "+hi") {
		t.Fatalf("agent patch: %v %s", err, b)
	}
	if w := byID["wrong"]; w.Success || len(w.FailedChecks) != 1 || !strings.Contains(w.FailedChecks[0], "does not match") {
		t.Fatalf("wrong: %+v", w)
	}
	if s := rep.Summary; s.CacheHitRate < 0.8 || s.MeanCalls < 1.3 {
		t.Fatalf("rates: %+v", s)
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

// The shipped suite loads and every task fits the direct runner.
func TestDirectSuiteDefinitions(t *testing.T) {
	tasks, err := LoadTasks(filepath.Join("..", "..", "benchmarks", "direct"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 12 {
		t.Fatalf("%d tasks", len(tasks))
	}
	cats := map[string]int{}
	for _, task := range tasks {
		if len(task.Repos) != 1 {
			t.Errorf("%s: %d repos", task.ID, len(task.Repos))
		}
		if task.Category == "" || task.Timeout <= 0 {
			t.Errorf("%s: category and timeout are required", task.ID)
		}
		cats[task.Category]++
	}
	for _, c := range []string{"explain", "locate", "diagnose", "small-fix", "add-test", "refactor", "feature", "large-locate", "large-change"} {
		if cats[c] == 0 {
			t.Errorf("no %s task", c)
		}
	}
}
