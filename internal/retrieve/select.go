package retrieve

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
)

// Capabilities are what the environment offers retrieval.
type Capabilities struct {
	Git bool // the root is a git checkout
	Nav bool // a language server answers symbol questions for some language
}

var pathSeedRE = regexp.MustCompile(`^(?:[\w.-]+/)*[\w.-]+\.[A-Za-z0-9]{1,6}$|^(?:[\w.-]+/)+[\w.-]+/?$`)

// isPathSeed: a seed that names a file or a directory: a path with a
// slash, or a file name with an extension some language uses
// ("ledger.Post" is a qualified name, "ledger.go" a file).
func isPathSeed(s Seed) bool {
	if s.Kind == "diagnostic" || !pathSeedRE.MatchString(s.Text) {
		return false
	}
	return strings.Contains(s.Text, "/") || lang.ForPath(s.Text, "") != nil
}

// isNameSeed: a seed that names code (an identifier, possibly qualified).
func isNameSeed(s Seed) bool {
	return (s.Kind == "name" || s.Kind == "sample") && !isPathSeed(s) && identOnlyRE.MatchString(s.Text)
}

// Select chooses the strategies for the signals, in the order they run.
// The choice is deterministic: a strategy runs when the request or the
// evidence holds what it needs (a frame, a test name, a quoted message, a
// path, a name, a route, a commit) and the environment can serve it.
// Discovery by path words, the least reliable, runs only when nothing more
// specific is known (Signals.Strong), unless Options.Always asks for it.
func Select(sig Signals, caps Capabilities, opt Options) []Step {
	var steps []Step
	add := func(s Strategy, ok bool, why string) {
		if opt.Disable[s] {
			return
		}
		if !ok && !opt.Always[s] {
			return
		}
		if !ok {
			why = "forced (benchmark)"
		}
		steps = append(steps, Step{Strategy: s, Why: why, Ran: true})
	}
	count := func(kinds ...string) (n int) {
		for _, sd := range sig.Seeds {
			for _, k := range kinds {
				if sd.Kind == k {
					n++
				}
			}
		}
		return
	}
	paths, names := 0, 0
	for _, sd := range sig.Seeds {
		switch {
		case isPathSeed(sd):
			paths++
		case isNameSeed(sd):
			names++
		}
	}
	add(StackTrace, len(sig.Frames) > 0, fmt.Sprintf("%d stack frame(s)", len(sig.Frames)))
	add(BuildError, len(sig.Diagnostics) > 0, fmt.Sprintf("%d located diagnostic(s)", len(sig.Diagnostics)))
	add(Paths, paths > 0, fmt.Sprintf("%d path(s) named", paths))
	add(Modules, len(sig.Modules) > 0, fmt.Sprintf("%d qualified name(s)", len(sig.Modules)))
	add(TestToImpl, len(sig.Tests)+len(sig.TestFiles) > 0 || testPathNamed(sig), fmt.Sprintf("%d test(s) named or failing", len(sig.Tests)))
	add(Lexical, count("diagnostic", "config") > 0, fmt.Sprintf("%d quoted message(s) or configuration key(s)", count("diagnostic", "config")))
	why := fmt.Sprintf("%d name(s), definition patterns", names)
	if caps.Nav {
		why = fmt.Sprintf("%d name(s), language server where it serves the language", names)
	}
	add(Symbols, names > 0, why)
	add(Graph, names > 0 && sig.Intent == IntentRefactor, "a refactor: the uses of the main symbol")
	add(Contracts, len(sig.Endpoints) > 0, fmt.Sprintf("%d route(s) or endpoint(s)", len(sig.Endpoints)))
	add(DocsConfig, len(sig.EnvVars) > 0, fmt.Sprintf("%d environment variable(s)", len(sig.EnvVars)))
	if len(sig.Commits) > 0 {
		if caps.Git || opt.Always[History] {
			add(History, true, fmt.Sprintf("%d commit(s) named", len(sig.Commits)))
		} else {
			steps = append(steps, Step{Strategy: History, Why: "commits named, but the root is not a git checkout"})
		}
	}
	add(Discovery, !sig.Strong() && len(sig.Terms) > 0, "no frame, test, message, path or name: the request's words against file paths")
	return steps
}

func testPathNamed(sig Signals) bool {
	for _, sd := range sig.Seeds {
		if isPathSeed(sd) && isTestPath(sd.Text) {
			return true
		}
	}
	for _, f := range append(append([]Frame{}, sig.Frames...), sig.Diagnostics...) {
		if isTestPath(f.File) {
			return true
		}
	}
	return false
}
