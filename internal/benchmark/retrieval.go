package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/lang"
	"github.com/akynte/boundedcode/internal/retrieve"
)

// The retrieval benchmark measures retrieval apart from patch correctness:
// for real issues (benchmarks/retrieval: SWE-bench Multilingual and
// SWE-bench Lite, split into development and held-out repositories before
// any run), which of the files their fix changed each approach puts in
// front of the model before its first call, at what precision, context
// size and latency. No model is involved. The approaches ("arms"):
//
//   - basic: the seed pack direct mode sends today (the request's names
//     looked up by grep);
//   - adaptive: internal/retrieve (strategies selected from the request);
//   - basic+tests, adaptive+tests: the same with the failing tests as
//     evidence, one "FAILED <id>" line each, as a verification retry or a
//     pasted test run would supply them;
//   - adaptive-<strategy>: adaptive without one strategy (ablation);
//   - adaptive+<strategy>: adaptive with one strategy always run.

// RetrievalTask is one instance (benchmarks/retrieval/prepare.py).
type RetrievalTask struct {
	ID           string   `json:"id"`
	Repo         string   `json:"repo"`
	Language     string   `json:"language"`
	Split        string   `json:"split"`
	Base         string   `json:"base"`
	Request      string   `json:"request"`
	Patch        string   `json:"patch"`
	FailingTests []string `json:"failing_tests"`
}

// RetrievalBench runs the benchmark.
type RetrievalBench struct {
	Tasks    []RetrievalTask
	ReposDir string // one checkout per repository, owner__name
	Arms     []string
	Split    string // dev | heldout | "" (all)
	Only     []string
	Tokens   int // block budget (default 1200, the seed pack's)
	Progress func(string)
}

// RetrievalArm is one approach's outcome on one instance.
type RetrievalArm struct {
	Files     []string `json:"files"`
	Symbols   []string `json:"symbols,omitempty"`
	Recall    float64  `json:"recall"`
	Precision float64  `json:"precision"`
	// Hit1 and Hit3: a gold file is first, or among the first three.
	Hit1 bool `json:"hit1"`
	Hit3 bool `json:"hit3"`
	// SymbolsRight counts retrieved symbols the fix changed.
	SymbolsRight int      `json:"symbols_right"`
	Tokens       int      `json:"tokens"`
	Millis       float64  `json:"ms"`
	Strategies   []string `json:"strategies,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// RetrievalCase is one instance's results.
type RetrievalCase struct {
	ID          string   `json:"id"`
	Repo        string   `json:"repo"`
	Language    string   `json:"language"`
	Split       string   `json:"split"`
	Category    string   `json:"category"`
	Gold        []string `json:"gold"`
	GoldSymbols []string `json:"gold_symbols,omitempty"`
	Files       int      `json:"repo_files"`
	// Languages are the repository's programming languages with at least
	// 10% of its code files; Polyglot is set when there are two or more.
	Languages []string                `json:"languages,omitempty"`
	Polyglot  bool                    `json:"polyglot"`
	Arms      map[string]RetrievalArm `json:"arms"`
}

// repoLanguages lists the programming languages with at least 10% of a
// checkout's code files.
func repoLanguages(root string) []string {
	counts, _, _ := lang.Census(root, lang.CensusOptions{})
	total := 0
	for _, c := range counts {
		if c.Language.Kind == lang.KindProgramming {
			total += c.Files
		}
	}
	var out []string
	for _, c := range counts {
		if c.Language.Kind == lang.KindProgramming && total > 0 && c.Files*10 >= total {
			out = append(out, c.Language.ID)
		}
	}
	sort.Strings(out)
	return out
}

// RetrievalReport is the benchmark's result.
type RetrievalReport struct {
	Started  time.Time       `json:"started"`
	Seconds  float64         `json:"seconds"`
	Tokens   int             `json:"tokens_budget"`
	Arms     []string        `json:"arms"`
	Cases    []RetrievalCase `json:"cases"`
	Skipped  []string        `json:"skipped,omitempty"`
	Settings map[string]any  `json:"settings,omitempty"`
}

var diffHeadRE = regexp.MustCompile(`^diff --git a/(\S+) b/(\S+)`)
var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,\d+)? @@`)

