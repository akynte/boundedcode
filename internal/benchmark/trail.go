package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/lease"
)

// The trail study asks whether a repository's own history predicts the
// files a change needs, before any trace of the agent's runs is recorded:
// git already holds a trail of which files change together. For each
// recent commit it scores, against what the commit changed outside tests,
// using only the commits before it:
//
//   - at request time, the files the commit message names (lease.Derive
//     without the "pkg:" prefix, as the seed pack would find them), the
//     same plus the files most often changed together with them, and the
//     files changed most often overall;
//   - at edit time, for each changed file, the files changed with it in at
//     least Support earlier commits and in at least Confidence of the
//     commits that changed it (an association rule, as in Zimmermann et
//     al., "Mining Version Histories to Guide Software Changes", 2004).
//     This is what a note after an edit ("x.go usually changes with y.go")
//     would have said.

// TrailStudy runs the study over one git repository.
type TrailStudy struct {
	Repo     string
	Rev      string // newest commit to consider ("" = HEAD); an older one holds out the newest commits
	Commits  int    // commits to score (0 = 100)
	History  int    // earlier commits that form each commit's trail (0 = 1000)
	MaxFiles int    // skip commits changing more files than this, scored or in a trail (0 = 30)
	Progress func(string)
}

// Rule is an edit-time threshold.
type Rule struct {
	Support    int     `json:"support"`
	Confidence float64 `json:"confidence"`
}

// TrailRules are the thresholds scored; the second is the one the
// decision rests on, fixed before the study was run.
var TrailRules = []Rule{{2, 0.3}, {3, 0.5}, {5, 0.7}}

const (
	trailNeighbours = 5  // files added to the request-time set
	trailHot        = 10 // size of the overall hot set
	trailPerEdit    = 3  // suggestions per edited file
)

// TrailCommit is one scored commit.
type TrailCommit struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Changed int    `json:"changed"`   // non-test files changed that existed before
	Added   int    `json:"new_files"` // files the commit created
	// Request-time sets: their size and how many changed files they hold.
	Named    Hits       `json:"named"`
	WithCo   Hits       `json:"named_with_cochange"`
	Hot      Hits       `json:"hot"`
	EditRule []EditHits `json:"edit"` // one per TrailRules entry
}

// Hits is a suggested set's size and the changed files in it.
type Hits struct {
	Size int `json:"size"`
	Hits int `json:"hits"`
}

// EditHits is what edit-time notes would have said for one commit.
type EditHits struct {
	Fired     int      `json:"fired"`     // changed files that produced a suggestion
	Suggested int      `json:"suggested"` // suggestions, summed over changed files
	Right     int      `json:"right"`     // suggestions that were other changed files
	Examples  []string `json:"examples,omitempty"`
}

// TrailReport is the study's result for one repository.
type TrailReport struct {
	Repo    string        `json:"repo"`
	Head    string        `json:"head"`
	Started time.Time     `json:"started"`
	Commits []TrailCommit `json:"commits"`
	Skipped int           `json:"skipped_commits"`
	Summary TrailSummary  `json:"summary"`
}

// TrailSummary aggregates a report.
type TrailSummary struct {
	Commits   int     `json:"commits"`
	MultiFile int     `json:"multi_file_commits"`
	NamedR    float64 `json:"named_recall"`
	NamedP    float64 `json:"named_precision"`
	WithCoR   float64 `json:"named_with_cochange_recall"`
	// AddedP: of the files the co-change step added, the share changed.
	AddedP float64     `json:"cochange_added_precision"`
	HotR   float64     `json:"hot_recall"`
	HotP   float64     `json:"hot_precision"`
	Edit   []EditStats `json:"edit"`
}

// EditStats aggregates edit-time notes at one threshold.
type EditStats struct {
	Rule
	Suggested int     `json:"suggested"`
	Precision float64 `json:"precision"`
	// Coverage: multi-file commits where a note named another changed file.
	Coverage float64 `json:"coverage"`
	// Nag: single-file commits where a note fired (every suggestion wrong).
	Nag float64 `json:"single_file_nag"`
}

type logCommit struct {
	hash, parent string
	files        []string // counted files: not tests, not skipped
	goChanged    bool
	oversized    bool
}

func (s *TrailStudy) progress(format string, a ...any) {
	if s.Progress != nil {
		s.Progress(fmt.Sprintf(format, a...))
	}
}

