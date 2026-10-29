// Package fileedit is the editing engine the agent's file tools share:
// text replacement (exact, then tolerant of indentation), changes to
// several files applied together or not at all, conflict detection against
// the content the caller last saw, and atomic replacement of each file.
// It knows nothing about any language; language-aware edits build on it.
package fileedit

import (
	"crypto/sha1" //nolint:gosec // git's object id, not a security hash
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Edit is one replacement in a file.
type Edit struct {
	Old, New string
	// All replaces every occurrence; otherwise Old must occur once.
	All bool
}

// ErrNotFound: the text to replace is not in the file.
var ErrNotFound = errors.New("old_string not found")

// AmbiguousError: the text to replace occurs more than once.
type AmbiguousError struct{ Count int }

func (e *AmbiguousError) Error() string {
	if e.Count < 0 {
		return "old_string matches more than once ignoring indentation"
	}
	return fmt.Sprintf("old_string occurs %d times", e.Count)
}

// Replace applies e to src and reports how many replacements it made. An
// exact match is used when there is one; otherwise lines are matched
// ignoring leading and trailing whitespace, and the replacement is
// re-indented the way the file is.
func Replace(src string, e Edit) (string, int, error) {
	if e.Old == "" {
		return "", 0, errors.New("old_string is empty")
	}
	switch n := strings.Count(src, e.Old); {
	case n == 0:
		out, err := fuzzyReplace(src, e.Old, e.New)
		if err != nil {
			return "", 0, err
		}
		return out, 1, nil
	case e.All:
		return strings.ReplaceAll(src, e.Old, e.New), n, nil
	case n > 1:
		return "", 0, &AmbiguousError{Count: n}
	default:
		return strings.Replace(src, e.Old, e.New, 1), 1, nil
	}
}

// Change is what to do to one file.
type Change struct {
	// Path is the file, already resolved and confined by the caller.
	Path string
	// Edits are applied in order to the file's current content, or
	// Content replaces it (creating the file when it does not exist).
	Edits   []Edit
	Content *string
	// Expect is the git blob id of the content the caller last saw; the
	// change is refused when the file differs. "" accepts any content.
	Expect string
}

// ConflictError: the file is not the content the caller last saw.
type ConflictError struct {
	Path string
	// Got is the file's blob now ("" when it no longer exists).
	Got string
}

func (e *ConflictError) Error() string {
	if e.Got == "" {
		return e.Path + " was deleted since it was last read"
	}
	return e.Path + " changed since it was last read"
}

// EditError names the change and the edit that failed.
type EditError struct {
	Path string
	// Change is the change's position (from 0) and Index the edit's
	// within it.
	Change, Index int
	Err           error
}

func (e *EditError) Error() string { return fmt.Sprintf("edit %d: %v", e.Index+1, e.Err) }
func (e *EditError) Unwrap() error { return e.Err }

// Prepared is a change with its result computed and nothing written yet.
type Prepared struct {
	Path string
	// Before is the content now (nil: the file does not exist).
	Before []byte
	After  []byte
	Mode   fs.FileMode
	// Replacements made by the edits.
	Replacements int
}

// Prepare reads each file and computes its new content without writing
// anything. Changes to the same file apply in turn to its new content.
func Prepare(changes []Change) ([]Prepared, error) {
	var out []Prepared
	at := map[string]int{} // path -> index in out
	for ci, c := range changes {
		i, seen := at[c.Path]
		if !seen {
			p := Prepared{Path: c.Path, Mode: 0o644}
			b, err := os.ReadFile(c.Path)
			switch {
			case err == nil:
				p.Before = b
				if st, serr := os.Stat(c.Path); serr == nil {
					p.Mode = st.Mode().Perm()
				}
			case errors.Is(err, fs.ErrNotExist):
			default:
				return nil, err
			}
			p.After = p.Before
			out = append(out, p)
			i = len(out) - 1
			at[c.Path] = i
		}
		p := &out[i]
		if c.Expect != "" {
			// Only the content on disk is checked: a second change to the
			// same file follows the first.
			if got := blobOf(p.Before); !seen && got != c.Expect {
				return nil, &ConflictError{Path: c.Path, Got: got}
			}
		}
		if c.Content != nil {
			p.After = []byte(*c.Content)
			continue
		}
		if p.Before == nil && !seen {
			return nil, fmt.Errorf("%s: %w", c.Path, fs.ErrNotExist)
		}
		src := string(p.After)
		for j, e := range c.Edits {
			s, n, err := Replace(src, e)
			if err != nil {
				return nil, &EditError{Path: c.Path, Change: ci, Index: j, Err: err}
			}
			src = s
			p.Replacements += n
		}
		p.After = []byte(src)
	}
	return out, nil
}

// Commit writes prepared changes, each file atomically (a temporary file
// in the same directory, renamed over it). When one fails, the files
// already replaced are restored, so the changes apply together or not at
// all as far as the file system allows.
func Commit(ps []Prepared) error {
	for i, p := range ps {
		if err := WriteAtomic(p.Path, p.After, p.Mode); err != nil {
			for _, done := range ps[:i] {
				if done.Before == nil {
					_ = os.Remove(done.Path)
				} else {
					_ = WriteAtomic(done.Path, done.Before, done.Mode)
				}
			}
			return err
		}
	}
	return nil
}

// Apply prepares and commits changes.
func Apply(changes []Change) ([]Prepared, error) {
	ps, err := Prepare(changes)
	if err != nil {
		return nil, err
	}
	return ps, Commit(ps)
}

// WriteAtomic replaces path with data: readers see the old content or the
// new, never part of it, and an interrupted write leaves the old file.
// Missing parent directories are created as project directories (0755);
// config.WriteFileAtomic is the variant for BoundedCode's own state, which
// keeps it to the owner.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		// A directory the agent may write files in but not create them
		// in: write in place.
		return os.WriteFile(path, data, perm)
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, perm)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	return nil
}

