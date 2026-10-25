package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// After a file change, the cheapest deterministic check for the file's
// language runs and its verdict is appended to the tool result: a parse
// for Go (then a build of its package when the Go toolchain is present),
// a parse for Python, JavaScript, shell, Ruby and PHP when their
// interpreters are present, and an in-process parse for JSON and YAML.
// A small model misplaces braces and indentation often; catching that at
// write time costs well under a second, a test run costs tens of seconds
// and a whole turn of context.

const (
	checkTimeout   = 60 * time.Second
	checkMaxLines  = 15
	checkMaxOutput = 2000
)

// checkFile checks a changed file and returns a line for the model:
// "check: ok (what)" or the diagnostic; "" when no check applies.
func (r *Runner) checkFile(ctx context.Context, abs, rel string) string {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".go":
		return r.checkGo(ctx, abs, rel)
	case ".py":
		return r.checkWith(ctx, "python3", "python3", "-c", pyParse, abs)
	case ".js", ".mjs", ".cjs":
		return r.checkWith(ctx, "node", "node", "--check", abs)
	case ".sh", ".bash":
		return r.checkWith(ctx, "bash", "bash", "-n", abs)
	case ".rb":
		return r.checkWith(ctx, "ruby", "ruby", "-c", abs)
	case ".php":
		return r.checkWith(ctx, "php", "php", "-l", abs)
	case ".json":
		b, err := os.ReadFile(abs)
		if err != nil {
			return ""
		}
		if len(bytes.TrimSpace(b)) == 0 {
			return "check: " + rel + " is empty, not JSON"
		}
		if !json.Valid(b) {
			var v any
			err := json.Unmarshal(b, &v)
			return "check: " + rel + " is not valid JSON: " + errString(err)
		}
		return "check: ok (json)"
	case ".yaml", ".yml":
		b, err := os.ReadFile(abs)
		if err != nil {
			return ""
		}
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			return "check: " + rel + " is not valid YAML: " + err.Error()
		}
		return "check: ok (yaml)"
	}
	return ""
}

// pyParse parses a Python file without importing it or writing bytecode.
const pyParse = "import ast,sys\nsrc=open(sys.argv[1],'rb').read()\nast.parse(src, sys.argv[1])\n"

// checkWith runs a syntax check with an interpreter when it is installed.
func (r *Runner) checkWith(ctx context.Context, what, bin string, args ...string) string {
	path, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	out, err := r.runCheck(ctx, r.cwd(), path, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("check: %s skipped (timed out)", what)
	case err != nil:
		return fmt.Sprintf("check: %s reports:\n%s\nFix this before going on.", what, out)
	}
	return "check: ok (" + what + " parse)"
}

// checkGo parses the file, then builds (or vets, for a test file) its
// package when the Go toolchain is installed.
func (r *Runner) checkGo(ctx context.Context, abs, rel string) string {
	src, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution); perr != nil {
		return "check: Go syntax error:\n" + perr.Error() + "\nFix this before going on."
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "check: ok (Go parse)"
	}
	modRoot := findUp(filepath.Dir(abs), r.Opts.Root, "go.mod")
	if modRoot == "" {
		return "check: ok (Go parse)"
	}
	pkgRel, err := filepath.Rel(modRoot, filepath.Dir(abs))
	if err != nil {
		return "check: ok (Go parse)"
	}
	pkg := "./" + filepath.ToSlash(pkgRel)
	if pkgRel == "." {
		pkg = "."
	}
	args := []string{"build", "-o", os.DevNull, pkg}
	what := "go build " + pkg
	if strings.HasSuffix(abs, "_test.go") {
		args = []string{"vet", pkg}
		what = "go vet " + pkg
	}
	out, err := r.runCheck(ctx, modRoot, goBin, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("check: %s skipped (timed out)", what)
	case err != nil:
		return fmt.Sprintf("check: %s failed:\n%s\nFix this before going on.", what, out)
	}
	return "check: ok (" + what + ")"
}

// runCheck runs a check command and returns its trimmed output (stdout and
// stderr, capped) and whether it failed.
func (r *Runner) runCheck(ctx context.Context, dir, bin string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, bin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	err := c.Run()
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
