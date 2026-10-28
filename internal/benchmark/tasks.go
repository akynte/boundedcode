package benchmark

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/xservice"
)

// TaskSpec is one engineering benchmark task (benchmarks/tasks/*.yaml).
type TaskSpec struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	// Language is what the task's code is written in ("go", "python",
	// "typescript", "rust", "java", "multi" for several); reports are
	// summarised per language.
	Language string `yaml:"language"`
	Fixture  string `yaml:"fixture"` // directory under benchmarks/fixtures
	// Sources adds repositories copied from elsewhere, by name. Supported:
	// "gomod:<module>@<version>" copies a module from the Go module cache
	// (large real-world repositories without vendoring them here);
	// "git:<url>@<commit>" checks out a public repository at an exact commit
	// (benchmark datasets such as SWE-bench), without its history;
	// "githistory:<url>@<commit>" keeps the history up to that commit (and
	// nothing after it), for tasks replayed from a repository's own commits.
	Sources  map[string]string `yaml:"sources"`
	Repos    []string          `yaml:"repos"` // repositories the task may change
	Setup    []Edit            `yaml:"setup"` // edits applied and committed before the task (planted defects)
	Request  string            `yaml:"request"`
	Criteria []string          `yaml:"criteria"`
	// Hidden files are written after the agent finishes, then Checks run.
	Hidden []File  `yaml:"hidden"`
	Checks []Check `yaml:"checks"`
	// Requires lists the executables the checks need besides their own
	// first word (a check run through sh -c); a task whose requirements
	// are not installed is skipped in the direct-mode suite.
	Requires []string `yaml:"requires,omitempty"`
	// Expect lists regular expressions (case-insensitive) the agent's final
	// answer must match; for questions in the direct-mode suite, where the
	// answer is the deliverable. A task needs checks or expect, or both.
	Expect []string `yaml:"expect,omitempty"`
	// Turns are follow-up messages in the same session, after Request
	// (direct-mode suite only): each is routed as the chat routes it and
	// its answer must match its own Expect (the task's Expect is the
	// first message's). Checks run after the last turn.
	Turns   []Turn          `yaml:"turns,omitempty"`
	Timeout config.Duration `yaml:"timeout"`
	// InterruptAfter cancels the run after this long, then resumes the same
	// task with a freshly built runner (context-continuity evidence).
	InterruptAfter config.Duration `yaml:"interrupt_after"`
	// Source records where the task comes from (dataset and instance id).
	Source string `yaml:"source"`
	// ReadOnly runs the direct-mode task with the read-only tool set (no
	// edits, only safe commands): for questions about third-party code
	// that must not be built or run.
	ReadOnly bool `yaml:"read_only,omitempty"`
}

// Turn is one follow-up message of a direct-mode session task.
type Turn struct {
	Request string   `yaml:"request"`
	Expect  []string `yaml:"expect,omitempty"`
}

// Edit replaces text in a fixture file (or writes a file when Old is empty).
type Edit struct {
	Repo string `yaml:"repo"`
	File string `yaml:"file"`
	Old  string `yaml:"old"`
	New  string `yaml:"new"`
}

// File is a hidden acceptance file, or a hidden acceptance patch: the files
// a Patch touches are reset to the base commit, then the patch is applied
// (the SWE-bench test_patch convention).
type File struct {
	Repo    string `yaml:"repo"`
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Patch   string `yaml:"patch"`
}

// Check is a command whose exit status decides acceptance.
type Check struct {
	Repo string   `yaml:"repo"`
	Run  []string `yaml:"run"`
}

