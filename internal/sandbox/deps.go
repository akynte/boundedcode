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

// dependencyCacheDirs are the directories tools write inside a dependency
// directory (Vite/Vitest, babel/eslint/terser loaders). They get a writable
// scratch layer so the installed packages can stay read-only.
var dependencyCacheDirs = []string{".cache", ".vite", ".vitest"}

// Dependencies are the mounts that make a checkout's installed dependencies
// available in a worktree's sandbox.
type Dependencies struct {
	Mounts  []Mount  // read-only
	Scratch []string // writable tool-cache directories inside them
}

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
//
// Tool caches inside them (dependencyCacheDirs) get a writable scratch layer;
// a missing cache directory is created, empty, in the checkout, because a
// mount point cannot be made inside a read-only mount. Nothing else in the
// checkout is written.
func DependencyMounts(source, worktree string) (Dependencies, error) {
	var deps Dependencies
	if source == "" || worktree == "" {
		return deps, nil
	}
	source, worktree = filepath.Clean(source), filepath.Clean(worktree)
	if source == worktree || !filepath.IsAbs(source) || !filepath.IsAbs(worktree) {
		return deps, nil
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
		target := filepath.Join(worktree, rel)
		out = append(out, Mount{Host: p, Target: target, ReadOnly: true})
		for _, c := range dependencyCacheDirs {
			if ensureCacheDir(filepath.Join(p, c)) {
				deps.Scratch = append(deps.Scratch, filepath.Join(target, c))
			}
		}
		return filepath.SkipDir
	})
	if err != nil {
		return Dependencies{}, err
	}
	deps.Mounts = out
	return deps, nil
}

// ensureCacheDir makes dir exist as a real directory; it reports false when
// it cannot (then the tool sees the read-only mount and fails loudly).
func ensureCacheDir(dir string) bool {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.Mkdir(dir, 0o755) == nil
	}
	return err == nil && fi.IsDir() && fi.Mode()&fs.ModeSymlink == 0
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
