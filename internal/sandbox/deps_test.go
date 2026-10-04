package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDependencyMounts(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	mkdirs(t, src, "node_modules/.bin", "packages/a/node_modules/x", "apps/web/ui/node_modules", ".git/node_modules", "node_modules/y/node_modules")
	// A symlinked dependency dir in the checkout is not followed.
	if err := os.Symlink(t.TempDir(), filepath.Join(src, "packages", "linked")); err != nil {
		t.Fatal(err)
	}
	mkdirs(t, wt, "packages/a/node_modules/agent-planted")
	ms, err := DependencyMounts(src, wt)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		if !m.ReadOnly || !strings.HasPrefix(m.Host, src) {
			t.Fatalf("mount must be read-only from the checkout: %+v", m)
		}
		rel, _ := filepath.Rel(wt, m.Target)
		got = append(got, rel)
	}
	// Root and workspace packages; not deeper than 3 levels, not inside
	// .git or other dependency dirs. The agent-planted directory is shadowed.
	if strings.Join(got, ",") != "node_modules,packages/a/node_modules" {
		t.Fatalf("got %v", got)
	}
	if ms, _ := DependencyMounts(src, src); ms != nil {
		t.Fatal("no mounts for the checkout itself")
	}
	if ms, _ := DependencyMounts("", wt); ms != nil {
		t.Fatal("no mounts without a source")
	}
}

// TestDependencyMountsRefuseSymlinkTargets: the worktree is agent-writable.
// A planted link at (or above) the mount point must not redirect the mount
// elsewhere in the sandbox, e.g. over the image's tools.
func TestDependencyMountsRefuseSymlinkTargets(t *testing.T) {
	cases := map[string]func(wt string){
		"link at target": func(wt string) { _ = os.Symlink("/usr/local/bin", filepath.Join(wt, "node_modules")) },
		"link above target": func(wt string) {
			_ = os.Symlink("/usr", filepath.Join(wt, "packages"))
		},
		"file at target": func(wt string) { _ = os.WriteFile(filepath.Join(wt, "node_modules"), nil, 0o644) },
	}
	for name, plant := range cases {
		t.Run(name, func(t *testing.T) {
			src, wt := t.TempDir(), t.TempDir()
			mkdirs(t, src, "node_modules", "packages/a/node_modules")
			plant(wt)
			if _, err := DependencyMounts(src, wt); !errors.Is(err, ErrUnsafeDependencyTarget) {
				t.Fatalf("want ErrUnsafeDependencyTarget, got %v", err)
			}
		})
	}
}

func TestDependencyMountsLimit(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	for i := 0; i <= MaxDependencyMounts; i++ {
		mkdirs(t, src, filepath.Join("packages", "p"+strings.Repeat("x", i), "node_modules"))
	}
	if _, err := DependencyMounts(src, wt); err == nil {
		t.Fatal("expected an error above the mount limit")
	}
}
