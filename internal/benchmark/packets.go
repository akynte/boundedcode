package benchmark

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

// PacketMeasure is one recorded run's escalation packet in one data mode.
type PacketMeasure struct {
	Task   string `json:"task"`
	Report string `json:"report"`
	Mode   string `json:"mode"`
	Tokens int    `json:"tokens"`
	Lines  int    `json:"lines"` // non-empty
	// SourceLines are packet lines that repeat a line of the repositories
	// (as BoundedCode compares them: 16 characters or more).
	SourceLines int `json:"source_lines"`
	// Identifiers are names from the repositories' source (4 characters
	// or more, not in the request) that the packet mentions; Paths the
	// repository files it names.
	Identifiers int  `json:"identifiers"`
	Paths       int  `json:"paths"`
	Gate        bool `json:"passes_abstract_gate"`
}

// PacketReport is the bench's.
type PacketReport struct {
	Started  time.Time       `json:"started"`
	Measures []PacketMeasure `json:"measures"`
	Skipped  []string        `json:"skipped,omitempty"`
}

var identRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{3,}`)

// MeasurePackets rebuilds, for the latest recorded run of each task, the
// escalation packet the runner would build after its last attempt (the
// recorded task, strategies and verification results, and the agent's
// patch applied to its repositories) in each data mode, and measures what
// of the repositories it carries. The pack has no code section: the
// recorded runs' code index is not kept, so full packets here understate
// what a full packet carries.
func MeasurePackets(ctx context.Context, runs []EscalationRun, workRoot string, budget int) (*PacketReport, error) {
	rep := &PacketReport{Started: time.Now().UTC()}
	latest := map[string]EscalationRun{}
	for _, r := range runs {
		latest[r.Task] = r // runs are in report order: the last wins
	}
	var names []string
	for n := range latest {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		run := latest[name]
		ms, err := measureRun(ctx, run, workRoot, budget)
		if err != nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s (%s): %v", run.Task, run.Report, err))
			continue
		}
		rep.Measures = append(rep.Measures, ms...)
	}
	return rep, nil
}

func measureRun(ctx context.Context, run EscalationRun, workRoot string, budget int) ([]PacketMeasure, error) {
	benchDir := filepath.Dir(filepath.Dir(filepath.Dir(run.Ledger)))
	tmp, err := os.MkdirTemp(workRoot, "packets-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	db, err := sql.Open("sqlite", "file:"+run.Ledger+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	l := task.Ledger{DB: db}
	t, err := l.Get(ctx, run.TaskID)
	if err != nil {
		return nil, err
	}
	strategies, _ := l.Strategies(ctx, t.ID)
	results, _ := verify.LoadRuns(ctx, db, t.ID, 16)
	repos, _ := filepath.Glob(filepath.Join(benchDir, "repos", "*"))
	var wts []task.Worktree
	var roots []string
	for _, repo := range repos {
		name := filepath.Base(repo)
		dst := filepath.Join(tmp, name)
		if out, err := exec.CommandContext(ctx, "git", "clone", "-q", "--local", repo, dst).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("clone %s: %w %s", name, err, out)
		}
		base, err := gitops.Run(ctx, dst, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		if patch := filepath.Join(benchDir, "agent-"+name+".patch"); fileHasContent(patch) {
			if out, err := exec.CommandContext(ctx, "git", "-C", dst, "apply", "--whitespace=nowarn", "--exclude=*.pyc", patch).CombinedOutput(); err != nil {
				return nil, fmt.Errorf("apply the agent's patch to %s: %w %s", name, err, out)
			}
		}
		wts = append(wts, task.Worktree{RepoName: name, RepoPath: repo, Path: dst, BaseCommit: strings.TrimSpace(base)})
		roots = append(roots, dst)
	}
	pack, err := contextplan.Build(ctx, contextplan.Inputs{Task: t, Worktrees: wts, WorkDir: tmp, Strategies: strategies, Verification: results,
		Mode: contextplan.ModeRetry, BudgetTokens: budget, MaxAttempts: t.Budget.MaxAttempts, ChangedFiles: t.ChangedFiles})
	if err != nil {
		return nil, err
	}
	src, err := frontier.CollectSourceLines(ctx, roots, 2<<20)
	if err != nil {
		return nil, err
	}
	idents, files := sourceNames(roots)
	paths := frontier.PathMap{tmp: "."}
	for _, w := range wts {
		paths[w.Path] = w.RepoName
		paths[w.RepoPath] = w.RepoName
	}
	home, _ := os.UserHomeDir()
	tr := frontier.Trigger{Code: frontier.Z2, Reason: "replayed after the run's last attempt"}
	q := frontier.DefaultQuestion(tr)
	request := strings.ToLower(t.OriginalRequest + " " + t.Goal + " " + strings.Join(t.AcceptanceCriteria, " "))
	var out []PacketMeasure
	for _, mode := range []string{frontier.DataFull, frontier.DataAbstract} {
		p := frontier.BuildPacket(tr, pack, q, paths, home)
		if mode == frontier.DataAbstract {
			p = frontier.BuildAbstractPacket(tr, pack, q, paths, home, src)
		}
		m := PacketMeasure{Task: run.Task, Report: run.Report, Mode: mode, Tokens: contextplan.EstimateTokens(p), Gate: frontier.CheckAbstract(p, src) == nil}
		seen := map[string]bool{}
		for _, line := range strings.Split(p, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			m.Lines++
			if frontier.CheckAbstract(line, src) != nil {
				m.SourceLines++
			}
			for _, id := range identRE.FindAllString(line, -1) {
				if idents[id] && !seen[id] && !strings.Contains(request, strings.ToLower(id)) {
					seen[id] = true
					m.Identifiers++
				}
			}
		}
		for f := range files {
			if strings.Contains(p, f) {
				m.Paths++
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func fileHasContent(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

// sourceNames are the identifiers in the repositories' Go, Python,
// TypeScript, Rust, Java and SQL files (keywords included; they are
// common to every program and rarely absent from a request), and the
// repository-relative file paths with a directory.
func sourceNames(roots []string) (map[string]bool, map[string]bool) {
	ids, files := map[string]bool{}, map[string]bool{}
	exts := map[string]bool{".go": true, ".py": true, ".ts": true, ".js": true, ".rs": true, ".java": true, ".sql": true, ".proto": true}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // unreadable entries are skipped
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "vendor", "target":
					return filepath.SkipDir
				}
				return nil
			}
			if !exts[filepath.Ext(p)] {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Contains(rel, string(filepath.Separator)) {
				files[filepath.ToSlash(rel)] = true
			}
			b, err := os.ReadFile(p)
			if err != nil || len(b) > 2<<20 {
				return nil //nolint:nilerr // unreadable files are skipped
			}
			for _, id := range identRE.FindAllString(string(b), -1) {
				ids[id] = true
			}
			return nil
		})
	}
	return ids, files
}

// WritePacketMarkdown writes the per-mode summary and the per-task rows.
func WritePacketMarkdown(w io.Writer, r *PacketReport) error {
	fmt.Fprintf(w, "# Escalation packets: full and abstract\n\nStarted %s. One packet per task, rebuilt from its latest recorded run after its last attempt.\n\n", r.Started.Format(time.RFC3339))
	fmt.Fprintln(w, "| Data | Packets | Tokens per packet | Lines repeating the source | Packets with any | Source identifiers per packet | Repository paths per packet | Pass the abstract gate |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|")
	for _, mode := range []string{frontier.DataFull, frontier.DataAbstract} {
		var n, tok, src, any, ids, paths, gate int
		for _, m := range r.Measures {
			if m.Mode != mode {
				continue
			}
			n++
			tok += m.Tokens
			src += m.SourceLines
			ids += m.Identifiers
			paths += m.Paths
			if m.SourceLines > 0 {
				any++
			}
			if m.Gate {
				gate++
			}
		}
		if n == 0 {
			continue
		}
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %.1f | %.1f | %d / %d |\n", mode, n, tok/n, src, any, float64(ids)/float64(n), float64(paths)/float64(n), gate, n)
	}
	fmt.Fprintln(w, "\n| Task | Data | Tokens | Lines | Repeating the source | Identifiers | Paths |\n|---|---|---|---|---|---|---|")
	for _, m := range r.Measures {
		fmt.Fprintf(w, "| %s | %s | %d | %d | %d | %d | %d |\n", m.Task, m.Mode, m.Tokens, m.Lines, m.SourceLines, m.Identifiers, m.Paths)
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "\nSkipped: %s\n", s)
	}
	return nil
}
