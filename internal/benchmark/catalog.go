package benchmark

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// The evaluation catalog: every task of every suite with its group,
// capability, languages, split and provenance, whether it can run on this
// machine, and the integrity problems found in the suite definitions.

// CatalogEntry is one task.
type CatalogEntry struct {
	Suite      string     `json:"suite"` // direct, bounded, verify
	ID         string     `json:"id"`
	Group      string     `json:"group"`
	Capability string     `json:"capability"`
	Split      string     `json:"split"`
	Languages  []string   `json:"languages"`
	Provenance Provenance `json:"provenance"`
	// PublicSince is when the task became public: the issue's date for a
	// real task, else the first commit of its definition in this public
	// repository ("" when not committed yet).
	PublicSince string `json:"public_since,omitempty"`
	// Requires are the executables its grading needs; Runnable says
	// whether this machine (direct) or the sandbox image (bounded,
	// verify) has them, and Why when not.
	Requires     []string `json:"requires,omitempty"`
	Runnable     bool     `json:"runnable"`
	Why          string   `json:"why,omitempty"`
	HasReference bool     `json:"has_reference"`
	Grading      string   `json:"grading"` // checks, answer, checks+answer, security
}

// Catalog is the whole set.
type Catalog struct {
	Built    time.Time      `json:"built"`
	Entries  []CatalogEntry `json:"entries"`
	Problems []string       `json:"problems"`
}

// CatalogSource is a directory of task definitions for a suite.
type CatalogSource struct {
	Suite string
	Dir   string
}

// BuildCatalog reads the suites' tasks and the verification corpus. here
// reports whether a tool is on this machine and inImage whether it is in
// the sandbox image (nil: not checked).
func BuildCatalog(ctx context.Context, sources []CatalogSource, fixturesDir string, here, inImage func(string) bool) (*Catalog, error) {
	c := &Catalog{Built: time.Now().UTC()}
	for _, src := range sources {
		tasks, err := LoadTasks(src.Dir)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, t := range tasks {
			if seen[t.ID] {
				c.Problems = append(c.Problems, fmt.Sprintf("%s: duplicate id %s", src.Suite, t.ID))
			}
			seen[t.ID] = true
			e := entryFor(ctx, src, t)
			check := here
			if src.Suite == "bounded" {
				check = inImage
			}
			e.Runnable, e.Why = runnable(e.Requires, check)
			c.Entries = append(c.Entries, e)
			c.Problems = append(c.Problems, taskProblems(src, t, fixturesDir)...)
		}
	}
	verifyFirst := firstCommit(ctx, "internal/benchmark/verify2_cases.go")
	for _, vc := range VerifyCases() {
		langs := []string{vc.Language}
		if vc.Language == "mixed" {
			langs = []string{"go", "javascript"}
		}
		e := CatalogEntry{Suite: "verify", ID: vc.ID, Group: "verification", Capability: verifyCapability(vc.Kind), Split: verifySplit(vc),
			Languages: langs, Provenance: Provenance{Kind: "synthetic"}, HasReference: true, Grading: "hidden tests on the change's code"}
		if e.Split == "dev" {
			e.PublicSince = verifyFirst
		}
		e.Requires = verifyTools(vc.Language)
		e.Runnable, e.Why = runnable(e.Requires, inImage)
		c.Entries = append(c.Entries, e)
	}
	for g := range Groups {
		var dev, held int
		for _, e := range c.Entries {
			if e.Group == g {
				switch e.Split {
				case "dev":
					dev++
				case "heldout":
					held++
				}
			}
		}
		if held == 0 {
			c.Problems = append(c.Problems, fmt.Sprintf("group %s has no held-out task", g))
		}
		if dev+held == 0 {
			c.Problems = append(c.Problems, fmt.Sprintf("group %s has no task", g))
		}
	}
	sort.Strings(c.Problems)
	return c, nil
}

func entryFor(ctx context.Context, src CatalogSource, t TaskSpec) CatalogEntry {
	langs := t.Languages
	if len(langs) == 0 {
		langs = []string{t.Language}
	}
	e := CatalogEntry{Suite: src.Suite, ID: t.ID, Group: t.Group, Capability: t.Capability, Split: t.Split, Languages: langs,
		Provenance: t.Provenance, HasReference: len(t.Reference) > 0}
	switch {
	case t.Provenance.IssueDate != "":
		e.PublicSince = t.Provenance.IssueDate
	default:
		e.PublicSince = firstCommit(ctx, filepath.Join(src.Dir, t.ID+".yaml"))
	}
	switch {
	case t.Security != nil:
		e.Grading = "security"
	case len(t.Checks) > 0 && len(t.Expect) > 0:
		e.Grading = "checks+answer"
	case len(t.Checks) > 0:
		e.Grading = "checks"
	default:
		e.Grading = "answer"
	}
	e.Requires = checkToolNames(t)
	return e
}

