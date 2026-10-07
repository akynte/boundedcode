package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testApp returns an App with default configuration over an isolated home.
func testApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("BOUNDEDCODE_HOME", t.TempDir())
	app := &App{Out: io.Discard, Err: io.Discard, Log: newLogger(io.Discard, false)}
	if err := app.load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.close)
	return app
}

// fakeDocker puts a stand-in docker CLI first on PATH. state: ready, down
// (daemon unreachable) or noimage.
func fakeDocker(t *testing.T, state string) {
	t.Helper()
	const script = `#!/bin/sh
case "$1:$FAKE_ENGINE" in
info:down) echo "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?" >&2; exit 1 ;;
version:down) echo "Cannot connect to the Docker daemon" >&2; exit 1 ;;
version:*) echo "27.0.0" ;;
image:ready) echo "sha256:0123456789abcdef0123456789" ;;
image:*) echo "Error: No such image" >&2; exit 1 ;;
esac
exit 0
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ENGINE", state)
}

func closedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String()
	_ = l.Close()
	return url
}

func wantCheck(t *testing.T, c check, status checkStatus, detail, hint string) {
	t.Helper()
	if c.Status != status || !strings.Contains(c.Detail, detail) || !strings.Contains(c.Hint, hint) {
		t.Fatalf("got %+v; want status %s, detail ~%q, hint ~%q", c, status, detail, hint)
	}
}

func TestDoctorContainerEngine(t *testing.T) {
	ctx := context.Background()
	t.Run("not installed", func(t *testing.T) {
		var engineErr error
		wantCheck(t, engineCheck(ctx, "bc-no-such-engine", &engineErr), statusFail, "bc-no-such-engine is not installed", "setup --only sandbox")
		wantCheck(t, imageCheck(ctx, "bc-no-such-engine", "bc-agent:test", engineErr), statusWarn, "not checked", "container engine first")
	})
	t.Run("daemon down", func(t *testing.T) {
		fakeDocker(t, "down")
		var engineErr error
		wantCheck(t, engineCheck(ctx, "docker", &engineErr), statusFail, "daemon running", "docker group")
		c := imageCheck(ctx, "docker", "bc-agent:test", engineErr)
		if strings.Contains(c.Detail, "not built") {
			t.Fatalf("image reported as not built while the daemon is down: %+v", c)
		}
	})
	t.Run("image missing", func(t *testing.T) {
		fakeDocker(t, "noimage")
		var engineErr error
		wantCheck(t, engineCheck(ctx, "docker", &engineErr), statusOK, "27.0.0", "")
		c := imageCheck(ctx, "docker", "bc-agent:test", engineErr)
		wantCheck(t, c, statusWarn, "bc-agent:test is not built", "setup --only sandbox")
		if strings.Contains(c.Hint, "--dir") {
			t.Fatalf("hint needs a source checkout: %+v", c)
		}
	})
	t.Run("ready", func(t *testing.T) {
		fakeDocker(t, "ready")
		wantCheck(t, imageCheck(ctx, "docker", "bc-agent:test", nil), statusOK, "bc-agent:test sha256:", "")
	})
}

func TestDoctorSetupHints(t *testing.T) {
	ctx := context.Background()
	app := testApp(t)
	wantCheck(t, cbmCheck(ctx, "/nonexistent/codebase-memory-mcp"), statusWarn, "not installed", "setup --only tools")

	app.Config.Inference.ServerBinary = "bc-no-such-llama-server"
	app.Config.ModelsDir = t.TempDir()
	cs := inferenceChecks(ctx, app)
	if len(cs) != 2 {
		t.Fatalf("checks = %+v", cs)
	}
	wantCheck(t, cs[0], statusFail, "bc-no-such-llama-server", "setup --only inference")
	wantCheck(t, cs[1], statusFail, "not found", "setup --only model")

	app.Config.Inference.Mode, app.Config.Inference.ExternalURL = "external", closedURL(t)
	cs = inferenceChecks(ctx, app)
	wantCheck(t, cs[0], statusWarn, "is not reachable", "inference.external_url")
	wantCheck(t, cs[1], statusOK, "external server", "")
}

// TestTaskRunPreflight: a task run reports a missing dependency with its fix
// before starting anything.
func TestTaskRunPreflight(t *testing.T) {
	ctx := context.Background()
	t.Run("engine not installed", func(t *testing.T) {
		app := testApp(t)
		app.Config.Sandbox.Kind, app.Config.Sandbox.Engine = "docker", "bc-no-such-engine"
		_, _, err := app.buildRunner(ctx, runFlags{})
		if err == nil || !strings.Contains(err.Error(), "bc-no-such-engine is not installed") || !strings.Contains(err.Error(), "setup --only sandbox") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("daemon down", func(t *testing.T) {
		fakeDocker(t, "down")
		app := testApp(t)
		app.Config.Sandbox.Kind, app.Config.Sandbox.Engine = "docker", "docker"
		_, _, err := app.buildRunner(ctx, runFlags{})
		if err == nil || !strings.Contains(err.Error(), "daemon running") || strings.Contains(err.Error(), "not built") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("image missing", func(t *testing.T) {
		fakeDocker(t, "noimage")
		app := testApp(t)
		app.Config.Sandbox.Kind, app.Config.Sandbox.Engine = "docker", "docker"
		_, _, err := app.buildRunner(ctx, runFlags{})
		if err == nil || !strings.Contains(err.Error(), "is not built") || !strings.Contains(err.Error(), "setup --only sandbox") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("external server unreachable", func(t *testing.T) {
		app := testApp(t)
		url := closedURL(t)
		app.Config.Sandbox.Kind = "none"
		app.Config.Agent.AdapterDir = t.TempDir()
		app.Config.Inference.Mode, app.Config.Inference.ExternalURL = "external", url
		_, _, err := app.buildRunner(ctx, runFlags{unsafeNoSandbox: true})
		if err == nil || !strings.Contains(err.Error(), "external inference server "+url+" is not reachable") || !strings.Contains(err.Error(), "inference.external_url") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("repository intelligence missing", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()
		app := testApp(t)
		var errOut bytes.Buffer
		app.Err = &errOut
		app.Config.Sandbox.Kind = "none"
		app.Config.Agent.AdapterDir = t.TempDir()
		app.Config.Inference.Mode, app.Config.Inference.ExternalURL = "external", srv.URL
		app.Config.RepoIntel.Binary = "/nonexistent/codebase-memory-mcp"
		app.Config.RepoIntel.Serena.Enabled = false
		r, cleanup, err := app.buildRunner(ctx, runFlags{unsafeNoSandbox: true})
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		if r.Intel != nil {
			t.Fatal("runner uses a missing codebase-memory-mcp")
		}
		out := errOut.String()
		if strings.Count(out, "warning:") != 1 || !strings.Contains(out, "setup --only tools") || !strings.Contains(out, "continuing without graph context") {
			t.Fatalf("want one actionable warning, got:\n%s", out)
		}
	})
}
