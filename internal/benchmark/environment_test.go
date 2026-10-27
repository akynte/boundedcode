package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The model server's version is read past its log lines (the 2026-10-27
// baseline recorded "0.00.000.258 I srv  llama_server: initializing ...").
func TestEnvironmentServerVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	bin := filepath.Join(t.TempDir(), "llama-server")
	script := "#!/bin/sh\necho '0.00.000.258 I srv  llama_server: initializing ...'\necho 'version: 0.5.0-dev (build 11146, commit 7fe450e19)'\necho 'built with GNU 13.3.0 for Linux x86_64'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := captureEnvironment(context.Background(), nil, bin, nil, nil, "")
	if e.Server != "llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19)" {
		t.Fatalf("server = %q", e.Server)
	}
	if e := captureEnvironment(context.Background(), nil, filepath.Join(t.TempDir(), "missing"), nil, nil, ""); e.Server == "" {
		t.Fatal("an unreadable server version is not reported")
	}
}
