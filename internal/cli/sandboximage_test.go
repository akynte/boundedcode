package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSandboxContextHash: an image built from the checkout and one built
// from the binary's embedded context get the same label, and any change to
// the definition (a new toolchain in the Dockerfile) changes it, so setup
// rebuilds an outdated image.
func TestSandboxContextHash(t *testing.T) {
	checkout := filepath.Join("..", "..", "adapters", "openhands")
	if got, want := sandboxContextHash(checkout), sandboxContextHash(""); got != want {
		t.Fatalf("checkout %s != embedded %s", got, want)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(checkout)); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "Dockerfile")
	b, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(b, "RUN true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if sandboxContextHash(dir) == sandboxContextHash("") {
		t.Fatal("a changed Dockerfile must change the hash")
	}
}
