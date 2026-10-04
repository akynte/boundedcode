package benchmark

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/repointel/serena"
)

// IntelSpec is one repository-intelligence study (benchmarks/intel/*.yaml):
// ground-truth questions about one repository, asked of every backend.
type IntelSpec struct {
	ID        string          `yaml:"id"`
	Language  string          `yaml:"language"`
	Fixture   string          `yaml:"fixture"` // one repository under benchmarks/fixtures, e.g. symbol-nav/billing-service
	Source    string          `yaml:"source"`  // or gomod:<module>@<version>
	Questions []IntelQuestion `yaml:"questions"`
}

// IntelQuestion is one navigation question with its ground truth.
type IntelQuestion struct {
	ID     string `yaml:"id"`
	Kind   string `yaml:"kind"`   // definition | references | implementations
	Symbol string `yaml:"symbol"` // e.g. CreatePayment, PaymentRepository.Insert, backoff.Strategy
	// Method is a method of the interface (implementations; used by the
	// grep baseline, which cannot resolve types).
	Method string   `yaml:"method"`
	Expect []string `yaml:"expect"` // repo-relative files a correct answer contains
	Forbid []string `yaml:"forbid"` // decoy files a correct answer must not contain
}

// IntelAnswer is one backend's answer to one question.
type IntelAnswer struct {
	Question  string   `json:"question"`
	Kind      string   `json:"kind"`
	Backend   string   `json:"backend"`
	Files     []string `json:"files"`
	Recall    float64  `json:"recall"`
	Precision float64  `json:"precision"`
	Decoy     bool     `json:"decoy"` // returned a forbidden file
	Calls     int      `json:"calls"`
	Millis    float64  `json:"ms"`
	Tokens    int      `json:"context_tokens"` // what would be placed into the model context
	Error     string   `json:"error,omitempty"`
}

// IntelReport is the result of one study.
type IntelReport struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	// SerenaVersion and SerenaCommit identify the measured release; the CI
	// pin guard requires a report for the pinned version.
	SerenaVersion  string        `json:"serena_version"`
	SerenaCommit   string        `json:"serena_commit"`
	Repository     string        `json:"repository"`
	Files          int           `json:"source_files"`
	Started        time.Time     `json:"started"`
	Answers        []IntelAnswer `json:"answers"`
	SerenaStartSec float64       `json:"serena_start_s"` // first call, including process and language server start
	SerenaUsage    serena.Usage  `json:"serena_usage"`   // after all questions
	GraphIndexSec  float64       `json:"graph_index_s"`
	Notes          []string      `json:"notes,omitempty"`
}