// Run scores the newest commits that change Go code.
func (s *TrailStudy) Run(ctx context.Context) (*TrailReport, error) {
	n, hist, maxFiles := s.Commits, s.History, s.MaxFiles
	if n <= 0 {
		n = 100
	}
	if hist <= 0 {
		hist = 1000
	}
	if maxFiles <= 0 {
		maxFiles = 30
	}
	g, err := newGitReader(ctx, s.Repo)
	if err != nil {
		return nil, err
	}
	defer g.close()
	rev := s.Rev
	if rev == "" {
		rev = "HEAD"
	}
	head, err := g.run("rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return nil, err
	}
	rep := &TrailReport{Repo: path.Base(s.Repo), Head: strings.TrimSpace(head), Started: time.Now().UTC()}
	raw, err := g.run("log", "--no-merges", "--no-renames", "--format=%x00%H %P", "--name-only", "-n", strconv.Itoa(n*4+hist), strings.TrimSpace(head))
	if err != nil {
		return nil, err
	}
	log := parseNameLog(raw, maxFiles)
	cache := map[string][]lease.Decl{}
	for i, c := range log {
		if len(rep.Commits) == n || i >= n*4 || ctx.Err() != nil {
			break
		}
		if c.parent == "" || !c.goChanged || c.oversized {
			rep.Skipped++
			continue
		}
		tc, err := s.score(g, c, log[i+1:min(len(log), i+1+hist)], cache)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", c.hash, err)
		}
		rep.Commits = append(rep.Commits, *tc)
		if len(rep.Commits)%20 == 0 {
			s.progress("%s: %d commits scored", rep.Repo, len(rep.Commits))
		}
	}
	rep.Summary = summarizeTrail(rep.Commits)
	return rep, ctx.Err()
}

// parseNameLog reads `git log --format=%x00%H %P --name-only`.
func parseNameLog(raw string, maxFiles int) []logCommit {
	var out []logCommit
	for _, chunk := range strings.Split(raw, "\x00") {
		lines := strings.Split(strings.TrimSpace(chunk), "\n")
		f := strings.Fields(lines[0])
		if len(f) == 0 {
			continue
		}
		c := logCommit{hash: f[0]}
		if len(f) == 2 {
			c.parent = f[1]
		}
		for _, p := range lines[1:] {
			p = strings.TrimSpace(p)
			if p == "" || lease.Skipped(p) || strings.HasSuffix(p, "_test.go") {
				continue
			}
			c.files = append(c.files, p)
			if strings.HasSuffix(p, ".go") {
				c.goChanged = true
			}
		}
		c.oversized = len(c.files) > maxFiles
		out = append(out, c)
	}
	return out
}

func (s *TrailStudy) score(g *gitReader, c logCommit, window []logCommit, cache map[string][]lease.Decl) (*TrailCommit, error) {
	ix, err := g.index(c.parent, cache)
	if err != nil {
		return nil, err
	}
	raw, err := g.run("log", "-1", "--format=%B", c.hash)
	if err != nil {
		return nil, err
	}
	msg := message(raw)
	subject, _, _ := strings.Cut(msg, "\n")
	tc := &TrailCommit{Commit: c.hash[:12], Subject: tailStr(subject, 120)}
	changed := map[string]bool{}
	var changedList []string
	for _, f := range c.files {
		if ix.Files[f] {
			changed[f] = true
			changedList = append(changedList, f)
		} else {
			tc.Added++
		}
	}
	tc.Changed = len(changed)
	named := map[string]bool{}
	for f := range lease.Derive(ix, prefixRE.ReplaceAllString(msg, "")).Files {
		if !strings.HasSuffix(f, "_test.go") {
			named[f] = true
		}
	}

	// One pass over the trail: how often each file changed, and how often
	// together with a file the commit names or changes.
	of := map[string]bool{}
	for f := range named {
		of[f] = true
	}
	for f := range changed {
		of[f] = true
	}
	count := map[string]int{}
	pair := map[string]map[string]int{}
	for _, h := range window {
		if h.oversized {
			continue
		}
		for _, x := range h.files {
			count[x]++
			if !of[x] {
				continue
			}
			if pair[x] == nil {
				pair[x] = map[string]int{}
			}
			for _, y := range h.files {
				if y != x {
					pair[x][y]++
				}
			}
		}
	}
	exists := func(f string) bool { return ix.Files[f] && !strings.HasSuffix(f, "_test.go") }
	hits := func(set map[string]bool) Hits {
		h := Hits{Size: len(set)}
		for f := range set {
			if changed[f] {
				h.Hits++
			}
		}
		return h
	}

	tc.Named = hits(named)
	co := map[string]int{}
	for x := range named {
		for y, k := range pair[x] {
			if !named[y] && exists(y) {
				co[y] += k
			}
		}
	}
	withCo := map[string]bool{}
	for f := range named {
		withCo[f] = true
	}
	for _, f := range topKeys(co, trailNeighbours) {
		withCo[f] = true
	}
	tc.WithCo = hits(withCo)
	hot := map[string]int{}
	for f, k := range count {
		if exists(f) {
			hot[f] = k
		}
	}
	tc.Hot = hits(keySet(topKeys(hot, trailHot)))

	sort.Strings(changedList)
	for _, r := range TrailRules {
		var e EditHits
		for _, x := range changedList {
			cand := map[string]float64{}
			for y, k := range pair[x] {
				if conf := float64(k) / float64(count[x]); k >= r.Support && conf >= r.Confidence && exists(y) {
					cand[y] = conf
				}
			}
			sug := topKeys(cand, trailPerEdit)
			if len(sug) == 0 {
				continue
			}
			e.Fired++
			e.Suggested += len(sug)
			for _, y := range sug {
				if changed[y] {
					e.Right++
					if len(e.Examples) < 3 {
						e.Examples = append(e.Examples, x+" -> "+y)
					}
				}
			}
		}
		tc.EditRule = append(tc.EditRule, e)
	}
	return tc, nil
}

