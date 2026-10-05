package contextplan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/task"
)

// IntelStats records which repository-intelligence backend served the
// pack's symbol context, for telemetry and the Serena benchmark.
type IntelStats struct {
	NavCalls     int     `json:"nav_calls"`     // Serena (LSP) tool calls
	NavErrors    int     `json:"nav_errors"`    // failed or unavailable calls
	NavMillis    float64 `json:"nav_ms"`        // time spent in Serena calls
	GraphCalls   int     `json:"graph_calls"`   // codebase-memory-mcp calls for symbol context
	GraphMillis  float64 `json:"graph_ms"`      // time spent in those calls
	NavSymbols   int     `json:"nav_symbols"`   // symbols answered by Serena
	GraphSymbols int     `json:"graph_symbols"` // symbols answered by the graph (default or fallback)
	Fallbacks    int     `json:"fallbacks"`     // symbols Serena could not answer that the graph did
	ReposSkipped int     `json:"repos_skipped"` // repositories not queried because the graph lacks the name
	// Lexical search (ripgrep over the task worktrees) is the last stage,
	// for names neither Serena nor the graph answered.
	LexicalCalls   int     `json:"lexical_calls"`
	LexicalMillis  float64 `json:"lexical_ms"`
	LexicalSymbols int     `json:"lexical_symbols"` // symbols answered only by lexical search
	// Seeds are the ranked retrieval seeds tried ("kind:text"), DiagnosticHits
	// the error messages located in the code, Demoted the sample names
	// skipped as too common or placeholders.
	Seeds          []string `json:"seeds,omitempty"`
	DiagnosticHits int      `json:"diagnostic_hits"`
	Demoted        []string `json:"demoted,omitempty"`
}

// Limits keep symbol context compact: the point of LSP navigation is to send
// precise pieces instead of whole files.
const (
	maxSymbolsShown   = 6
	maxMatchesPerName = 3
	maxBodyLines      = 80
	maxReferences     = 12
	maxImplementation = 10
	maxLexicalHits    = 10              // per name, across worktrees
	lexicalRadius     = 3               // lines of context around a hit
	lexicalTimeout    = 5 * time.Second // per name, across worktrees
)

// timed runs f and adds its duration to *ms.
func timed[T any](ms *float64, f func() (T, error)) (T, error) {
	t0 := time.Now()
	v, err := f()
	*ms += float64(time.Since(t0).Microseconds()) / 1000
	return v, err
}

// requestSymbols writes context for symbols named in the request. Routing
// (ADR-0008): Serena answers "where is X / who references X / what
// implements X" for each task worktree; in a multi-repository task the code
// graph first decides which repositories mention X at all (breadth), then
// Serena gives the precise symbols there (depth). When Serena is absent,
// fails or finds nothing, the graph's snippet and callers are used, as before.
// A name neither answers (or that has no index at all) falls back to a
// bounded lexical search of the task worktrees.
//
// Seeds are tried in rank order (see seeds.go): a quoted error message is
// searched literally first; sample-code names that match a large share of
// the repository are skipped.
func requestSymbols(ctx context.Context, in Inputs, st *IntelStats) string {
	var b strings.Builder
	shown, freqChecks := 0, 0
	for _, seed := range Seeds(in.Task.OriginalRequest + "\n" + in.Task.Goal) {
		if shown >= maxSymbolsShown {
			break
		}
		if len(st.Seeds) < 12 {
			st.Seeds = append(st.Seeds, seed.Kind+":"+trunc1(seed.Text, 80))
		}
		if seed.Kind == "diagnostic" {
			if text, ok := diagnosticSymbol(ctx, in, seed.Text, st); ok {
				b.WriteString(text)
				st.DiagnosticHits++
				shown++
			}
			continue
		}
		if seed.Kind == "sample" && freqChecks < frequencyCheckLimit && len(in.Worktrees) > 0 {
			freqChecks++
			if n := fileMatches(ctx, in.Worktrees[0].Path, seed.Text, maxSeedFileMatches); n > maxSeedFileMatches {
				st.Demoted = append(st.Demoted, seed.Text)
				continue
			}
		}
		if looksLikePath(seed.Text) {
			if text, ok := pathSymbol(ctx, in, seed.Text, st); ok {
				b.WriteString(text)
				st.LexicalSymbols++
				shown++
			}
			continue
		}
		n := seed.Text
		before := shown
		for _, w := range in.Worktrees {
			if shown >= maxSymbolsShown {
				break
			}
			if in.Nav != nil {
				if len(in.Worktrees) > 1 && !graphMentions(ctx, in, w, n, st) {
					st.ReposSkipped++
					continue
				}
				if text, ok := navSymbol(ctx, in, w, n, st); ok {
					b.WriteString(text)
					st.NavSymbols++
					shown++
					continue
				}
			}
			if text, ok := graphSymbol(ctx, in, w, n, st); ok {
				b.WriteString(text)
				st.GraphSymbols++
				if in.Nav != nil {
					st.Fallbacks++
				}
				shown++
				if in.Nav == nil {
					break // graph-only behaviour: first repository with the symbol
				}
			}
		}
		if shown == before {
			// Lexical fallback only for names specific enough to help: one
			// that matches a large share of the repository gives scattered
			// hits, not context.
			if freqChecks < frequencyCheckLimit && len(in.Worktrees) > 0 {
				freqChecks++
				if fileMatches(ctx, in.Worktrees[0].Path, n, maxSeedFileMatches) > maxSeedFileMatches {
					st.Demoted = append(st.Demoted, n)
					continue
				}
			}
			if text, ok := lexicalSymbol(ctx, in, n, st); ok {
				b.WriteString(text)
				st.LexicalSymbols++
				shown++
			}
		}
	}
	return b.String()
}

