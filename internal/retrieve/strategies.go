package retrieve

import (
	"context"
	"math"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/execbox"
	"github.com/akynte/boundedcode/internal/lang"
	"github.com/akynte/boundedcode/internal/repointel"
)

// Reliability of each kind of finding: how often the place it points at
// is one the task needs. A frame or a compiler's location is where the
// failure happened; a definition of a named symbol is usually where to
// look; a file whose path shares a word with the request often is not.
const (
	relFrame       = 0.95
	relBuild       = 0.9
	relPath        = 0.9
	relModuleLong  = 0.85 // three or more segments of a qualified name
	relModule      = 0.7  // two
	relBareName    = 0.5  // a file name without directories that several files share
	relMessage     = 0.85
	relDefLSP      = 0.85
	relDefText     = 0.8
	relImplByName  = 0.75
	relTestDef     = 0.6
	relConfig      = 0.6
	relHistory     = 0.6
	relContract    = 0.7
	relContractUse = 0.55
	relTestUses    = 0.5
	relRefs        = 0.5
	relDiscovery   = 0.35
	relUse         = 0.25
	// sampleFactor discounts names that appear only in a request's code
	// samples (often the caller's code, not the project's).
	sampleFactor = 0.6
	// maxUseFiles: a name used in more files than this is too common for
	// its uses to point anywhere.
	maxUseFiles = 40
)

// specificity is how much an anchor found in n files says about each:
// 1 for one file, falling slowly (a name defined in an interface and its
// implementation still points well).
func specificity(n int) float64 {
	if n <= 1 {
		return 1
	}
	return 1 / (1 + 0.5*math.Log2(float64(n)))
}

// collector accumulates reasons per file.
type collector struct {
	r     *repo
	files map[string]*acc
}

type acc struct {
	why    []Reason
	line   int
	symbol string
	best   float64 // weight of the reason that set line
}

func newCollector(r *repo) *collector { return &collector{r: r, files: map[string]*acc{}} }

// best is the weight of the strongest reason collected for a file outside
// tests (a test is evidence, not the place to change).
func (c *collector) best() float64 {
	b := 0.0
	for f, a := range c.files {
		if isTestPath(f) || isTestData(f) {
			continue
		}
		for _, w := range a.why {
			b = max(b, w.Weight)
		}
	}
	return b
}

func (c *collector) add(file string, why Reason, symbol string) {
	if why.Weight <= 0 {
		return
	}
	a := c.files[file]
	if a == nil {
		a = &acc{}
		c.files[file] = a
	}
	a.why = append(a.why, why)
	if why.Line > 0 && why.Weight > a.best {
		a.line, a.symbol, a.best = why.Line, symbol, why.Weight
	}
}

// runner runs the selected strategies over one repository view.
type runner struct {
	ctx  context.Context
	sig  Signals
	r    *repo
	c    *collector
	opt  *Options
	scan scanResult // the shared content scan
}

// needles are what the selected strategies look for in file contents:
// one scan serves them all.
func (x *runner) needles(steps []Step) []needle {
	var out []needle
	seen := map[string]bool{}
	add := func(t string, word bool) {
		if len(t) < 3 || seen[t] || len(out) >= 32 {
			return
		}
		seen[t] = true
		out = append(out, needle{text: t, word: word})
	}
	on := map[Strategy]bool{}
	for _, s := range steps {
		on[s.Strategy] = s.Ran
	}
	if on[TestToImpl] {
		for _, t := range x.sig.Tests {
			add(t, identOnlyRE.MatchString(t))
		}
	}
	for _, sd := range x.sig.Seeds {
		switch {
		case sd.Kind == "diagnostic" && on[Lexical]:
			add(sd.Text, false)
		case sd.Kind == "config" && (on[Lexical] || on[DocsConfig]):
			add(sd.Text, true)
		case isNameSeed(sd) && on[Symbols]:
			add(sd.Text, true)
			if last := lastSegment(sd.Text); last != sd.Text {
				add(last, true)
			}
		}
	}
	if on[Contracts] {
		for _, e := range x.sig.Endpoints {
			add(e, false)
		}
	}
	if on[DocsConfig] {
		for _, e := range x.sig.EnvVars {
			add(e, true)
		}
	}
	return out
}

