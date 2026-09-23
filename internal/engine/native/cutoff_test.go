package native_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/workflow"
)

// The recorded failure: an edit_file cut off at the output limit arrived with
// unterminated JSON arguments, the transcript holding them could not be
// saved, and the task ended as an environment failure. A cut-off call is
// answered, not executed, and the attempt goes on.
func TestACutOffToolCallIsAnsweredNotPersistedRaw(t *testing.T) {
	const original = "package calc\n\nfunc Add(x, y int) int { return x - y }\n"
	wt := worktree(t, map[string]string{"calc.go": original})
	p := newScripted(
		&llm.ChatResponse{FinishReason: "length", ToolCalls: []llm.ToolCall{{
			ID: "1", Name: "edit_file",
			Arguments: json.RawMessage(`{"path":"calc.go","old":"return x - y","new":"return x + y // and a long`),
		}}},
		call("2", "edit_file", map[string]any{"path": "calc.go", "old": "return x - y", "new": "return x + y"}),
		call("3", "done", map[string]any{"summary": "fixed"}),
	)
	e := newEngine(t, p)
	tr := &workflow.Transcript{}
	save := func(_ context.Context, tr *workflow.Transcript) error {
		_, err := json.Marshal(tr) // what persistence does
		return err
	}
	resp, err := e.Step(context.Background(), engine.Request{Access: firewall.Access{WriteScope: []string{"."}},
		TaskID: "t1", Objective: "fix Add", Worktree: wt, Attempt: 1, Transcript: tr, SaveTranscript: save})
	if err != nil {
		t.Fatalf("a cut-off call ended the attempt: %v", err)
	}
	if !resp.ClaimsDone {
		t.Fatalf("the attempt did not continue past the cut-off call: %+v", resp)
	}
	var note string
	for _, m := range tr.Messages {
		if m.Role == "tool" && m.ToolCallID == "1" {
			note = m.Content
		}
	}
	if !strings.Contains(note, "Not executed") || !strings.Contains(note, "output limit") {
		t.Errorf("the model was not told its call was cut off: %q", note)
	}
	body, _ := os.ReadFile(filepath.Join(wt, "calc.go"))
	if !strings.Contains(string(body), "x + y") || strings.Contains(string(body), "and a long") {
		t.Errorf("the worktree is not what the complete edit made:\n%s", body)
	}
	if tr.InvalidCalls != 1 {
		t.Errorf("invalid calls = %d, want 1", tr.InvalidCalls)
	}
}

// A reply cut off at the output limit before any tool call was the model
// interrupted, not the model finishing. The recorded attempt ended on
// "Actually this is a bit awkward… let me think about whether Memory can be
// made to fail." It is asked to act, and does.
func TestAReplyCutOffBeforeAToolCallDoesNotEndTheAttempt(t *testing.T) {
	wt := worktree(t, map[string]string{"calc.go": "package calc\n\nfunc Add(x, y int) int { return x - y }\n"})
	p := newScripted(
		&llm.ChatResponse{FinishReason: "length", Content: "The sign is wrong. Actually, let me think about"},
		call("1", "edit_file", map[string]any{"path": "calc.go", "old": "return x - y", "new": "return x + y"}),
		call("2", "done", map[string]any{"summary": "fixed"}),
	)
	e := newEngine(t, p)
	resp, err := e.Step(context.Background(), engine.Request{Access: firewall.Access{WriteScope: []string{"."}},
		TaskID: "t1", Objective: "fix Add", Worktree: wt, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.ClaimsDone || !resp.Edited {
		t.Fatalf("the attempt ended at the cut-off reply: %+v", resp)
	}
	var told bool
	for _, m := range p.lastRequest(t).Messages {
		told = told || (m.Role == "user" && strings.Contains(m.Content, "reached the output limit"))
	}
	if !told {
		t.Error("the model was not told its reply was cut off")
	}
}

// And it stays bounded: a model that keeps running out is stopped.
func TestRepeatedCutOffRepliesStillEndTheAttempt(t *testing.T) {
	wt := worktree(t, map[string]string{"calc.go": "package calc\n"})
	cut := func() *llm.ChatResponse { return &llm.ChatResponse{FinishReason: "length", Content: "thinking"} }
	p := newScripted(cut(), cut(), cut(), call("1", "done", map[string]any{"summary": "never reached"}))
	e := newEngine(t, p)
	resp, err := e.Step(context.Background(), engine.Request{Access: firewall.Access{WriteScope: []string{"."}},
		TaskID: "t1", Objective: "fix Add", Worktree: wt, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ClaimsDone {
		t.Error("the attempt went on past the bound on cut-off replies")
	}
}
