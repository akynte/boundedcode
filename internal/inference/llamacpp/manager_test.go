package llamacpp

import (
	"errors"
	"io/fs"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/model"
)

// freePort returns a TCP port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func TestEnsureMissingDependenciesHints(t *testing.T) {
	models := t.TempDir()
	p := model.Profile{Name: "m", File: "bc-test-weights.gguf"}
	m := &Manager{Binary: "bc-no-such-llama-server", Host: "127.0.0.1", Port: freePort(t), ModelsDir: models, StateDir: t.TempDir()}

	_, err := m.Ensure(t.Context(), p)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "model file for m") || !strings.Contains(err.Error(), "setup --only model") {
		t.Fatalf("missing weights: %v", err)
	}

	if err := writeFile(filepath.Join(models, p.File), "gguf"); err != nil {
		t.Fatal(err)
	}
	_, err = m.Ensure(t.Context(), p)
	if !errors.Is(err, exec.ErrNotFound) || !strings.Contains(err.Error(), "setup --only inference") || !strings.Contains(err.Error(), "inference.mode: external") {
		t.Fatalf("missing llama-server: %v", err)
	}
}