// LoadTasks reads every *.yaml task in dir, sorted by id.
func LoadTasks(dir string) ([]TaskSpec, error) {
	var out []TaskSpec
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var t TaskSpec
		if err := config.DecodeStrict(b, &t); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if t.ID == "" || t.Request == "" || (len(t.Checks) == 0 && len(t.Expect) == 0) || (t.Fixture == "" && len(t.Sources) == 0) {
			return fmt.Errorf("%s: id, fixture (or sources), request and checks (or expect) are required", p)
		}
		exps := t.Expect
		for i, turn := range t.Turns {
			if strings.TrimSpace(turn.Request) == "" {
				return fmt.Errorf("%s: turn %d has no request", p, i+2)
			}
			exps = append(exps, turn.Expect...)
		}
		for _, e := range exps {
			if _, err := regexp.Compile("(?is)" + e); err != nil {
				return fmt.Errorf("%s: expect %q: %w", p, e, err)
			}
		}
		out = append(out, t)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

// TaskResult is the outcome of one benchmark task.
type TaskResult struct {
	ID              string   `json:"id"`
	Category        string   `json:"category"`
	Model           string   `json:"model"`
	Success         bool     `json:"success"` // hidden acceptance checks passed
	TaskStatus      string   `json:"task_status"`
	SelfVerified    bool     `json:"self_verified"` // task_verified: checks green and a test demonstrates the change
	TestsGreen      bool     `json:"tests_green"`   // all configured checks passed (with or without evidence)
	Attempts        int      `json:"attempts"`
	LocalTokens     int      `json:"local_tokens"` // processed (uncached prompt + generated)
	GeneratedTokens int      `json:"generated_tokens"`
	CachedTokens    int      `json:"cached_prompt_tokens"`
	Escalations     int      `json:"escalations"`
	LocalOnly       bool     `json:"local_only"`
	Condensations   int      `json:"condensations"`
	Resumes         int      `json:"resumes"`
	WallSeconds     float64  `json:"wall_seconds"`
	FailedChecks    []string `json:"failed_checks,omitempty"`
	Error           string   `json:"error,omitempty"`
	TaskID          string   `json:"task_id"`
	Interrupted     bool     `json:"interrupted,omitempty"` // a controlled interruption and resume happened
	// StateDir holds the task's state database and agent persistence.
	StateDir string `json:"state_dir,omitempty"`
	// AgentPatch is the agent's final change against the base commit
	// (git diff, untracked files included), saved before the hidden
	// acceptance files are applied.
	AgentPatch string `json:"agent_patch,omitempty"`
	// Intel summarizes repository intelligence and agent tool use, from the
	// task's audit events.
	Intel IntelMetrics `json:"intel"`

	Language string `json:"language,omitempty"`
	// Calls attributes the model calls (from the task's ledger: the
	// adapter's calls are not streamed, so there is no time to first
	// token; history rewrites by the agent's condenser are not marked).
	Calls CallSummary `json:"calls"`
	// VerifySeconds is the time in verification runs; VerifyRuns each
	// run's scope, outcome and duration.
	VerifySeconds float64     `json:"verify_seconds"`
	VerifyRuns    []VerifyRun `json:"verify_runs,omitempty"`
	// Stalls: the strategy governor stopped an attempt, the agent reported
	// itself stuck, the task was blocked (with its reason).
	Stalls []string `json:"stalls,omitempty"`
	// FailureClass: "" (passed), "model", "verification_false_pass" (self-
	// verified, hidden checks failed), "timeout", "environment", "harness".
	// VerificationMissed marks a pass the gate did not verify.
	FailureClass       string        `json:"failure_class,omitempty"`
	VerificationMissed bool          `json:"verification_missed,omitempty"`
	Resources          ResourcePeaks `json:"resources"`
}

// VerifyRun is one verification run of a task.
type VerifyRun struct {
	Repo    string  `json:"repo"`
	Scope   string  `json:"scope"`
	Passed  bool    `json:"passed"`
	Seconds float64 `json:"seconds"`
}

// IntelMetrics are per-task repository-intelligence measurements.
type IntelMetrics struct {
	Packs          int            `json:"context_packs"`
	PackTokens     int            `json:"pack_tokens"` // all context packs sent
	CodeTokens     int            `json:"code_tokens"` // their RELEVANT CODE sections (source placed into context)
	NavCalls       int            `json:"serena_calls"`
	NavErrors      int            `json:"serena_errors"`
	NavMillis      float64        `json:"serena_ms"`
	GraphCalls     int            `json:"graph_calls"`
	GraphMillis    float64        `json:"graph_ms"`
	NavSymbols     int            `json:"serena_symbols"`
	GraphSymbols   int            `json:"graph_symbols"`
	Fallbacks      int            `json:"fallbacks"`
	AgentToolCalls int            `json:"agent_tool_calls"`
	AgentTools     map[string]int `json:"agent_tools,omitempty"`
}

// SuiteReport aggregates a run.
type SuiteReport struct {
	ID       string       `json:"id"`
	Model    string       `json:"model"`
	Started  time.Time    `json:"started"`
	Finished time.Time    `json:"finished"`
	Results  []TaskResult `json:"results"`
	Summary  Summary      `json:"summary"`
	Notes    []string     `json:"notes,omitempty"`

	Environment Environment `json:"environment"`
}

// Summary holds the product KPIs (docs/product-spec.md §4.11).
type Summary struct {
	Tasks                  int     `json:"tasks"`
	Verified               int     `json:"verified"` // hidden checks passed
	SuccessRate            float64 `json:"success_rate"`
	LocalOnlyRate          float64 `json:"local_only_completion_rate"`
	FrontierEscalationRate float64 `json:"frontier_escalation_rate"`
	VerifiedPerHour        float64 `json:"verified_tasks_per_hour"`
	MeanAttemptsSuccess    float64 `json:"attempts_per_successful_task"`
	MeanWallSeconds        float64 `json:"wall_seconds_per_task"`
	MeanLocalTokens        float64 `json:"local_tokens_per_task"`
	SelfVerifyFalsePass    int     `json:"self_verified_but_hidden_failed"`
}

// SuiteRunner runs tasks with a fresh fixture and isolated state each time.
type SuiteRunner struct {
	FixturesDir string
	WorkRoot    string // scratch root (must be shared with the container engine, e.g. under $HOME)
	Sandbox     sandbox.Sandbox
	// NewRunner builds an orchestrator bound to a fresh state dir.
	NewRunner func(ctx context.Context, stateDir string) (*orchestrator.Runner, func(), error)
	Progress  func(string)
	// Model, ServerBinary and TaskDirs pin the run's environment.
	Model        map[string]any
	ServerBinary string
	TaskDirs     []string
}

// Run executes the given tasks sequentially.
func (s *SuiteRunner) Run(ctx context.Context, model string, tasks []TaskSpec, out string) (*SuiteReport, error) {
	return s.run(ctx, model, tasks, out, s.runOne)
}

// RunBaselines runs the given tasks with RunBaseline.
func (s *SuiteRunner) RunBaselines(ctx context.Context, model string, tasks []TaskSpec, out string) (*SuiteReport, error) {
	return s.run(ctx, model, tasks, out, s.RunBaseline)
}

func (s *SuiteRunner) run(ctx context.Context, model string, tasks []TaskSpec, out string, one func(context.Context, string, TaskSpec) TaskResult) (*SuiteReport, error) {
	rep := &SuiteReport{ID: "suite-" + time.Now().UTC().Format("20060102T150405Z"), Model: model, Started: time.Now().UTC(),
		Environment: captureEnvironment(ctx, s.Model, s.ServerBinary, tasks, s.TaskDirs, s.FixturesDir)}
	save := func() {
		rep.Summary = summarize(rep.Results)
		if out != "" {
			b, _ := json.MarshalIndent(rep, "", "  ")
			_ = config.WriteFileAtomic(out, RedactHome(b), 0o644)
		}
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		s.progress("task %s (%s)", t.ID, t.Category)
		res := one(ctx, model, t)
		s.progress("task %s: success=%v status=%s attempts=%d tokens=%d escalations=%d %.0fs %s",
			t.ID, res.Success, res.TaskStatus, res.Attempts, res.LocalTokens, res.Escalations, res.WallSeconds, res.Error)
		rep.Results = append(rep.Results, res)
		save()
	}
	rep.Finished = time.Now().UTC()
	save()
	return rep, ctx.Err()
}

func (s *SuiteRunner) progress(format string, args ...any) {
	if s.Progress != nil {
		s.Progress(fmt.Sprintf(format, args...))
	}
}

func (s *SuiteRunner) runOne(ctx context.Context, model string, spec TaskSpec) (res TaskResult) {
	res = TaskResult{ID: spec.ID, Category: spec.Category, Language: spec.Language, Model: model}
	start := time.Now()
	stopSampling := sampleResources(ctx, true)
	defer func() {
		res.WallSeconds = time.Since(start).Seconds()
		if res.Resources.Samples == 0 {
			res.Resources = stopSampling()
		}
		res.FailureClass, res.VerificationMissed = boundedFailure(res)
	}()
	dir := filepath.Join(s.WorkRoot, fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()))
	fixture := ""
	if spec.Fixture != "" {
		fixture = filepath.Join(s.FixturesDir, spec.Fixture)
	}
	repos, err := materialize(ctx, fixture, spec.Sources, filepath.Join(dir, "repos"), spec.Setup)
	if err != nil {
		res.Error = "materialize: " + err.Error()
		return res
	}
	r, cleanup, err := s.NewRunner(ctx, filepath.Join(dir, "state"))
	if err != nil {
		res.Error = "runner: " + err.Error()
		return res
	}
	defer func() { cleanup() }()
	w, err := r.WS.Create(ctx, "bench")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for _, name := range spec.Repos {
		repo, err := r.WS.AddRepo(ctx, w, repos[name], name)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		// Index so the agent gets graph and contract context, as in normal use.
		if r.CrossService {
			if eps, _, err := xservice.Scan(name, repo.Path, xservice.ScanOptions{}); err == nil {
				_ = xservice.SaveRepo(ctx, r.DB, w.ID, repo.ID, eps)
			}
		}
		if r.Intel != nil {
			if ir, err := r.Intel.Index(ctx, repo.Path, "bench."+spec.ID+"."+name, "full"); err == nil {
				_ = r.WS.MarkIndexed(ctx, repo.ID, ir.Project)
			} else {
				s.progress("index %s failed: %v", name, err)
			}
		}
	}
	tk, err := r.Create(ctx, w, spec.Request, nil, spec.Criteria)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.TaskID, res.StateDir = tk.ID, filepath.Join(dir, "state")
	tctx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		tctx, cancel = context.WithTimeout(ctx, spec.Timeout.D())
		defer cancel()
	}
	var final *task.Task
	var runErr error
	if spec.InterruptAfter > 0 {
		// Controlled interruption: stop the run, discard the runner (as a
		// process restart would), then resume the same task from the
		// persisted ledger and agent session with a new runner.
		ictx, icancel := context.WithTimeout(tctx, spec.InterruptAfter.D())
		final, runErr = r.Run(ictx, tk.ID, orchestrator.RunOptions{})
		icancel()
		if tctx.Err() == nil && (final == nil || final.Status == task.StatusActive) {
			s.progress("task %s: interrupted after %s (status %v, err %v); resuming with a new runner", spec.ID, spec.InterruptAfter.D(), statusOf(final), runErr)
			res.Interrupted = true
			cleanup()
			r, cleanup, err = s.NewRunner(ctx, filepath.Join(dir, "state"))
			if err != nil {
				res.Error = "runner: " + err.Error()
				return res
			}
			final, runErr = r.Run(tctx, tk.ID, orchestrator.RunOptions{})
		}
	} else {
		final, runErr = r.Run(tctx, tk.ID, orchestrator.RunOptions{})
	}
	if final != nil {
		res.TaskStatus = string(final.Status)
		res.SelfVerified = final.VerificationState == task.VerificationTaskVerified
		res.TestsGreen = final.VerificationState == task.VerificationTestsGreen || res.SelfVerified
		res.Attempts = final.AttemptCount
		res.LocalTokens = final.Budget.UsedLocalTokens
		res.GeneratedTokens, res.CachedTokens = final.Budget.GeneratedTokens, final.Budget.CachedTokens
		res.Escalations = final.Budget.UsedEscalations
		res.Condensations = final.Budget.Condensations
		res.Resumes = final.Budget.SessionsResumed
	}
	res.Resources = stopSampling()
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
		res.Error = runErr.Error()
	}
	if errors.Is(runErr, context.DeadlineExceeded) || tctx.Err() != nil {
		res.Stalls = append(res.Stalls, "timeout")
	}
	collectRun(ctx, r.DB, tk.ID, &res)
	wts, err := r.Ledger.Worktrees(ctx, tk.ID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	byRepo := map[string]task.Worktree{}
	for _, wt := range wts {
		byRepo[wt.RepoName] = wt
	}
	for _, name := range spec.Repos {
		if wt, ok := byRepo[name]; ok {
			res.AgentPatch = saveAgentPatch(ctx, dir, name, wt.Path, wt.BaseCommit)
		}
	}
	// Hidden acceptance: write files, run checks in the sandbox.
	for _, h := range spec.Hidden {
		wt := byRepo[h.Repo]
		if h.Patch != "" {
			if err := applyHiddenPatch(ctx, wt.Path, wt.BaseCommit, h.Patch); err != nil {
				res.Error = "hidden patch: " + err.Error()
				return res
			}
			continue
		}
		p := filepath.Join(wt.Path, h.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(h.Content), 0o644); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	res.Success = true
	for _, c := range spec.Checks {
		wt, ok := byRepo[c.Repo]
		if !ok {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, c.Repo+": repo not in task")
			continue
		}
		cs := checkSpec(ctx, spec, wt.Path, c.Run)
		// Dependencies installed in the materialized repository (ignored by
		// git, so absent from the worktree) are provided as a dataset's
		// evaluation image provides them.
		if err := withDependencies(&cs, repos[c.Repo], wt.Path); err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, err.Error())
			continue
		}
		cmd, err := s.Sandbox.Command(ctx, cs)
		if err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, err.Error())
			continue
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, fmt.Sprintf("%s: %s: %s", c.Repo, strings.Join(c.Run, " "), tailStr(string(out), 600)))
		}
	}
	res.LocalOnly = res.Success && res.Escalations == 0
	res.Intel = collectIntel(ctx, r.DB, tk.ID)
	return res
}

