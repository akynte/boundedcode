package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/xservice"
)

// TaskSpec is one engineering benchmark task (benchmarks/tasks/*.yaml).
type TaskSpec struct {
	ID       string   `yaml:"id"`
	Category string   `yaml:"category"`
	Fixture  string   `yaml:"fixture"` // directory under benchmarks/fixtures
	Repos    []string `yaml:"repos"`   // repositories the task may change
	Setup    []Edit   `yaml:"setup"`   // edits applied and committed before the task (planted defects)
	Request  string   `yaml:"request"`
	Criteria []string `yaml:"criteria"`
	// Hidden files are written after the agent finishes, then Checks run.
	Hidden  []File          `yaml:"hidden"`
	Checks  []Check         `yaml:"checks"`
	Timeout config.Duration `yaml:"timeout"`
}

// Edit replaces text in a fixture file (or writes a file when Old is empty).
type Edit struct {
	Repo string `yaml:"repo"`
	File string `yaml:"file"`
	Old  string `yaml:"old"`
	New  string `yaml:"new"`
}

// File is a hidden acceptance file.
type File struct {
	Repo    string `yaml:"repo"`
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
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
		if t.ID == "" || t.Request == "" || len(t.Checks) == 0 || t.Fixture == "" {
			return fmt.Errorf("%s: id, fixture, request and checks are required", p)
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
	SelfVerified    bool     `json:"self_verified"` // our verification gate passed
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
}

// Run executes the given tasks sequentially.
func (s *SuiteRunner) Run(ctx context.Context, model string, tasks []TaskSpec, out string) (*SuiteReport, error) {
	rep := &SuiteReport{ID: "suite-" + time.Now().UTC().Format("20060102T150405Z"), Model: model, Started: time.Now().UTC()}
	save := func() {
		rep.Summary = summarize(rep.Results)
		if out != "" {
			b, _ := json.MarshalIndent(rep, "", "  ")
			_ = config.WriteFileAtomic(out, b, 0o644)
		}
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		s.progress("task %s (%s)", t.ID, t.Category)
		res := s.runOne(ctx, model, t)
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
	res = TaskResult{ID: spec.ID, Category: spec.Category, Model: model}
	start := time.Now()
	defer func() { res.WallSeconds = time.Since(start).Seconds() }()
	dir := filepath.Join(s.WorkRoot, fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()))
	repos, err := materialize(ctx, filepath.Join(s.FixturesDir, spec.Fixture), filepath.Join(dir, "repos"), spec.Setup)
	if err != nil {
		res.Error = "materialize: " + err.Error()
		return res
	}
	r, cleanup, err := s.NewRunner(ctx, filepath.Join(dir, "state"))
	if err != nil {
		res.Error = "runner: " + err.Error()
		return res
	}
	defer cleanup()
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
	res.TaskID = tk.ID
	tctx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		tctx, cancel = context.WithTimeout(ctx, spec.Timeout.D())
		defer cancel()
	}
	final, runErr := r.Run(tctx, tk.ID, orchestrator.RunOptions{})
	if final != nil {
		res.TaskStatus = string(final.Status)
		res.SelfVerified = final.VerificationState == "full_pass"
		res.Attempts = final.AttemptCount
		res.LocalTokens = final.Budget.UsedLocalTokens
		res.GeneratedTokens, res.CachedTokens = final.Budget.GeneratedTokens, final.Budget.CachedTokens
		res.Escalations = final.Budget.UsedEscalations
		res.Condensations = final.Budget.Condensations
		res.Resumes = final.Budget.SessionsResumed
	}
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
		res.Error = runErr.Error()
	}
	wts, err := r.Ledger.Worktrees(ctx, tk.ID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	byRepo := map[string]task.Worktree{}
	for _, wt := range wts {
		byRepo[wt.RepoName] = wt
	}
	// Hidden acceptance: write files, run checks in the sandbox.
	for _, h := range spec.Hidden {
		wt := byRepo[h.Repo]
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
		mounts := []sandbox.Mount{{Host: wt.Path, Target: wt.Path}}
		if common, err := gitops.CommonDir(ctx, wt.Path); err == nil {
			mounts = append(mounts, sandbox.Mount{Host: common, Target: common, ReadOnly: true})
		}
		cmd, err := s.Sandbox.Command(ctx, sandbox.Spec{Argv: c.Run, Workdir: wt.Path, Mounts: mounts,
			Env: map[string]string{"GOFLAGS": "-buildvcs=false", "GOTOOLCHAIN": "local"}})
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
	return res
}

// materialize copies the fixture's repositories into dst as git repos,
// applies setup edits and commits them as the base.
func materialize(ctx context.Context, fixture, dst string, setup []Edit) (map[string]string, error) {
	entries, err := os.ReadDir(fixture)
	if err != nil {
		return nil, err
	}
	repos := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return nil, err
		}
		if out, err := exec.CommandContext(ctx, "cp", "-r", filepath.Join(fixture, e.Name()), target).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cp: %s", out)
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
		for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-qm", "base"}} {
			if _, err := gitops.Run(ctx, path, a...); err != nil {
				return nil, err
			}
		}
	}
	return repos, nil
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