// LoadIntelSpecs reads benchmarks/intel/*.yaml.
func LoadIntelSpecs(dir string) ([]IntelSpec, error) {
	var out []IntelSpec
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var s IntelSpec
		if err := config.DecodeStrict(b, &s); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if s.ID == "" || (s.Fixture == "") == (s.Source == "") || len(s.Questions) == 0 {
			return fmt.Errorf("%s: id, exactly one of fixture/source, and questions are required", p)
		}
		out = append(out, s)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

// IntelStudy runs specs against Serena, codebase-memory-mcp and a grep
// baseline (an agent without repository intelligence reading whole files).
type IntelStudy struct {
	FixturesDir string
	WorkRoot    string
	Serena      func(root string) *serena.Manager // new manager with its own instance root
	Graph       func(cacheDir string) *cbm.Client
	Progress    func(string)
}

// Run executes one spec.
func (st *IntelStudy) Run(ctx context.Context, spec IntelSpec) (*IntelReport, error) {
	rep := &IntelReport{ID: spec.ID, Language: spec.Language, Started: time.Now().UTC(), SerenaCommit: serena.PinnedCommit}
	dir := filepath.Join(st.WorkRoot, fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()))
	fixture, sources := "", map[string]string{}
	name := filepath.Base(spec.Fixture)
	if spec.Fixture != "" {
		fixture = filepath.Join(st.FixturesDir, filepath.Dir(spec.Fixture))
	} else {
		name = spec.ID
		sources[name] = spec.Source
	}
	repos, err := materialize(ctx, fixture, sources, filepath.Join(dir, "repos"), nil)
	if err != nil {
		return nil, err
	}
	root := repos[name]
	if root == "" {
		return nil, fmt.Errorf("repository %s not materialized", name)
	}
	rep.Repository = spec.Fixture + spec.Source
	rep.Files = countSource(root)

	// codebase-memory-mcp: index once (persistent session, as in tasks).
	g := st.Graph(filepath.Join(dir, "cbm"))
	if err := g.Open(ctx); err != nil {
		rep.Notes = append(rep.Notes, "codebase-memory-mcp unavailable: "+err.Error())
		g = nil
	}
	project := ""
	if g != nil {
		defer g.Close()
		t0 := time.Now()
		ir, err := g.Index(ctx, root, "study."+spec.ID, "full")
		rep.GraphIndexSec = time.Since(t0).Seconds()
		if err != nil {
			rep.Notes = append(rep.Notes, "index failed: "+err.Error())
			g = nil
		}
		project = ir.Project
	}

	m := st.Serena(filepath.Join(dir, "serena"))
	defer m.Close()
	if inst, err := serena.Detect(ctx, m.Executable); err == nil {
		rep.SerenaVersion = inst.PackageVersion
	}
	nav := &serena.Navigator{M: m}
	first := true
	for _, q := range spec.Questions {
		st.progress("%s/%s", spec.ID, q.ID)
		a := st.askSerena(ctx, nav, root, q)
		if first {
			rep.SerenaStartSec = a.Millis / 1000
			first = false
			// Ask again warm so latency is comparable across backends.
			a = st.askSerena(ctx, nav, root, q)
		}
		rep.Answers = append(rep.Answers, score(a, q))
		if g != nil {
			rep.Answers = append(rep.Answers, score(askGraph(ctx, g, project, root, q), q))
		}
		rep.Answers = append(rep.Answers, score(askGrep(ctx, root, spec.Language, q), q))
	}
	rep.SerenaUsage = m.Usage()
	return rep, nil
}

func (st *IntelStudy) progress(format string, args ...any) {
	if st.Progress != nil {
		st.Progress(fmt.Sprintf(format, args...))
	}
}

func timeIt(a *IntelAnswer, f func()) {
	t0 := time.Now()
	f()
	a.Millis = float64(time.Since(t0).Microseconds()) / 1000
}

// askSerena answers like the context planner does: find the symbol, then
// references or implementations of the best match.
func (st *IntelStudy) askSerena(ctx context.Context, nav *serena.Navigator, root string, q IntelQuestion) IntelAnswer {
	a := IntelAnswer{Question: q.ID, Kind: q.Kind, Backend: "serena"}
	timeIt(&a, func() {
		patterns := []string{q.Symbol}
		if strings.Contains(q.Symbol, ".") {
			patterns = []string{strings.ReplaceAll(q.Symbol, ".", "/"), q.Symbol[strings.LastIndexByte(q.Symbol, '.')+1:]}
		}
		var syms []repointel.Symbol
		for _, p := range patterns {
			a.Calls++
			found, err := nav.FindSymbol(ctx, root, p, repointel.FindOptions{IncludeBody: q.Kind == "definition"})
			if err != nil {
				a.Error = err.Error()
				return
			}
			if len(found) > 0 {
				syms = rankForStudy(found, q.Symbol)
				break
			}
		}
		if len(syms) == 0 {
			return
		}
		var ctxText strings.Builder
		switch q.Kind {
		case "definition":
			for _, s := range syms {
				a.Files = append(a.Files, s.File)
				fmt.Fprintf(&ctxText, "%s %s:%d-%d\n%s\n", s.NamePath, s.File, s.StartLine, s.EndLine, s.Body)
			}
		case "references":
			a.Calls++
			refs, err := nav.References(ctx, root, syms[0])
			if err != nil {
				a.Error = err.Error()
				return
			}
			for _, r := range refs {
				a.Files = append(a.Files, r.File)
				fmt.Fprintf(&ctxText, "%s:%d in %s: %s\n", r.File, r.Line, r.Symbol, r.Snippet)
			}
		case "implementations":
			a.Calls++
			impls, err := nav.Implementations(ctx, root, syms[0])
			if err != nil {
				a.Error = err.Error()
				return
			}
			for _, s := range impls {
				a.Files = append(a.Files, s.File)
				fmt.Fprintf(&ctxText, "%s (%s) %s:%d-%d\n", s.NamePath, s.Kind, s.File, s.StartLine, s.EndLine)
			}
		}
		a.Tokens = contextplan.EstimateTokens(ctxText.String())
	})
	return a
}