// RedactHome replaces the home directory in a report with "~": reports are
// kept in the repository, and must not carry machine-specific paths. The
// state directory and errors name paths under it.
func RedactHome(b []byte) []byte {
	home, _ := os.UserHomeDir()
	if home == "" || home == "/" {
		return b
	}
	return bytes.ReplaceAll(b, []byte(home), []byte("~"))
}

// collectRun reads a task's model calls, verification runs and stalls
// from its ledger.
func collectRun(ctx context.Context, db *sql.DB, taskID string, res *TaskResult) {
	var log []inference.CallStat
	var sources []string
	if rows, err := db.QueryContext(ctx, `SELECT source, prompt_tokens, cached_tokens, completion_tokens, reasoning_tokens, prompt_ms, decode_ms, total_ms, ttft_ms, status
		FROM model_calls WHERE task_id = ? ORDER BY id`, taskID); err == nil {
		for rows.Next() {
			var c inference.CallStat
			var src string
			if rows.Scan(&src, &c.Prompt, &c.Cached, &c.Completion, &c.Reasoning, &c.PrefillMS, &c.DecodeMS, &c.TotalMS, &c.TTFTMS, &c.Status) == nil {
				if c.Reasoning > 0 {
					c.ReasoningSource = "provider"
				}
				log = append(log, c)
				sources = append(sources, src)
			}
		}
		rows.Close()
	}
	res.Calls = summarizeCalls(log, expectedMisses(sources))
	for _, c := range log {
		res.Calls.PromptTokens += c.Prompt
	}
	if rows, err := db.QueryContext(ctx, `SELECT repository, scope, passed, duration_ms FROM verification_runs WHERE task_id = ? ORDER BY id`, taskID); err == nil {
		for rows.Next() {
			var v VerifyRun
			var ms int64
			if rows.Scan(&v.Repo, &v.Scope, &v.Passed, &ms) == nil {
				v.Seconds = float64(ms) / 1000
				res.VerifySeconds += v.Seconds
				res.VerifyRuns = append(res.VerifyRuns, v)
			}
		}
		rows.Close()
	}
	if rows, err := db.QueryContext(ctx, `SELECT kind, data FROM events WHERE task_id = ? AND kind IN ('strategy.stopped', 'agent.turn', 'task.blocked') ORDER BY id`, taskID); err == nil {
		for rows.Next() {
			var kind, data string
			if rows.Scan(&kind, &data) != nil {
				continue
			}
			var e struct {
				Reason string `json:"reason"`
				Stuck  bool   `json:"stuck"`
			}
			_ = json.Unmarshal([]byte(data), &e)
			switch {
			case kind == "strategy.stopped":
				res.Stalls = append(res.Stalls, "governor: "+tailStr(e.Reason, 80))
			case kind == "agent.turn" && e.Stuck:
				res.Stalls = append(res.Stalls, "agent stuck")
			case kind == "task.blocked":
				res.Stalls = append(res.Stalls, "blocked: "+tailStr(e.Reason, 80))
			}
		}
		rows.Close()
	}
}

