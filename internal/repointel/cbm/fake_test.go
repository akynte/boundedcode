package cbm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/jsonrpc"
)

// The test binary doubles as a fake codebase-memory-mcp when FAKE_CBM is
// set: `--version` prints FAKE_CBM_VERSION, `cli TOOL ARGS` answers
// {"via":"cli","tool":TOOL}, `config set` succeeds, and no arguments serve
// MCP over stdio, where the tool "hang" never answers and "crash" exits.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CBM") == "1" {
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

func fakeMain() int {
	args := os.Args[1:]
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Printf("codebase-memory-mcp %s\n", os.Getenv("FAKE_CBM_VERSION"))
		return 0
	case len(args) > 0 && args[0] == "config":
		return 0
	case len(args) >= 3 && args[0] == "cli":
		fmt.Printf(`{"via":"cli","tool":%q}`+"\n", args[2])
		return 0
	case len(args) > 0:
		return 2
	}
	p := jsonrpc.New(os.Stdin, os.Stdout)
	p.Handle("initialize", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": "2025-06-18"}, nil
	})
	p.Handle("tools/call", func(_ context.Context, raw json.RawMessage) (any, error) {
		var req struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &req)
		switch req.Name {
		case "hang":
			select {}
		case "crash":
			os.Exit(3)
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"via":"session","tool":"` + req.Name + `"}`}}}, nil
	})
	p.Serve()
	if os.Getenv("FAKE_CBM_IGNORE_EOF") == "1" {
		select {}
	}
	return 0
}

func fakeClient(t *testing.T, kv ...string) *Client {
	t.Helper()
	t.Setenv("FAKE_CBM", "1")
	for i := 0; i+1 < len(kv); i += 2 {
		t.Setenv(kv[i], kv[i+1])
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Binary: self, CacheDir: t.TempDir(), Timeout: 10 * time.Second}
}

func shortGrace(t *testing.T) {
	old := closeGrace
	closeGrace = 200 * time.Millisecond
	t.Cleanup(func() { closeGrace = old })
}

func openSession(t *testing.T, c *Client) *session {
	t.Helper()
	if err := c.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	b, err := c.Call(context.Background(), "ping", nil)
	if err != nil || !strings.Contains(string(b), `"session"`) {
		t.Fatalf("session call: %s %v", b, err)
	}
	return c.sess
}

func exited(s *session) bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func TestSessionHangTimesOut(t *testing.T) {
	shortGrace(t)
	c := fakeClient(t)
	s := openSession(t, c)
	c.Timeout = 300 * time.Millisecond
	t0 := time.Now()
	_, err := c.Call(context.Background(), "hang", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hang: %v", err)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Fatalf("hung call took %s", d)
	}
	if c.sess != nil || !exited(s) {
		t.Fatal("hung session not dropped and killed")
	}
	// Later calls use the CLI.
	c.Timeout = 10 * time.Second
	if b, err := c.Call(context.Background(), "search_graph", nil); err != nil || !strings.Contains(string(b), `"cli"`) {
		t.Fatalf("after hang: %s %v", b, err)
	}
}

func TestSessionCrashFallsBackToCLI(t *testing.T) {
	shortGrace(t)
	c := fakeClient(t)
	s := openSession(t, c)
	b, err := c.Call(context.Background(), "crash", nil)
	if err != nil || !strings.Contains(string(b), `{"via":"cli","tool":"crash"}`) {
		t.Fatalf("crash fallback: %s %v", b, err)
	}
	if c.sess != nil || !exited(s) {
		t.Fatal("dead session not dropped")
	}
	// A new session can be opened afterwards.
	openSession(t, c)
}

func TestCloseKillsUnresponsiveServer(t *testing.T) {
	shortGrace(t)
	c := fakeClient(t, "FAKE_CBM_IGNORE_EOF", "1")
	s := openSession(t, c)
	t0 := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 3*time.Second || !exited(s) {
		t.Fatalf("close took %s, exited=%v", d, exited(s))
	}
}

func TestCheckVersion(t *testing.T) {
	c := fakeClient(t, "FAKE_CBM_VERSION", RequiredVersion)
	if got, err := CheckVersion(context.Background(), c.Binary); err != nil || got != RequiredVersion {
		t.Fatalf("pinned: %q %v", got, err)
	}
	t.Setenv("FAKE_CBM_VERSION", "0.12.1")
	got, err := CheckVersion(context.Background(), c.Binary)
	if err == nil || got != "0.12.1" || !strings.Contains(err.Error(), RequiredVersion) {
		t.Fatalf("mismatch: %q %v", got, err)
	}
	t.Setenv("FAKE_CBM_VERSION", "unknown")
	if _, err := CheckVersion(context.Background(), c.Binary); err == nil {
		t.Fatal("unparseable version accepted")
	}
	if _, err := CheckVersion(context.Background(), "/nonexistent/codebase-memory-mcp"); err == nil {
		t.Fatal("missing binary accepted")
	}
}
