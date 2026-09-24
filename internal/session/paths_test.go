package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeSessionRootIsPrivateAndDisposable(t *testing.T) {
	openCodeDir := filepath.Join(t.TempDir(), "opencode")
	root, err := MakeOpenCodeSessionRoot(openCodeDir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("session root mode = %o, want 700", info.Mode().Perm())
	}
	if err := RemoveOpenCodeSessionRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("session root remains after cleanup: %v", err)
	}
}
