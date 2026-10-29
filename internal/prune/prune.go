// Package prune shortens command output for the model without losing what
// debugging needs: errors, failing tests and their assertions, the
// locations the output names, the stack frames in the project, and the
// summary at the end. It is deterministic and language-agnostic: lines are
// classified by the formats internal/lang knows (located messages and
// stack frames of every registered language) and by words that build
// tools, test runners, linters and package managers share. What it drops
// is counted in the result and named in the text; the caller keeps the
// original for recall.
package prune

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
)

// Options configure a pruning.
type Options struct {
	// Budget is the size the text should fit, in bytes.
	Budget int
	// Project reports whether a file a stack frame names belongs to the
	// project; nil treats every relative path outside dependency
	// directories as the project's.
	Project func(file string) bool
}

// Result is a pruned output.
type Result struct {
	Text string
	// Pruned: lines were dropped or folded, so Text is not the output.
	Pruned bool
	Lines  int // lines in the output
	Bytes  int // bytes in the output
	// Kept lines by what they are; Dropped lines by class.
	Errors, Locations, Frames, Kept int
	Dropped                         map[string]int
	// ErrorsDropped counts error lines that did not fit the budget.
	ErrorsDropped int
	// Repeats counts lines folded into a repeat marker.
	Repeats int
}

// Signal reports whether the output has what pruning recognises:
// errors, warnings, locations, stack frames, progress, downloads or
// passing tests. Output without any (a diff, a file, a program's own
// results) is content, which the caller may rather keep whole.
func Signal(out string) bool {
	lines, _ := clean(out)
	for _, t := range lines {
		switch classify(t, Options{}).class {
		case classOther, classBlank:
		default:
			return true
		}
	}
	return false
}

// Line classes.
const (
	classError    = "error"
	classLocation = "location"
	classFrame    = "frame"
	classLibFrame = "library frame"
	classWarning  = "warning"
	classPassing  = "passing test"
	classProgress = "progress"
	classDeps     = "dependency"
	classBlank    = "blank"
	classRepeated = "repeated message"
	classOther    = "other"
)

var (
	ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][A-Z0-9]`)
	// Counts of zero are not failures ("0 failed", "Failures: 0").
	zeroRE  = regexp.MustCompile(`(?i)\b0 (?:failed|failures?|errors?|warnings?)\b|\b(?:failed|failures|errors|warnings)\s*[:=]\s*0\b`)
	errorRE = regexp.MustCompile(`(?i)\b(?:errors?|err!|fatal|fail(?:ed|ure|ures|ing|s)?|panic(?:ked)?|exception|traceback|assert(?:ion)?(?:error)?|abort(?:ed)?|segmentation fault|core dumped|undefined reference|unresolved|cannot find|could not|couldn't|no such file|not found|mismatch|unexpected|timed out|timeout|killed|denied|refused|expected|actual|received|got|want(?:ed)?)\b`)
	// Markers runners put in front of a failure or its detail.
	failMarkRE = regexp.MustCompile(`^\s*(?:✗|✕|×|✘|●|E\s{2,}|>\s+\d+\s*\||-\s+Expected|\+\s+Received|\^+\s*$)`)
	warningRE  = regexp.MustCompile(`(?i)\b(?:warn(?:ing)?s?|deprecat\w*)\b`)
	passingRE  = regexp.MustCompile(`^\s*(?:ok\s+\S+|--- (?:PASS|SKIP)|=== (?:RUN|PAUSE|CONT|NAME)|PASS\s*$|✓|✔|√|\[\s*(?:OK|PASS(?:ED)?)\s*\]|test \S+ \.\.\. ok\s*$)|\bPASSED\b|^\S+::\S+ PASSED|^\s*Tests run: \d+, Failures: 0, Errors: 0`)
	progressRE = regexp.MustCompile(`(?i)^\s*(?:\[?\s*\d{1,3}(?:\.\d+)?\s*%\s*\]?|[#=>\-.·•\s]{8,}\s*\d*%?|(?:receiving|resolving|compressing|counting|writing|enumerating|unpacking) (?:objects|deltas)|\d+/\d+ \[[=>.\s]*\])`)
	depsRE     = regexp.MustCompile(`(?i)^\s*(?:go: (?:downloading|finding|extracting|added|upgraded)|downloading|downloaded|fetching|fetched|resolving|resolved|installing|installed|collecting|using cached|requirement already satisfied|unpacking|preparing|building wheels?|created wheel|stored in directory|successfully (?:built|installed)|added \d+ packages?|npm (?:warn|notice|http|timing|sill|verb)|\[info\] download(?:ing|ed)|download(?:ing|ed) from|progress \(\d+\)|get:\d+|hit:\d+|updating (?:crates\.io )?index|(?:down)?loaded \d+ crates?|(?:compiling|checking|fresh|locking|adding|updating) [\w.-]+ v\d|\s*\|\s*$)`)
	// Variable parts that make otherwise equal lines differ.
	volatileRE = regexp.MustCompile(`0x[0-9a-fA-F]+|\b\d{1,2}:\d{2}:\d{2}(?:[.,]\d+)?\b|\b\d{4}-\d{2}-\d{2}[T ]?|\b\d+(?:\.\d+)?\s*(?:ns|µs|us|ms|s|sec|seconds?)\b|\b\d+(?:\.\d+)?%`)
	digitsRE   = regexp.MustCompile(`\d+`)
	// A search result line: path:line:text or path-line-text.
	// The path has a file extension, so a date or a time is not one.
	searchRE = regexp.MustCompile(`^((?:[^\s:]*/)?[^\s:/]+\.[A-Za-z0-9]{1,8}):(\d+)[:-]|^((?:[^\s:]*/)?[^\s:/-]+\.[A-Za-z0-9]{1,8})-(\d+)-`)
)