// expectedMisses marks the calls whose prompt cannot share the previous
// call's cache: a different caller (the planner, then the agent), the
// history condenser's own call, and the agent's first call after the
// condenser rewrote the history. Invalidations there are expected.
func expectedMisses(sources []string) map[int]bool {
	m := map[int]bool{}
	for i := 1; i < len(sources); i++ {
		prev, cur := sources[i-1], sources[i]
		if cur != prev || strings.HasSuffix(cur, ":condenser") || strings.HasSuffix(prev, ":condenser") {
			m[i] = true
		}
	}
	return m
}

// boundedFailure classifies a bounded task's outcome.
func boundedFailure(r TaskResult) (class string, missed bool) {
	missed = r.Success && !r.SelfVerified
	switch {
	case r.Success:
		return "", missed
	case r.SelfVerified:
		return "verification_false_pass", missed
	case slices.Contains(r.Stalls, "timeout"):
		return "timeout", missed
	case strings.HasPrefix(r.Error, "materialize"), strings.HasPrefix(r.Error, "runner:"), strings.Contains(r.Error, "sandbox"),
		strings.Contains(r.Error, "docker"), strings.Contains(r.Error, "image"):
		return "environment", missed
	case r.Error != "":
		return "harness", missed
	}
	return "model", missed
}

