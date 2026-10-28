package retrieve

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/akynte/boundedcode/internal/lang"
)

// Intent is what the request asks for, as far as retrieval cares.
type Intent string

// Intents. A request can read as several; the first match in this order
// wins (a "failing test" is a fix before it is a test task).
const (
	IntentFix      Intent = "fix"      // a bug, a crash, a failure
	IntentRefactor Intent = "refactor" // rename, move, every use of something
	IntentExplain  Intent = "explain"  // how something works, where it is
	IntentTest     Intent = "test"     // write or change tests
	IntentConfig   Intent = "config"   // configuration, flags, documentation
	IntentChange   Intent = "change"   // a feature or another change
)

var intentRules = []struct {
	intent Intent
	re     *regexp.Regexp
}{
	{IntentFix, regexp.MustCompile(`(?i)\b(fix|bug|broken|crash(es|ed)?|panics?|exception|traceback|regression|fails?|failing|failure|incorrect(ly)?|wrong|error|doesn'?t work|does not work|unexpected(ly)?|should(n'?t| not)? (return|be|accept|reject))\b`)},
	{IntentRefactor, regexp.MustCompile(`(?i)\b(rename|refactor|move|extract|inline|every (use|call|caller)|all (uses|usages|callers|call sites)|who calls|callers of|usages of|deprecate)\b`)},
	{IntentExplain, regexp.MustCompile(`(?i)\b(explain|how does|how do|what does|why does|describe|walk me through|where is|where are|which (file|function|class|method)|find where|locate)\b`)},
	{IntentTest, regexp.MustCompile(`(?i)\b(add|write|extend|improve) (a |more |unit |integration )?tests?\b|\btest coverage\b`)},
	{IntentConfig, regexp.MustCompile(`(?i)\b(config(uration)?|setting|option|flag|environment variable|env var|documentation|docs|readme)\b`)},
}

func classify(request string) Intent {
	for _, r := range intentRules {
		if r.re.MatchString(request) {
			return r.intent
		}
	}
	return IntentChange
}

// Frame is a source location from a stack trace or a tool's diagnostic.
type Frame struct {
	File   string
	Line   int
	Symbol string
	// Kind is "trace" (a stack frame) or "build" (a compiler, linter or
	// test runner's located message).
	Kind    string
	Message string
	// Order is the frame's position among the frames of its kind,
	// innermost (the failing call) first.
	Order int
}

// Signals are what a request and the evidence collected so far say about
// where to look. Extract finds them; Select turns them into strategies.
type Signals struct {
	Intent      Intent
	Frames      []Frame    // stack frames
	Diagnostics []Frame    // build, lint and test-runner locations
	Tests       []string   // test names reported failing or named in the request
	TestFiles   []string   // test files or classes those reports name
	Seeds       []Seed     // names, quoted messages, config keys, paths (Seeds)
	Endpoints   []string   // HTTP paths, RPC methods
	Modules     [][]string // qualified names split into segments (tokio::sync::broadcast, django.db.models)
	EnvVars     []string   // UPPER_SNAKE environment variables
	Commits     []string   // commit ids named in the request
	Terms       []string   // content words, for path discovery
	// TermFreq counts each term's occurrences: a word the request repeats
	// is what it is about.
	TermFreq map[string]int
}

// Strong reports whether the signals locate something by themselves (a
// frame, a diagnostic, a failing test, a quoted message, a path or a
// code-like name) rather than only by words.
func (s Signals) Strong() bool {
	if len(s.Frames)+len(s.Diagnostics)+len(s.Tests)+len(s.TestFiles) > 0 {
		return true
	}
	for _, sd := range s.Seeds {
		if sd.Kind != "sample" {
			return true
		}
	}
	return false
}