// blobOf is git's blob id of content ("" for a file that does not exist),
// the same id the session's file stamps record.
func blobOf(content []byte) string {
	if content == nil {
		return ""
	}
	h := sha1.New() //nolint:gosec // git's object id
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

func fuzzyReplace(src, old, new string) (string, error) {
	oldLines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	if len(oldLines) == 0 || strings.TrimSpace(old) == "" {
		return "", ErrNotFound
	}
	srcLines := strings.Split(src, "\n")
	trim := strings.TrimSpace
	start := -1
	for i := 0; i+len(oldLines) <= len(srcLines); i++ {
		ok := true
		for j, ol := range oldLines {
			if trim(srcLines[i+j]) != trim(ol) {
				ok = false
				break
			}
		}
		if ok {
			if start >= 0 {
				return "", &AmbiguousError{Count: -1}
			}
			start = i
		}
	}
	if start < 0 {
		return "", ErrNotFound
	}
	// Indentation mapping: old's first line indent -> the file's.
	fileIndent := leadingWS(srcLines[start])
	oldIndent := leadingWS(oldLines[0])
	unit := indentUnit(oldLines, srcLines[start:start+len(oldLines)])
	var repl []string
	for _, nl := range strings.Split(strings.TrimRight(new, "\n"), "\n") {
		if strings.TrimSpace(nl) == "" {
			repl = append(repl, "")
			continue
		}
		ws := leadingWS(nl)
		body := nl[len(ws):]
		var extra string
		if strings.HasPrefix(ws, oldIndent) {
			extra = ws[len(oldIndent):]
		} else {
			extra = strings.TrimLeft(ws, " \t") // shallower than old's first line: keep the file's indent
		}
		if strings.HasPrefix(fileIndent, "\t") || (fileIndent == "" && strings.Contains(strings.Join(srcLines[start:start+len(oldLines)], ""), "\t")) {
			if unit > 0 && !strings.Contains(extra, "\t") {
				extra = strings.Repeat("\t", len(extra)/unit)
			}
		} else if unit > 0 && strings.Contains(extra, "\t") && !strings.Contains(fileIndent, "\t") {
			extra = strings.Repeat(" ", strings.Count(extra, "\t")*unit)
		}
		repl = append(repl, fileIndent+extra+body)
	}
	out := make([]string, 0, len(srcLines)-len(oldLines)+len(repl))
	out = append(append(append(out, srcLines[:start]...), repl...), srcLines[start+len(oldLines):]...)
	return strings.Join(out, "\n"), nil
}

func leadingWS(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// indentUnit guesses how many spaces one indentation level is in old (or,
// when the file indents with spaces and old with tabs, in the file): the
// smallest positive difference between consecutive lines' indentation.
func indentUnit(oldLines, fileLines []string) int {
	best := 0
	for _, ls := range [][]string{oldLines, fileLines} {
		for i := 1; i < len(ls); i++ {
			a, b := leadingWS(ls[i-1]), leadingWS(ls[i])
			if strings.Contains(a+b, "\t") || strings.TrimSpace(ls[i]) == "" || strings.TrimSpace(ls[i-1]) == "" {
				continue
			}
			if d := len(b) - len(a); d < 0 {
				d = -d
				if d > 0 && (best == 0 || d < best) {
					best = d
				}
			} else if d > 0 && (best == 0 || d < best) {
				best = d
			}
		}
		if best > 0 {
			return best
		}
	}
	return 4
}
