package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEngineScript is a stand-in docker CLI. FAKE_ENGINE selects the state:
// ready, down (daemon unreachable) or noimage.
const fakeEngineScript = `#!/bin/sh
case "$1:$FAKE_ENGINE" in
info:down) echo "Client: Docker Engine - Community"; echo "ERROR: Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?" >&2; exit 1 ;;
info:*) exit 0 ;;
image:ready) exit 0 ;;
image:*) echo "Error: No such image" >&2; exit 1 ;;
esac
exit 0
`

// fakeDocker puts a fake `docker` first on PATH.
func fakeDocker(t *testing.T, state string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fakeEngineScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ENGINE", state)
}

func TestCheckEngine(t *testing.T) {
	ctx := context.Background()
	t.Run("not installed", func(t *testing.T) {
		err := CheckEngine(ctx, "bc-no-such-engine", "img")
		if !errors.Is(err, ErrEngineMissing) || !strings.Contains(err.Error(), "bc-no-such-engine is not installed") ||
			!strings.Contains(err.Error(), "install Docker") || !strings.Contains(err.Error(), "setup --only sandbox") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("daemon down", func(t *testing.T) {
		fakeDocker(t, "down")
		err := CheckEngine(ctx, "docker", "img")
		var ee *EngineError
		if !errors.Is(err, ErrEngineUnreachable) || !errors.As(err, &ee) {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(ee.Problem(), "daemon running") || !strings.Contains(ee.Detail, "Cannot connect to the Docker daemon") ||
			!strings.Contains(ee.Hint(), "docker group") || strings.Contains(err.Error(), "not built") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("image missing", func(t *testing.T) {
		fakeDocker(t, "noimage")
		err := CheckEngine(ctx, "docker", "bc-agent:test")
		if !errors.Is(err, ErrImageMissing) || !strings.Contains(err.Error(), "bc-agent:test is not built") ||
			!strings.Contains(err.Error(), "setup --only sandbox") {
			t.Fatalf("err = %v", err)
		}
		if err := CheckEngine(ctx, "docker", ""); err != nil {
			t.Fatalf("engine alone is usable: %v", err)
		}
	})
	t.Run("ready", func(t *testing.T) {
		fakeDocker(t, "ready")
		if err := (&Container{Engine: "docker", Image: "img"}).Check(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEngineFailure(t *testing.T) {
	for _, c := range []struct {
		engine string
		code   int
		out    string
		want   bool
	}{
		{"docker", 125, "docker: Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n", true},
		{"docker", 125, "Unable to find image 'x:latest' locally\ndocker: Error response from daemon: pull access denied\n", true},
		{"/usr/bin/podman", 125, "Error: short-name resolution enforced\n", true},
		{"docker", 125, "Error: the stage's own message\n", false}, // only Podman prefixes "Error:"
		{"docker", 125, "FAIL example.com/x\n", false},
		{"docker", 1, "docker: something\n", false},
	} {
		if got := EngineFailure(c.engine, c.code, c.out); got != c.want {
			t.Errorf("EngineFailure(%q, %d, %q) = %v", c.engine, c.code, c.out, got)
		}
	}
}

// TestEngineHintForEnginePath: an engine configured by path gets the same
// fix as by name.
func TestEngineHintForEnginePath(t *testing.T) {
	byName := (&EngineError{Kind: ErrEngineUnreachable, Engine: "docker"}).Hint()
	byPath := (&EngineError{Kind: ErrEngineUnreachable, Engine: "/usr/bin/docker"}).Hint()
	if byPath != byName || !strings.Contains(byPath, "docker group") {
		t.Fatalf("hint for /usr/bin/docker = %q, want %q", byPath, byName)
	}
}

// TestEngineErrorLine picks the daemon error out of `docker info`, whose
// output starts with client details.
func TestEngineErrorLine(t *testing.T) {
	out := "Client: Docker Engine - Community\n Version:    29.8.2\n Context:    default\n\nServer:\n" +
		"failed to connect to the docker API at unix:///var/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /var/run/docker.sock: connect: no such file or directory\n"
	if got := engineErrorLine(out); !strings.HasPrefix(got, "failed to connect to the docker API") {
		t.Fatalf("got %q", got)
	}
	if got := engineErrorLine("Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n"); !strings.HasPrefix(got, "Cannot connect") {
		t.Fatalf("got %q", got)
	}
}