var (
	// Test names as runners report failures and people name tests.
	failedTestRE = []*regexp.Regexp{
		regexp.MustCompile(`--- FAIL: (\w+)`),                                                  // go test
		regexp.MustCompile(`FAILED [\w/.-]+\.py::(?:\w+::)?(\w+)`),                             // pytest
		regexp.MustCompile(`(?m)^(?:ERROR|FAIL): (\w+) \(`),                                    // unittest
		regexp.MustCompile(`test ([\w:]+) \.\.\. FAILED`),                                      // cargo test
		regexp.MustCompile(`\d+\) [\w\\]+::(\w+)`),                                             // phpunit
		regexp.MustCompile(`(?m)^\s*(\w+)\(([\w.$]+)\)\s+Time elapsed.*<<< (?:FAILURE|ERROR)`), // surefire
		regexp.MustCompile(`\[ERROR\]\s+([\w.$]+\.\w+):\d+`),                                   // surefire summary Class.method:line
		regexp.MustCompile(`(?m)^\s*● (?:[^›\n]+ › )*([^›\n]+)$`),                              // jest
	}
	// A failed test's id as most runners print it on its own line:
	// "FAILED tests/test_x.py::test_y[1]", "FAIL: com.acme.OrdersTest#paid",
	// "✕ routing::tests::nest (5 ms)".
	failedIDRE  = regexp.MustCompile(`(?m)^\s*(?:FAIL(?:ED)?|ERROR|✕|✗|×)\s*:?\s+(\S[^\n]*?)\s*(?:\(\d+(?:\.\d+)?\s*m?s\))?$`)
	namedTestRE = regexp.MustCompile(`\b(Test[A-Z_]\w*|test_\w+|\w+Tests?\b|\w+_spec|\w+Spec)\b`)
	// A route starts a token: "internal/ledger" holds no route "/ledger".
	endpointRE = regexp.MustCompile("(?:^|[\\s\"'`(\\[])(/(?:[\\w.{}:-]+/)+[\\w.{}:-]*|/[a-z][\\w-]{2,}/?)\\b")
	envVarRE   = regexp.MustCompile(`\b([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)\b`)
	commitRE   = regexp.MustCompile(`\b(?:commit|revert(?:ed|s)?|since|after|before|introduced (?:in|by))\s+([0-9a-f]{7,40})\b`)
	termRE     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]+`)
	// a::b::c (Rust, C++), a.b.c (Python, Java, JavaScript), A\B\C (PHP).
	qualifiedRE    = regexp.MustCompile(`\b[A-Za-z_]\w*(?:(?:::|\\|\.)[A-Za-z_]\w*)+\b`)
	qualifiedSepRE = regexp.MustCompile(`::|\\|\.`)
	// #include "x.h", import "./x", require('./x.js'), use x/y.rs paths.
	includeRE    = regexp.MustCompile(`(?:#include\s*[<"]|\b(?:import|require|from)\s*\(?\s*['"])((?:[\w.@-]+/)*[\w.-]+\.[A-Za-z]{1,5})[>"']`)
	fileExtRE    = regexp.MustCompile(`\.(?:go|py|pyi|rs|java|kt|kts|scala|js|jsx|mjs|cjs|ts|tsx|rb|php|c|h|cc|cpp|hpp|cs|swift|dart|ex|exs|md|txt|json|ya?ml|toml|xml|html|css|lock|cfg|ini|sh|sql|proto)$`)
	camelSplitRE = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[A-Z][a-z]|$)`)
)

// stopwords are words that say nothing about where code is.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the a an and or but if then else when while for to of in on at by with from into onto
		is are was were be been being has have had do does did not no yes this that these those there here it its it's
		should would could can cannot will shall may might must need needs needed want wants like also just only some any
		all each every other same such more most less very much many few than too so as up down out over under again
		about after before between through during without within use used uses using get gets got set sets make makes
		made add adds added new old fix fixes fixed bug issue issues problem error errors fail fails failed failing work
		works working expected actual instead currently seems seem example examples case cases way thing things value
		values return returns returned call calls called file files code line lines function functions method methods
		class classes type types version versions following above below please thanks thank hi hello one two three
		first second last now still even because since which what where who why how does what's don't doesn't isn't
		i we you they he she me my our your their them us test tests`) {
		stopwords[w] = true
	}
}

