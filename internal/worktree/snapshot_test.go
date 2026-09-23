package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/worktree"
)

// The snapshot is what verification examines, so these pin what it holds: the
// candidate a commit of the task would contain, with the verification
// configuration of the base, and nothing the editing checkout left lying about.

func snapshotRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	files := map[string]string{
		"a.txt":           "base\n",
		"b.txt":           "deleted by the task\n",
		".gitignore":      "build/\n",
		".bc/verify.yaml": "base config\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "initial"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) (string, bool) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(body), true
}

func TestSnapshotHoldsExactlyTheCandidate(t *testing.T) {
	ctx := context.Background()
	repo := snapshotRepo(t)
	m, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wt, err := m.Create(ctx, repo, "t1")
	if err != nil {
		t.Fatal(err)
	}

	write(t, wt.Path, "a.txt", "changed by the task\n")
	write(t, wt.Path, "sub/new.txt", "added by the task\n")
	if err := os.Remove(filepath.Join(wt.Path, "b.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, wt.Path, "build/out.bin", "ignored build output\n")
	write(t, wt.Path, ".bc/verify.yaml", "the candidate's own config\n")

	snap, err := m.Snapshot(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snap.Remove(ctx) })

	if snap.Path == wt.Path {
		t.Fatal("the snapshot must not be the task's worktree")
	}
	if got, _ := read(t, snap.Path, "a.txt"); got != "changed by the task\n" {
		t.Errorf("a modified file must carry the change, got %q", got)
	}
	if got, ok := read(t, snap.Path, "sub/new.txt"); !ok || got != "added by the task\n" {
		t.Errorf("a new file must be present, got %q (present=%v)", got, ok)
	}
	if _, ok := read(t, snap.Path, "b.txt"); ok {
		t.Error("a deleted file must be absent")
	}
	if _, ok := read(t, snap.Path, "build/out.bin"); ok {
		t.Error("ignored files are not part of the candidate and must not reach verification")
	}
	if got, _ := read(t, snap.Path, ".bc/verify.yaml"); got != "base config\n" {
		t.Errorf("verification config must come from the base, got %q", got)
	}
	if len(snap.PatchSHA256) != 64 || snap.Manifest == "" || snap.Base != wt.Base {
		t.Errorf("the snapshot must identify what it holds: %+v", snap)
	}

	// A check that writes into the snapshot must not reach the worktree.
	write(t, snap.Path, "leak.txt", "written by a check\n")
	if _, ok := read(t, wt.Path, "leak.txt"); ok {
		t.Error("a write in the snapshot reached the task's worktree")
	}

	if err := snap.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snap.Path); !os.IsNotExist(err) {
		t.Errorf("the snapshot must be gone after Remove: %v", err)
	}
	list, err := exec.Command("git", "-C", repo, "worktree", "list").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(list), snap.Path) {
		t.Errorf("the snapshot is still registered:\n%s", list)
	}
}

// State from one verification run must not survive into the next.
func TestSnapshotIsRecreatedNotReused(t *testing.T) {
	ctx := context.Background()
	repo := snapshotRepo(t)
	m, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wt, err := m.Create(ctx, repo, "t2")
	if err != nil {
		t.Fatal(err)
	}

	first, err := m.Snapshot(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	write(t, first.Path, "stale.txt", "left by an interrupted run\n")

	second, err := m.Snapshot(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Remove(ctx) })
	if _, ok := read(t, second.Path, "stale.txt"); ok {
		t.Error("a snapshot reused state from a previous run")
	}
	if first.PatchSHA256 != second.PatchSHA256 {
		t.Error("the same candidate must produce the same patch digest")
	}
}
