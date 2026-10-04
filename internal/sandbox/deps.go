package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DependencyDirNames are installed-dependency directories that a project's
// tooling needs but git does not track. A task worktree is a fresh checkout,
// so without them a JavaScript/TypeScript project cannot be type-checked or
// tested offline. (Go needs no equivalent: its module cache is shared.)
var DependencyDirNames = map[string]bool{"node_modules": true}

const (
	// maxDependencyDepth bounds where dependency directories are looked for
	// (root, packages/x, apps/a/b covers workspaces and monorepos).
	maxDependencyDepth = 3
	// MaxDependencyMounts bounds the mounts added for one worktree.
	MaxDependencyMounts = 64
)

// ErrUnsafeDependencyTarget is returned when a dependency mount's target in
// the worktree is not a plain directory path (a symlink, a file, or below a
// symlink). The worktree is agent-writable; following an agent-planted link
// could place the mount over tools in the sandbox image.
var ErrUnsafeDependencyTarget = errors.New("sandbox: unsafe dependency mount target")

// DependencyMounts returns read-only mounts that make the dependency
// directories installed in source (the user's checkout of the repository)
// appear at the same relative paths in worktree (the task's checkout of it).
//
// Only real directories inside source are used (symlinked ones are ignored),
// at most maxDependencyDepth levels deep; dependency directories are not
// descended into. The mount always shadows whatever the worktree has at the
// target, so an agent cannot substitute its own tools (a fake test runner in
// node_modules/.bin) for the installed ones. A target that is, or lies
// below, a symlink, or exists as a non-directory, fails the call: callers
// must neither follow the link nor silently run without the dependencies.
func DependencyMounts(source, worktree string) ([]Mount, error) {
	if source == "" || worktree == "" {
		return nil, nil
	}
	source, worktree = filepath.Clean(source), filepath.Clean(worktree)
	if source == worktree || !filepath.IsAbs(source) || !filepath.IsAbs(worktree) {
		return nil, nil
	}
	var out []Mount
	err := filepath.WalkDir(source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == source {
				return err
			}
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if !d.IsDir() || p == source {
			return nil
		}
		rel, _ := filepath.Rel(source, p)
		if d.Name() == ".git" || strings.Count(rel, string(filepath.Separator)) >= maxDependencyDepth {
			return filepath.SkipDir
		}
		if !DependencyDirNames[d.Name()] {
			return nil
		}
		if err := safeDependencyTarget(worktree, rel); err != nil {
			return err
		}
		if len(out) >= MaxDependencyMounts {
			return fmt.Errorf("sandbox: more than %d dependency directories under %s", MaxDependencyMounts, source)
		}
		out = append(out, Mount{Host: p, Target: filepath.Join(worktree, rel), ReadOnly: true})
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// safeDependencyTarget checks that no component of worktree/rel is a
// symlink or a non-directory. Missing components are fine: the container
// engine creates the mount point.
func safeDependencyTarget(worktree, rel string) error {
	cur := worktree
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
			return fmt.Errorf("%w: %s", ErrUnsafeDependencyTarget, cur)
		}
	}
	return nil
}