// rankForStudy prefers definitions whose path matches the qualifier
// ("backoff.Strategy" -> files under a backoff directory).
func rankForStudy(syms []repointel.Symbol, name string) []repointel.Symbol {
	qual := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		qual = strings.ToLower(name[:i])
	}
	out := append([]repointel.Symbol(nil), syms...)
	sort.SliceStable(out, func(i, j int) bool {
		mi := qual != "" && strings.Contains(strings.ToLower(out[i].File+"/"+out[i].NamePath), qual)
		mj := qual != "" && strings.Contains(strings.ToLower(out[j].File+"/"+out[j].NamePath), qual)
		if mi != mj {
			return mi
		}
		return out[i].File < out[j].File
	})
	return out
}

var graphFileRE = regexp.MustCompile(`(?m)file_path:\s*(\S+)|"file_path":"([^"]+)"`)

// askGraph uses codebase-memory-mcp's best tool for each question: the
// code snippet for definitions and Cypher over CALLS/USAGE/IMPLEMENTS edges.
func askGraph(ctx context.Context, g *cbm.Client, project, root string, q IntelQuestion) IntelAnswer {
	a := IntelAnswer{Question: q.ID, Kind: q.Kind, Backend: "codebase-memory"}
	timeIt(&a, func() {
		var outs []string
		call := func(tool string, args map[string]any) {
			a.Calls++
			b, err := g.Call(ctx, tool, args)
			if err != nil {
				a.Error = err.Error()
				return
			}
			outs = append(outs, string(b))
		}
		suffix := "." + q.Symbol
		switch q.Kind {
		case "definition":
			call("get_code_snippet", map[string]any{"project": project, "qualified_name": q.Symbol, "max_output_tokens": 2500})
		case "references":
			for _, edge := range []string{"CALLS", "USAGE"} {
				call("query_graph", map[string]any{"project": project, "query": fmt.Sprintf(
					"MATCH (a)-[:%s]->(b) WHERE b.qualified_name ENDS WITH '%s' RETURN DISTINCT a.name, a.file_path", edge, suffix)})
			}
		case "implementations":
			call("query_graph", map[string]any{"project": project, "query": fmt.Sprintf(
				"MATCH (a)-[:IMPLEMENTS]->(b) WHERE b.qualified_name ENDS WITH '%s' RETURN DISTINCT a.name, a.file_path", suffix)})
		}
		text := strings.Join(outs, "\n")
		a.Tokens = contextplan.EstimateTokens(text)
		a.Files = graphFiles(text, root)
	})
	return a
}

// graphFiles extracts repo-relative file paths from snippet output
// (file_path: …), ambiguity suggestions, and Cypher rows ("name path").
func graphFiles(text, root string) []string {
	var files []string
	for _, m := range graphFileRE.FindAllStringSubmatch(text, -1) {
		files = append(files, m[1]+m[2])
	}
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.Contains(f[1], ".") && strings.Contains(f[1], "/") || len(f) == 2 && looksLikeSource(f[1]) {
			files = append(files, f[1])
		}
	}
	for i, f := range files {
		files[i] = strings.TrimPrefix(strings.TrimPrefix(f, root), "/")
	}
	return files
}

func looksLikeSource(s string) bool {
	switch filepath.Ext(s) {
	case ".go", ".ts", ".tsx", ".js", ".py", ".rs":
		return true
	}
	return false
}

// askGrep is the no-intelligence baseline: find candidate files lexically and
// read them whole.
func askGrep(ctx context.Context, root, lang string, q IntelQuestion) IntelAnswer {
	a := IntelAnswer{Question: q.ID, Kind: q.Kind, Backend: "grep+read"}
	timeIt(&a, func() {
		name := q.Symbol[strings.LastIndexByte(q.Symbol, '.')+1:]
		var pattern string
		switch q.Kind {
		case "definition":
			pattern = `(func (\([^)]*\) )?|type |interface |class |function |const |enum )` + regexp.QuoteMeta(name) + `\b`
		case "references":
			pattern = `\b` + regexp.QuoteMeta(name) + `\b`
		case "implementations":
			if lang == "typescript" {
				pattern = `implements[^{]*\b` + regexp.QuoteMeta(name) + `\b`
			} else {
				pattern = `^func \([^)]*\) ` + regexp.QuoteMeta(q.Method) + `\(`
			}
		}
		a.Calls++
		cmd := exec.CommandContext(ctx, "rg", "-l", "--no-messages", "-g", "!.git", "-e", pattern, ".")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			if _, lerr := exec.LookPath("rg"); lerr != nil {
				a.Error = "ripgrep not installed"
			}
			return
		}
		total := 0
		for f := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			f = strings.TrimPrefix(f, "./")
			if f == "" {
				continue
			}
			a.Files = append(a.Files, f)
			a.Calls++ // reading the file
			if fi, err := os.Stat(filepath.Join(root, f)); err == nil {
				total += int(fi.Size())
			}
		}
		a.Tokens = (total*10 + 31) / 32
	})
	return a
}

