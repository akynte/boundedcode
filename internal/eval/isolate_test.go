package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A test file the candidate added is set aside before the hidden test is
// placed beside it; a fixture test file the candidate changed is kept. The
// recorded run added restock_test.go with a helper named like the hidden
// test's, and all four hidden tests failed on the clash without running.
func TestAcceptanceSetsAsideOnlyTestFilesTheCandidateAdded(t *testing.T) {
	fixture, work := t.TempDir(), t.TempDir()
	writeTree(t, fixture, map[string]string{
		"internal/worker/restock.go":        "package worker\n",
		"internal/worker/reconcile_test.go": "package worker\n// fixture\n",
		"internal/other/other_test.go":      "package other\n",
	})
	writeTree(t, work, map[string]string{
		"internal/worker/restock.go":        "package worker\n// fixed\n",
		"internal/worker/reconcile_test.go": "package worker\n// updated by the candidate\n",
		"internal/worker/restock_test.go":   "package worker\ntype failingLog struct{}\n",
		"internal/other/extra_test.go":      "package other\n",
	})
	task := Task{Fixture: fixture, Acceptance: Acceptance{Files: map[string]string{
		"internal/worker/hidden_acceptance_test.go": "package worker\ntype failingLog struct{}\n",
	}}}

	note, err := isolateAcceptance(task, work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "internal/worker/restock_test.go")); !os.IsNotExist(err) {
		t.Error("the candidate's added test file was left beside the hidden test")
	}
	if !strings.Contains(note, "internal/worker/restock_test.go") {
		t.Errorf("the grading output does not say what was set aside: %q", note)
	}
	kept, _ := os.ReadFile(filepath.Join(work, "internal/worker/reconcile_test.go"))
	if !strings.Contains(string(kept), "updated by the candidate") {
		t.Error("a fixture test the candidate updated was changed; a correct change updates its callers")
	}
	fixed, _ := os.ReadFile(filepath.Join(work, "internal/worker/restock.go"))
	if !strings.Contains(string(fixed), "fixed") {
		t.Error("the candidate's code was touched")
	}
	if _, err := os.Stat(filepath.Join(work, "internal/other/extra_test.go")); err != nil {
		t.Error("a package with no hidden test lost a test file")
	}
}