// goldOf derives what the fix needed: the existing files its patch
// changes that are code (any language), infrastructure or an API
// definition, and the definitions enclosing the changed lines.
func goldOf(root, patch string) (files, symbols []string) {
	type fileHunks struct {
		file  string
		isNew bool
		lines []int
	}
	var cur *fileHunks
	var all []*fileHunks
	old := 0
	for _, l := range strings.Split(patch, "\n") {
		if m := diffHeadRE.FindStringSubmatch(l); m != nil {
			cur = &fileHunks{file: m[1]}
			all = append(all, cur)
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(l, "new file mode"):
			cur.isNew = true
		case hunkRE.MatchString(l):
			m := hunkRE.FindStringSubmatch(l)
			old, _ = strconv.Atoi(m[1])
		case strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++"):
		case strings.HasPrefix(l, "-"):
			cur.lines = append(cur.lines, old)
			old++
		case strings.HasPrefix(l, "+"):
			cur.lines = append(cur.lines, max(old-1, 1))
		case strings.HasPrefix(l, " "):
			old++
		}
	}
	seenSym := map[string]bool{}
	for _, f := range all {
		if f.isNew {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.file))); err != nil {
			continue
		}
		l := lang.ForPath(f.file, "")
		if l == nil || !(l.Kind == lang.KindProgramming || l.Kind == lang.KindInfra || l.ID == "protobuf" || l.ID == "graphql") {
			continue
		}
		files = append(files, f.file)
		for _, ln := range f.lines {
			if s := retrieve.Enclosing(root, f.file, ln); s != "" && !seenSym[f.file+"\x00"+s] {
				seenSym[f.file+"\x00"+s] = true
				symbols = append(symbols, f.file+"#"+s)
			}
		}
	}
	return files, symbols
}

// category classifies a request by what it gives retrieval to work with.
func category(request string) string {
	s := retrieve.Extract(request)
	switch {
	case len(s.Frames) > 0:
		return "trace"
	case len(s.Diagnostics) > 0:
		return "located-error"
	case s.Strong():
		return "names"
	}
	return "prose"
}

// A place is "rel:line" or "rel:line (definition text)"; the text has
// parentheses of its own and ends at "), " or the end of the line.
var seedLocRE = regexp.MustCompile(`([\w@./+-]+\.[A-Za-z0-9]{1,8}):(\d+)(?: \((.*?)\)(?:, |$))?`)
var seedPathRE = regexp.MustCompile(`^- (\S+): (?:\d+ bytes|a directory)$|^- \S+: ((?:[\w@./+-]+(?:, )?)+)$`)

// seedPackFiles reads the files (in order) and definition names out of a
// seed pack block, as the model would.
func seedPackFiles(block string) (files, symbols []string) {
	seen := map[string]bool{}
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	for _, line := range strings.Split(block, "\n") {
		if m := seedPathRE.FindStringSubmatch(line); m != nil {
			if m[1] != "" {
				add(m[1])
				continue
			}
			for _, f := range strings.Split(m[2], ", ") {
				add(f)
			}
			continue
		}
		for _, m := range seedLocRE.FindAllStringSubmatch(line, -1) {
			add(m[1])
			if m[3] != "" {
				if l := lang.ForPath(m[1], ""); l != nil {
					if name, ok := l.Definition(m[3]); ok && name != "" {
						symbols = append(symbols, m[1]+"#"+name)
					}
				}
			}
		}
	}
	return files, symbols
}

func failingEvidence(tests []string) string {
	var b strings.Builder
	for _, t := range tests {
		fmt.Fprintf(&b, "FAILED %s\n", t)
	}
	return b.String()
}

// Run runs every arm on every selected instance.
func (b *RetrievalBench) Run(ctx context.Context) (*RetrievalReport, error) {
	if b.Tokens <= 0 {
		b.Tokens = 1200
	}
	rep := &RetrievalReport{Started: time.Now().UTC(), Tokens: b.Tokens, Arms: b.Arms}
	only := map[string]bool{}
	for _, o := range b.Only {
		only[o] = true
	}
	t0 := time.Now()
	// Instances of one repository run together, one checkout each.
	tasks := append([]RetrievalTask(nil), b.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Repo < tasks[j].Repo })
	for _, t := range tasks {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		if b.Split != "" && t.Split != b.Split || len(only) > 0 && !only[t.ID] && !only[t.Repo] {
			continue
		}
		root := filepath.Join(b.ReposDir, strings.ReplaceAll(t.Repo, "/", "__"))
		if err := checkoutBase(ctx, root, t.Base); err != nil {
			rep.Skipped = append(rep.Skipped, t.ID+": "+err.Error())
			continue
		}
		gold, goldSyms := goldOf(root, t.Patch)
		if len(gold) == 0 {
			rep.Skipped = append(rep.Skipped, t.ID+": the fix changes no existing code file")
			continue
		}
		c := RetrievalCase{ID: t.ID, Repo: t.Repo, Language: t.Language, Split: t.Split, Category: category(t.Request),
			Gold: gold, GoldSymbols: goldSyms, Arms: map[string]RetrievalArm{}}
		for _, arm := range b.Arms {
			a := b.runArm(ctx, arm, root, t)
			scoreArm(&a, gold, goldSyms)
			c.Arms[arm] = a
		}
		if res, err := retrieve.Retrieve(ctx, retrieve.Input{}, retrieve.Options{Root: root, Budget: time.Second}); err == nil {
			c.Files = res.Files
		}
		c.Languages = repoLanguages(root)
		c.Polyglot = len(c.Languages) > 1
		rep.Cases = append(rep.Cases, c)
		if b.Progress != nil {
			var parts []string
			for _, arm := range b.Arms {
				a := c.Arms[arm]
				parts = append(parts, fmt.Sprintf("%s r=%.2f p=%.2f %dt %.0fms", arm, a.Recall, a.Precision, a.Tokens, a.Millis))
			}
			b.Progress(fmt.Sprintf("%s [%s/%s/%s] gold=%d: %s", t.ID, t.Split, t.Language, c.Category, len(gold), strings.Join(parts, " | ")))
		}
	}
	rep.Seconds = time.Since(t0).Seconds()
	return rep, nil
}