func (x *runner) run(s Strategy) int {
	switch s {
	case StackTrace:
		return x.frames(x.sig.Frames, StackTrace, relFrame, 0.8)
	case BuildError:
		return x.frames(x.sig.Diagnostics, BuildError, relBuild, 0.85)
	case Paths:
		return x.paths()
	case Modules:
		return x.modules()
	case TestToImpl:
		return x.testToImpl()
	case Lexical:
		return x.lexical()
	case Symbols:
		return x.symbols()
	case Graph:
		return x.graph()
	case Contracts:
		return x.contracts()
	case DocsConfig:
		return x.docsConfig()
	case History:
		return x.history()
	case Discovery:
		return x.discovery()
	}
	return 0
}

// frames maps frames onto files; the innermost weighs most.
func (x *runner) frames(fs []Frame, s Strategy, rel, decay float64) int {
	n, inRepo := 0, 0
	for _, f := range fs {
		if inRepo >= 10 {
			break
		}
		cands := x.r.resolve(f.File, f.Symbol)
		if len(cands) == 0 {
			continue // a dependency's or the runtime's frame
		}
		n++
		w := rel * math.Pow(decay, float64(inRepo)) / float64(len(cands))
		inRepo++
		for _, c := range cands {
			sym, _ := x.r.enclosing(c, f.Line)
			anchor := f.File + ":" + strconv.Itoa(f.Line)
			x.c.add(c, Reason{Strategy: s, Anchor: anchor, Line: f.Line, Weight: w}, sym)
		}
	}
	return n
}

// paths resolves the files and directories the request names.
func (x *runner) paths() int {
	n := 0
	for _, sd := range x.sig.Seeds {
		if !isPathSeed(sd) {
			continue
		}
		n++
		p := strings.TrimSuffix(sd.Text, "/")
		if cands := x.r.resolve(p, ""); len(cands) > 0 {
			rel := relPath
			if sd.Kind == "sample" {
				rel *= sampleFactor // an include or import in a code sample
			}
			if !strings.Contains(p, "/") && len(x.r.byBase[path.Base(p)]) > 1 {
				rel = relBareName // "Cargo.toml" in a workspace of crates: which one?
			}
			for _, c := range cands {
				x.c.add(c, Reason{Strategy: Paths, Anchor: sd.Text, Weight: rel / float64(len(cands))}, "")
			}
			continue
		}
		// A directory: its files, a little each.
		var in []string
		for _, f := range x.r.files {
			if strings.HasPrefix(f, p+"/") || strings.Contains(f, "/"+p+"/") {
				in = append(in, f)
			}
		}
		if len(in) > 0 && len(in) <= 60 {
			for _, f := range in {
				x.c.add(f, Reason{Strategy: Paths, Anchor: sd.Text, Weight: 0.4 * specificity(len(in))}, "")
			}
		}
	}
	return n
}

// layoutDirs are directories a module's name leaves out (src/main/java/,
// lib/, src/).
var layoutDirs = map[string]bool{"src": true, "lib": true, "main": true, "java": true, "kotlin": true, "scala": true, "source": true, "sources": true}

// moduleStems are file names that stand for their directory's module.
var moduleStems = map[string]bool{"mod": true, "__init__": true, "index": true, "lib": true}

// moduleKey is a file's module path: its directories without layout
// directories, then its stem unless the stem stands for the directory.
func moduleKey(rel string) []string {
	var out []string
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		if i == len(parts)-1 {
			stem := strings.TrimSuffix(p, path.Ext(p))
			if !moduleStems[stem] {
				out = append(out, stem)
			}
			continue
		}
		if !layoutDirs[p] {
			out = append(out, p)
		}
	}
	return out
}

// modules maps qualified names onto files: the longest run of a name's
// segments that ends a file's module path (or a directory's) names it.
func (x *runner) modules() int {
	byLast := map[string][]string{}
	keys := map[string][]string{}
	for _, f := range x.r.files {
		if l := x.r.langOf[f]; l == nil || l.Kind != lang.KindProgramming || isTestPath(f) || isTestData(f) {
			continue
		}
		k := moduleKey(f)
		if len(k) == 0 {
			continue
		}
		keys[f] = k
		byLast[k[len(k)-1]] = append(byLast[k[len(k)-1]], f)
	}
	endsWith := func(k, w []string) bool {
		if len(w) > len(k) {
			return false
		}
		for i := range w {
			if k[len(k)-len(w)+i] != w[i] {
				return false
			}
		}
		return true
	}
	n := 0
	for _, segs := range x.sig.Modules {
		if n == 12 {
			break
		}
		var best []string
		bestLen := 0
		for end := len(segs); end >= 2 && bestLen == 0; end-- {
			for start := 0; start <= end-2; start++ {
				w := segs[start:end]
				var cands []string
				for _, f := range byLast[w[len(w)-1]] {
					if endsWith(keys[f], w) {
						cands = append(cands, f)
					}
				}
				if len(cands) > 0 && len(cands) <= 3 {
					best, bestLen = cands, len(w)
					break
				}
			}
		}
		if bestLen == 0 {
			continue
		}
		n++
		rel := relModule
		if bestLen >= 3 {
			rel = relModuleLong
		}
		anchor := strings.Join(segs, ".")
		for _, f := range best {
			x.c.add(f, Reason{Strategy: Modules, Anchor: "module " + anchor, Weight: rel / float64(len(best))}, "")
		}
	}
	return n
}