const (
	maxLine     = 600 // longer lines are cut
	tailKeep    = 15  // the last lines: the runner's summary and exit status
	headKeep    = 6   // the first lines: what ran
	sameMessage = 5   // located messages kept per repeated message
)

// Prune fits out within opt.Budget. Escape sequences and progress
// overwrites are removed, and runs of the same line folded, whatever the
// size; lines are dropped only when that is not enough.
func Prune(out string, opt Options) Result {
	res := Result{Bytes: len(out), Dropped: map[string]int{}}
	lines, repeats := clean(out)
	res.Lines = strings.Count(out, "\n") + 1
	res.Repeats = repeats
	text := strings.Join(lines, "\n")
	if opt.Budget <= 0 || len(text) <= opt.Budget {
		res.Text, res.Kept = text, len(lines)
		res.Pruned = repeats > 0
		return res
	}
	if isSearch(lines) {
		return pruneSearch(lines, opt, res)
	}
	return pruneLines(lines, opt, res)
}

// clean removes escape sequences and carriage-return overwrites and folds
// runs of three or more lines that differ only in times, durations,
// percentages and addresses into the first, the last and a count.
func clean(out string) ([]string, int) {
	out = ansiRE.ReplaceAllString(out, "")
	out = strings.ReplaceAll(out, "\r\n", "\n")
	raw := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, l := range raw {
		if strings.Contains(l, "\r") {
			// A progress line redrawn in place: what it showed last.
			parts := strings.Split(l, "\r")
			last := ""
			for _, p := range parts {
				if strings.TrimSpace(p) != "" {
					last = p
				}
			}
			raw[i] = last
		}
		raw[i] = strings.TrimRight(raw[i], " \t")
	}
	var lines []string
	repeats := 0
	for i := 0; i < len(raw); {
		j := i + 1
		key := volatileRE.ReplaceAllString(raw[i], "#")
		for j < len(raw) && volatileRE.ReplaceAllString(raw[j], "#") == key {
			j++
		}
		n := j - i
		switch {
		case n >= 3 && raw[i] == raw[j-1] && allSame(raw[i:j]):
			lines = append(lines, raw[i], fmt.Sprintf("  … [the line above %d more times]", n-1))
			repeats += n - 1
		case n >= 3:
			lines = append(lines, raw[i], fmt.Sprintf("  … [%d more lines like these]", n-2), raw[j-1])
			repeats += n - 2
		default:
			lines = append(lines, raw[i:j]...)
		}
		i = j
	}
	return lines, repeats
}