// diagnosticSymbol locates an error message in the task worktrees: a
// fixed-string search (not whole-word) for its invariant text, which finds
// the code that produces it.
func diagnosticSymbol(ctx context.Context, in Inputs, msg string, st *IntelStats) (string, bool) {
	rg, err := exec.LookPath(rgBinary)
	if err != nil || len(in.Worktrees) == 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, lexicalTimeout)
	defer cancel()
	t0 := time.Now()
	defer func() { st.LexicalMillis += float64(time.Since(t0).Microseconds()) / 1000 }()
	var b strings.Builder
	hits := 0
	for _, w := range in.Worktrees {
		st.LexicalCalls++
		for _, h := range ripgrepMode(ctx, rg, w.Path, msg, maxMatchesPerName-hits, false) {
			fmt.Fprintf(&b, "### %s: diagnostic %q raised at ./%s/%s:%d\n```\n%s```\n", w.RepoName, trunc1(msg, 100), w.RepoName, h.file, h.line,
				readAround([]task.Worktree{w}, w.RepoName, h.file, h.line, lexicalRadius+3))
			hits++
		}
		if hits >= maxMatchesPerName {
			break
		}
	}
	return b.String(), hits > 0
}

func trunc1(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// rgBinary is the ripgrep executable; lexical search is skipped without it.
var rgBinary = "rg"

// lexicalSymbol is the last retrieval stage: whole-word, fixed-string
// ripgrep matches of name in the task worktrees, with a few lines of
// context each. ripgrep skips hidden and git-ignored files and does not
// follow symlinks; secret paths are dropped as well.
func lexicalSymbol(ctx context.Context, in Inputs, name string, st *IntelStats) (string, bool) {
	rg, err := exec.LookPath(rgBinary)
	if err != nil || len(in.Worktrees) == 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, lexicalTimeout)
	defer cancel()
	t0 := time.Now()
	defer func() { st.LexicalMillis += float64(time.Since(t0).Microseconds()) / 1000 }()
	var b strings.Builder
	hits := 0
	for _, w := range in.Worktrees {
		for _, pattern := range lexicalPatterns(name) {
			if hits >= maxLexicalHits || ctx.Err() != nil {
				break
			}
			st.LexicalCalls++
			found := ripgrep(ctx, rg, w.Path, pattern, maxLexicalHits-hits)
			for _, h := range found {
				fmt.Fprintf(&b, "### %s: %s (lexical) ./%s/%s:%d\n```\n%s```\n", w.RepoName, pattern, w.RepoName, h.file, h.line,
					readAround([]task.Worktree{w}, w.RepoName, h.file, h.line, lexicalRadius))
			}
			hits += len(found)
			if len(found) > 0 {
				break // the qualified name matched; skip the bare last segment
			}
		}
	}
	return b.String(), hits > 0
}

// lexicalPatterns searches a qualified name ("ledger.Post") literally, then
// by its last segment, which is how a definition spells it.
func lexicalPatterns(name string) []string {
	if last := lastSegment(name); last != name {
		return []string{name, last}
	}
	return []string{name}
}

type lexicalHit struct {
	file string // slash-separated, relative to the worktree
	line int
}

// ripgrep returns up to limit matches of pattern under root in path order,
// excluding secret paths. Errors (including a timeout) yield what was found.
func ripgrep(ctx context.Context, rg, root, pattern string, limit int) []lexicalHit {
	return ripgrepMode(ctx, rg, root, pattern, limit, true)
}