func checkoutBase(ctx context.Context, root, base string) error {
	for _, args := range [][]string{{"checkout", "-q", "--detach", "-f", base}, {"clean", "-ffdxq"}} {
		c := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// runArm runs one approach.
func (b *RetrievalBench) runArm(ctx context.Context, arm, root string, t RetrievalTask) RetrievalArm {
	var a RetrievalArm
	evidence := ""
	name := arm
	if strings.HasSuffix(arm, "+tests") {
		evidence = failingEvidence(t.FailingTests)
		name = strings.TrimSuffix(arm, "+tests")
	}
	t0 := time.Now()
	if name == "basic" {
		req := t.Request
		if evidence != "" {
			req += "\n\n" + evidence
		}
		block := direct.SeedPackFor(ctx, root, req)
		a.Millis = float64(time.Since(t0).Microseconds()) / 1000
		a.Tokens = contextplan.EstimateTokens(block)
		a.Files, a.Symbols = seedPackFiles(block)
		return a
	}
	opt := retrieve.Options{Root: root}
	switch {
	case strings.HasPrefix(name, "adaptive-"):
		opt.Disable = map[retrieve.Strategy]bool{retrieve.Strategy(strings.TrimPrefix(name, "adaptive-")): true}
	case strings.HasPrefix(name, "adaptive+"):
		opt.Always = map[retrieve.Strategy]bool{retrieve.Strategy(strings.TrimPrefix(name, "adaptive+")): true}
	case name != "adaptive":
		a.Error = "unknown arm"
		return a
	}
	in := retrieve.Input{Request: t.Request}
	if evidence != "" {
		in.Evidence = []string{evidence}
	}
	res, err := retrieve.Retrieve(ctx, in, opt)
	a.Millis = float64(time.Since(t0).Microseconds()) / 1000
	if err != nil {
		a.Error = err.Error()
		return a
	}
	block := retrieve.Render(res, b.Tokens)
	a.Tokens = contextplan.EstimateTokens(block)
	for _, st := range res.Steps {
		if st.Ran {
			a.Strategies = append(a.Strategies, string(st.Strategy))
		}
	}
	// What the model sees: the rendered items.
	for _, it := range res.Items {
		if !strings.Contains(block, "- "+it.File) {
			continue
		}
		a.Files = append(a.Files, it.File)
		if it.Symbol != "" {
			a.Symbols = append(a.Symbols, it.File+"#"+it.Symbol)
		}
	}
	return a
}

func scoreArm(a *RetrievalArm, gold, goldSyms []string) {
	g := map[string]bool{}
	for _, f := range gold {
		g[f] = true
	}
	right := 0
	for i, f := range a.Files {
		if g[f] {
			right++
			if i == 0 {
				a.Hit1 = true
			}
			if i < 3 {
				a.Hit3 = true
			}
		}
	}
	a.Recall = float64(right) / float64(len(gold))
	if len(a.Files) > 0 {
		a.Precision = float64(right) / float64(len(a.Files))
	}
	gs := map[string]bool{}
	for _, s := range goldSyms {
		gs[s] = true
	}
	for _, s := range a.Symbols {
		if gs[s] {
			a.SymbolsRight++
		}
	}
}

// MarshalRetrieval encodes a report.
func MarshalRetrieval(r *RetrievalReport) ([]byte, error) { return json.MarshalIndent(r, "", " ") }

type armStats struct {
	n                                     int
	recall, precision, hit1, hit3, anyHit float64
	tokens, empty                         float64
	symRight, syms                        float64
	ms                                    []float64
}

func statsOf(cases []RetrievalCase, arm string) armStats {
	var s armStats
	for _, c := range cases {
		a, ok := c.Arms[arm]
		if !ok {
			continue
		}
		s.n++
		s.recall += a.Recall
		s.precision += a.Precision
		if a.Hit1 {
			s.hit1++
		}
		if a.Hit3 {
			s.hit3++
		}
		if a.Recall > 0 {
			s.anyHit++
		}
		if len(a.Files) == 0 {
			s.empty++
		}
		s.tokens += float64(a.Tokens)
		s.symRight += float64(a.SymbolsRight)
		s.syms += float64(len(a.Symbols))
		s.ms = append(s.ms, a.Millis)
	}
	return s
}

func rpct(x, n float64) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*x/n)
}

