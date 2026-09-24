package session

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeWaitsForReadinessAndStopsItsProcessGroup(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-supervisor")
	body := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=TestRuntimeHelper -- \"$@\"\n", bin)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	addr, err := ReserveLoopbackAddr(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BC_RUNTIME_HELPER", "1")
	runtime, err := Start(context.Background(), RuntimeOptions{
		Binary: script, DataDir: t.TempDir(), Dir: t.TempDir(), Addr: addr,
		LogPath: filepath.Join(t.TempDir(), "runtime.log"), StartTimeout: 5 * time.Second,
		StopTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	pid := runtime.PID()
	if pid <= 0 {
		t.Fatal("runtime has no pid")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.Done():
	case <-time.After(time.Second):
		t.Fatal("runtime did not finish after Close")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("supervisor pid %d survived cleanup", pid)
	}
}

func TestMergeEnvironmentOverridesStaleValues(t *testing.T) {
	got := mergeEnvironment([]string{"BC_DATA=/old", "KEEP=yes", "BC_DATA=/older"},
		[]string{"EXTRA=value"}, map[string]string{"BC_DATA": "/new", "BC_API_ADDR": "127.0.0.1:1234"})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "BC_DATA=/old") || strings.Contains(joined, "BC_DATA=/older") {
		t.Fatalf("stale data values survived:\n%s", joined)
	}
	if !strings.Contains(joined, "BC_DATA=/new") || !strings.Contains(joined, "BC_API_ADDR=127.0.0.1:1234") || !strings.Contains(joined, "KEEP=yes") || !strings.Contains(joined, "EXTRA=value") {
		t.Fatalf("merged environment is incomplete:\n%s", joined)
	}
}

// TestRuntimeHelper is executed only by the shell shim above. It implements
// the small readiness/status contract the real supervisor exposes.
func TestRuntimeHelper(t *testing.T) {
	if os.Getenv("BC_RUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args
	addr := ""
	for i, arg := range args {
		if arg == "--addr" && i+1 < len(args) {
			addr = args[i+1]
		}
	}
	if addr == "" {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		os.Exit(3)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"children":[]}`)) })
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	_ = server.Serve(listener)
}