func allSame(ls []string) bool {
	for _, l := range ls[1:] {
		if l != ls[0] {
			return false
		}
	}
	return true
}

type line struct {
	text     string
	class    string
	priority int
	loc      string // file:line a location or frame names
	message  string // a located message's text, for repeats
	keep     bool
}

// pruneLines keeps lines by priority until the budget is spent, in their
// original order, and names what each gap held.
func pruneLines(raw []string, opt Options, res Result) Result {
	ls := make([]line, len(raw))
	for i, t := range raw {
		if len(t) > maxLine {
			t = t[:maxLine] + fmt.Sprintf(" … [%d more bytes on this line]", len(t)-maxLine)
		}
		ls[i] = classify(t, opt)
	}
	// A frame's function line before it (Go, Rust) and its source line
	// after it (Python) belong to the frame.
	for i := range ls {
		if ls[i].class != classFrame && ls[i].class != classLibFrame {
			continue
		}
		for _, j := range []int{i - 1, i + 1} {
			if j < 0 || j >= len(ls) {
				continue
			}
			if c := ls[j].class; c == classOther || (c == classError && ls[i].class == classLibFrame && j == i-1) {
				ls[j].class, ls[j].priority = ls[i].class+" ", ls[i].priority
			}
		}
	}
	for i := range ls {
		ls[i].class = strings.TrimSuffix(ls[i].class, " ")
	}
	// Library code between the failure and the project: the first two
	// frames of each run, the rest folded.
	for i, run := 0, 0; i < len(ls); i++ {
		switch ls[i].class {
		case classLibFrame:
			_, frame, _ := lang.LineLocation(ls[i].text)
			if frame {
				run++
			}
			if run > 2 {
				ls[i].priority = 0
			}
		case classBlank:
		default:
			run = 0
		}
	}
	// A line the output already showed (a build tool repeating the
	// compiler's messages) is a repeat.
	seen := map[string]bool{}
	for i := range ls {
		if ls[i].priority < 5 {
			continue
		}
		k := strings.TrimSpace(ls[i].text)
		if seen[k] {
			ls[i].class, ls[i].priority, ls[i].message = classRepeated, 1, ""
			continue
		}
		seen[k] = true
	}
	// What surrounds a failure explains it: the line before and three
	// after an error, a location or a frame of the project.
	for i := range ls {
		if ls[i].priority < 8 || ls[i].class == classRepeated {
			continue
		}
		for j := max(0, i-1); j <= min(len(ls)-1, i+3); j++ {
			if ls[j].priority < 7 && ls[j].class != classBlank && !isNoise(ls[j].class) && ls[j].class != classRepeated {
				ls[j].priority = 7
			}
		}
	}
	for i := 0; i < min(headKeep, len(ls)); i++ {
		ls[i].priority = max(ls[i].priority, 6)
	}
	// The end is the outcome: the last lines always, errors among the
	// last lines before anything else.
	for i, n := max(0, len(ls)-tailKeep), 0; i < len(ls); i++ {
		if ls[i].class == classBlank {
			continue
		}
		n++
		switch {
		case i >= len(ls)-3, ls[i].priority >= 9:
			ls[i].priority = 12
		default:
			ls[i].priority = max(ls[i].priority, 9)
		}
	}
	// The same located message many times: the first few, and the other
	// places on one line.
	byMsg := map[string][]int{}
	for i, l := range ls {
		if l.message != "" {
			byMsg[l.message] = append(byMsg[l.message], i)
		}
	}
	extra := map[int]string{} // line index -> note appended after it
	for msg, idx := range byMsg {
		if len(idx) <= sameMessage {
			continue
		}
		var locs []string
		for _, i := range idx[sameMessage:] {
			ls[i].priority, ls[i].class = 1, classRepeated
			locs = append(locs, ls[i].loc)
		}
		if len(locs) > 20 {
			locs = append(locs[:20:20], fmt.Sprintf("and %d more", len(locs)-20))
		}
		if len(msg) > 80 {
			msg = msg[:80] + "…"
		}
		extra[idx[sameMessage-1]] = fmt.Sprintf("  … [%q at %d more places: %s]", msg, len(idx)-sameMessage, strings.Join(locs, ", "))
	}
	budget := opt.Budget - 600 // the header and gap markers
	used := 0
	order := make([]int, len(ls))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return ls[order[a]].priority > ls[order[b]].priority })
	for _, i := range order {
		cost := len(ls[i].text) + 1 + len(extra[i])
		if ls[i].priority == 0 || used+cost > budget {
			continue
		}
		ls[i].keep = true
		used += cost
	}
	text, kept := render(ls, extra)
	// Gap markers are not counted while selecting: drop the lowest kept
	// lines until the text fits.
	for tries := 0; len(text)+300 > opt.Budget && tries < 8; tries++ {
		excess := len(text) + 300 - opt.Budget
		for k := len(order) - 1; k >= 0 && excess > 0; k-- {
			if i := order[k]; ls[i].keep && ls[i].priority < 12 {
				ls[i].keep = false
				excess -= len(ls[i].text) + 1
			}
		}
		text, kept = render(ls, extra)
	}
	res.Kept = 0
	for _, l := range ls {
		if !l.keep {
			if l.class == classError || l.class == classLocation {
				res.ErrorsDropped++
			}
			res.Dropped[l.class]++
			continue
		}
		res.Kept++
		switch l.class {
		case classError:
			res.Errors++
		case classLocation:
			res.Locations++
		case classFrame:
			res.Frames++
		}
	}
	_ = kept
	res.Text = header(res) + text
	res.Pruned = true
	return res
}