// Extract finds the signals in a request and in evidence (tool output,
// verification failures). Evidence is untrusted: it only supplies places
// and names to look up inside the repository, never instructions.
func Extract(request string, evidence ...string) Signals {
	s := Signals{Intent: classify(request)}
	all := append([]string{request}, evidence...)
	seenFrame := map[string]bool{}
	for _, text := range all {
		for _, d := range lang.ParseFrames(text, 40) {
			key := d.File + ":" + strconv.Itoa(d.Line)
			if seenFrame[key] {
				continue
			}
			seenFrame[key] = true
			s.Frames = append(s.Frames, Frame{File: d.File, Line: d.Line, Symbol: d.Symbol, Kind: "trace", Order: len(s.Frames)})
		}
	}
	for _, text := range all {
		for _, d := range lang.ParseDiagnostics(text, nil, 40) {
			key := d.File + ":" + strconv.Itoa(d.Line)
			if seenFrame[key] || strings.Contains(d.File, "://") {
				continue
			}
			seenFrame[key] = true
			s.Diagnostics = append(s.Diagnostics, Frame{File: d.File, Line: d.Line, Kind: "build", Message: d.Message, Order: len(s.Diagnostics)})
		}
	}
	tests := map[string]bool{}
	addTest := func(n string) {
		n = strings.TrimSpace(n)
		if n != "" && len(n) <= 120 && !tests[n] {
			tests[n] = true
			s.Tests = append(s.Tests, n)
		}
	}
	for _, text := range all {
		for _, re := range failedTestRE {
			for _, m := range re.FindAllStringSubmatch(text, 20) {
				addTest(m[1])
			}
		}
	}
	files := map[string]bool{}
	for _, text := range evidence {
		for _, m := range failedIDRE.FindAllStringSubmatch(text, 40) {
			name, file := testID(m[1])
			if name != "" && len(name) >= 3 {
				addTest(name)
			}
			if file != "" && !files[file] {
				files[file] = true
				s.TestFiles = append(s.TestFiles, file)
			}
		}
	}
	for _, m := range namedTestRE.FindAllStringSubmatch(request, 20) {
		if n := m[1]; strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "test_") {
			addTest(n)
		}
	}
	s.Seeds = Seeds(request)
	// Files a code sample includes or imports ("#include <fmt/printf.h>",
	// "from app.models import X" is a module, see Modules).
	have := map[string]bool{}
	for _, sd := range s.Seeds {
		have[sd.Text] = true
	}
	for _, m := range fenceRE.FindAllStringSubmatch(request, -1) {
		for _, p := range includeRE.FindAllStringSubmatch(m[1], 10) {
			if !have[p[1]] {
				have[p[1]] = true
				s.Seeds = append(s.Seeds, Seed{Text: p[1], Kind: "sample", rank: 4})
			}
		}
	}
	// A member named in the prose ("Ledger.Balance", "Inventory.remove",
	// "cart.Total", "Order::total") is a name to look up as well as a
	// module path: its last segment is defined in the code.
	for _, m := range qualifiedRE.FindAllString(fenceRE.ReplaceAllString(urlRE.ReplaceAllString(request, " "), " "), 40) {
		segs := qualifiedSepRE.Split(m, -1)
		if len(segs) != 2 || fileExtRE.MatchString(m) || !memberLike(segs[0], segs[1]) {
			continue
		}
		name := segs[0] + "." + segs[1]
		if !have[name] {
			have[name] = true
			s.Seeds = append(s.Seeds, Seed{Text: name, Kind: "name", rank: 2})
		}
	}
	for _, text := range evidence {
		// Quoted messages in the evidence locate where they are raised.
		for _, sd := range Seeds(text) {
			if sd.Kind == "diagnostic" {
				s.Seeds = append(s.Seeds, sd)
			}
		}
	}
	seen := map[string]bool{}
	prose := urlRE.ReplaceAllString(request, " ")
	// Qualified names, code samples included: an import or a call path
	// names the module that holds the code.
	for _, m := range qualifiedRE.FindAllString(prose, 40) {
		segs := qualifiedSepRE.Split(m, -1)
		if len(segs) < 2 || seen["q:"+m] || fileExtRE.MatchString(m) {
			continue
		}
		seen["q:"+m] = true
		s.Modules = append(s.Modules, segs)
	}
	// Routes in prose only: a sample's routes are usually the reporter's.
	for _, m := range endpointRE.FindAllStringSubmatch(fenceRE.ReplaceAllString(prose, " "), 10) {
		e := strings.TrimRight(m[1], "/.")
		if strings.Count(e, "/") >= 1 && len(e) > 3 && !seen[e] && !looksLikeFilePath(e) {
			seen[e] = true
			s.Endpoints = append(s.Endpoints, e)
		}
	}
	// In prose only: upper-case names in a code sample are its constants.
	for _, m := range envVarRE.FindAllStringSubmatch(fenceRE.ReplaceAllString(prose, " "), 10) {
		if !seen[m[1]] && len(m[1]) >= 5 {
			seen[m[1]] = true
			s.EnvVars = append(s.EnvVars, m[1])
		}
	}
	for _, m := range commitRE.FindAllStringSubmatch(request, 5) {
		if !seen[m[1]] {
			seen[m[1]] = true
			s.Commits = append(s.Commits, m[1])
		}
	}
	s.Terms, s.TermFreq = terms(fenceRE.ReplaceAllString(prose, " "))
	return s
}

