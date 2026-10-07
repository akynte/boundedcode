package verify

import (
	"bufio"
	"context"
	"go/build/constraint"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Go files behind build tags (`//go:build stringlabels`) are not compiled by
// the default `go build`/`go test`. A change to such a file can break the
// tagged variant while every default check stays green (found on Prometheus,
// whose labels package has three tag-selected implementations). The built-in
// go-build-tags stage compiles each changed package, with its tests, under
// every custom tag a changed file needs.

// platformTags are satisfied (or not) by the target platform, not by -tags.
var platformTags = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true, "illumos": true,
	"ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true, "plan9": true, "solaris": true,
	"wasip1": true, "windows": true, "zos": true, "unix": true,
	"386": true, "amd64": true, "arm": true, "arm64": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true,
	"mips64le": true, "ppc64": true, "ppc64le": true, "riscv64": true, "s390x": true, "wasm": true,
	"cgo": true, "gc": true, "gccgo": true, "ignore": true,
}

// defaultTag returns whether a build tag is satisfied by a default build
// for goos (on this machine's architecture: a container engine runs images
// of the host's architecture).
func defaultTag(goos string) func(tag string) bool {
	return func(tag string) bool {
		unix := goos != "windows" && goos != "plan9" && goos != "js" && goos != "wasip1"
		return tag == goos || tag == runtime.GOARCH || (tag == "unix" && unix) || tag == "gc" || tag == "cgo" || strings.HasPrefix(tag, "go1.")
	}
}

// customTagsNeeded returns the custom tags under which a Go file (its
// //go:build line) is compiled but the default build does not compile it.
func customTagsNeeded(path, goos string) []string {
	defaultTag := defaultTag(goos)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") && !constraint.IsGoBuild(line) {
			continue
		}
		if !constraint.IsGoBuild(line) {
			return nil // past the header: no constraint
		}
		expr, err := constraint.Parse(line)
		if err != nil || expr.Eval(defaultTag) {
			return nil
		}
		var out []string
		seen := map[string]bool{}
		var walk func(constraint.Expr)
		walk = func(e constraint.Expr) {
			switch x := e.(type) {
			case *constraint.TagExpr:
				if !platformTags[x.Tag] && !strings.HasPrefix(x.Tag, "go1.") && !seen[x.Tag] {
					seen[x.Tag] = true
					if expr.Eval(func(t string) bool { return t == x.Tag || defaultTag(t) }) {
						out = append(out, x.Tag)
					}
				}
			case *constraint.NotExpr:
				walk(x.X)
			case *constraint.AndExpr:
				walk(x.X)
				walk(x.Y)
			case *constraint.OrExpr:
				walk(x.X)
				walk(x.Y)
			}
		}
		walk(expr)
		return out
	}
	return nil
}

// buildTagsStage compiles changed Go packages under the custom build tags
// their changed files need. It is skipped when no changed file needs one.
func (e *Engine) buildTagsStage(ctx context.Context, t RepoTarget, changed []string) (StageResult, bool) {
	if _, err := os.Stat(filepath.Join(t.Worktree, "go.mod")); err != nil {
		return StageResult{}, false
	}
	// Stages compile inside the Linux container; only the unsandboxed
	// development mode builds for the host.
	goos := "linux"
	if e.Sandbox != nil && !e.Sandbox.Isolated() {
		goos = runtime.GOOS
	}
	byTag := map[string]map[string]bool{}
	for _, f := range changed {
		if !strings.HasSuffix(f, ".go") || !safeRel(f) {
			continue
		}
		for _, tag := range customTagsNeeded(filepath.Join(t.Worktree, f), goos) {
			if byTag[tag] == nil {
				byTag[tag] = map[string]bool{}
			}
			byTag[tag]["./"+filepath.ToSlash(filepath.Dir(f))] = true
		}
	}
	if len(byTag) == 0 {
		return StageResult{}, false
	}
	tags := make([]string, 0, len(byTag))
	for tag := range byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	res := StageResult{Name: "go-build-tags", Status: "pass", Command: "(built-in) go test -run ^$ -tags <tag> <changed packages>"}
	var outputs []string
	for _, tag := range tags {
		var pkgs []string
		for p := range byTag[tag] {
			pkgs = append(pkgs, p)
		}
		sort.Strings(pkgs)
		st := Stage{Name: "go-build-tags", Run: append([]string{"go", "test", "-count=1", "-tags", tag, "-run", "^$"}, pkgs...), Requires: []string{"go.mod"}}
		r := e.runStage(ctx, t, st, nil)
		outputs = append(outputs, "-tags "+tag+": "+r.Status+"\n"+r.Output)
		res.DurationMS += r.DurationMS
		if r.Status == "fail" || r.Status == "error" {
			res.Status, res.ExitCode = r.Status, r.ExitCode
		}
	}
	res.Output = tail(strings.Join(outputs, "\n"), 6000)
	return res, true
}
