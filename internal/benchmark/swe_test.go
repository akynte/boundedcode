package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

func TestPatchFiles(t *testing.T) {
	p := "diff --git a/x/a_test.go b/x/a_test.go\n--- a/x/a_test.go\n+++ b/x/a_test.go\n" +
		"diff --git a/old.go b/new.go\nrename from old.go\n"
	got := strings.Join(patchFiles(p), ",")
	if got != "x/a_test.go,old.go,new.go" {
		t.Fatal(got)
	}
}

// TestApplyHiddenPatchResetsAgentEdits: acceptance tests are the dataset's,
// even when the agent edited (or weakened) the same test file.
func TestApplyHiddenPatchResetsAgentEdits(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	write := func(p, s string) {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a_test.go", "package a\n\nfunc TestA() {}\n")
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@e", "commit", "-qm", "base"}} {
		if _, err := gitops.Run(ctx, dir, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, dir, "rev-parse", "HEAD")
	write("a_test.go", "package a\n// agent weakened this\n")
	patch := "diff --git a/a_test.go b/a_test.go\n--- a/a_test.go\n+++ b/a_test.go\n@@ -1,3 +1,4 @@\n package a\n \n func TestA() {}\n+func TestB() {}\n" +
		"diff --git a/b_test.go b/b_test.go\nnew file mode 100644\n--- /dev/null\n+++ b/b_test.go\n@@ -0,0 +1 @@\n+package a\n"
	write("b_test.go", "agent-created\n")
	if err := applyHiddenPatch(ctx, dir, base, patch); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a_test.go"))
	b, _ := os.ReadFile(filepath.Join(dir, "b_test.go"))
	if string(a) != "package a\n\nfunc TestA() {}\nfunc TestB() {}\n" || string(b) != "package a\n" {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

// TestAcceptanceGetsDependencies: acceptance checks get the materialized
// repository's dependencies like verification does, including writable tool
// caches (vitest writes node_modules/.vite: without the layer the vuejs
// acceptance check failed before running a test).
func TestAcceptanceGetsDependencies(t *testing.T) {
	repo, wt := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var cs sandbox.Spec
	if err := withDependencies(&cs, repo, wt); err != nil {
		t.Fatal(err)
	}
	if len(cs.Mounts) != 1 || !cs.Mounts[0].ReadOnly || cs.Mounts[0].Target != filepath.Join(wt, "node_modules") {
		t.Fatalf("mounts = %+v", cs.Mounts)
	}
	if !strings.Contains(strings.Join(cs.Scratch, ","), filepath.Join(wt, "node_modules", ".vite")) {
		t.Fatalf("no writable tool cache: %v", cs.Scratch)
	}
}

func TestGitSourceNeedsPinnedCommit(t *testing.T) {
	for _, s := range []string{"git:https://github.com/a/b", "git:https://github.com/a/b@main", "git:https://github.com/a/b@abc123"} {
		if _, err := sourceDir(context.Background(), s); err == nil || !strings.Contains(err.Error(), "commit") {
			t.Fatalf("%s: %v", s, err)
		}
	}
}