func score(a IntelAnswer, q IntelQuestion) IntelAnswer {
	got := map[string]bool{}
	for _, f := range a.Files {
		got[f] = true
	}
	a.Files = a.Files[:0]
	for f := range got {
		a.Files = append(a.Files, f)
	}
	sort.Strings(a.Files)
	hit := 0
	for _, e := range q.Expect {
		if got[e] {
			hit++
		}
	}
	if len(q.Expect) > 0 {
		a.Recall = float64(hit) / float64(len(q.Expect))
	}
	if len(got) > 0 {
		a.Precision = float64(hit) / float64(len(got))
	}
	for _, f := range q.Forbid {
		a.Decoy = a.Decoy || got[f]
	}
	return a
}

func countSource(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && looksLikeSource(d.Name()) {
			n++
		}
		return nil
	})
	return n
}

// WriteIntelMarkdown renders study reports.
func WriteIntelMarkdown(w io.Writer, reps []*IntelReport) error {
	var b strings.Builder
	b.WriteString("# Repository intelligence overlap study (Serena vs codebase-memory-mcp vs grep)\n\n")
	b.WriteString("Generated by `boundedcode bench intel`. Recall/precision are over repo-relative files against hand-checked ground truth\n" +
		"(benchmarks/intel/*.yaml). Context tokens estimate what each answer would put into the model context; the grep\n" +
		"baseline reads every matching file whole. Serena latencies are warm (the first, cold call is reported separately).\n\n")
	type agg struct {
		n                    int
		recall, prec, ms     float64
		tokens, calls, decoy int
	}
	total := map[string]*agg{}
	for _, r := range reps {
		fmt.Fprintf(&b, "## %s (%s, %d source files; Serena %s @ %.8s)\n\n", r.ID, r.Repository, r.Files, r.SerenaVersion, r.SerenaCommit)
		fmt.Fprintf(&b, "Serena first call (process + language server start): %.1f s · after the study: %d processes, %.0f MiB RSS, %.1f CPU s · codebase-memory index: %.1f s\n\n",
			r.SerenaStartSec, r.SerenaUsage.Processes, r.SerenaUsage.RSSMiB, r.SerenaUsage.CPUSeconds, r.GraphIndexSec)
		b.WriteString("| question | kind | backend | recall | precision | decoy | calls | ms | context tokens | note |\n|---|---|---|---|---|---|---|---|---|---|\n")
		for _, a := range r.Answers {
			fmt.Fprintf(&b, "| %s | %s | %s | %.2f | %.2f | %v | %d | %.0f | %d | %s |\n", a.Question, a.Kind, a.Backend, a.Recall, a.Precision,
				a.Decoy, a.Calls, a.Millis, a.Tokens, strings.ReplaceAll(firstLine(a.Error), "|", "/"))
			g := total[a.Backend]
			if g == nil {
				g = &agg{}
				total[a.Backend] = g
			}
			g.n++
			g.recall += a.Recall
			g.prec += a.Precision
			g.ms += a.Millis
			g.tokens += a.Tokens
			g.calls += a.Calls
			if a.Decoy {
				g.decoy++
			}
		}
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "\nnote: %s\n", n)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Totals\n\n| backend | questions | mean recall | mean precision | decoys returned | calls | total ms | context tokens |\n|---|---|---|---|---|---|---|---|\n")
	names := make([]string, 0, len(total))
	for n := range total {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		g := total[n]
		fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %d | %d | %.0f | %d |\n", n, g.n, g.recall/float64(g.n), g.prec/float64(g.n), g.decoy, g.calls, g.ms, g.tokens)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
