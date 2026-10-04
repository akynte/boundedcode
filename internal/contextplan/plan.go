// Package contextplan assembles the working context sent to the agent from
// authoritative sources: the task ledger, Git, verification results and the
// code graph. It never relies on conversation memory, so a task can be
// resumed after any crash, restart or condensation with an equivalent pack.
// Packs are deterministic for a given state and fit a token budget.
package contextplan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/xservice"
)

// Mode says why the pack is built.
type Mode string

// Modes.
const (
	ModeInitial Mode = "initial" // first message of a task
	ModeRetry   Mode = "retry"   // after a failed verification, same session
	ModeResume  Mode = "resume"  // new session or after context loss: full re-hydration
)

// Inputs are the authoritative sources.
type Inputs struct {
	Task         *task.Task
	Worktrees    []task.Worktree
	WorkDir      string // agent-visible root containing one directory per repo
	Strategies   []task.Strategy
	Verification []verify.Result // latest results, any order
	Intel        repointel.Intelligence
	Mode         Mode
	BudgetTokens int
	MaxAttempts  int
	// Advice is frontier guidance to apply (Z1-Z4), if any.
	Advice string
	// Contracts are cross-service links (HTTP, topics, env) involving the
	// task's repositories; nil disables the section.
	Contracts []xservice.Link
	// ChangedFiles are repo-qualified paths changed so far ("repo/file").
	ChangedFiles []string
}

// Section is one block of the pack.
type Section struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Body      string `json:"-"`
	Tokens    int    `json:"tokens"`
	Truncated bool   `json:"truncated"`
}

// Pack is the assembled context.
type Pack struct {
	Mode     Mode      `json:"mode"`
	Sections []Section `json:"sections"`
	Tokens   int       `json:"tokens"`
	Budget   int       `json:"budget"`
	Dropped  []string  `json:"dropped,omitempty"`
}

// EstimateTokens is a conservative chars-per-token estimate for code-heavy
// text (measured ~3.3 chars/token on Qwen3.6 for Go code in Phase 1).
func EstimateTokens(s string) int { return (len(s)*10 + 31) / 32 }

// shares cap each section as a fraction of the budget (sum may exceed 1;
// sections are filled in priority order until the budget is spent).
var plan = []struct {
	key   string
	share float64
}{
	{"task", 0.10},
	{"advice", 0.15},
	{"verification", 0.25},
	{"rejected", 0.06},
	{"diff", 0.25},
	{"impact", 0.10},
	{"contracts", 0.10},
	{"code", 0.30},
	{"adr", 0.08},
	{"rules", 0.05},
}

// Build assembles a pack.
func Build(ctx context.Context, in Inputs) (Pack, error) {
	if in.BudgetTokens <= 0 {
		in.BudgetTokens = 24000
	}
	gen := map[string]func() (string, string){
		"task":         func() (string, string) { return "TASK", taskSection(in) },
		"advice":       func() (string, string) { return "FRONTIER GUIDANCE (apply it; verify locally)", in.Advice },
		"verification": func() (string, string) { return "LATEST VERIFICATION FAILURES", verificationSection(in.Verification) },
		"rejected": func() (string, string) {
			return "STRATEGIES ALREADY TRIED (do not repeat)", rejectedSection(in.Strategies)
		},
		"diff":      func() (string, string) { return "CURRENT CHANGES", diffSection(ctx, in) },
		"impact":    func() (string, string) { return "IMPACT OF CURRENT CHANGES", impactSection(ctx, in) },
		"contracts": func() (string, string) { return "CROSS-SERVICE CONTRACTS", contractsSection(in) },
		"code":      func() (string, string) { return "RELEVANT CODE", codeSection(ctx, in) },
		"adr":       func() (string, string) { return "ARCHITECTURE DECISIONS", adrSection(in) },
		"rules":     func() (string, string) { return "RULES", rulesSection() },
	}
	p := Pack{Mode: in.Mode, Budget: in.BudgetTokens}
	remaining := in.BudgetTokens
	for _, s := range plan {
		title, body := gen[s.key]()
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		limit := min(int(float64(in.BudgetTokens)*s.share), remaining)
		if limit < 50 {
			p.Dropped = append(p.Dropped, s.key)
			continue
		}
		sec := Section{Key: s.key, Title: title, Body: body}
		if EstimateTokens(body) > limit {
			sec.Body = truncateToTokens(body, limit)
			sec.Truncated = true
		}
		sec.Tokens = EstimateTokens(sec.Body)
		remaining -= sec.Tokens
		p.Tokens += sec.Tokens
		p.Sections = append(p.Sections, sec)
	}
	return p, nil
}

