package benchmark

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/lease"
)

// The lease study scores lease.Derive before anything is built on it: for
// each recent commit of a repository, the lease derived from the commit
// message against the parent's tree is compared with what the commit
// changed, by declaration, file and directory. A commit message is a proxy
// for a request: it is written after the fact and, by convention in many
// Go projects, starts with the package path, so the study also scores the
// message without that prefix.

// LeaseStudy runs the study over one git repository.
type LeaseStudy struct {
	Repo     string // a git checkout (any branch; HEAD is the newest commit)
	Commits  int    // commits to score (0 = 100)
	MaxFiles int    // skip commits changing more files than this (0 = 30)
	Progress func(string)
}

// LeaseCommit is one scored commit.
type LeaseCommit struct {
	Commit  string   `json:"commit"`
	Subject string   `json:"subject"`
	Matched []string `json:"matched,omitempty"`
	// Changed and Leased sizes and their overlap, per level, for the
	// message as written and without its "pkg/path:" prefix.
	Full  LeaseScore `json:"full"`
	Bare  LeaseScore `json:"bare"`
	Tests int        `json:"test_files_changed"`
}

// LeaseScore is one commit's overlap at three levels.
type LeaseScore struct {
	Symbols Overlap `json:"symbols"`
	Files   Overlap `json:"files"`
	Dirs    Overlap `json:"dirs"`
	// Contained: every changed non-test file is inside the lease.
	Contained bool `json:"contained"`
	Empty     bool `json:"empty"`
}

// Overlap counts the changed set, the leased set and both.
type Overlap struct {
	Changed int `json:"changed"`
	Leased  int `json:"leased"`
	Both    int `json:"both"`
}

// LeaseReport is the study's result for one repository.
type LeaseReport struct {
	Repo    string        `json:"repo"`
	Head    string        `json:"head"`
	Started time.Time     `json:"started"`
	Commits []LeaseCommit `json:"commits"`
	Full    LeaseSummary  `json:"full"`
	Bare    LeaseSummary  `json:"bare"`
	Skipped int           `json:"skipped_commits"`
}

// LeaseSummary aggregates scores: micro-averaged precision and recall per
// level, how many leases named anything, and how many contained the
// whole change.
type LeaseSummary struct {
	Commits       int     `json:"commits"`
	NonEmpty      int     `json:"non_empty"`
	SymbolP       float64 `json:"symbol_precision"`
	SymbolR       float64 `json:"symbol_recall"`
	FileP         float64 `json:"file_precision"`
	FileR         float64 `json:"file_recall"`
	DirP          float64 `json:"dir_precision"`
	DirR          float64 `json:"dir_recall"`
	Contained     int     `json:"contained"`
	SymbolAnyHit  int     `json:"commits_with_a_leased_symbol_changed"`
	MeanLeaseSyms float64 `json:"mean_leased_symbols"`
}

func (s *LeaseStudy) progress(format string, a ...any) {
	if s.Progress != nil {
		s.Progress(fmt.Sprintf(format, a...))
	}
}

// Run scores the newest commits that change Go code.
func (s *LeaseStudy) Run(ctx context.Context) (*LeaseReport, error) {
	n := s.Commits
	if n <= 0 {
		n = 100
	}
	maxFiles := s.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 30
	}
	g, err := newGitReader(ctx, s.Repo)
	if err != nil {
		return nil, err
	}
	defer g.close()
	head, err := g.run("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	rep := &LeaseReport{Repo: path.Base(s.Repo), Head: strings.TrimSpace(head), Started: time.Now().UTC()}
	list, err := g.run("log", "--no-merges", "--format=%H %P", "-n", strconv.Itoa(n*4), "HEAD")
	if err != nil {
		return nil, err
	}
	cache := map[string][]lease.Decl{} // blob id -> declarations
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		if len(rep.Commits) == n || ctx.Err() != nil {
			break
		}
		f := strings.Fields(line)
		if len(f) != 2 { // root commits and merges
			continue
		}
		c, err := s.score(g, f[0], f[1], maxFiles, cache)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", f[0], err)
		}
		if c == nil {
			rep.Skipped++
			continue
		}
		rep.Commits = append(rep.Commits, *c)
		if len(rep.Commits)%20 == 0 {
			s.progress("%s: %d commits scored", rep.Repo, len(rep.Commits))
		}
	}
	rep.Full = summarizeLease(rep.Commits, func(c LeaseCommit) LeaseScore { return c.Full })
	rep.Bare = summarizeLease(rep.Commits, func(c LeaseCommit) LeaseScore { return c.Bare })
	return rep, ctx.Err()
}

