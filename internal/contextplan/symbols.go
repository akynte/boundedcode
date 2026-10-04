package contextplan

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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
}

// Limits keep symbol context compact: the point of LSP navigation is to send
// precise pieces instead of whole files.
const (
	maxSymbolsShown   = 6
	maxMatchesPerName = 3
	maxBodyLines      = 80
	maxReferences     = 12
	maxImplementation = 10
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
func requestSymbols(ctx context.Context, in Inputs, st *IntelStats) string {
	if in.Intel == nil && in.Nav == nil {
		return ""
	}
	var b strings.Builder
	shown := 0
	for _, n := range identifiers(in.Task.OriginalRequest + "\n" + in.Task.Goal) {
		if shown >= maxSymbolsShown {
			break
		}
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
	}
	return b.String()
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