// Render formats the pack as the agent message.
func (p Pack) Render() string {
	var b strings.Builder
	switch p.Mode {
	case ModeResume:
		b.WriteString("You are resuming a task after a context reset. The state below was rebuilt from the task ledger, git and verification results; trust it over memory.\n\n")
	case ModeRetry:
		b.WriteString("Verification failed. Fix the problems below, then stop.\n\n")
	}
	for _, s := range p.Sections {
		fmt.Fprintf(&b, "## %s\n%s\n", s.Title, s.Body)
		if s.Truncated {
			b.WriteString("[section truncated to fit the context budget]\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func taskSection(in Inputs) string {
	t := in.Task
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (attempt %d of %d, phase %s)\n\nRequest:\n%s\n", t.ID, t.AttemptCount, in.MaxAttempts, t.Phase, t.OriginalRequest)
	if t.Goal != "" && t.Goal != t.OriginalRequest {
		fmt.Fprintf(&b, "\nGoal: %s\n", t.Goal)
	}
	if len(t.AcceptanceCriteria) > 0 {
		b.WriteString("\nAcceptance criteria:\n")
		for _, c := range t.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	if len(t.CompletedSteps) > 0 {
		fmt.Fprintf(&b, "\nCompleted steps: %s\n", strings.Join(t.CompletedSteps, "; "))
	}
	if len(t.RemainingSteps) > 0 {
		fmt.Fprintf(&b, "Remaining steps: %s\n", strings.Join(t.RemainingSteps, "; "))
	}
	if len(t.Decisions) > 0 {
		b.WriteString("\nDecisions so far:\n")
		for _, d := range lastN(t.Decisions, 8) {
			fmt.Fprintf(&b, "- [%s] %s\n", d.Source, d.Text)
		}
	}
	b.WriteString("\nRepositories (each is a git worktree on the task branch):\n")
	for _, w := range in.Worktrees {
		rel, _ := filepath.Rel(in.WorkDir, w.Path)
		fmt.Fprintf(&b, "- %s: ./%s (branch %s)\n", w.RepoName, rel, w.Branch)
	}
	return b.String()
}

func lastN[T any](xs []T, n int) []T {
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

func verificationSection(results []verify.Result) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, r := range results {
		for _, f := range r.Failures() {
			key := r.Repository + "/" + f.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(&b, "### %s: %s failed (`%s`, exit %d)\n```\n%s\n```\n", r.Repository, f.Name, f.Command, f.ExitCode, strings.TrimSpace(f.Output))
		}
	}
	return b.String()
}

func rejectedSection(st []task.Strategy) string {
	var b strings.Builder
	for _, s := range st {
		if s.Outcome != "rejected" {
			continue
		}
		fmt.Fprintf(&b, "- attempt %d: %s\n  rejected because: %s\n", s.Attempt, oneLine(s.Summary, 300), oneLine(s.Reason, 300))
	}
	return b.String()
}

func diffSection(ctx context.Context, in Inputs) string {
	var b strings.Builder
	for _, w := range in.Worktrees {
		stat, err := gitops.Diff(ctx, w.Path, w.BaseCommit, true)
		if err != nil || strings.TrimSpace(stat) == "" {
			continue
		}
		patch, _ := gitops.Diff(ctx, w.Path, w.BaseCommit, false)
		fmt.Fprintf(&b, "### %s\n```\n%s\n```\n```diff\n%s\n```\n", w.RepoName, strings.TrimSpace(stat), strings.TrimSpace(patch))
	}
	if b.Len() == 0 && in.Mode != ModeInitial {
		return "No changes yet."
	}
	return b.String()
}

func impactSection(ctx context.Context, in Inputs) string {
	if in.Intel == nil {
		return ""
	}
	var b strings.Builder
	for _, w := range in.Worktrees {
		if w.IndexProject == "" {
			continue
		}
		if files, err := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit); err != nil || len(files) == 0 {
			continue
		}
		out, err := in.Intel.Impact(ctx, w.IndexProject, "", 2)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		fmt.Fprintf(&b, "### %s\n%s\n", w.RepoName, out)
	}
	return b.String()
}

var (
	identRE    = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.]*)`|\\b([A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+|[a-z]+(?:[A-Z][a-z0-9]+)+)\\b")
	fileLineRE = regexp.MustCompile(`([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|py|rs|java)):(\d+)`)
)

// codeSection prefers exact sources: lines referenced by failures, then
// symbols named in the request, looked up in the graph.
func codeSection(ctx context.Context, in Inputs) string {
	var b strings.Builder
	// 1. file:line references from verification output, read from disk.
	refs := map[string]bool{}
	for _, r := range in.Verification {
		for _, f := range r.Failures() {
			for _, m := range fileLineRE.FindAllStringSubmatch(f.Output, -1) {
				key := r.Repository + "|" + m[1] + "|" + m[2]
				if refs[key] || len(refs) >= 6 {
					continue
				}
				refs[key] = true
				if snip := readAround(in.Worktrees, r.Repository, m[1], atoi(m[2]), 12); snip != "" {
					fmt.Fprintf(&b, "### %s %s:%s\n```\n%s\n```\n", r.Repository, m[1], m[2], snip)
				}
			}
		}
	}
	// 2. Symbols named in the request.
	if in.Intel != nil {
		names := identifiers(in.Task.OriginalRequest + "\n" + in.Task.Goal)
		shown := 0
		for _, n := range names {
			if shown >= 6 {
				break
			}
			for _, w := range in.Worktrees {
				if w.IndexProject == "" {
					continue
				}
				snip, err := in.Intel.Snippet(ctx, w.IndexProject, n)
				if err != nil || strings.TrimSpace(snip) == "" || strings.Contains(snip, "not found") {
					continue
				}
				fmt.Fprintf(&b, "### %s: %s\n%s\n", w.RepoName, n, strings.TrimSpace(snip))
				if callers, err := in.Intel.Trace(ctx, w.IndexProject, n, "inbound", 1); err == nil {
					fmt.Fprintf(&b, "callers:\n%s\n", strings.TrimSpace(callers))
				}
				shown++
				break
			}
		}
	}
	return b.String()
}

func identifiers(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range identRE.FindAllStringSubmatch(s, -1) {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func readAround(wts []task.Worktree, repo, file string, line, radius int) string {
	for _, w := range wts {
		if w.RepoName != repo {
			continue
		}
		p := filepath.Join(w.Path, filepath.Clean("/" + file)[1:])
		b, err := os.ReadFile(p)
		if err != nil {
			// go test output paths are relative to the package dir; search.
			matches, _ := filepath.Glob(filepath.Join(w.Path, "*", filepath.Base(file)))
			matches2, _ := filepath.Glob(filepath.Join(w.Path, "*", "*", filepath.Base(file)))
			matches = append(matches, matches2...)
			if len(matches) != 1 {
				return ""
			}
			if b, err = os.ReadFile(matches[0]); err != nil {
				return ""
			}
		}
		lines := strings.Split(string(b), "\n")
		lo, hi := max(0, line-1-radius), min(len(lines), line+radius)
		var out strings.Builder
		for i := lo; i < hi; i++ {
			mark := "  "
			if i == line-1 {
				mark = "> "
			}
			fmt.Fprintf(&out, "%s%4d %s\n", mark, i+1, lines[i])
		}
		return out.String()
	}
	return ""
}

func adrSection(in Inputs) string {
	words := map[string]bool{}
	for w := range strings.FieldsSeq(strings.ToLower(in.Task.OriginalRequest)) {
		w = strings.Trim(w, ".,:;()\"'`")
		if len(w) > 4 {
			words[w] = true
		}
	}
	type adr struct {
		path  string
		score int
		text  string
	}
	var found []adr
	for _, w := range in.Worktrees {
		for _, dir := range []string{"docs/adr", "docs/architecture/adr", "adr", "docs/decisions"} {
			files, _ := filepath.Glob(filepath.Join(w.Path, dir, "*.md"))
			for _, f := range files {
				b, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				low := strings.ToLower(string(b))
				score := 0
				for wd := range words {
					if strings.Contains(low, wd) {
						score++
					}
				}
				if score > 0 {
					rel, _ := filepath.Rel(in.WorkDir, f)
					found = append(found, adr{rel, score, string(b)})
				}
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].path < found[j].path
	})
	var b strings.Builder
	for i, a := range found {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&b, "### %s\n%s\n", a.path, truncateToTokens(a.text, 600))
	}
	return b.String()
}

