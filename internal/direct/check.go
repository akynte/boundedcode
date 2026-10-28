package direct

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/akynte/boundedcode/internal/lang"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// After a file change, the cheapest deterministic check for the file's
// language (internal/lang) runs and its verdict is appended to the tool
// result: an in-process parse (Go, JSON, YAML, XML), the language's own
// parse (Python, JavaScript, shell, Ruby, PHP) or a build of the file's
// package (Go), with host tools only, in the session's sandbox. A small
// model misplaces braces and indentation often; catching that at write
// time costs well under a second, a test run costs tens of seconds and a
// whole turn of context.
//
// When a language has no check, or its tool is not installed, the model is
// told once per session, so "not checked" is never mistaken for "passed".

const (
	checkTimeout   = 60 * time.Second
	checkMaxLines  = 15
	checkMaxOutput = 2000
)

// finder looks up host tools for checks (never the project's own).
func (r *Runner) finder() *lang.Finder {
	r.finderOnce.Do(func() {
		root, err := filepath.EvalSymlinks(r.Opts.Root)
		if err != nil {
			root = r.Opts.Root
		}
		r.toolFinder = &lang.Finder{Exclude: root}
	})
	return r.toolFinder
}

// noteOnce returns msg the first time key is seen in the session.
func (r *Runner) noteOnce(key, msg string) string {
	r.checkNotesMu.Lock()
	defer r.checkNotesMu.Unlock()
	if r.checkNotes == nil {
		r.checkNotes = map[string]bool{}
	}
	if r.checkNotes[key] {
		return ""
	}
	r.checkNotes[key] = true
	return msg
}

// checkFile checks a changed file and returns a line for the model:
// "check: ok (what)", the diagnostic, or (once per language and session)
// why it is not checked; "" when nothing applies. before is the file's
// content before the change (nil when new), for notes that cover only
// what the change added.
func (r *Runner) checkFile(ctx context.Context, abs, rel string, before []byte) string {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	l := lang.ForPath(rel, lang.FirstLine(abs))
	if l == nil {
		return "" // any text: edited and searched like the rest, nothing to check
	}
	c, bin, status := r.finder().FindCheck(l, abs)
	switch status {
	case lang.Unsupported:
		if l.Kind != lang.KindProgramming {
			return ""
		}
		why := l.Note
		if why == "" {
			why = "BoundedCode has no check for it"
		}
		return r.noteOnce("unsupported "+l.ID, "check: "+l.Name+" files are not checked after edits ("+why+"); build or test the project to verify a change.")
	case lang.MissingTool:
		return r.noteOnce("missing "+l.ID, "check: "+l.Name+" files are not checked here: "+strings.Join(c.Tools, " or ")+
			" is not installed (missing tool, not a problem in the file).")
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	verdict := r.runFileCheck(ctx, l, c, bin, abs, rel, src)
	if notes := languageNotes(l.ID, rel, before, src); len(notes) > 0 && !strings.HasSuffix(verdict, "Fix this before going on.") {
		verdict += "\ncheck note (compiles, but probably not what you meant):\n- " + strings.Join(notes, "\n- ")
	}
	return verdict
}

// runFileCheck runs one check (and the one after it).
func (r *Runner) runFileCheck(ctx context.Context, l *lang.Language, c *lang.Check, bin, abs, rel string, src []byte) string {
	if c.InProcess != nil {
		if err := c.InProcess(rel, src); err != nil {
			if l.Kind == lang.KindProgramming {
				return "check: " + l.Name + " syntax error:\n" + err.Error() + "\nFix this before going on."
			}
			return "check: " + rel + " is not valid " + l.Name + ": " + err.Error()
		}
		if c.Then == nil {
			return "check: ok (" + c.What + ")"
		}
		then, thenBin, st := c.Then, "", lang.MissingTool
		for _, t := range then.Tools {
			if p := r.finder().Find(t); p != "" {
				thenBin, st = p, lang.Available
				break
			}
		}
		if st != lang.Available {
			return "check: ok (" + c.What + ")" + r.noteOnce("missing "+l.ID, "\ncheck: "+then.What+" did not run: "+strings.Join(then.Tools, " or ")+
				" is not installed (missing tool).")
		}
		out := r.runToolCheck(ctx, l, then, thenBin, abs, rel)
		if out == "" {
			return "check: ok (" + c.What + ")"
		}
		return out
	}
	return r.runToolCheck(ctx, l, c, bin, abs, rel)
}

// runToolCheck runs a tool check in the directory its Root names; "" when
// it cannot apply (no such root).
func (r *Runner) runToolCheck(ctx context.Context, l *lang.Language, c *lang.Check, bin, abs, rel string) string {
	dir := filepath.Dir(abs)
	if len(c.Root) > 0 {
		dir = ""
		for _, marker := range c.Root {
			if d := findUp(filepath.Dir(abs), r.Opts.Root, marker); d != "" {
				dir = d
				break
			}
		}
		if dir == "" {
			return ""
		}
	}
	pkgRel, err := filepath.Rel(dir, filepath.Dir(abs))
	if err != nil {
		return ""
	}
	pkg := "./" + filepath.ToSlash(pkgRel)
	if pkgRel == "." {
		pkg = "."
	}
	args := c.Args
	if len(c.TestArgs) > 0 && l.IsTest(rel) {
		args = c.TestArgs
	}
	args = lang.Expand(args, map[string]string{"file": abs, "dir": filepath.Dir(abs), "pkg": pkg, "root": dir})
	what := c.What
	if strings.Contains(strings.Join(c.Args, " "), "{pkg}") {
		what = filepath.Base(bin) + " " + args[0] + " " + pkg
	}
	out, err := r.runCheck(ctx, dir, bin, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("check: %s skipped (timed out: not verified)", what)
	case err != nil:
		return fmt.Sprintf("check: %s failed:\n%s\nFix this before going on.", what, out)
	}
	return "check: ok (" + what + ")"
}

// languageNotes are notes about code that compiles but is probably wrong,
// by language.
func languageNotes(id, rel string, before, after []byte) []string {
	if id == "go" {
		return newPitfalls(rel, before, after)
	}
	return nil
}

// runCheck runs a check command and returns its trimmed output (stdout and
// stderr, capped) and whether it failed.
func (r *Runner) runCheck(ctx context.Context, dir, bin string, args ...string) (string, error) {
	c, err := r.command(ctx, append([]string{bin}, args...), dir, []string{"GIT_TERMINAL_PROMPT=0"}, "")
	if err != nil {
		return err.Error(), err
	}
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	err = c.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	out := strings.TrimSpace(buf.String())
	lines := strings.Split(out, "\n")
	if len(lines) > checkMaxLines {
		lines = append(lines[:checkMaxLines], fmt.Sprintf("… (%d more lines)", len(lines)-checkMaxLines))
	}
	out = strings.Join(lines, "\n")
	if len(out) > checkMaxOutput {
		out = out[:checkMaxOutput] + "…"
	}
	return out, err
}

// findUp returns the nearest directory from dir up to (and including)
// stop that contains name, or "".
func findUp(dir, stop, name string) string {
	stop = filepath.Clean(stop)
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, name)); err == nil {
			return d
		}
		if d == stop || filepath.Dir(d) == d {
			return ""
		}
	}
}