var (
	trailerRE = regexp.MustCompile(`^[A-Z][A-Za-z-]+: `)
	prefixRE  = regexp.MustCompile(`^[\w./{},* -]+: `)
	hunkLine  = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// message is the commit message without trailers (Signed-off-by,
// Change-Id, Reviewed-on and the like).
func message(raw string) string {
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(raw), "\n") {
		if k, _, _ := strings.Cut(l, ":"); trailerRE.MatchString(l) && !strings.Contains(k, " ") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

func (s *LeaseStudy) score(g *gitReader, commit, parent string, maxFiles int, cache map[string][]lease.Decl) (*LeaseCommit, error) {
	diff, err := g.run("diff", "-U0", "--no-color", "--no-ext-diff", "--no-renames", parent, commit)
	if err != nil {
		return nil, err
	}
	type ranges struct{ old, new [][2]int }
	files := map[string]*ranges{}
	var order []string
	cur := ""
	header := false // between "diff --git" and the first hunk; a removed
	// line can itself start with "--- " (a txtar marker)
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			cur, header = "", true
		case !header && !strings.HasPrefix(l, "@@"):
			continue
		case strings.HasPrefix(l, "--- "):
			if p := strings.TrimPrefix(l, "--- "); p != "/dev/null" {
				cur = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(l, "+++ "):
			// A deleted file keeps its old path.
			if p := strings.TrimPrefix(l, "+++ "); p != "/dev/null" {
				cur = strings.TrimPrefix(p, "b/")
			}
			if cur != "" && files[cur] == nil {
				files[cur] = &ranges{}
				order = append(order, cur)
			}
		case strings.HasPrefix(l, "@@") && files[cur] != nil:
			header = false
			m := hunkLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			num := func(s string, def int) int {
				if s == "" {
					return def
				}
				v, _ := strconv.Atoi(s)
				return v
			}
			ostart, ol, ns, nl := num(m[1], 0), num(m[2], 1), num(m[3], 0), num(m[4], 1)
			if ol > 0 {
				files[cur].old = append(files[cur].old, [2]int{ostart, ostart + ol - 1})
			}
			if nl > 0 {
				files[cur].new = append(files[cur].new, [2]int{ns, ns + nl - 1})
			}
		}
	}
	changedFiles, changedSyms, changedDirs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	tests, goChanged := 0, false
	for _, f := range order {
		if lease.Skipped(f) {
			continue
		}
		if strings.HasSuffix(f, "_test.go") {
			tests++
			continue
		}
		changedFiles[f] = true
		changedDirs[dirOfPath(f)] = true
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		goChanged = true
		r := files[f]
		for _, side := range []struct {
			rev string
			rng [][2]int
		}{{parent, r.old}, {commit, r.new}} {
			if len(side.rng) == 0 {
				continue
			}
			decls, err := g.decls(side.rev, f, cache)
			if err != nil {
				return nil, err
			}
			for _, d := range lease.Touched(decls, side.rng) {
				changedSyms[d.Key()] = true
			}
		}
	}
	if !goChanged || len(changedFiles) > maxFiles {
		return nil, nil
	}
	ix, err := g.index(parent, cache)
	if err != nil {
		return nil, err
	}
	raw, err := g.run("log", "-1", "--format=%B", commit)
	if err != nil {
		return nil, err
	}
	msg := message(raw)
	subject, _, _ := strings.Cut(msg, "\n")
	full := lease.Derive(ix, msg)
	bareMsg := prefixRE.ReplaceAllString(msg, "")
	bare := lease.Derive(ix, bareMsg)
	return &LeaseCommit{Commit: commit[:12], Subject: tailStr(subject, 120), Matched: full.Matched,
		Full:  scoreLease(full, changedSyms, changedFiles, changedDirs),
		Bare:  scoreLease(bare, changedSyms, changedFiles, changedDirs),
		Tests: tests}, nil
}

func dirOfPath(f string) string {
	d := path.Dir(f)
	if d == "." {
		return ""
	}
	return d
}

func scoreLease(l lease.Lease, syms, files, dirs map[string]bool) LeaseScore {
	ov := func(changed, leased map[string]bool) Overlap {
		o := Overlap{Changed: len(changed), Leased: len(leased)}
		for k := range leased {
			if changed[k] {
				o.Both++
			}
		}
		return o
	}
	s := LeaseScore{Symbols: ov(syms, l.Symbols), Files: ov(files, l.Files), Dirs: ov(dirs, l.Dirs), Empty: l.Empty()}
	s.Contained = !s.Empty
	for f := range files {
		if !l.Covers(f) {
			s.Contained = false
		}
	}
	return s
}

func summarizeLease(cs []LeaseCommit, pick func(LeaseCommit) LeaseScore) LeaseSummary {
	var s LeaseSummary
	var sym, file, dir Overlap
	add := func(a *Overlap, b Overlap) { a.Changed += b.Changed; a.Leased += b.Leased; a.Both += b.Both }
	ratio := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b)
	}
	for _, c := range cs {
		sc := pick(c)
		s.Commits++
		if sc.Empty {
			continue
		}
		s.NonEmpty++
		add(&sym, sc.Symbols)
		add(&file, sc.Files)
		add(&dir, sc.Dirs)
		if sc.Contained {
			s.Contained++
		}
		if sc.Symbols.Both > 0 {
			s.SymbolAnyHit++
		}
	}
	// Precision over leases that named something; recall over every
	// commit's change (an empty lease recalls nothing).
	var allSym, allFile, allDir int
	for _, c := range cs {
		sc := pick(c)
		allSym += sc.Symbols.Changed
		allFile += sc.Files.Changed
		allDir += sc.Dirs.Changed
	}
	s.SymbolP, s.SymbolR = ratio(sym.Both, sym.Leased), ratio(sym.Both, allSym)
	s.FileP, s.FileR = ratio(file.Both, file.Leased), ratio(file.Both, allFile)
	s.DirP, s.DirR = ratio(dir.Both, dir.Leased), ratio(dir.Both, allDir)
	s.MeanLeaseSyms = ratio(sym.Leased, max(s.NonEmpty, 1))
	return s
}