// ripgrepMode is ripgrep with or without whole-word matching.
func ripgrepMode(ctx context.Context, rg, root, pattern string, limit int, word bool) []lexicalHit {
	// Source maps and minified bundles repeat the source they were built
	// from and only add noise.
	args := []string{"--json", "-n", "--fixed-strings", "--max-count", "3", "--max-columns", "200",
		"--max-filesize", "1M", "--sort", "path", "-g", "!vendor", "-g", "!node_modules", "-g", "!*.map", "-g", "!*.min.*"}
	if word {
		args = append(args, "-w")
	}
	cmd := exec.CommandContext(ctx, rg, append(args, "--", pattern, ".")...)
	cmd.Dir = root
	out, _ := cmd.Output() // exit 1 means no match
	var hits []lexicalHit
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	// Collect beyond the limit so that code can be preferred over
	// documentation and build output before truncating.
	for sc.Scan() && len(hits) < 3*limit {
		var m struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				LineNumber int `json:"line_number"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Type != "match" || m.Data.Path.Text == "" {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(m.Data.Path.Text))
		if policy.IsSecretPath(rel) || strings.HasPrefix(rel, "../") {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(root, rel)); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		hits = append(hits, lexicalHit{file: rel, line: m.Data.LineNumber})
	}
	// Code before documentation (changelogs, READMEs mention every name).
	sort.SliceStable(hits, func(i, j int) bool { return !isDocFile(hits[i].file) && isDocFile(hits[j].file) })
	if len(hits) > limit {
		hits = hits[:max(limit, 0)]
	}
	return hits
}

// graphMentions asks the code graph whether a repository has a symbol with
// this name. Without a graph (or an index), every repository is a candidate.
func graphMentions(ctx context.Context, in Inputs, w task.Worktree, name string, st *IntelStats) bool {
	if in.Intel == nil || w.IndexProject == "" {
		return true
	}
	st.GraphCalls++
	out, err := timed(&st.GraphMillis, func() (string, error) { return in.Intel.Search(ctx, w.IndexProject, lastSegment(name), 5) })
	if err != nil {
		return true
	}
	return strings.Contains(out, lastSegment(name))
}

// graphSymbol is the graph-based context: snippet plus direct callers.
func graphSymbol(ctx context.Context, in Inputs, w task.Worktree, n string, st *IntelStats) (string, bool) {
	if in.Intel == nil || w.IndexProject == "" {
		return "", false
	}
	st.GraphCalls++
	snip, err := timed(&st.GraphMillis, func() (string, error) { return in.Intel.Snippet(ctx, w.IndexProject, n) })
	if err != nil || strings.TrimSpace(snip) == "" || strings.Contains(snip, "not found") {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### %s: %s\n%s\n", w.RepoName, n, strings.TrimSpace(snip))
	st.GraphCalls++
	if callers, err := timed(&st.GraphMillis, func() (string, error) { return in.Intel.Trace(ctx, w.IndexProject, n, "inbound", 1) }); err == nil {
		fmt.Fprintf(&b, "callers:\n%s\n", strings.TrimSpace(callers))
	}
	return b.String(), true
}

// navSymbol is the LSP-based context for name in one worktree: definition
// body, implementations of interfaces, and references as file:line pointers.
func navSymbol(ctx context.Context, in Inputs, w task.Worktree, name string, st *IntelStats) (string, bool) {
	var syms []repointel.Symbol
	for _, pattern := range namePatterns(name) {
		st.NavCalls++
		found, err := timed(&st.NavMillis, func() ([]repointel.Symbol, error) {
			return in.Nav.FindSymbol(ctx, w.Path, pattern, repointel.FindOptions{IncludeBody: true})
		})
		if err != nil {
			st.NavErrors++
			if errors.Is(err, repointel.ErrUnavailable) || ctx.Err() != nil {
				return "", false
			}
			continue
		}
		if len(found) > 0 {
			syms = rankMatches(found, name)
			break
		}
	}
	if len(syms) == 0 {
		return "", false
	}
	var b strings.Builder
	for i, s := range syms {
		if i >= maxMatchesPerName {
			fmt.Fprintf(&b, "(%d more definitions named %s not shown)\n", len(syms)-i, name)
			break
		}
		fmt.Fprintf(&b, "### %s: %s (%s) ./%s/%s:%d-%d\n```\n%s\n```\n", w.RepoName, s.NamePath, strings.ToLower(s.Kind),
			w.RepoName, s.File, s.StartLine, s.EndLine, clipLines(s.Body, maxBodyLines))
		if repointel.IsInterfaceKind(s.Kind) {
			st.NavCalls++
			impls, err := timed(&st.NavMillis, func() ([]repointel.Symbol, error) { return in.Nav.Implementations(ctx, w.Path, s) })
			if err != nil {
				st.NavErrors++
			} else if len(impls) > 0 {
				sort.Slice(impls, func(i, j int) bool { return impls[i].File+impls[i].NamePath < impls[j].File+impls[j].NamePath })
				b.WriteString("implementations:\n")
				for k, im := range impls {
					if k >= maxImplementation {
						fmt.Fprintf(&b, "- … %d more\n", len(impls)-k)
						break
					}
					fmt.Fprintf(&b, "- %s (%s) ./%s/%s:%d-%d\n", im.NamePath, strings.ToLower(im.Kind), w.RepoName, im.File, im.StartLine, im.EndLine)
				}
			}
		}
		st.NavCalls++
		refs, err := timed(&st.NavMillis, func() ([]repointel.Reference, error) { return in.Nav.References(ctx, w.Path, s) })
		if err != nil {
			st.NavErrors++
			continue
		}
		if len(refs) > 0 {
			b.WriteString("referenced from:\n")
			for k, r := range refs {
				if k >= maxReferences {
					fmt.Fprintf(&b, "- … %d more\n", len(refs)-k)
					break
				}
				fmt.Fprintf(&b, "- ./%s/%s:%d in %s: %s\n", w.RepoName, r.File, r.Line, r.Symbol, oneLine(r.Snippet, 140))
			}
		}
	}
	return b.String(), true
}

// namePatterns turns a request identifier into Serena name-path patterns:
// "Store.Get" -> "Store/Get" (nested symbols), then "Get" (Go methods are
// top-level symbols in gopls); "ledger.Post" -> "ledger/Post", "Post".
func namePatterns(name string) []string {
	if !strings.Contains(name, ".") {
		return []string{name}
	}
	return []string{strings.ReplaceAll(name, ".", "/"), lastSegment(name)}
}

func lastSegment(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// rankMatches orders definitions so that, for a qualified request name like
// "ledger.Post", matches in a matching path come first; exact name matches
// precede suffix matches. Order is deterministic.
func rankMatches(syms []repointel.Symbol, name string) []repointel.Symbol {
	qual := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		qual = strings.ToLower(name[:i])
	}
	last := lastSegment(name)
	score := func(s repointel.Symbol) int {
		sc := 0
		if qual != "" && (strings.Contains(strings.ToLower(s.File), qual) || strings.HasPrefix(strings.ToLower(s.NamePath), qual)) {
			sc += 2
		}
		if s.NamePath == last || strings.HasSuffix(s.NamePath, "/"+last) {
			sc++
		}
		return sc
	}
	out := append([]repointel.Symbol(nil), syms...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := score(out[i]), score(out[j]); a != b {
			return a > b
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out
}

func clipLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… (%d more lines)", len(lines)-n)
}

// isDocFile reports files that mention names without implementing them:
// documentation and changelogs, and build output (dist/, build/, out/).
func isDocFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown", ".rst", ".txt", ".adoc":
		return true
	}
	for _, d := range strings.Split(filepath.ToSlash(filepath.Dir(p)), "/") {
		if d == "dist" || d == "build" || d == "out" {
			return true
		}
	}
	return false
}

// looksLikePath reports a seed that names a file (dir/file.ext or file.ext).
func looksLikePath(s string) bool {
	return pathRE.MatchString(s) && pathRE.FindString(s) == s || fileNameRE.MatchString(s)
}

var fileNameRE = regexp.MustCompile(`^[\w-]+\.(go|ts|tsx|js|jsx|mjs|cjs|py|rs|java|vue|rb|c|h|cc|cpp|yaml|yml|json|toml|tf)$`)

// pathSymbol shows the start of a file the request names, found by its path
// suffix in the task worktrees (at most two matches).
func pathSymbol(ctx context.Context, in Inputs, name string, st *IntelStats) (string, bool) {
	rg, err := exec.LookPath(rgBinary)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	found := 0
	for _, w := range in.Worktrees {
		st.LexicalCalls++
		cmd := exec.CommandContext(ctx, rg, "--files", "-g", "**/"+name, "-g", "!vendor", "-g", "!node_modules")
		cmd.Dir = w.Path
		out, _ := cmd.Output()
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f = filepath.ToSlash(filepath.Clean(f))
			if f == "." || f == "" || found >= 2 || policy.IsSecretPath(f) || !strings.HasSuffix("/"+f, "/"+name) {
				continue
			}
			if snip := readAround([]task.Worktree{w}, w.RepoName, f, 1, 30); snip != "" {
				fmt.Fprintf(&b, "### %s: file named in the request ./%s/%s\n```\n%s```\n", w.RepoName, w.RepoName, f, snip)
				found++
			}
		}
	}
	return b.String(), found > 0
}
