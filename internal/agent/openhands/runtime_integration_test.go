package openhands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// fakeLLM scripts tool calls: for each user message it runs one terminal
// command taken from the message (after "RUN:"), then calls finish.
type fakeLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	last := req.Messages[len(req.Messages)-1]
	var msg map[string]any
	if last.Role == "user" {
		text := contentText(last.Content)
		cmd := "true"
		if _, after, ok := strings.Cut(text, "RUN:"); ok {
			cmd = strings.TrimSpace(after)
		}
		args, _ := json.Marshal(map[string]any{"command": cmd})
		msg = toolCall("terminal", string(args))
	} else {
		msg = toolCall("finish", `{"message":"done"}`)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "x", "object": "chat.completion", "model": "fake",
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "tool_calls"}},
		"usage":   map[string]int{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
	})
}

// contentText decodes OpenAI message content (a string or a list of parts).
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func toolCall(name, args string) map[string]any {
	return map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
		"id": fmt.Sprintf("call_%d", time.Now().UnixNano()), "type": "function",
		"function": map[string]any{"name": name, "arguments": args}}}}
}

// TestAdapterRunAndResume exercises the real OpenHands SDK through the
// adapter with a scripted LLM: run a tool, close the process, resume the
// same conversation in a new process, and continue. Requires uv and the
// adapter environment; skipped otherwise.
func TestAdapterRunAndResume(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	adapterDir, _ := filepath.Abs("../../../adapters/openhands/python")
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	if _, err := os.Stat(filepath.Join(adapterDir, ".venv")); err != nil {
		t.Skip("adapter venv missing; run `uv sync` in " + adapterDir)
	}
	llm := &fakeLLM{}
	srv := httptest.NewServer(llm)
	defer srv.Close()

	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	persist := filepath.Join(tmp, "persist")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	gw := &inference.Gateway{Client: inference.NewClient(srv.URL, time.Minute), Model: "fake"}
	rt := &Runtime{
		Sandbox: sandbox.None{},
		Argv:    []string{"uv", "run", "--frozen", "--project", adapterDir, "bc-openhands-adapter"},
		Gateway: func(string) *inference.Gateway { return gw },
		LogDir:  tmp,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var events []agent.Event
	var evMu sync.Mutex
	req := agent.OpenRequest{TaskID: "t1", Workspace: ws, PersistenceDir: persist, MaxIterations: 10,
		OnEvent: func(e agent.Event) { evMu.Lock(); events = append(events, e); evMu.Unlock() }}
	s, err := rt.Open(ctx, req)
	if err != nil {
		t.Fatal(adapterLog(tmp, err))
	}
	if s.Resumed() {
		t.Fatal("new session reported resumed")
	}
	res, err := s.Send(ctx, "RUN: echo first > first.txt")
	if err != nil {
		t.Fatal(adapterLog(tmp, err))
	}
	if res.Status != "finished" {
		t.Fatalf("status = %s (%+v)", res.Status, res)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "first.txt")); err != nil || strings.TrimSpace(string(b)) != "first" {
		t.Fatalf("first.txt: %q %v", b, err)
	}
	id := s.ID()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// New process, same conversation.
	req.SessionID = id
	s2, err := rt.Open(ctx, req)
	if err != nil {
		t.Fatal(adapterLog(tmp, err))
	}
	defer s2.Close()
	if !s2.Resumed() || s2.ID() != id {
		t.Fatalf("resume failed: resumed=%v id=%s want %s", s2.Resumed(), s2.ID(), id)
	}
	st, err := s2.State(ctx)
	if err != nil || st.EventCount < 4 {
		t.Fatalf("state after resume: %+v %v", st, err)
	}
	res, err = s2.Send(ctx, "RUN: cat first.txt > second.txt")
	if err != nil || res.Status != "finished" {
		t.Fatalf("second send: %+v %v", res, adapterLog(tmp, err))
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "second.txt")); strings.TrimSpace(string(b)) != "first" {
		t.Fatalf("second.txt = %q", b)
	}
	evMu.Lock()
	defer evMu.Unlock()
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"ActionEvent", "ObservationEvent"} {
		if !kinds[k] {
			t.Errorf("no %s events received (got %v)", k, kinds)
		}
	}
	if gw.Used() == 0 {
		t.Error("gateway metered no tokens")
	}
}

func adapterLog(dir string, err error) error {
	if err == nil {
		return nil
	}
	b, _ := os.ReadFile(filepath.Join(dir, "adapter.log"))
	s := string(b)
	if len(s) > 4000 {
		s = s[len(s)-4000:]
	}
	return fmt.Errorf("%w\n--- adapter.log ---\n%s", err, s)
}