// WriteLeaseMarkdown renders the study's reports.
func WriteLeaseMarkdown(w io.Writer, reps []*LeaseReport) {
	fmt.Fprintf(w, "# Lease study\n\nFor each commit, the lease derived from its message (against the parent's tree) is compared with what the commit changed outside tests. Precision is over the leases that named anything; recall is over every commit. \"Contained\" counts leases whose directories and files hold every changed file.\n\n")
	fmt.Fprintf(w, "| Repository | Message | Commits | Lease named something | Symbol P | Symbol R | File P | File R | Dir P | Dir R | Contained | Leased symbols |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range reps {
		for _, v := range []struct {
			name string
			s    LeaseSummary
		}{{"as written", r.Full}, {"without pkg: prefix", r.Bare}} {
			s := v.s
			fmt.Fprintf(w, "| %s | %s | %d | %d (%.0f%%) | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %d (%.0f%%) | %.1f |\n",
				r.Repo, v.name, s.Commits, s.NonEmpty, pct(s.NonEmpty, s.Commits), 100*s.SymbolP, 100*s.SymbolR, 100*s.FileP, 100*s.FileR,
				100*s.DirP, 100*s.DirR, s.Contained, pct(s.Contained, s.Commits), s.MeanLeaseSyms)
		}
	}
	fmt.Fprintln(w)
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

// MarshalLease renders reports as indented JSON.
func MarshalLease(reps []*LeaseReport) ([]byte, error) { return json.MarshalIndent(reps, "", "  ") }

// gitReader runs git commands in a repository and reads blobs through
// one long-lived `git cat-file --batch`.
type gitReader struct {
	ctx  context.Context
	dir  string
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	revs map[string]string // "rev:path" -> blob id
}

func newGitReader(ctx context.Context, dir string) (*gitReader, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &gitReader{ctx: ctx, dir: dir, cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), revs: map[string]string{}}, nil
}