// checkToolNames are the executables a task's grading starts: each check's
// first word and the well-known tools of a shell check, and its declared
// requirements.
func checkToolNames(t TaskSpec) []string {
	tools := slices.Clone(t.Requires)
	for _, c := range t.Checks {
		if len(c.Run) == 0 {
			continue
		}
		tools = append(tools, c.Run[0])
		if c.Run[0] == "sh" && len(c.Run) > 2 {
			for _, w := range []string{"go", "node", "python3", "npm", "cargo", "mvn", "gradle", "cmake", "ctest", "make", "php", "ruby", "rake", "dotnet"} {
				if regexp.MustCompile(`(^|[\s(;&|])` + w + `(\s|$)`).MatchString(c.Run[2]) {
					tools = append(tools, w)
				}
			}
		}
	}
	slices.Sort(tools)
	return slices.Compact(tools)
}

func runnable(tools []string, has func(string) bool) (bool, string) {
	if has == nil {
		return true, "not checked"
	}
	var missing []string
	for _, t := range tools {
		if t != "sh" && !has(t) {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return false, "missing " + strings.Join(missing, ", ")
	}
	return true, ""
}

func verifyCapability(kind string) string {
	switch kind {
	case "correct-strong", "legit-config":
		return "correct"
	case "correct-weak":
		return "weak-tests"
	case "partial-weak":
		return "incomplete"
	case "caught":
		return "incorrect"
	case "no-op":
		return "no-op"
	default: // weakening-skip, weakening-script, tamper-runner
		return "tamper"
	}
}

// verifySplit: the cases verification was developed on (Phase 04) are
// dev; the languages and kinds added later are held out.
func verifySplit(c VerifyCase) string {
	switch {
	case slices.Contains([]string{"c", "cpp", "ruby", "php"}, c.Language), c.Kind == "no-op", c.Kind == "tamper-runner":
		return "heldout"
	}
	return "dev"
}

func verifyTools(lang string) []string {
	switch lang {
	case "go":
		return []string{"go"}
	case "python":
		return []string{"python3"}
	case "javascript", "typescript":
		return []string{"node"}
	case "rust":
		return []string{"cargo"}
	case "java":
		return []string{"mvn"}
	case "c", "cpp":
		return []string{"cmake", "ninja"}
	case "ruby":
		return []string{"rake"}
	case "php":
		return []string{"php", "make"}
	case "mixed":
		return []string{"go", "node"}
	}
	return nil
}

var (
	gomodPin = regexp.MustCompile(`^gomod:[^@]+@v\d`)
	gitPin   = regexp.MustCompile(`^(git|githistory):.+@[0-9a-f]{40}$`)
)

// taskProblems are a definition's integrity problems.
func taskProblems(src CatalogSource, t TaskSpec, fixturesDir string) []string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, src.Suite+"/"+t.ID+": "+fmt.Sprintf(format, args...))
	}
	if t.Group == "" || t.Capability == "" {
		add("no group or capability")
	}
	if t.Split == "" {
		add("no split (dev or heldout)")
	}
	switch t.Provenance.Kind {
	case "":
		add("no provenance")
	case "real":
		if t.Provenance.Upstream == "" || t.Provenance.IssueDate == "" {
			add("a real task needs provenance.upstream and provenance.issue_date")
		}
	}
	for name, s := range t.Sources {
		if !gomodPin.MatchString(s) && !gitPin.MatchString(s) {
			add("source %s is not pinned to a version or a full commit: %s", name, s)
		}
	}
	if t.Group == "security" && t.Security == nil {
		add("a security task needs a security section")
	}
	// Hidden files must not be visible to the agent: none may exist in the
	// fixture it works on.
	if t.Fixture != "" {
		for _, h := range t.Hidden {
			if h.Path == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(fixturesDir, t.Fixture, h.Repo, h.Path)); err == nil {
				add("hidden file %s/%s exists in the fixture: the agent can read it", h.Repo, h.Path)
			}
		}
	}
	return out
}