func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1)+0.5)]
}

// signTest is the two-sided exact sign test p-value for w wins and l
// losses (ties dropped).
func signTest(w, l int) float64 {
	n := w + l
	if n == 0 {
		return 1
	}
	k := min(w, l)
	p := 0.0
	for i := 0; i <= k; i++ {
		p += binom(n, i)
	}
	return math.Min(1, 2*p/math.Pow(2, float64(n)))
}

func binom(n, k int) float64 {
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n-k+i) / float64(i)
	}
	return r
}

// WriteRetrievalMarkdown renders the report: per split and arm, then per
// category and language, then the paired comparison of each arm with
// basic.
func WriteRetrievalMarkdown(w io.Writer, r *RetrievalReport) {
	fmt.Fprintf(w, "# Retrieval benchmark (%s)\n\n", r.Started.Format("2006-01-02"))
	fmt.Fprintf(w, "%d instances, block budget %d tokens, %.0f s. Gold: the existing code files each fix changed. Recall and precision are per instance, then averaged.\n\n", len(r.Cases), r.Tokens, r.Seconds)
	splits := []string{"dev", "heldout"}
	for _, sp := range splits {
		var cs []RetrievalCase
		for _, c := range r.Cases {
			if c.Split == sp {
				cs = append(cs, c)
			}
		}
		if len(cs) == 0 {
			continue
		}
		fmt.Fprintf(w, "## %s (%d instances)\n\n", map[string]string{"dev": "Development split", "heldout": "Held-out split"}[sp], len(cs))
		fmt.Fprintln(w, "| Arm | File recall | File precision | Gold first | Gold in top 3 | Any gold | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|")
		for _, arm := range r.Arms {
			s := statsOf(cs, arm)
			n := float64(s.n)
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %.0f / %.0f | %.0f | %.0f | %.0f |\n", arm, rpct(s.recall, n), rpct(s.precision, n), rpct(s.hit1, n), rpct(s.hit3, n),
				rpct(s.anyHit, n), rpct(s.empty, n), s.symRight, s.syms, s.tokens/math.Max(n, 1), quantile(s.ms, 0.5), quantile(s.ms, 0.95))
		}
		fmt.Fprintln(w)
		for _, by := range []string{"category", "language", "repository"} {
			groups := map[string][]RetrievalCase{}
			for _, c := range cs {
				k := c.Category
				switch by {
				case "language":
					k = c.Language
				case "repository":
					k = "single-language"
					if c.Polyglot {
						k = "multi-language"
					}
				}
				groups[k] = append(groups[k], c)
			}
			fmt.Fprintf(w, "By %s (file recall / precision):\n\n| %s | n |", by, by)
			for _, arm := range r.Arms {
				fmt.Fprintf(w, " %s |", arm)
			}
			fmt.Fprint(w, "\n|---|---|")
			for range r.Arms {
				fmt.Fprint(w, "---|")
			}
			fmt.Fprintln(w)
			keys := make([]string, 0, len(groups))
			for k := range groups {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(w, "| %s | %d |", k, len(groups[k]))
				for _, arm := range r.Arms {
					s := statsOf(groups[k], arm)
					fmt.Fprintf(w, " %s / %s |", rpct(s.recall, float64(s.n)), rpct(s.precision, float64(s.n)))
				}
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "Paired with basic (per instance; ties dropped; two-sided sign test):")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| Arm | Recall better / worse | p | Precision better / worse | p |")
		fmt.Fprintln(w, "|---|---|---|---|---|")
		for _, arm := range r.Arms {
			base := "basic"
			if strings.HasSuffix(arm, "+tests") {
				base = "basic+tests"
			}
			if arm == base {
				continue
			}
			rw, rl, pw, pl := 0, 0, 0, 0
			for _, c := range cs {
				a, ok1 := c.Arms[arm]
				bb, ok2 := c.Arms[base]
				if !ok1 || !ok2 {
					continue
				}
				switch {
				case a.Recall > bb.Recall:
					rw++
				case a.Recall < bb.Recall:
					rl++
				}
				switch {
				case a.Precision > bb.Precision:
					pw++
				case a.Precision < bb.Precision:
					pl++
				}
			}
			fmt.Fprintf(w, "| %s vs %s | %d / %d | %.3f | %d / %d | %.3f |\n", arm, base, rw, rl, signTest(rw, rl), pw, pl, signTest(pw, pl))
		}
		fmt.Fprintln(w)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "Skipped (%d): %s\n", len(r.Skipped), strings.Join(r.Skipped, "; "))
	}
}
