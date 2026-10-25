package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/direct"
)

// fakeOpenAI is an OpenAI-compatible server: health for the probe, and a
// scripted chat: first a read of README.md, then an answer.
func fakeOpenAI(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var reqs []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		reqs = append(reqs, body)
		n := len(reqs)
		mu.Unlock()
		msgs, _ := body["messages"].([]any)
		last, _ := msgs[len(msgs)-1].(map[string]any)
		var msg map[string]any
		if last["role"] == "tool" {
			parts := strings.SplitN(last["content"].(string), "\t", 2)
			text := "tool said: " + parts[0]
			if len(parts) == 2 {
				text = "The README says: " + strings.TrimSpace(parts[1])
			}
			msg = map[string]any{"role": "assistant", "content": text}
		} else if n == 1 {
			msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"../README.md"}`}}}}
		} else {
			msg = map[string]any{"role": "assistant", "content": "continued"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 50, "completion_tokens": 7}})
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestChatHeadless(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOUNDEDCODE_HOME", home)
	srv, reqs := fakeOpenAI(t)
	if _, err := runCLI(t, "", "setup", "--only", "config"); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config", "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Inference.Mode, cfg.Inference.ExternalURL = "external", srv.URL
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "README.md"), []byte("hello from the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("Be terse."), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir = proj
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	sub := filepath.Join(proj, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "", "chat", "-C", sub, "-p", "what does the README say?")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"· mode: chat (a question)", "⚙ read ../README.md", "The README says: hello from the project", "· session "} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if len(*reqs) != 2 {
		t.Fatalf("%d model calls", len(*reqs))
	}
	sys := (*reqs)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "Be terse.") || !strings.Contains(sys, "git branch") {
		t.Fatalf("system prompt:\n%s", sys)
	}
	if _, has := (*reqs)[0]["tools"]; !has {
		t.Fatal("no tools sent")
	}
	// The session was stored for the git root (not the subdirectory) and can be continued.
	out, err = runCLI(t, "", "chat", "-C", proj, "sessions")
	if err != nil || !strings.Contains(out, "what does the README say?") || strings.Count(out, "\n") != 1 {
		t.Fatalf("sessions: %v\n%s", err, out)
	}
	out, err = runCLI(t, "", "chat", "-C", sub, "--continue", "--mode", "code", "-p", "go on")
	if err != nil || !strings.Contains(out, "continued") {
		t.Fatalf("continue: %v\n%s", err, out)
	}
	last := (*reqs)[2]["messages"].([]any)
	if len(last) != 6 || last[5].(map[string]any)["content"] != "go on" { // system + 4 earlier + new
		t.Fatalf("continued history has %d messages", len(last))
	}
	// The transcript exports as Markdown, to stdout or a file.
	out, err = runCLI(t, "", "chat", "-C", proj, "export")
	if err != nil || !strings.Contains(out, "## User\n\nwhat does the README say?") || !strings.Contains(out, "- 🛠 `read`") {
		t.Fatalf("export: %v\n%s", err, out)
	}
	exp := filepath.Join(t.TempDir(), "t.md")
	if out, err = runCLI(t, "", "chat", "-C", proj, "export", "-o", exp); err != nil || !strings.Contains(out, "wrote "+exp) {
		t.Fatalf("export -o: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(exp); !strings.HasPrefix(string(b), "# what does the README say?") {
		t.Fatalf("exported file:\n%s", b)
	}
	// Root -p is the same thing; stdin is appended as context.
	out, err = runCLI(t, "some piped diff", "-p", "review this")
	if err != nil {
		// the current directory is the repository under test; it is a git root too
		t.Fatalf("root -p: %v\n%s", err, out)
	}
	// Model calls are metered under the direct source.
	app := &App{Out: os.Stdout, Err: os.Stderr, Log: newLogger(os.Stderr, false)}
	if err := app.load(); err != nil {
		t.Fatal(err)
	}
	defer app.close()
	st, err := app.Store(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT count(*) FROM model_calls WHERE source='direct'`).Scan(&n); err != nil || n < 3 {
		t.Fatalf("model_calls direct = %d %v", n, err)
	}
	if err := st.DB.QueryRow(`SELECT count(*) FROM events WHERE kind='direct.tool'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit events = %d %v", n, err)
	}
}

func TestChatWithoutMessage(t *testing.T) {
	t.Setenv("BOUNDEDCODE_HOME", t.TempDir())
	if _, err := runCLI(t, "", "chat"); err == nil || !strings.Contains(err.Error(), "no message") {
		t.Fatalf("err = %v", err)
	}
	if _, err := runCLI(t, "", "chat", "--approval", "sometimes", "-p", "x"); err == nil || !strings.Contains(err.Error(), "ask|edits|auto") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatRoutesToVerification(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOUNDEDCODE_HOME", home)
	srv, _ := fakeOpenAI(t)
	if _, err := runCLI(t, "", "setup", "--only", "config"); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config", "config.yaml")
	cfg, _ := config.Load(cfgPath)
	cfg.Inference.Mode, cfg.Inference.ExternalURL = "external", srv.URL
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "internal/auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "internal/auth/login.go"), []byte("package auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir = proj
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	var bounded []string
	prev, prevCaps := runBounded, capabilitiesFn
	runBounded = func(_ context.Context, _ *App, _, request, _ string) error {
		bounded = append(bounded, request)
		return nil
	}
	capabilitiesFn = func(context.Context, *App, string) direct.Capabilities {
		return direct.Capabilities{BoundedAvailable: true}
	}
	t.Cleanup(func() { runBounded, capabilitiesFn = prev, prevCaps })

	// Risky by keyword: straight to verification, no model call.
	out, err := runCLI(t, "", "chat", "-C", proj, "-p", "add retries to the payment client")
	if err != nil || !strings.Contains(out, "· mode: verify") || len(bounded) != 1 {
		t.Fatalf("verify route: %v\n%s\n%v", err, out, bounded)
	}
	// A forced mode wins.
	if out, err = runCLI(t, "", "chat", "-C", proj, "--mode", "code", "-p", "add retries to the payment client"); err != nil || !strings.Contains(out, "· mode: code (requested)") || len(bounded) != 1 {
		t.Fatalf("forced: %v\n%s", err, out)
	}
	// Verification unavailable: the risky request runs directly, with a reason.
	capabilitiesFn = func(context.Context, *App, string) direct.Capabilities {
		return direct.Capabilities{BoundedReason: "no sandbox"}
	}
	if out, err = runCLI(t, "", "chat", "-C", proj, "-p", "add retries to the payment client"); err != nil || !strings.Contains(out, "· mode: code (the change concerns payment; no sandbox, so the change is made directly)") {
		t.Fatalf("fallback: %v\n%s", err, out)
	}
	if len(bounded) != 1 {
		t.Fatal("bounded task created without a sandbox")
	}
	if _, err := runCLI(t, "", "chat", "-C", proj, "--mode", "verify", "-p", "x"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("forced verify without sandbox: %v", err)
	}
}