// topKeys returns up to k keys with the highest values, ties by name.
func topKeys[V int | float64](m map[string]V, k int) []string {
	keys := make([]string, 0, len(m))
	for f := range m {
		keys = append(keys, f)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys[:min(k, len(keys))]
}

func keySet(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func summarizeTrail(cs []TrailCommit) TrailSummary {
	s := TrailSummary{Commits: len(cs)}
	var changed, named, namedHits, withCoHits, added, addedHits, hot, hotHits int
	for _, c := range cs {
		changed += c.Changed
		named += c.Named.Size
		namedHits += c.Named.Hits
		withCoHits += c.WithCo.Hits
		added += c.WithCo.Size - c.Named.Size
		addedHits += c.WithCo.Hits - c.Named.Hits
		hot += c.Hot.Size
		hotHits += c.Hot.Hits
		if c.Changed > 1 {
			s.MultiFile++
		}
	}
	ratio := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b)
	}
	s.NamedR, s.NamedP = ratio(namedHits, changed), ratio(namedHits, named)
	s.WithCoR, s.AddedP = ratio(withCoHits, changed), ratio(addedHits, added)
	s.HotR, s.HotP = ratio(hotHits, changed), ratio(hotHits, hot)
	for i, r := range TrailRules {
		st := EditStats{Rule: r}
		var right, covered, single, nag int
		for _, c := range cs {
			e := c.EditRule[i]
			st.Suggested += e.Suggested
			right += e.Right
			switch {
			case c.Changed > 1 && e.Right > 0:
				covered++
			case c.Changed == 1:
				single++
				if e.Fired > 0 {
					nag++
				}
			}
		}
		st.Precision, st.Coverage, st.Nag = ratio(right, st.Suggested), ratio(covered, s.MultiFile), ratio(nag, single)
		s.Edit = append(s.Edit, st)
	}
	return s
}

// WriteTrailMarkdown renders the study's reports.
func WriteTrailMarkdown(w io.Writer, reps []*TrailReport) {
	fmt.Fprintf(w, "# Trail study\n\nFor each commit, files suggested from the commits before it are compared with the files it changed outside tests (that existed before it). Recall is over every commit's changed files; precision over the files suggested.\n\n")
	fmt.Fprintf(w, "## At request time\n\n| Repository | Commits | Named: recall | Named: precision | Named + %d co-changed: recall | Co-changed files that were changed | %d most changed: recall | precision |\n|---|---|---|---|---|---|---|---|\n", trailNeighbours, trailHot)
	for _, r := range reps {
		s := r.Summary
		fmt.Fprintf(w, "| %s | %d | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.0f%% |\n", r.Repo, s.Commits,
			100*s.NamedR, 100*s.NamedP, 100*s.WithCoR, 100*s.AddedP, 100*s.HotR, 100*s.HotP)
	}
	fmt.Fprintf(w, "\n## At edit time\n\nAfter each changed file, up to %d files changed with it in at least *support* earlier commits and in at least *confidence* of the commits that changed it.\n\n| Repository | Support | Confidence | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |\n|---|---|---|---|---|---|---|\n", trailPerEdit)
	for _, r := range reps {
		for _, e := range r.Summary.Edit {
			fmt.Fprintf(w, "| %s | %d | %.1f | %d | %.0f%% | %.0f%% (of %d) | %.0f%% |\n", r.Repo, e.Support, e.Confidence, e.Suggested,
				100*e.Precision, 100*e.Coverage, r.Summary.MultiFile, 100*e.Nag)
		}
	}
	fmt.Fprintln(w)
}

// MarshalTrail renders reports as indented JSON.
func MarshalTrail(reps []*TrailReport) ([]byte, error) { return json.MarshalIndent(reps, "", "  ") }