// render writes the kept lines in order, a marker naming each gap.
func render(ls []line, extra map[int]string) (string, int) {
	var b strings.Builder
	gap := map[string]int{}
	flush := func() {
		if len(gap) == 0 {
			return
		}
		n := 0
		var parts []string
		for _, c := range sortedClasses(gap) {
			n += gap[c]
			parts = append(parts, fmt.Sprintf("%d %s", gap[c], c))
		}
		fmt.Fprintf(&b, "  … [%d lines: %s]\n", n, strings.Join(parts, ", "))
		gap = map[string]int{}
	}
	kept := 0
	for i, l := range ls {
		if !l.keep {
			if l.class != classBlank {
				gap[l.class]++
			}
			continue
		}
		flush()
		b.WriteString(l.text + "\n")
		if e := extra[i]; e != "" {
			b.WriteString(e + "\n")
		}
		kept++
	}
	flush()
	return strings.TrimRight(b.String(), "\n"), kept
}

func classify(t string, opt Options) line {
	l := line{text: t, class: classOther, priority: 2}
	trimmed := strings.TrimSpace(t)
	if trimmed == "" {
		l.class, l.priority = classBlank, 0
		return l
	}
	if d, frame, ok := lang.LineLocation(t); ok {
		l.loc = fmt.Sprintf("%s:%d", d.File, d.Line)
		if frame {
			if project(d, opt) {
				l.class, l.priority = classFrame, 8
			} else {
				l.class, l.priority = classLibFrame, 3
			}
			return l
		}
		l.class, l.priority = classLocation, 9
		if msg := strings.TrimSpace(d.Message); msg != "" {
			l.message = digitsRE.ReplaceAllString(volatileRE.ReplaceAllString(msg, "#"), "#")
		}
		return l
	}
	words := zeroRE.ReplaceAllString(t, "")
	switch {
	case depsRE.MatchString(t):
		l.class, l.priority = classDeps, 0
	case passingRE.MatchString(t):
		l.class, l.priority = classPassing, 0
	case progressRE.MatchString(t) && !hasErrorWord(words):
		l.class, l.priority = classProgress, 0
	case hasErrorWord(words) || failMarkRE.MatchString(t):
		l.class, l.priority = classError, 10
	case warningRE.MatchString(t):
		l.class, l.priority = classWarning, 5
	}
	return l
}