// collectIntel aggregates context-pack and agent-tool events of a task.
func collectIntel(ctx context.Context, db *sql.DB, taskID string) IntelMetrics {
	m := IntelMetrics{AgentTools: map[string]int{}}
	rows, err := db.QueryContext(ctx, `SELECT kind, data FROM events WHERE task_id = ? AND kind IN ('context.pack', 'agent.event')`, taskID)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var kind, data string
		if rows.Scan(&kind, &data) != nil {
			continue
		}
		switch kind {
		case "context.pack":
			var p struct {
				Tokens   int `json:"tokens"`
				Sections []struct {
					Key    string `json:"key"`
					Tokens int    `json:"tokens"`
				} `json:"sections"`
				Intel contextplan.IntelStats `json:"intel"`
			}
			if json.Unmarshal([]byte(data), &p) != nil {
				continue
			}
			m.Packs++
			m.PackTokens += p.Tokens
			for _, sec := range p.Sections {
				if sec.Key == "code" {
					m.CodeTokens += sec.Tokens
				}
			}
			in := p.Intel
			m.NavCalls += in.NavCalls
			m.NavErrors += in.NavErrors
			m.NavMillis += in.NavMillis
			m.GraphCalls += in.GraphCalls
			m.GraphMillis += in.GraphMillis
			m.NavSymbols += in.NavSymbols
			m.GraphSymbols += in.GraphSymbols
			m.Fallbacks += in.Fallbacks
		case "agent.event":
			var e struct {
				Kind string `json:"kind"`
				Tool string `json:"tool"`
			}
			if json.Unmarshal([]byte(data), &e) == nil && e.Tool != "" && e.Kind == "ActionEvent" {
				m.AgentToolCalls++
				m.AgentTools[e.Tool]++
			}
		}
	}
	return m
}

