package task

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The recorded failure: LOCALIZE's selection named internal/worker/restock_test.go
// beside restock.go, the file did not exist, and stat-ing it failed the task
// before the model had been shown a single body. A guessed path is dropped
// and reported; the real ones carry on.
func TestExistingFilesDropsOnlyWhatIsNotThere(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "worker", "restock.go"), []byte("package worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept, missing := existingFiles(root, []string{
		"internal/worker/restock.go",
		"internal/worker/restock_test.go",
		"../outside.go",
	})
	if !slices.Equal(missing, []string{"internal/worker/restock_test.go"}) {
		t.Errorf("missing = %v, want only the invented test file", missing)
	}
	// A path outside the worktree is not "missing": it stays for the access
	// check, which refuses it with its own reason rather than having it
	// quietly disappear here.
	if !slices.Equal(kept, []string{"internal/worker/restock.go", "../outside.go"}) {
		t.Errorf("kept = %v", kept)
	}
}