func rulesSection() string {
	return strings.TrimSpace(`
- Work only inside the repositories listed above. Do not read .env files, secrets, keys or credentials.
- Never push, force-push, reset, or merge protected branches. Committing is handled by the control plane.
- There is no network access. Use only tools and dependencies already available.
- Make the smallest change that satisfies the acceptance criteria; keep the existing style.
- Run the relevant tests yourself before finishing. A deterministic verification pipeline will check your work.
- When done, call finish with a short summary of what you changed and why.`)
}

func truncateToTokens(s string, tokens int) string {
	maxChars := tokens * 32 / 10
	if len(s) <= maxChars {
		return s
	}
	head := maxChars * 2 / 3
	tailN := max(maxChars-head-40, 0)
	return s[:head] + "\n…[" + strconv.Itoa(len(s)-head-tailN) + " chars omitted]…\n" + s[len(s)-tailN:]
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// contractsSection lists cross-service links: first those touching files the
// task changed (with the counterpart that may need updating), then the other
// contracts of the task's repositories.
func contractsSection(in Inputs) string {
	if len(in.Contracts) == 0 {
		return ""
	}
	touched := xservice.Touching(in.Contracts, in.ChangedFiles)
	isTouched := map[string]bool{}
	var b strings.Builder
	if len(touched) > 0 {
		b.WriteString("Your changes touch these contracts. Check that the other side still works, and update it if it is in your workspace:\n")
		for _, l := range touched {
			isTouched[linkID(l)] = true
			fmt.Fprintf(&b, "- %s\n", formatLink(l))
		}
		b.WriteString("\n")
	}
	n := 0
	for _, l := range in.Contracts {
		if isTouched[linkID(l)] {
			continue
		}
		if n == 0 {
			b.WriteString("Contracts between services in this workspace (producer/caller -> consumer/handler):\n")
		}
		if n >= 40 {
			fmt.Fprintf(&b, "- … %d more\n", len(in.Contracts)-len(touched)-n)
			break
		}
		fmt.Fprintf(&b, "- %s\n", formatLink(l))
		n++
	}
	return b.String()
}

func linkID(l xservice.Link) string { return l.Kind + "|" + l.From.Where() + "|" + l.To.Where() }

func formatLink(l xservice.Link) string {
	side := func(e xservice.Endpoint) string {
		s := fmt.Sprintf("%s ./%s/%s:%d", e.Kind, e.Repo, e.File, e.Line)
		if e.Symbol != "" {
			s += " (" + e.Symbol + ")"
		}
		return s
	}
	return fmt.Sprintf("[%s] %s: %s -> %s", l.Kind, l.Contract, side(l.From), side(l.To))
}