// materialize copies the fixture's repositories (and extra sources) into dst
// as git repos, applies setup edits and commits them as the base.
func materialize(ctx context.Context, fixture string, sources map[string]string, dst string, setup []Edit) (map[string]string, error) {
	var entries []os.DirEntry
	if fixture != "" {
		var err error
		if entries, err = os.ReadDir(fixture); err != nil {
			return nil, err
		}
	}
	repos := map[string]string{}
	for name, src := range sources {
		dir, err := sourceDir(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", name, err)
		}
		target := filepath.Join(dst, name)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return nil, err
		}
		if strings.HasPrefix(src, "githistory:") {
			// A local clone hardlinks the cached objects (git never
			// rewrites an object file) and keeps only the cached history.
			if _, err := gitops.Run(ctx, dst, "clone", "-q", dir, name); err != nil {
				return nil, err
			}
			if _, err := gitops.Run(ctx, target, "remote", "remove", "origin"); err != nil {
				return nil, err
			}
			repos[name] = target
			continue
		}
		// Copied files are writable (module cache files are read-only).
		if err := copyTree(ctx, dir, target); err != nil {
			return nil, err
		}
		_ = os.Remove(filepath.Join(target, ".bc-source-ready"))
		repos[name] = target
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return nil, err
		}
		if err := copyTree(ctx, filepath.Join(fixture, e.Name()), target); err != nil {
			return nil, err
		}
		repos[e.Name()] = target
	}
	for _, ed := range setup {
		p := filepath.Join(repos[ed.Repo], ed.File)
		if ed.Old == "" {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(ed.New), 0o644); err != nil {
				return nil, err
			}
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(b), ed.Old) {
			return nil, fmt.Errorf("setup edit: %q not found in %s", ed.Old, p)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(b), ed.Old, ed.New, 1)), 0o644); err != nil {
			return nil, err
		}
	}
	for _, path := range repos {
		// A repository with history gets an empty base commit when the
		// task plants nothing.
		for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "--allow-empty", "-m", "base"}} {
			if _, err := gitops.Run(ctx, path, a...); err != nil {
				return nil, err
			}
		}
	}
	return repos, nil
}

// checkSpec is the sandbox spec for one hidden check in a checkout.
func checkSpec(ctx context.Context, spec TaskSpec, dir string, run []string) sandbox.Spec {
	mounts := []sandbox.Mount{{Host: dir, Target: dir}}
	if common, err := gitops.CommonDir(ctx, dir); err == nil && common != filepath.Join(dir, ".git") {
		mounts = append(mounts, sandbox.Mount{Host: common, Target: common, ReadOnly: true})
	}
	env := map[string]string{"GOFLAGS": "-buildvcs=false", "GOTOOLCHAIN": "local"}
	// Repositories with dependencies (Sources) build offline from the host
	// module cache, mounted read-only as in verification.
	if mc := goModCache(ctx); mc != "" && len(spec.Sources) > 0 {
		mounts = append(mounts, sandbox.Mount{Host: mc, Target: mc, ReadOnly: true})
		env["GOMODCACHE"], env["GOFLAGS"], env["GOPROXY"] = mc, "-buildvcs=false -mod=mod", "off"
	}
	// The other languages' package caches (Cargo, Maven, Gradle), as the
	// agent and verification get them, with a private writable layer.
	if len(spec.Sources) > 0 {
		if work, err := os.MkdirTemp(filepath.Dir(dir), "acceptance-packages-"); err == nil {
			if pm, err := sandbox.HostPackageCaches().Apply(work, env); err == nil {
				mounts = append(mounts, pm...)
			}
		}
	}
	return sandbox.Spec{Argv: run, Workdir: dir, Mounts: mounts, Env: env}
}