var paramRE = regexp.MustCompile(`\[[^\]]*\]$|\([^)]*\)$`)

// testID splits a failed test's id into the test's name and the file or
// class it is in ("" when the id does not say): pytest's
// "tests/test_x.py::TestA::test_y[p]", JUnit's "com.acme.OrdersTest#paid",
// cargo's "routing::tests::nest", Jest's "Cart › applies discount".
func testID(id string) (name, file string) {
	id = strings.TrimSpace(paramRE.ReplaceAllString(strings.TrimSpace(id), ""))
	switch {
	case strings.Contains(id, "::"):
		parts := strings.Split(id, "::")
		name = parts[len(parts)-1]
		if strings.Contains(parts[0], "/") || strings.Contains(parts[0], ".") {
			file = parts[0]
		}
	case strings.Contains(id, "#"):
		class, method, _ := strings.Cut(id, "#")
		name, file = method, class[strings.LastIndexByte(class, '.')+1:]
	case strings.Contains(id, " › ") || strings.Contains(id, " > "):
		parts := regexp.MustCompile(` [›>] `).Split(id, -1)
		name = strings.TrimSpace(parts[len(parts)-1])
	case strings.Contains(id, ".") && !strings.Contains(id, " "):
		parts := strings.Split(id, ".")
		name = parts[len(parts)-1]
		if len(parts) > 1 && len(parts[len(parts)-2]) > 0 && parts[len(parts)-2][0] >= 'A' && parts[len(parts)-2][0] <= 'Z' {
			file = parts[len(parts)-2]
		}
	default:
		name = id
	}
	return strings.TrimSpace(name), file
}

// looksLikeFilePath: "/src/app.ts" is a file, not an endpoint.
func looksLikeFilePath(p string) bool {
	return regexp.MustCompile(`\.[a-z]{1,5}$`).MatchString(p) && !strings.Contains(p, "{")
}

// terms are the request's distinct content words, lower-cased, with
// CamelCase and snake_case split, in order of first appearance.
func terms(text string) ([]string, map[string]int) {
	var out []string
	freq := map[string]int{}
	add := func(w string) {
		w = strings.ToLower(w)
		if len(w) < 3 || stopwords[w] || isNumber(w) {
			return
		}
		if freq[w]++; freq[w] == 1 {
			out = append(out, w)
		}
	}
	for _, w := range termRE.FindAllString(text, -1) {
		parts := splitIdent(w)
		if len(parts) > 1 {
			add(strings.Join(parts, ""))
		}
		for _, p := range parts {
			add(p)
		}
	}
	return out, freq
}

// splitIdent splits CamelCase and snake_case into lower-case words.
func splitIdent(w string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(w, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		for _, p := range camelSplitRE.FindAllString(part, -1) {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

func isNumber(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sortedKeys returns m's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// memberLike: a qualified name that reads as a type's or package's
// member rather than a module path or prose ("os.path", "e.g"): one of
// its two segments is capitalised, and neither is a single letter.
func memberLike(a, b string) bool {
	if len(a) < 2 || len(b) < 2 {
		return false
	}
	return unicode.IsUpper(rune(a[0])) || unicode.IsUpper(rune(b[0]))
}
