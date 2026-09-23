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

// A later localization call that returns no files or symbols keeps the
// selection before it. The recorded refinement answered with empty lists and
// ended the task in LOCALIZE with the first call's nine files in hand.
func TestAnEmptyRefinementKeepsTheEarlierSelection(t *testing.T) {
	earlier := selection{Files: []string{"internal/shipping/shipping.go"}, Symbols: []string{"Quote"}, Hypothesis: "first"}
	var logged []string
	got := keepSelection(selection{Hypothesis: "refined"}, earlier, func(what string, _ int) { logged = append(logged, what) })
	if !slices.Equal(got.Files, earlier.Files) || !slices.Equal(got.Symbols, earlier.Symbols) {
		t.Errorf("the earlier selection was not kept: %+v", got)
	}
	if got.Hypothesis != "refined" {
		t.Errorf("the newer hypothesis was replaced: %q", got.Hypothesis)
	}
	if !slices.Equal(logged, []string{"files", "symbols"}) {
		t.Errorf("the fallback was not reported: %v", logged)
	}
	// A newer answer that says something is taken as it is.
	newer := selection{Files: []string{"internal/orders/orders.go"}, Symbols: []string{"Place"}, Hypothesis: "h"}
	if got := keepSelection(newer, earlier, func(string, int) {}); !slices.Equal(got.Files, newer.Files) {
		t.Errorf("a non-empty newer selection was replaced: %+v", got)
	}
}