// sourceDir resolves a Sources entry to a local directory.
func sourceDir(ctx context.Context, src string) (string, error) {
	if g, ok := strings.CutPrefix(src, "git:"); ok {
		return gitSourceDir(ctx, g, false)
	}
	if g, ok := strings.CutPrefix(src, "githistory:"); ok {
		return gitSourceDir(ctx, g, true)
	}
	spec, ok := strings.CutPrefix(src, "gomod:")
	if !ok {
		return "", fmt.Errorf("unsupported source %q (want gomod:<module>@<version>, git:<url>@<commit> or githistory:<url>@<commit>)", src)
	}
	mod, ver, ok := strings.Cut(spec, "@")
	if !ok || ver == "" || strings.Contains(ver, "latest") {
		return "", fmt.Errorf("source %q must pin an exact version", src)
	}
	mc := goModCache(ctx)
	if mc == "" {
		return "", errors.New("no Go module cache")
	}
	dir := filepath.Join(mc, escapeModulePath(mod)+"@"+ver)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		if err := goCommand(ctx, "", "mod", "download", mod+"@"+ver); err != nil {
			return "", fmt.Errorf("%s@%s is not in the module cache and could not be downloaded: %w", mod, ver, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			return "", fmt.Errorf("%s@%s was downloaded but %s is missing", mod, ver, dir)
		}
	}
	// The task builds and tests the module as the main module, offline:
	// every module in its build list must be in the cache.
	if err := downloadBuildList(ctx, dir); err != nil {
		return "", fmt.Errorf("the modules %s@%s needs could not be downloaded: %w", mod, ver, err)
	}
	return dir, nil
}

// downloadBuildList fetches every module in the build list of the module
// in dir (go mod download all, as that module): through the configured
// proxy, with checksums verified against the checksum database (Go's
// defaults). It needs no network when they are all cached. The module
// cache is read-only, so its go.mod and go.sum are copied to a scratch
// directory first.
func downloadBuildList(ctx context.Context, dir string) error {
	tmp, err := os.MkdirTemp("", "bc-gomod-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, f := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			if f == "go.sum" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, f), b, 0o644); err != nil {
			return err
		}
	}
	return goCommand(ctx, tmp, "mod", "download", "all")
}

