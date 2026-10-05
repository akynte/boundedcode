package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/verify"
)

// The strategy governor stops an attempt that keeps generating without
// progress. In the 2026-10 runs an attempt could spend an hour and ~100K
// generated tokens re-reading the same files and re-trying near-identical
// edits; a per-response thinking budget does not bound that, since every
// response stays under it. The governor is deterministic: it watches the
// agent's tool events and the gateway's generated-token count, and never
// asks a model whether the model is stuck.
//
// Progress is evidence the attempt is moving towards a verifiable change:
//   - its first edit to the repository,
//   - creating a test file (a reproduction),
//   - an agent-run test passing after a test failed in this attempt.
// Reading new files is useful but is not progress: an attempt can read
// forever.

// governorTick is how often the governor checks the attempt's budgets.
var governorTick = 5 * time.Second

// errNoProgress is the turn's cancellation cause when the governor stops it.
type errNoProgress struct{ reason string }

func (e errNoProgress) Error() string { return "strategy stopped: " + e.reason }

var (
	exitCodeRE = regexp.MustCompile(`exit code (\d+)`)
	testCmdRE  = regexp.MustCompile(`\b(go test|npm (run )?test|npx (vitest|jest|mocha)|vitest|jest|mocha|pytest|cargo test|yarn test|pnpm (run )?test)\b`)
)

type governor struct {
	cfg   config.StrategyBudget
	start time.Time

	mu            sync.Mutex
	genStart      int // gateway generated tokens when the attempt started
	genAtProgress int
	edits         int
	testFailed    bool
	lastCmd       string
	progress      []string
	inspected     map[string]int
	commands      int
}

func newGovernor(cfg config.StrategyBudget, generated int, now time.Time) *governor {
	return &governor{cfg: cfg, start: now, genStart: generated, genAtProgress: generated, inspected: map[string]int{}}
}

// observe updates the governor from one agent event. It is called from the
// runtime's event callback, so the generated-token count of the moment is
// passed in by the caller.
func (g *governor) observe(e agent.Event, generated int) {
	if e.Kind != "ActionEvent" && e.Kind != "ObservationEvent" {
		return
	}
	var raw struct {
		Action string `json:"action"`
		Text   string `json:"text"`
	}
	_ = json.Unmarshal(e.Raw, &raw)
	g.mu.Lock()
	defer g.mu.Unlock()
	switch e.Kind {
	case "ActionEvent":
		var act struct {
			Command string `json:"command"`
			Path    string `json:"path"`
		}
		_ = json.Unmarshal([]byte(raw.Action), &act)
		switch e.Tool {
		case "file_editor":
			if act.Command == "view" {
				g.inspected[act.Path]++
				return
			}
			if act.Command == "create" || act.Command == "str_replace" || act.Command == "insert" {
				if g.edits == 0 {
					g.mark(generated, "first edit")
				}
				g.edits++
				if act.Command == "create" && verify.IsTestFile(act.Path) {
					g.mark(generated, "created test file "+act.Path)
				}
			}
		case "terminal":
			g.commands++
			g.lastCmd = act.Command
		}
	case "ObservationEvent":
		if e.Tool != "terminal" || !testCmdRE.MatchString(g.lastCmd) {
			return
		}
		m := exitCodeRE.FindAllStringSubmatch(raw.Text, -1)
		if len(m) == 0 {
			return
		}
		passed := m[len(m)-1][1] == "0"
		if !passed {
			g.testFailed = true
		} else if g.testFailed {
			g.testFailed = false
			g.mark(generated, "a failing test now passes")
		}
	}
}

func (g *governor) mark(generated int, what string) {
	g.genAtProgress = generated
	g.progress = append(g.progress, what)
}

// check reports whether the attempt must stop, and why.
func (g *governor) check(generated int, now time.Time) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case g.cfg.NoProgressTokens > 0 && generated-g.genAtProgress >= g.cfg.NoProgressTokens:
		return true, fmt.Sprintf("%d tokens generated since the last progress (%s)", generated-g.genAtProgress, g.lastProgressLocked())
	case g.cfg.MaxTokens > 0 && generated-g.genStart >= g.cfg.MaxTokens:
		return true, fmt.Sprintf("the attempt generated %d tokens", generated-g.genStart)
	case g.cfg.MaxDuration > 0 && now.Sub(g.start) >= g.cfg.MaxDuration.D():
		return true, fmt.Sprintf("the attempt ran %s", now.Sub(g.start).Round(time.Minute))
	}
	return false, ""
}

func (g *governor) lastProgressLocked() string {
	if len(g.progress) == 0 {
		return "none yet"
	}
	return "last: " + g.progress[len(g.progress)-1]
}

// summary describes the stopped strategy for the ledger and the next attempt.
func (g *governor) summary() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	type kv struct {
		path string
		n    int
	}
	var files []kv
	for p, n := range g.inspected {
		files = append(files, kv{p, n})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].n > files[j].n || files[i].n == files[j].n && files[i].path < files[j].path
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d edits, %d shell commands, %d files read", g.edits, g.commands, len(files))
	if len(files) > 0 {
		b.WriteString("; most re-read: ")
		for i, f := range files[:min(5, len(files))] {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s (%d×)", relWork(f.path), f.n)
		}
	}
	if len(g.progress) > 0 {
		b.WriteString("; progress: " + strings.Join(g.progress, ", "))
	}
	return b.String()
}

// relWork shortens a worktree path to its repository-relative part.
func relWork(p string) string {
	if i := strings.Index(p, "/work/"); i >= 0 {
		return p[i+len("/work/"):]
	}
	return p
}
