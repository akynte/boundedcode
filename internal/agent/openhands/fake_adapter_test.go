package openhands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/jsonrpc"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// The test binary doubles as a fake adapter when BC_FAKE_ADAPTER is set, so
// runtime behaviour (handshake, cancellation, logging) is tested without
// Python or Docker. Modes:
//
//	ok         protocol v1; session.send blocks until session.interrupt
//	stubborn   protocol v1; session.send ignores session.interrupt
//	v0         ready without protocol_version
//	v99        ready with protocol_version 99
func TestMain(m *testing.M) {
	if mode := os.Getenv("BC_FAKE_ADAPTER"); mode != "" {
		fakeAdapter(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeAdapter(mode string) {
	fmt.Fprintln(os.Stderr, "fake adapter starting; api_key=sk-proj-abcdefghijklmnopqrstuvwxyz0123")
	if pf := os.Getenv("BC_FAKE_ADAPTER_PIDFILE"); pf != "" {
		_ = os.WriteFile(pf, fmt.Appendf(nil, "%d", os.Getpid()), 0o600)
	}
	peer := jsonrpc.New(os.Stdin, os.Stdout)
	interrupted := make(chan struct{}, 1)
	shutdown := make(chan struct{})
	peer.Handle("session.open", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"conversation_id": "c1", "resumed": false}, nil
	})
	peer.Handle("session.send", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if mode == "stubborn" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		select {
		case <-interrupted:
			return map[string]any{"status": "paused"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	peer.Handle("session.interrupt", func(context.Context, json.RawMessage) (any, error) {
		select {
		case interrupted <- struct{}{}:
		default:
		}
		return map[string]any{"status": "paused"}, nil
	})
	peer.Handle("shutdown", func(context.Context, json.RawMessage) (any, error) {
		defer close(shutdown)
		return map[string]any{"ok": true}, nil
	})
	go peer.Serve()
	ready := map[string]any{"adapter": "fake"}
	switch mode {
	case "v0":
	case "v99":
		ready["protocol_version"] = 99
	default:
		ready["protocol_version"] = ProtocolVersion
	}
	_ = peer.Notify("ready", ready)
	select {
	case <-shutdown:
		time.Sleep(100 * time.Millisecond) // let the reply flush
	case <-peer.Done():
	}
}

func fakeRuntime(t *testing.T, mode string) (*Runtime, agent.OpenRequest) {
	t.Helper()
	t.Setenv("BC_FAKE_ADAPTER", mode)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	gw := &inference.Gateway{Model: "fake"}
	rt := &Runtime{
		Sandbox:        sandbox.None{},
		Argv:           []string{os.Args[0], "-test.run=^$"},
		Gateway:        func(string) *inference.Gateway { return gw },
		ReadyTimeout:   30 * time.Second,
		InterruptGrace: time.Second,
	}
	return rt, agent.OpenRequest{TaskID: "t1", Workspace: ws, PersistenceDir: filepath.Join(tmp, "task", "runtime")}
}

func TestProtocolVersionMismatchRefused(t *testing.T) {
	for mode, want := range map[string]string{"v0": "no protocol version", "v99": "adapter protocol v99, control plane expects v1"} {
		t.Run(mode, func(t *testing.T) {
			rt, req := fakeRuntime(t, mode)
			_, err := rt.Open(t.Context(), req)
			if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "boundedcode sandbox build") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPerTaskRedactedLog(t *testing.T) {
	rt, req := fakeRuntime(t, "ok")
	s, err := rt.Open(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(req.PersistenceDir), "adapter.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "fake adapter starting") || strings.Contains(string(b), "sk-proj-abc") {
		t.Fatalf("log not written or not redacted:\n%s", b)
	}
}

func TestSendDeadlineInterrupts(t *testing.T) {
	rt, req := fakeRuntime(t, "ok")
	s, err := rt.Open(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	res, err := s.Send(ctx, "work forever")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if res.Status != "paused" {
		t.Fatalf("interrupted run result = %+v", res)
	}
	// The adapter survived the graceful interrupt and still answers (the fake
	// does not implement session.state, so an RPC error proves it is alive).
	var re *jsonrpc.Error
	if _, err := s.State(t.Context()); !errors.As(err, &re) {
		t.Fatalf("adapter not alive after interrupt: %v", err)
	}
}

func TestSendCancelKillsUnresponsiveAdapter(t *testing.T) {
	rt, req := fakeRuntime(t, "stubborn")
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("BC_FAKE_ADAPTER_PIDFILE", pidFile)
	s, err := rt.Open(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	if _, err := s.Send(ctx, "ignore interrupts"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("send returned after %s", d)
	}
	select {
	case <-s.(*session).exited:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter not killed after grace period")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Sscan(string(b), &pid); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("adapter pid %d still alive", pid)
	}
}