// hasErrorWord: an error word stands on its own, not inside a qualified
// name (std::panic::catch_unwind, errors.New, on_error_handler).
func hasErrorWord(t string) bool {
	for _, m := range errorRE.FindAllStringIndex(t, -1) {
		before, after := byte(' '), byte(' ')
		if m[0] > 0 {
			before = t[m[0]-1]
		}
		if m[1] < len(t) {
			after = t[m[1]]
		}
		if strings.IndexByte(".:_/<>[$#", before) >= 0 || strings.IndexByte(".:_/<>[($#", after) >= 0 && (after != ':' || (m[1]+1 < len(t) && t[m[1]+1] != ' ')) {
			continue
		}
		return true
	}
	return false
}

func isNoise(c string) bool {
	return c == classPassing || c == classProgress || c == classDeps
}

// dependencyDirs hold code that is not the project's, whatever the
// language; libraryPackages are runtimes and frameworks that frames name
// by package rather than by path (JVM, .NET).
var (
	dependencyDirs  = []string{"node_modules/", "vendor/", "site-packages/", "dist-packages/", ".venv/", "/venv/", "/usr/", "/opt/", ".cargo/registry/", "/rustc/", "pkg/mod/", "/go/src/", "/libexec/", "/lib/python", ".m2/repository/", ".gradle/caches/", "<frozen ", "node:", "<anonymous>"}
	libraryPackages = []string{"java.", "javax.", "jdk.", "sun.", "kotlin.", "scala.", "org.junit.", "junit.", "org.apache.maven.", "org.gradle.", "org.springframework.", "System.", "Microsoft.", "NUnit.", "Xunit."}
)

func project(d lang.Diagnostic, opt Options) bool {
	if opt.Project != nil {
		return opt.Project(d.File)
	}
	f := strings.ReplaceAll(d.File, "\\", "/")
	for _, dir := range dependencyDirs {
		if strings.Contains(f, dir) {
			return false
		}
	}
	sym := d.Symbol
	if i := strings.Index(sym, "/"); i >= 0 && !strings.Contains(f, "/") {
		sym = sym[i+1:] // java.base/java.lang.Thread.run
	}
	for _, p := range libraryPackages {
		if strings.HasPrefix(sym, p) {
			return false
		}
	}
	return true
}