// goCommand runs the go command in dir (a fresh scratch directory when
// "") and returns its output's tail on failure.
func goCommand(ctx context.Context, dir string, args ...string) error {
	if dir == "" {
		tmp, err := os.MkdirTemp("", "bc-go-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}
	c := exec.CommandContext(ctx, "go", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, tailStr(strings.TrimSpace(string(out)), 600))
	}
	return nil
}

// escapeModulePath applies the module cache's case encoding ("A" -> "!a").
func escapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func goModCache(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func summarize(rs []TaskResult) Summary {
	s := Summary{Tasks: len(rs)}
	var wall, tokens, attempts float64
	esc := 0
	for _, r := range rs {
		wall += r.WallSeconds
		tokens += float64(r.LocalTokens)
		if r.Escalations > 0 {
			esc++
		}
		if r.Success {
			s.Verified++
			attempts += float64(r.Attempts)
			if r.LocalOnly {
				s.LocalOnlyRate++
			}
		}
		if r.SelfVerified && !r.Success {
			s.SelfVerifyFalsePass++
		}
	}
	if s.Tasks > 0 {
		n := float64(s.Tasks)
		s.SuccessRate = float64(s.Verified) / n
		s.LocalOnlyRate /= n
		s.FrontierEscalationRate = float64(esc) / n
		s.MeanWallSeconds = wall / n
		s.MeanLocalTokens = tokens / n
		if wall > 0 {
			s.VerifiedPerHour = float64(s.Verified) / (wall / 3600)
		}
	}
	if s.Verified > 0 {
		s.MeanAttemptsSuccess = attempts / float64(s.Verified)
	}
	return s
}

func tailStr(s string, n int) string {
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// SourcesDir caches "git:" sources (under $HOME so the container engine can
// see them). Dependencies a dataset's evaluation environment provides (Go
// modules, node_modules) are installed there once by the preparation script.
// BC_BENCH_SOURCES overrides it (an evaluation with its own sources).
func SourcesDir() string {
	if d := os.Getenv("BC_BENCH_SOURCES"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "boundedcode", "bench-sources")
}

// gitSourceDir checks out url at an exact commit into the sources cache,
// without .git, so the task repository has no upstream history (and no
// future commits that could contain the fix). With history, the commit's
// ancestors are fetched and kept on a branch "main"; nothing newer is.
func gitSourceDir(ctx context.Context, spec string, history bool) (string, error) {
	i := strings.LastIndex(spec, "@")
	if i < 0 {
		return "", fmt.Errorf("git source %q: want <url>@<commit>", spec)
	}
	url, commit := spec[:i], spec[i+1:]
	if len(commit) != 40 || strings.Trim(commit, "0123456789abcdef") != "" {
		return "", fmt.Errorf("git source %q must pin a full commit hash", spec)
	}
	name := strings.NewReplacer("https://github.com/", "", "/", "__", ".git", "").Replace(url)
	dir := filepath.Join(SourcesDir(), name+"@"+commit[:12])
	ready := filepath.Join(dir, ".bc-source-ready")
	steps := [][]string{{"init", "-q"}, {"fetch", "-q", "--depth", "1", url, commit}, {"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"}}
	if history {
		dir += "-history"
		ready = filepath.Join(dir, ".git", "bc-source-ready")
		steps = [][]string{{"init", "-q", "-b", "main"}, {"fetch", "-q", "--no-tags", url, commit}, {"checkout", "-q", "-B", "main", "FETCH_HEAD"}}
	}
	if _, err := os.Stat(ready); err == nil {
		return dir, nil
	}
	tmp := dir + ".partial"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	for _, a := range steps {
		if _, err := gitops.Run(ctx, tmp, a...); err != nil {
			return "", err
		}
	}
	if head, err := gitops.Run(ctx, tmp, "rev-parse", "HEAD"); err != nil || head != commit {
		return "", fmt.Errorf("git source %s: checked out %q", spec, head)
	}
	if history {
		ready = filepath.Join(tmp, ".git", "bc-source-ready")
	} else {
		if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
			return "", err
		}
		ready = filepath.Join(tmp, ".bc-source-ready")
	}
	if err := os.WriteFile(ready, []byte(spec+"\n"), 0o644); err != nil {
		return "", err
	}
	_ = os.RemoveAll(dir)
	return dir, os.Rename(tmp, dir)
}

// applyHiddenPatch resets the files a patch touches to the base commit and
// applies it, so the acceptance tests are exactly the dataset's, whatever
// the agent did to those files.
// saveAgentPatch writes the agent's change in worktree against base to
// dir/agent-<repo>.patch and returns its path ("" when it cannot).
func saveAgentPatch(ctx context.Context, dir, repo, worktree, base string) string {
	d, err := gitops.Diff(ctx, worktree, base, false)
	if err != nil {
		return ""
	}
	p := filepath.Join(dir, "agent-"+repo+".patch")
	if d != "" && !strings.HasSuffix(d, "\n") {
		d += "\n" // Diff trims it; git apply needs it
	}
	if err := os.WriteFile(p, []byte(d), 0o644); err != nil {
		return ""
	}
	return p
}

func applyHiddenPatch(ctx context.Context, dir, base, patch string) error {
	f, err := os.CreateTemp("", "bc-hidden-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(patch); err != nil {
		return err
	}
	f.Close()
	for _, p := range patchFiles(patch) {
		if _, err := gitops.Run(ctx, dir, "cat-file", "-e", base+":"+p); err == nil {
			if _, err := gitops.Run(ctx, dir, "checkout", base, "--", p); err != nil {
				return err
			}
		} else if err := os.Remove(filepath.Join(dir, p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	_, err = gitops.Run(ctx, dir, "apply", "--whitespace=nowarn", f.Name())
	return err
}

// patchFiles lists the paths a unified git diff touches.
func patchFiles(patch string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range strings.Split(patch, "\n") {
		rest, ok := strings.CutPrefix(l, "diff --git a/")
		if !ok {
			continue
		}
		a, b, ok := strings.Cut(rest, " b/")
		if !ok {
			continue
		}
		for _, p := range []string{a, b} {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// withDependencies adds the materialized repository's installed
// dependencies to an acceptance-check spec, exactly as BoundedCode's
// verification gets them (read-only, with writable tool caches).
func withDependencies(cs *sandbox.Spec, repo, worktree string) error {
	deps, err := sandbox.DependencyMounts(repo, worktree)
	if err != nil {
		return fmt.Errorf("acceptance dependencies: %w", err)
	}
	cs.Mounts = append(cs.Mounts, deps.Mounts...)
	cs.Scratch = append(cs.Scratch, deps.Scratch...)
	return nil
}

func statusOf(t *task.Task) string {
	if t == nil {
		return "none"
	}
	return string(t.Status)
}

// copyTree copies a directory tree. os.CopyFS makes the copies writable and
// keeps execute bits; a tree with symlinks (which it does not copy) falls
// back to cp -r on Unix.
func copyTree(ctx context.Context, src, dst string) error {
	err := os.CopyFS(dst, os.DirFS(src))
	if err == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	_ = os.RemoveAll(dst)
	if out, cerr := exec.CommandContext(ctx, "cp", "-r", src, dst).CombinedOutput(); cerr != nil {
		return fmt.Errorf("cp: %s", out)
	}
	if out, cerr := exec.CommandContext(ctx, "chmod", "-R", "u+w", dst).CombinedOutput(); cerr != nil {
		return fmt.Errorf("chmod: %s", out)
	}
	return nil
}
