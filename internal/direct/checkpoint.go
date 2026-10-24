package direct

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// checkpoints keeps a copy of every file before direct mode changes it,
// one directory per turn, so a turn can be undone. Only files inside the
// project are kept; a file created by the turn is recorded as absent.
type checkpoints struct {
	dir   string // <CheckpointDir>/<session>
	turn  int
	saved map[string]bool // this turn
}

func (r *Runner) checkpointsTurn() {
	if r.Opts.CheckpointDir == "" || r.Session == nil {
		return
	}
	if r.cp == nil {
		r.cp = &checkpoints{dir: filepath.Join(r.Opts.CheckpointDir, r.Session.ID)}
		// Continue numbering after stored turns.
		if es, err := os.ReadDir(r.cp.dir); err == nil {
			for _, e := range es {
				if n, err := strconv.Atoi(e.Name()); err == nil && n > r.cp.turn {
					r.cp.turn = n
				}
			}
		}
	}
	r.cp.turn++
	r.cp.saved = map[string]bool{}
}

// checkpoint saves abs before its first change in this turn.
func (r *Runner) checkpoint(abs string) {
	if r.cp == nil || r.cp.saved[abs] {
		return
	}
	rel := r.rootRel(abs)
	if rel == "" || strings.HasPrefix(rel, "..") {
		return
	}
	dst := filepath.Join(r.cp.dir, strconv.Itoa(r.cp.turn), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return
	}
	src, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(dst+".absent", nil, 0o600)
			r.cp.saved[abs] = true
		}
		return
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_, cerr := io.Copy(out, src)
	_ = out.Close()
	if cerr == nil {
		r.cp.saved[abs] = true
	}
}

// Undo restores the files changed by the most recent turn that changed
// any, and returns what it restored. Each call goes one turn further back.
func (r *Runner) Undo() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Opts.CheckpointDir == "" || r.Session == nil {
		return nil, fmt.Errorf("checkpoints are not enabled")
	}
	base := filepath.Join(r.Opts.CheckpointDir, r.Session.ID)
	es, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("nothing to undo in this session")
	}
	var turns []int
	for _, e := range es {
		if n, err := strconv.Atoi(e.Name()); err == nil {
			turns = append(turns, n)
		}
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("nothing to undo in this session")
	}
	sort.Sort(sort.Reverse(sort.IntSlice(turns)))
	tdir := filepath.Join(base, strconv.Itoa(turns[0]))
	var restored []string
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = r.Opts.Root
	}
	err = filepath.Walk(tdir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(tdir, p)
		if strings.HasSuffix(rel, ".absent") {
			target := filepath.Join(root, strings.TrimSuffix(rel, ".absent"))
			if rerr := os.Remove(target); rerr == nil || os.IsNotExist(rerr) {
				restored = append(restored, filepath.ToSlash(strings.TrimSuffix(rel, ".absent"))+" (removed)")
			}
			return nil
		}
		target := filepath.Join(root, rel)
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if werr := os.MkdirAll(filepath.Dir(target), 0o755); werr != nil {
			return werr
		}
		if werr := os.WriteFile(target, b, 0o644); werr != nil {
			return werr
		}
		restored = append(restored, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return restored, err
	}
	_ = os.RemoveAll(tdir)
	if r.cp != nil && r.cp.turn == turns[0] {
		r.cp.saved = map[string]bool{}
	}
	sort.Strings(restored)
	if len(restored) == 0 {
		return nil, fmt.Errorf("the last turn changed no files")
	}
	return restored, nil
}