// firstCommit is the date of the first commit of path ("" if none).
func firstCommit(ctx context.Context, path string) string {
	out, err := exec.CommandContext(ctx, "git", "log", "--diff-filter=A", "--follow", "--format=%as", "--", path).Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// Coverage counts entries per key: tasks, held out, runnable here.
type Coverage struct {
	Tasks, HeldOut, Runnable int
}

// CoverageBy counts c's entries per (row, column) of key.
func (c *Catalog) CoverageBy(key func(CatalogEntry) []([2]string)) map[[2]string]*Coverage {
	out := map[[2]string]*Coverage{}
	for _, e := range c.Entries {
		for _, k := range key(e) {
			cv := out[k]
			if cv == nil {
				cv = &Coverage{}
				out[k] = cv
			}
			cv.Tasks++
			if e.Split == "heldout" {
				cv.HeldOut++
			}
			if e.Runnable {
				cv.Runnable++
			}
		}
	}
	return out
}

// groupOrder lists the groups in the order of the roadmap.
var groupOrder = []string{"everyday", "long-running", "verification", "security", "polyglot"}

// WriteCatalogMarkdown writes the coverage tables, the contamination view
// against a model published on published (YYYY-MM-DD; "" to leave it out)
// and the problems.
func WriteCatalogMarkdown(w io.Writer, c *Catalog, model, published string) {
	fmt.Fprintf(w, "# Evaluation coverage\n\nGenerated by `bcode bench catalog` on %s: %d tasks (%d held out).\n\n", c.Built.Format("2006-01-02"), len(c.Entries), count(c.Entries, func(e CatalogEntry) bool { return e.Split == "heldout" }))
	langs := map[string]bool{}
	for _, e := range c.Entries {
		for _, l := range e.Languages {
			langs[l] = true
		}
	}
	var ls []string
	for l := range langs {
		ls = append(ls, l)
	}
	sort.Strings(ls)
	byLang := c.CoverageBy(func(e CatalogEntry) [][2]string {
		var ks [][2]string
		for _, l := range e.Languages {
			ks = append(ks, [2]string{l, e.Group})
		}
		return ks
	})
	fmt.Fprintln(w, "## By language and group\n\nEach cell: tasks (held out), and how many can run here when fewer. A multi-language task counts under each of its languages.")
	fmt.Fprintf(w, "| Language | %s | Suites |\n|---|%s---|\n", strings.Join(groupOrder, " | "), strings.Repeat("---|", len(groupOrder)))
	for _, l := range ls {
		var cells []string
		suites := map[string]bool{}
		for _, e := range c.Entries {
			if slices.Contains(e.Languages, l) {
				suites[e.Suite] = true
			}
		}
		for _, g := range groupOrder {
			cv := byLang[[2]string{l, g}]
			switch {
			case cv == nil:
				cells = append(cells, "–")
			case cv.Runnable < cv.Tasks:
				cells = append(cells, fmt.Sprintf("%d (%d); %d run here", cv.Tasks, cv.HeldOut, cv.Runnable))
			default:
				cells = append(cells, fmt.Sprintf("%d (%d)", cv.Tasks, cv.HeldOut))
			}
		}
		var ss []string
		for s := range suites {
			ss = append(ss, s)
		}
		sort.Strings(ss)
		fmt.Fprintf(w, "| %s | %s | %s |\n", l, strings.Join(cells, " | "), strings.Join(ss, ", "))
	}
	byCap := c.CoverageBy(func(e CatalogEntry) [][2]string { return [][2]string{{e.Group, e.Capability}} })
	fmt.Fprintln(w, "\n## By capability\n\n| Group | Capability | Tasks | Held out | Run here | Languages |\n|---|---|---|---|---|---|")
	for _, g := range groupOrder {
		for _, cp := range Groups[g] {
			cv := byCap[[2]string{g, cp}]
			if cv == nil {
				fmt.Fprintf(w, "| %s | %s | **0** | 0 | 0 | |\n", g, cp)
				continue
			}
			lset := map[string]bool{}
			for _, e := range c.Entries {
				if e.Group == g && e.Capability == cp {
					for _, l := range e.Languages {
						lset[l] = true
					}
				}
			}
			var lls []string
			for l := range lset {
				lls = append(lls, l)
			}
			sort.Strings(lls)
			fmt.Fprintf(w, "| %s | %s | %d | %d | %d | %s |\n", g, cp, cv.Tasks, cv.HeldOut, cv.Runnable, strings.Join(lls, ", "))
		}
	}
	notRun := map[string][]string{}
	for _, e := range c.Entries {
		if !e.Runnable {
			notRun[e.Why] = append(notRun[e.Why], e.ID)
		}
	}
	if len(notRun) > 0 {
		fmt.Fprint(w, "\n## Not runnable here\n\n")
		var whys []string
		for k := range notRun {
			whys = append(whys, k)
		}
		sort.Strings(whys)
		for _, why := range whys {
			fmt.Fprintf(w, "- %s: %s\n", why, strings.Join(notRun[why], ", "))
		}
	}
	if published != "" {
		fmt.Fprintf(w, "\n## Contamination against %s (published %s)\n\nA task public before the model's weights were published may be in its training data.\n\n", model, published)
		fmt.Fprintln(w, "| Group | Public before | Public after, or not yet public |\n|---|---|---|")
		for _, g := range groupOrder {
			var before, after int
			for _, e := range c.Entries {
				if e.Group != g {
					continue
				}
				if e.PublicSince != "" && e.PublicSince < published {
					before++
				} else {
					after++
				}
			}
			fmt.Fprintf(w, "| %s | %d | %d |\n", g, before, after)
		}
	}
	fmt.Fprintln(w, "\n## Integrity problems")
	if len(c.Problems) == 0 {
		fmt.Fprintln(w, "\nNone.")
	}
	for _, p := range c.Problems {
		fmt.Fprintln(w, "- "+p)
	}
}

func count(es []CatalogEntry, f func(CatalogEntry) bool) int {
	n := 0
	for _, e := range es {
		if f(e) {
			n++
		}
	}
	return n
}