func sortedClasses(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func header(r Result) string {
	kept := []string{}
	if r.Errors > 0 {
		kept = append(kept, count(r.Errors, "error line"))
	}
	if r.Locations > 0 {
		kept = append(kept, count(r.Locations, "located message"))
	}
	if r.Frames > 0 {
		kept = append(kept, count(r.Frames, "stack frame")+" in the project")
	}
	kept = append(kept, "the last lines")
	dropped := 0
	var parts []string
	for _, c := range sortedClasses(r.Dropped) {
		if c == classBlank {
			continue
		}
		dropped += r.Dropped[c]
		parts = append(parts, fmt.Sprintf("%s %d", c, r.Dropped[c]))
	}
	errs := "every error and located line was kept"
	if r.ErrorsDropped > 0 {
		errs = fmt.Sprintf("%d error or located lines did not fit and were dropped", r.ErrorsDropped)
	}
	return fmt.Sprintf("[output shortened: %d lines (%s) → %d lines; kept %s; dropped %d lines (%s); %s]\n",
		r.Lines, size(r.Bytes), r.Kept, strings.Join(kept, ", "), dropped, strings.Join(parts, ", "), errs)
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func size(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%d KB", (n+512)/1024)
}

// isSearch: most lines are search matches (path:line:text).
func isSearch(lines []string) bool {
	n, hits, diagnostics := 0, 0, 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || l == "--" {
			continue
		}
		n++
		if searchRE.MatchString(l) {
			hits++
			if hasErrorWord(l) || warningRE.MatchString(l) {
				diagnostics++
			}
		}
	}
	// A compiler's or linter's list of messages has the same shape.
	return n >= 10 && hits*10 >= n*7 && diagnostics*10 < hits*3
}

// pruneSearch keeps the first matches of every file, as many per file as
// the budget allows, and counts the rest per file.
func pruneSearch(lines []string, opt Options, res Result) Result {
	type file struct {
		name  string
		lines []string
	}
	var files []*file
	byName := map[string]*file{}
	var other []string
	for _, l := range lines {
		m := searchRE.FindStringSubmatch(l)
		if m == nil {
			if strings.TrimSpace(l) != "" && l != "--" {
				other = append(other, l)
			}
			continue
		}
		name := m[1]
		if name == "" {
			name = m[3]
		}
		f := byName[name]
		if f == nil {
			f = &file{name: name}
			byName[name] = f
			files = append(files, f)
		}
		if len(l) > maxLine {
			l = l[:maxLine] + " …"
		}
		f.lines = append(f.lines, l)
	}
	total := 0
	for _, f := range files {
		total += len(f.lines)
	}
	render := func(per int) string {
		var b strings.Builder
		for _, f := range files {
			n := min(per, len(f.lines))
			for _, l := range f.lines[:n] {
				b.WriteString(l + "\n")
			}
			if len(f.lines) > n {
				fmt.Fprintf(&b, "  … [%d more matches in %s]\n", len(f.lines)-n, f.name)
			}
		}
		for _, l := range other {
			b.WriteString(l + "\n")
		}
		return b.String()
	}
	budget := opt.Budget - 300
	body := ""
	kept := 0
	for per := 20; per >= 1; per-- {
		if body = render(per); len(body) <= budget {
			for _, f := range files {
				kept += min(per, len(f.lines))
			}
			break
		}
		body = ""
	}
	if body == "" {
		// Not even one match per file fits: the first match of as many
		// files as fit, then the count of the others.
		var b strings.Builder
		for i, f := range files {
			if b.Len()+len(f.lines[0]) > budget*3/4 {
				rest := 0
				for _, g := range files[i:] {
					rest += len(g.lines)
				}
				fmt.Fprintf(&b, "  … [%d more files with %d matches]\n", len(files)-i, rest)
				break
			}
			b.WriteString(f.lines[0] + "\n")
			kept++
			if len(f.lines) > 1 {
				fmt.Fprintf(&b, "  … [%d more matches in %s]\n", len(f.lines)-1, f.name)
			}
		}
		body = b.String()
	}
	res.Kept = kept
	res.Dropped["search match"] = total - kept
	res.Pruned = true
	res.Text = fmt.Sprintf("[search results shortened: %d matches in %d files → %d shown, the first of each file; the counts name the rest]\n", total, len(files), kept) + strings.TrimRight(body, "\n")
	return res
}

// Clip cuts s to n bytes keeping the head and the tail (a test run's
// failures are at the end): what tool results got before pruning, and what
// output that is not a command's still gets.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	head := s[:n*2/3]
	tail := s[len(s)-n/3:]
	return head + fmt.Sprintf("\n… [%d bytes cut] …\n", len(s)-len(head)-len(tail)) + tail
}

// Failures are the first n lines of out that report a problem: errors,
// failing tests, assertions and located messages, as Prune keeps them.
func Failures(out string, n int) []string {
	lines, _ := clean(out)
	var res []string
	seen := map[string]bool{}
	for _, t := range lines {
		c := classify(t, Options{}).class
		if c != classError && c != classLocation {
			continue
		}
		t = strings.TrimSpace(t)
		if len(t) > 200 {
			t = t[:200] + "…"
		}
		if seen[t] || strings.HasPrefix(t, "[output shortened") || strings.HasPrefix(t, "error: exit status") || strings.HasPrefix(t, "exit status") {
			continue
		}
		seen[t] = true
		if res = append(res, t); len(res) == n {
			break
		}
	}
	return res
}