func (g *gitReader) close() {
	_ = g.in.Close()
	_ = g.cmd.Wait()
}

func (g *gitReader) run(args ...string) (string, error) {
	c := exec.CommandContext(g.ctx, "git", args...)
	c.Dir = g.dir
	b, err := c.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("git %s: %w %s", strings.Join(args, " "), err, tailStr(stderr, 300))
	}
	return string(b), nil
}

// object reads one object ("rev:path" or a blob id); missing is nil.
func (g *gitReader) object(name string) ([]byte, error) {
	if _, err := fmt.Fprintln(g.in, name); err != nil {
		return nil, err
	}
	header, err := g.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	f := strings.Fields(header)
	if len(f) == 2 && f[1] == "missing" {
		return nil, nil
	}
	if len(f) != 3 {
		return nil, fmt.Errorf("cat-file: %q", header)
	}
	size, err := strconv.Atoi(f[2])
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size+1) // the content and a newline
	if _, err := io.ReadFull(g.out, buf); err != nil {
		return nil, err
	}
	return buf[:size], nil
}

// index is the lease index of the tree at rev.
func (g *gitReader) index(rev string, cache map[string][]lease.Decl) (*lease.Index, error) {
	tree, err := g.run("ls-tree", "-r", "--full-tree", rev)
	if err != nil {
		return nil, err
	}
	var paths []string
	blobs := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(tree), "\n") {
		meta, p, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		mf := strings.Fields(meta)
		if len(mf) == 3 && mf[1] == "blob" {
			paths = append(paths, p)
			blobs[p] = mf[2]
		}
	}
	return lease.NewIndex(paths, func(p string) []lease.Decl {
		d, _ := g.blobDecls(blobs[p], p, cache)
		return d
	}), nil
}

// decls returns the declarations of path at rev.
func (g *gitReader) decls(rev, p string, cache map[string][]lease.Decl) ([]lease.Decl, error) {
	id, ok := g.revs[rev+":"+p]
	if !ok {
		out, err := g.run("rev-parse", "--verify", "--quiet", rev+":"+p)
		if err != nil {
			return nil, nil //nolint:nilerr // the file does not exist at rev
		}
		id = strings.TrimSpace(out)
		g.revs[rev+":"+p] = id
	}
	return g.blobDecls(id, p, cache)
}

func (g *gitReader) blobDecls(id, p string, cache map[string][]lease.Decl) ([]lease.Decl, error) {
	if d, ok := cache[id]; ok {
		return relocate(d, p), nil
	}
	b, err := g.object(id)
	if err != nil {
		return nil, err
	}
	d := lease.ParseGo(p, b)
	if d == nil {
		d = []lease.Decl{}
	}
	cache[id] = d
	return d, nil
}

// relocate gives cached declarations of an identical blob the path they
// are read at (the same content can live at two paths).
func relocate(d []lease.Decl, p string) []lease.Decl {
	if len(d) == 0 || d[0].File == p {
		return d
	}
	out := make([]lease.Decl, len(d))
	dir := dirOfPath(p)
	for i, x := range d {
		x.File, x.Dir = p, dir
		out[i] = x
	}
	return out
}
