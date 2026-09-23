package evidence

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// cleanupTimeout bounds a detached cleanup command.
//
// `docker stop -t 2` gives a container two seconds to exit before the daemon
// kills it, so the command itself finishes in about that plus one daemon
// round trip. This is deliberately far longer than that: it is a ceiling that
// stops a wedged daemon hanging a run's exit, not a budget anyone should
// reach.
const cleanupTimeout = 30 * time.Second

// cleanupContext detaches a cleanup from the operation's cancellation, under a
// bound.
//
// Threading the run's context into the *work* commands is the point of this
// change: cancelling an evaluation should stop the docker exec, the git clone
// and the python that are running on its behalf. But the same context must
// not reach the commands that undo them. A cancelled context passed to
// `docker stop` means the container the run created is never stopped, so
// every cancelled run leaks one — trading a hung subprocess for a leaked
// container is not an improvement.
//
// Cancellation is dropped and values are kept, and the bound replaces the
// inherited deadline rather than inheriting a dead one. This is the same
// shape, and the same reasoning, as ledger.recording for a detached journal
// write.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}

// containedPath resolves p and reports it only if it stays inside root.
//
// The suite's run root is operator configuration, and everything under it is
// built from fixed names — but one step in between is not this package's to
// trust: EnsurePatchedHarborRuntime copies the installed harbor package with
// `cp -r`, and a copied tree can contain symlinks. A link at
// environments/docker would put the compose file this code then *writes*
// wherever the link points, outside the run root entirely.
//
// So the check is on the resolved path, not the joined one. EvalSymlinks
// follows every link in the chain; a path that does not exist yet is resolved
// through its parent, because the parent is the part a link could have
// redirected. Both sides are resolved, so a symlinked run root compares
// equal to itself rather than failing.
func containedPath(root, p string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Nothing at that path yet. Walk up to the nearest ancestor that does
		// exist, resolve *that* — it is the part a symlink could have
		// redirected — and re-attach the remainder, which cannot contain a
		// link because it does not exist. Resolving only the immediate parent
		// was not enough: a whole subtree may be uncreated.
		rest := ""
		probe := abs
		for {
			parent := filepath.Dir(probe)
			if parent == probe {
				return "", fmt.Errorf("evidence: resolving %s: no existing ancestor", p)
			}
			rest = filepath.Join(filepath.Base(probe), rest)
			if r, perr := filepath.EvalSymlinks(parent); perr == nil {
				resolved = filepath.Join(r, rest)
				break
			}
			probe = parent
		}
	}
	rel, err := filepath.Rel(absRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence: %s resolves to %s, outside the run root %s",
			p, resolved, absRoot)
	}
	return resolved, nil
}
