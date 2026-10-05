package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
)

// Verification must not change the candidate it judges. Stages run the
// repository's own commands, and some write tracked files (a build that
// regenerates committed bundles, a `lint --fix`, code generation): the
// working tree would then differ from the attempt's commit, later stages
// would judge content the task branch does not contain, and the next
// checkpoint would commit the residue as if the agent wrote it (found with
// axios in the 2026-10-04 validation: `npm run build` rewrote 12 tracked
// dist/ files). The engine snapshots the worktree before its stages and
// undoes what they changed afterwards.

// maxSnapshotBytes bounds the content kept for files already modified
// before verification (normally none: attempts are committed first).
const maxSnapshotBytes = 32 << 20

type worktreeSnapshot struct {
	dirty     map[string][]byte // tracked, modified before verification -> content (nil: deleted or too large)
	untracked map[string]bool
}

// snapshotWorktree records the non-ignored changes of a worktree.
func snapshotWorktree(ctx context.Context, wt string) (worktreeSnapshot, error) {
	s := worktreeSnapshot{dirty: map[string][]byte{}, untracked: map[string]bool{}}
	out, err := gitops.Run(ctx, wt, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return s, err
	}
	kept := 0
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if len(e) < 4 {
			continue
		}
		code, path := e[:2], e[3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the next field is the rename source
		}
		if !safeRel(path) {
			continue
		}
		if code == "??" {
			s.untracked[path] = true
			continue
		}
		var content []byte
		if b, err := os.ReadFile(filepath.Join(wt, path)); err == nil && kept+len(b) <= maxSnapshotBytes {
			content, kept = b, kept+len(b)
		}
		s.dirty[path] = content
	}
	return s, nil
}

// undoSideEffects restores tracked files the stages changed and removes
// untracked files they created. It returns what it undid.
func undoSideEffects(ctx context.Context, wt string, before worktreeSnapshot) ([]string, error) {
	after, err := snapshotWorktree(ctx, wt)
	if err != nil {
		return nil, err
	}
	var undone, restoreFromIndex []string
	var errs []error
	for path, content := range after.dirty {
		prev, wasDirty := before.dirty[path]
		switch {
		case !wasDirty:
			restoreFromIndex = append(restoreFromIndex, path)
		case prev != nil && !bytes.Equal(prev, content):
			if err := writeBack(wt, path, prev); err != nil {
				errs = append(errs, err)
				continue
			}
			undone = append(undone, path)
		}
	}
	if len(restoreFromIndex) > 0 {
		sort.Strings(restoreFromIndex)
		// The index is not writable by stages (the worktree admin dir is
		// mounted read-only), so it still holds the attempt's content.
		if _, err := gitops.Run(ctx, wt, append([]string{"checkout", "--"}, restoreFromIndex...)...); err != nil {
			errs = append(errs, err)
		} else {
			undone = append(undone, restoreFromIndex...)
		}
	}
	for path := range after.untracked {
		if before.untracked[path] {
			continue
		}
		p := filepath.Join(wt, path)
		if fi, err := os.Lstat(p); err == nil && !fi.IsDir() {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			undone = append(undone, path)
		}
	}
	sort.Strings(undone)
	return undone, errors.Join(errs...)
}

// writeBack restores a file's content without following a symlink planted
// at its path.
func writeBack(wt, path string, content []byte) error {
	p := filepath.Join(wt, path)
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return os.WriteFile(p, content, 0o644)
}

func safeRel(p string) bool {
	return p != "" && !filepath.IsAbs(p) && !strings.HasPrefix(filepath.Clean(p), "..")
}

// sideEffectsStage reports what verification undid (informational).
func sideEffectsStage(undone []string, err error) StageResult {
	sr := StageResult{Name: "side-effects", Status: "pass", Command: "(built-in)"}
	if err != nil {
		// The candidate may no longer match its commit: do not pass it.
		sr.Status, sr.ExitCode = "error", 1
		sr.Output = "could not undo changes made by verification: " + err.Error()
		return sr
	}
	shown := undone
	if len(shown) > 20 {
		shown = shown[:20]
	}
	sr.Output = fmt.Sprintf("undid %d change(s) made by verification stages: %s", len(undone), strings.Join(shown, ", "))
	return sr
}