// testToImpl finds the tests, then the code they exercise.
func (x *runner) testToImpl() int {
	tests := map[string]int{} // test file -> line of the test (0: unknown)
	for _, t := range x.sig.Tests {
		for _, h := range x.scan.hits[t] {
			if !isTestPath(h.file) {
				continue
			}
			if _, ok := tests[h.file]; !ok || h.def {
				if h.def || tests[h.file] == 0 {
					tests[h.file] = h.line
				}
			}
		}
	}
	for _, sd := range x.sig.Seeds {
		if isPathSeed(sd) && isTestPath(sd.Text) {
			for _, c := range x.r.resolve(sd.Text, "") {
				if _, ok := tests[c]; !ok {
					tests[c] = 0
				}
			}
		}
	}
	for _, tf := range x.sig.TestFiles {
		cands := x.r.resolve(tf, "")
		if len(cands) == 0 && !strings.Contains(tf, "/") && !strings.Contains(tf, ".") {
			cands = x.r.byStem(tf) // a JVM test class
		}
		for _, c := range cands {
			if _, ok := tests[c]; !ok {
				tests[c] = 0
			}
		}
	}
	for _, f := range append(append([]Frame{}, x.sig.Frames...), x.sig.Diagnostics...) {
		if isTestPath(f.File) {
			for _, c := range x.r.resolve(f.File, f.Symbol) {
				if tests[c] == 0 {
					tests[c] = f.Line
				}
			}
		}
	}
	files := sortedKeys(tests)
	if len(files) > 6 {
		files = files[:6] // the test of a common name in many suites says little
	}
	var bodyNames []string
	for _, tf := range files {
		x.c.add(tf, Reason{Strategy: TestToImpl, Anchor: "test " + path.Base(tf), Line: tests[tf], Weight: relTestDef * specificity(len(files))}, "")
		impl := x.r.implCandidates(tf)
		if len(impl) > 3 {
			impl = impl[:3]
		}
		for i, f := range impl {
			x.c.add(f, Reason{Strategy: TestToImpl, Anchor: "implementation of " + tf, Weight: relImplByName * math.Pow(0.6, float64(i))}, "")
		}
		if tests[tf] > 0 {
			bodyNames = append(bodyNames, x.testBodyNames(tf, tests[tf])...)
		}
	}
	// The names a test body uses, defined outside tests: a second scan of
	// the non-test files only.
	if len(bodyNames) > 0 {
		var ns []needle
		seen := map[string]bool{}
		for _, b := range bodyNames {
			if !seen[b] && len(ns) < 16 {
				seen[b] = true
				ns = append(ns, needle{text: b, word: true})
			}
		}
		res := x.r.scan(x.ctx, ns, func(rel string) bool { return !isTestPath(rel) })
		for _, n := range ns {
			var defs []hit
			for _, h := range res.hits[n.text] {
				if h.def {
					defs = append(defs, h)
				}
			}
			if len(defs) == 0 || len(defs) > 4 {
				continue
			}
			for _, h := range defs {
				x.c.add(h.file, Reason{Strategy: TestToImpl, Anchor: "used by the test: " + n.text, Line: h.line, Weight: relTestUses * specificity(len(defs))}, n.text)
			}
		}
	}
	return len(files)
}

var stringLitRE = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|'(?:[^'\\\\]|\\\\.)*'|`[^`]*`")

var bodyIdentRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]{3,})\s*[(.{]|\b([A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+)\b`)

// testBodyNames are the code-like names a test's body uses: calls and
// types, not the test framework's assertions.
func (x *runner) testBodyNames(file string, line int) []string {
	data, ok := x.r.read(file)
	if !ok {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return nil
	}
	l := x.r.langOf[file]
	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }
	start := indent(lines[line-1])
	var out []string
	seen := map[string]bool{}
	for i := line; i < len(lines) && i < line+80; i++ {
		s := lines[i]
		if strings.TrimSpace(s) != "" && indent(s) <= start {
			if _, ok := l.Definition(s); ok {
				break // the next test
			}
		}
		// Code inside the test's strings (sample input, expected output)
		// is data, not what the test calls.
		s = stringLitRE.ReplaceAllString(s, `""`)
		for _, m := range bodyIdentRE.FindAllStringSubmatch(s, -1) {
			n := m[1]
			if n == "" {
				n = m[2]
			}
			low := strings.ToLower(n)
			if seen[n] || stopwords[low] || testFramework[low] || strings.HasPrefix(low, "assert") || strings.HasPrefix(low, "expect") || strings.HasPrefix(low, "test") {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// testFramework are names test code uses that are not the project's.
var testFramework = map[string]bool{"self": true, "this": true, "fatal": true, "fatalf": true, "errorf": true, "error": true,
	"equal": true, "equals": true, "require": true, "should": true, "mock": true, "spy": true, "describe": true, "context": true,
	"func": true, "function": true, "return": true, "const": true, "let": true, "var": true, "new": true, "len": true, "make": true,
	"append": true, "string": true, "print": true, "println": true, "printf": true, "sprintf": true, "format": true, "list": true,
	"dict": true, "range": true, "with": true, "raises": true, "pytest": true, "unittest": true, "json": true, "console": true,
	"system": true, "math": true, "time": true, "strings": true, "bytes": true, "true": true, "false": true, "null": true, "none": true,
	"nil": true, "object": true, "array": true, "promise": true, "async": true, "await": true, "then": true, "catch": true}

// lexical: quoted messages where they are raised, configuration keys
// where they are read or set.
func (x *runner) lexical() int {
	n := 0
	for _, sd := range x.sig.Seeds {
		var rel float64
		switch sd.Kind {
		case "diagnostic":
			rel = relMessage
		case "config":
			rel = relConfig
		default:
			continue
		}
		n++
		files := x.scan.files[sd.Text]
		if files == 0 || files > maxUseFiles {
			continue
		}
		perFile := map[string]bool{}
		for _, h := range x.scan.hits[sd.Text] {
			if perFile[h.file] {
				continue
			}
			perFile[h.file] = true
			sym, _ := x.r.enclosing(h.file, h.line)
			x.c.add(h.file, Reason{Strategy: Lexical, Anchor: trunc(sd.Text, 60), Line: h.line, Weight: rel * specificity(files)}, sym)
		}
	}
	return n
}

// symbols: definitions of the names in the request (a language server's
// where one serves the file's language), then, weakly, their uses.
func (x *runner) symbols() int {
	n := 0
	lsp := x.opt.Nav != nil && x.opt.Nav.Name() != "text"
	nav := x.opt.Nav
	if c, ok := nav.(repointel.Composite); ok {
		c.NoText = true // the scan has the definition patterns' answers
		nav = c
	}
	asked := 0
	for _, sd := range x.sig.Seeds {
		if !isNameSeed(sd) {
			continue
		}
		n++
		factor := 1.0
		named := sd.Text
		if sd.Kind == "sample" {
			factor, named = sampleFactor, ""
		}
		names := []string{sd.Text}
		if last := lastSegment(sd.Text); last != sd.Text {
			names = append(names, last)
		}
		var defs []hit
		for _, nm := range names {
			for _, h := range x.scan.hits[nm] {
				if h.def {
					defs = append(defs, h)
				}
			}
			if len(defs) > 0 {
				break
			}
		}
		defs = byQualifier(sd.Text, defs)
		if lsp && asked < 4 && x.ctx.Err() == nil {
			asked++
			ctx, cancel := context.WithTimeout(x.ctx, min(1500*time.Millisecond, time.Until(x.r.deadline)))
			syms, err := nav.FindSymbol(ctx, x.r.root, sd.Text, repointel.FindOptions{})
			cancel()
			if err == nil {
				for _, s := range syms {
					if len(syms) > 8 {
						break
					}
					x.c.add(s.File, Reason{Strategy: Symbols, Anchor: "defines " + sd.Text, Line: s.StartLine, Weight: relDefLSP * factor * specificity(len(syms)), Named: named}, s.NamePath)
				}
			}
		}
		defFiles := map[string]bool{}
		for _, h := range defs {
			defFiles[h.file] = true
		}
		for _, h := range defs {
			x.c.add(h.file, Reason{Strategy: Symbols, Anchor: "defines " + sd.Text, Line: h.line, Weight: relDefText * factor * specificity(len(defFiles)), Named: named}, lastSegment(sd.Text))
		}
		// Uses, for a specific name only.
		files := x.scan.files[sd.Text]
		if files == 0 || files > maxUseFiles {
			continue
		}
		seen := map[string]bool{}
		for _, h := range x.scan.hits[sd.Text] {
			if h.def || seen[h.file] || defFiles[h.file] {
				continue
			}
			seen[h.file] = true
			x.c.add(h.file, Reason{Strategy: Symbols, Anchor: "uses " + sd.Text, Line: h.line, Weight: relUse * factor * specificity(files), Named: named}, "")
		}
	}
	return n
}

// byQualifier keeps, of a member's definitions in several files, those in
// a file named after its qualifier ("Ledger.Balance": ledger.go,
// "Inventory.remove": inventory.py), when there are any.
func byQualifier(name string, defs []hit) []hit {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return defs
	}
	q := strings.ToLower(strings.ReplaceAll(name[:i], "_", ""))
	var keep []hit
	for _, h := range defs {
		base := path.Base(h.file)
		base = strings.TrimSuffix(base, path.Ext(base))
		if strings.ReplaceAll(strings.ToLower(base), "_", "") == q {
			keep = append(keep, h)
		}
	}
	if len(keep) == 0 {
		return defs
	}
	return keep
}

// graph: the uses of the main symbols, which a rename or a refactor must
// change (a language server's references where one serves the language).
func (x *runner) graph() int {
	n := 0
	for _, sd := range x.sig.Seeds {
		if !isNameSeed(sd) || sd.Kind == "sample" || n == 2 {
			continue
		}
		n++
		var refs []repointel.Reference
		if x.opt.Nav != nil && x.opt.Nav.Name() != "text" {
			ctx, cancel := context.WithTimeout(x.ctx, min(2*time.Second, time.Until(x.r.deadline)))
			if syms, err := x.opt.Nav.FindSymbol(ctx, x.r.root, sd.Text, repointel.FindOptions{}); err == nil && len(syms) > 0 && len(syms) <= 3 {
				refs, _ = x.opt.Nav.References(ctx, x.r.root, syms[0])
			}
			cancel()
		}
		if len(refs) == 0 {
			for _, h := range x.scan.hits[lastSegment(sd.Text)] {
				if !h.def {
					refs = append(refs, repointel.Reference{File: h.file, Line: h.line})
				}
			}
		}
		files := map[string]int{}
		for _, ref := range refs {
			if _, ok := files[ref.File]; !ok {
				files[ref.File] = ref.Line
			}
		}
		for _, f := range sortedKeys(files) {
			x.c.add(f, Reason{Strategy: Graph, Anchor: "references " + sd.Text, Line: files[f], Weight: relRefs * specificity(len(files)/4+1)}, "")
		}
	}
	return n
}

// contracts: routes and endpoints in API definitions and handlers.
func (x *runner) contracts() int {
	for _, e := range x.sig.Endpoints {
		files := x.scan.files[e]
		if files == 0 || files > maxUseFiles {
			continue
		}
		seen := map[string]bool{}
		for _, h := range x.scan.hits[e] {
			if seen[h.file] {
				continue
			}
			seen[h.file] = true
			rel := relContractUse
			if isContractFile(h.file) {
				rel = relContract
			}
			sym, _ := x.r.enclosing(h.file, h.line)
			x.c.add(h.file, Reason{Strategy: Contracts, Anchor: e, Line: h.line, Weight: rel * specificity(files)}, sym)
		}
	}
	return len(x.sig.Endpoints)
}

func isContractFile(rel string) bool {
	low := strings.ToLower(path.Base(rel))
	switch path.Ext(low) {
	case ".proto", ".graphql", ".gql", ".graphqls", ".thrift", ".avsc":
		return true
	}
	return strings.Contains(low, "openapi") || strings.Contains(low, "swagger") || strings.Contains(low, "asyncapi")
}

// docsConfig: environment variables where they are read, set and
// documented.
func (x *runner) docsConfig() int {
	n := 0
	for _, e := range x.sig.EnvVars {
		n++
		files := x.scan.files[e]
		if files == 0 || files > maxUseFiles {
			continue
		}
		seen := map[string]bool{}
		for _, h := range x.scan.hits[e] {
			if !seen[h.file] {
				seen[h.file] = true
				x.c.add(h.file, Reason{Strategy: DocsConfig, Anchor: e, Line: h.line, Weight: relConfig * specificity(files)}, "")
			}
		}
	}
	// Documentation found by words was measured and dropped: on the
	// development split it never raised recall and lowered precision
	// (docs/benchmarks/retrieval-2026-10.md).
	return n
}

// history: the files of the commits the request names.
func (x *runner) history() int {
	n := 0
	for _, sha := range x.sig.Commits {
		c := exec.CommandContext(x.ctx, "git", append(append([]string{}, execbox.SafeGitArgs...), "show", "--no-renames", "--name-only", "--format=", "--end-of-options", sha+"^{commit}")...)
		c.Dir = x.r.root
		out, err := c.Output()
		if err != nil {
			continue
		}
		n++
		var files []string
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if _, ok := x.r.langOf[f]; ok {
				files = append(files, f)
			}
		}
		for _, f := range files {
			x.c.add(f, Reason{Strategy: History, Anchor: "commit " + sha, Weight: relHistory * specificity(len(files))}, "")
		}
	}
	return n
}

// discovery scores files by the request's words in their paths, weighted
// by how rare each word is among the paths and how often the request
// repeats it.
func (x *runner) discovery() int {
	ts := x.sig.Terms
	if len(ts) > 30 {
		ts = ts[:30]
	}
	if len(ts) == 0 {
		return 0
	}
	want := map[string]string{} // stem -> term
	for _, t := range ts {
		want[stem(t)] = t
	}
	df := map[string]int{}
	type cand struct {
		file    string
		matched []string
	}
	var cands []cand
	for _, f := range x.r.files {
		var m []string
		seen := map[string]bool{}
		for _, tok := range pathTokens(f) {
			st := stem(tok)
			if t, ok := want[st]; ok && !seen[st] {
				seen[st] = true
				m = append(m, t)
			}
		}
		for st := range seen {
			df[st]++
		}
		if len(m) > 0 {
			cands = append(cands, cand{f, m})
		}
	}
	if len(cands) == 0 {
		return 0
	}
	total := float64(len(x.r.files))
	tf := func(t string) float64 { return 1 + math.Log(float64(max(x.sig.TermFreq[t], 1))) }
	score := func(c cand) float64 {
		s := 0.0
		for _, t := range c.matched {
			s += math.Log(total/float64(1+df[stem(t)])) * tf(t)
		}
		// The file's own name matching counts double.
		b := path.Base(c.file)
		for _, tok := range pathTokens(strings.TrimSuffix(b, path.Ext(b))) {
			if t, ok := want[stem(tok)]; ok {
				s += math.Log(total/float64(1+df[stem(tok)])) * tf(t)
			}
		}
		return s
	}
	sort.SliceStable(cands, func(i, j int) bool {
		si, sj := score(cands[i]), score(cands[j])
		if si != sj {
			return si > sj
		}
		return cands[i].file < cands[j].file
	})
	top := score(cands[0])
	if top <= 0 {
		return 0
	}
	for i, c := range cands {
		if i == 10 {
			break
		}
		x.c.add(c.file, Reason{Strategy: Discovery, Anchor: "path words " + strings.Join(c.matched, ","), Weight: relDiscovery * score(c) / top}, "")
	}
	return len(ts)
}

// pathTokens splits a path into lower-case words.
func pathTokens(p string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '.' || r == '_' || r == '-' || r == ' ' }) {
		out = append(out, splitIdent(part)...)
	}
	return out
}

// stem is a crude, deterministic stem for matching words with paths:
// "routing", "router" and "routes" share "rout".
func stem(w string) string {
	w = strings.ToLower(w)
	for _, suf := range []string{"ations", "ation", "ings", "ing", "ers", "er", "ies", "es", "s", "ed"} {
		if len(w) > len(suf)+3 && strings.HasSuffix(w, suf) {
			w = strings.TrimSuffix(w, suf)
			break
		}
	}
	if len(w) > 6 {
		w = w[:6]
	}
	return w
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
