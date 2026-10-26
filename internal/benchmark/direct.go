package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/gitops"
)

// The direct-mode suite measures the everyday agent: each task is one
// message to a direct runner in a fresh copy of a fixture, on the host (as
// direct mode runs), judged by hidden checks (commands in the checkout) or
// by what the answer must say (expect). Besides pass or fail it records
// what the measurement plan asks for: model calls, tool calls by name,
// prompt tokens and how many the server served from its cache, tokens
// generated, results set aside and recalled, summaries, checks that caught
// a broken edit, and wall time.

// DirectRunner runs the direct-mode suite.
type DirectRunner struct {
	FixturesDir string
	WorkRoot    string
	// NewRunner builds a direct runner for the checkout at root, with its
	// state (sessions, checkpoints) under stateDir.
	NewRunner func(ctx context.Context, root, stateDir string) (*direct.Runner, func(), error)
	// Rules are the router's risk rules; every request is routed as the
	// chat would route it, with no bounded mode available.
	Rules    direct.RiskRules
	Progress func(string)
	// Timeout bounds one task when the task sets none (0 = 20 minutes).
	Timeout time.Duration
	// Settings are recorded in the report (working set, caps, approval).
	Settings map[string]any
}

// DirectResult is the outcome of one direct-mode task.
type DirectResult struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Model    string `json:"model"`
	Success  bool   `json:"success"`
	// Mode is the router's decision for the request (chat or code) and
	// Routed why; Switched is set when the turn changed mode.
	Mode     string `json:"mode"`
	Routed   string `json:"routed"`
	Switched string `json:"switched,omitempty"`
	// Turns is the number of messages sent (the request and its
	// follow-ups); TurnModes and TurnAnswers are per message, kept for
	// session tasks only.
	Turns       int      `json:"turns"`
	TurnModes   []string `json:"turn_modes,omitempty"`
	TurnAnswers []string `json:"turn_answers,omitempty"`
	// Calls is the number of model calls; ToolCalls the tool calls, by
	// name in Tools; Denied the calls refused by policy or approval.
	Calls     int            `json:"model_calls"`
	ToolCalls int            `json:"tool_calls"`
	Tools     map[string]int `json:"tools,omitempty"`
	Denied    int            `json:"denied,omitempty"`
	// Token accounting from the gateway: PromptTokens includes CachedTokens.
	PromptTokens    int `json:"prompt_tokens"`
	CachedTokens    int `json:"cached_prompt_tokens"`
	GeneratedTokens int `json:"generated_tokens"`
	// PeakPrompt is the largest prompt of one model call: the most history
	// the model was given at once.
	PeakPrompt int `json:"peak_prompt_tokens"`
	// ResultBytes is what tool results added to the history before
	// anything was set aside.
	ResultBytes int `json:"tool_result_bytes"`
	// Elided counts tool results set aside for the working set, Recalls
	// the recall calls, Compactions the summaries, ChecksCaught the file
	// changes the post-change check reported as broken.
	Elided       int `json:"elided"`
	Recalls      int `json:"recalls"`
	Compactions  int `json:"compactions"`
	ChecksCaught int `json:"checks_caught"`
	// Nudged: the model described a plan without acting and was asked to
	// continue.
	Nudged       bool     `json:"nudged,omitempty"`
	WallSeconds  float64  `json:"wall_seconds"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Answer       string   `json:"answer,omitempty"`
	FailedChecks []string `json:"failed_checks,omitempty"`
	Error        string   `json:"error,omitempty"`
	// AgentPatch is the agent's change against the base commit (capped).
	AgentPatch string `json:"agent_patch,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
}

// maxPatchBytes caps the patch kept in a report.
const maxPatchBytes = 24 << 10

// DirectReport aggregates a run of the direct-mode suite.
type DirectReport struct {
	ID       string          `json:"id"`
	Model    string          `json:"model"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished"`
	Results  []DirectResult  `json:"results"`
	Summary  DirectSummary   `json:"summary"`
	Settings map[string]any  `json:"settings,omitempty"`
	ByClass  []DirectSummary `json:"by_category,omitempty"`
}

// DirectSummary holds the suite's figures (over all tasks, or one category).
type DirectSummary struct {
	Category        string  `json:"category,omitempty"`
	Tasks           int     `json:"tasks"`
	Passed          int     `json:"passed"`
	PassRate        float64 `json:"pass_rate"`
	MeanCalls       float64 `json:"model_calls_per_task"`
	MeanToolCalls   float64 `json:"tool_calls_per_task"`
	MeanPrompt      float64 `json:"prompt_tokens_per_task"`
	CacheHitRate    float64 `json:"prompt_cache_hit_rate"` // cached / prompt, over the run
	MeanGenerated   float64 `json:"generated_tokens_per_task"`
	MeanResultBytes float64 `json:"tool_result_bytes_per_task"`
	MeanWallSeconds float64 `json:"wall_seconds_per_task"`
	PeakPrompt      int     `json:"peak_prompt_tokens"` // the largest prompt of one call in the run
	Elided          int     `json:"elided"`
	Recalls         int     `json:"recalls"`
	Compactions     int     `json:"compactions"`
	ChecksCaught    int     `json:"checks_caught"`
	Errors          int     `json:"errors"`
}

func (d *DirectRunner) progress(format string, args ...any) {
	if d.Progress != nil {
		d.Progress(fmt.Sprintf(format, args...))
	}
}

// Run executes the tasks sequentially, saving the report after each one.
func (d *DirectRunner) Run(ctx context.Context, model string, tasks []TaskSpec, out string) (*DirectReport, error) {
	rep := &DirectReport{ID: "direct-" + time.Now().UTC().Format("20060102T150405Z"), Model: model, Started: time.Now().UTC(), Settings: d.Settings}
	save := func() error {
		rep.Finished = time.Now().UTC()
		rep.Summary = summarizeDirect("", rep.Results)
		rep.ByClass = nil
		seen := map[string]bool{}
		for _, r := range rep.Results {
			if !seen[r.Category] {
				seen[r.Category] = true
				rep.ByClass = append(rep.ByClass, summarizeDirect(r.Category, rep.Results))
			}
		}
		if out == "" {
			return nil
		}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		return config.WriteFileAtomic(out, b, 0o644)
	}
	for i, t := range tasks {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		d.progress("task %d/%d %s", i+1, len(tasks), t.ID)
		res := d.runOne(ctx, model, t)
		verdict := "FAIL"
		if res.Success {
			verdict = "pass"
		}
		d.progress("task %s: %s in %.0fs, %d model calls, %d tool calls, %d prompt tokens (%d cached), %d generated%s",
			t.ID, verdict, res.WallSeconds, res.Calls, res.ToolCalls, res.PromptTokens, res.CachedTokens, res.GeneratedTokens, errSuffix(res))
		rep.Results = append(rep.Results, res)
		if err := save(); err != nil {
			return rep, err
		}
	}
	return rep, save()
}

func errSuffix(r DirectResult) string {
	switch {
	case r.Error != "":
		return "; error: " + r.Error
	case len(r.FailedChecks) > 0:
		return "; failed: " + strings.Join(r.FailedChecks, " | ")
	}
	return ""
}

func (d *DirectRunner) runOne(ctx context.Context, model string, spec TaskSpec) (res DirectResult) {
	res = DirectResult{ID: spec.ID, Category: spec.Category, Model: model, Tools: map[string]int{}}
	start := time.Now()
	defer func() { res.WallSeconds = time.Since(start).Seconds() }()
	if len(spec.Repos) != 1 {
		res.Error = "the direct-mode suite needs exactly one repo per task"
		return res
	}
	dir := filepath.Join(d.WorkRoot, fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()))
	fixture := ""
	if spec.Fixture != "" {
		fixture = filepath.Join(d.FixturesDir, spec.Fixture)
	}
	repos, err := materialize(ctx, fixture, spec.Sources, filepath.Join(dir, "repos"), spec.Setup)
	if err != nil {
		res.Error = "materialize: " + err.Error()
		return res
	}
	root, ok := repos[spec.Repos[0]]
	if !ok {
		res.Error = "repo " + spec.Repos[0] + " is not in the fixture"
		return res
	}
	base, err := gitops.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	base = strings.TrimSpace(base)
	r, cleanup, err := d.NewRunner(ctx, root, filepath.Join(dir, "state"))
	if err != nil {
		res.Error = "runner: " + err.Error()
		return res
	}
	defer cleanup()
	if r.Session != nil {
		res.SessionID = r.Session.ID
	}
	r.Opts.AllowRisky = true // the suite measures the everyday loop; hand-offs are counted, not run
	inner := r.Sink
	r.Sink = func(e direct.Event) {
		if inner != nil {
			inner(e)
		}
		switch e.Kind {
		case direct.EventToolStart:
			res.ToolCalls++
			res.Tools[e.Tool]++
			if e.Tool == "recall" {
				res.Recalls++
			}
		case direct.EventToolEnd:
			res.ResultBytes += len(e.Output)
			if strings.Contains(e.Output, "Fix this before going on") {
				res.ChecksCaught++
			}
		case direct.EventDenied:
			res.Denied++
		case direct.EventCompact:
			switch {
			case strings.HasPrefix(e.Text, "Set aside"):
				var n int
				if _, err := fmt.Sscanf(e.Text, "Set aside %d", &n); err == nil {
					res.Elided += n
				}
			case strings.HasPrefix(e.Text, "Summarised"):
				res.Compactions++
			}
		case direct.EventMode:
			res.Switched = e.Text
		case direct.EventNote:
			if strings.Contains(e.Text, "described a plan without acting") {
				res.Nudged = true
			}
		}
	}
	timeout := d.Timeout
	if spec.Timeout > 0 {
		timeout = spec.Timeout.D()
	}
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// The request, then each follow-up turn, in one session; each message
	// is routed as the chat would route it, with bounded mode unavailable.
	turns := append([]Turn{{Request: spec.Request, Expect: spec.Expect}}, spec.Turns...)
	res.Turns = len(turns)
	var answer string
	var sendErr error
	var turnFailures []string
	for i, t := range turns {
		dec := direct.Route(t.Request, d.Rules, direct.Capabilities{BoundedReason: "benchmark: direct mode only"})
		if i == 0 {
			res.Mode, res.Routed = dec.Mode, dec.Reason
			if dec.Fallback != "" {
				res.Routed += " (" + dec.Fallback + ")"
			}
		}
		if len(turns) > 1 {
			res.TurnModes = append(res.TurnModes, dec.Mode)
		}
		r.Opts.Mode = dec.Mode
		if len(turns) > 1 {
			d.progress("task %s: turn %d/%d (%s)", spec.ID, i+1, len(turns), dec.Mode)
		}
		answer, sendErr = r.Send(tctx, t.Request)
		if len(turns) > 1 {
			res.TurnAnswers = append(res.TurnAnswers, tailStr(answer, 1500))
		}
		for _, e := range t.Expect {
			if !regexp.MustCompile("(?is)" + e).MatchString(answer) {
				label := ""
				if len(turns) > 1 {
					label = fmt.Sprintf("turn %d: ", i+1)
				}
				turnFailures = append(turnFailures, label+"answer does not match /"+e+"/")
			}
		}
		if sendErr != nil {
			if i < len(turns)-1 {
				sendErr = fmt.Errorf("turn %d of %d: %w", i+1, len(turns), sendErr)
			}
			break
		}
	}
	res.Answer = tailStr(answer, 4000)
	p, g, c := r.Gateway.Stats()
	res.Calls = r.Gateway.Calls()
	res.GeneratedTokens, res.CachedTokens = g, c
	res.PromptTokens = p - g + c
	res.PeakPrompt = r.Gateway.PeakPrompt()
	var handoff *direct.Handoff
	switch {
	case errors.As(sendErr, &handoff):
		res.Error = "handed off to verification: " + handoff.Reason
	case errors.Is(sendErr, context.DeadlineExceeded) || (sendErr != nil && tctx.Err() != nil):
		res.Error = fmt.Sprintf("timed out after %s", timeout)
	case sendErr != nil:
		res.Error = sendErr.Error()
	}
	if p := saveAgentPatch(ctx, dir, spec.Repos[0], root, base); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			res.AgentPatch = string(b)
			if len(res.AgentPatch) > maxPatchBytes {
				res.AgentPatch = res.AgentPatch[:maxPatchBytes] + "\n… (patch cut)\n"
			}
		}
	}
	if st, err := gitops.Run(ctx, root, "status", "--porcelain", "--untracked-files=all"); err == nil {
		for _, l := range strings.Split(strings.TrimRight(st, "\n"), "\n") {
			if len(l) > 3 {
				res.FilesChanged = append(res.FilesChanged, strings.TrimSpace(l[3:]))
			}
		}
	}
	// Hidden acceptance: files, then checks on the host, then the answer.
	for _, h := range spec.Hidden {
		if h.Patch != "" {
			if err := applyHiddenPatch(ctx, root, base, h.Patch); err != nil {
				res.Error = "hidden patch: " + err.Error()
				return res
			}
			continue
		}
		p := filepath.Join(root, h.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(h.Content), 0o644); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	res.Success = res.Error == ""
	for _, c := range spec.Checks {
		if c.Repo != spec.Repos[0] {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, c.Repo+": repo not in task")
			continue
		}
		if out, err := hostCheck(ctx, spec, root, c.Run); err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, fmt.Sprintf("%s: %s", strings.Join(c.Run, " "), tailStr(out, 600)))
		}
	}
	if len(turnFailures) > 0 {
		res.Success = false
		res.FailedChecks = append(res.FailedChecks, turnFailures...)
	}
	// Reports are kept in the repository: no machine-specific paths.
	redact := newRedactor(dir, d.WorkRoot)
	res.Answer = redact(res.Answer)
	res.Error = redact(res.Error)
	res.AgentPatch = redact(res.AgentPatch)
	for i := range res.FailedChecks {
		res.FailedChecks[i] = redact(res.FailedChecks[i])
	}
	for i := range res.TurnAnswers {
		res.TurnAnswers[i] = redact(res.TurnAnswers[i])
	}
	return res
}

// newRedactor replaces the task's scratch directory, the work root and the
// home directory in report text.
func newRedactor(dir, work string) func(string) string {
	home, _ := os.UserHomeDir()
	pairs := [][2]string{{dir, "<task>"}, {work, "<work>"}, {home, "~"}}
	return func(s string) string {
		for _, p := range pairs {
			if p[0] != "" && p[0] != "/" {
				s = strings.ReplaceAll(s, p[0], p[1])
			}
		}
		return s
	}
}

// hostCheck runs one acceptance command in the checkout, as direct mode's
// own commands run: on the host, with the Go build settings verification
// uses.
func hostCheck(ctx context.Context, spec TaskSpec, dir string, run []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	c := exec.CommandContext(cctx, run[0], run[1:]...)
	c.Dir = dir
	env := append(os.Environ(), "GOFLAGS=-buildvcs=false", "GOTOOLCHAIN=local", "GIT_TERMINAL_PROMPT=0")
	if mc := goModCache(ctx); mc != "" && len(spec.Sources) > 0 {
		env = append(env, "GOMODCACHE="+mc, "GOFLAGS=-buildvcs=false -mod=mod", "GOPROXY=off")
	}
	c.Env = env
	out, err := c.CombinedOutput()
	return string(out), err
}

func summarizeDirect(category string, rs []DirectResult) DirectSummary {
	s := DirectSummary{Category: category}
	var prompt, cached int
	var calls, tools, gen, bytes int
	var wall float64
	for _, r := range rs {
		if category != "" && r.Category != category {
			continue
		}
		s.Tasks++
		if r.Success {
			s.Passed++
		}
		if r.Error != "" {
			s.Errors++
		}
		calls += r.Calls
		tools += r.ToolCalls
		prompt += r.PromptTokens
		cached += r.CachedTokens
		gen += r.GeneratedTokens
		bytes += r.ResultBytes
		wall += r.WallSeconds
		s.Elided += r.Elided
		s.Recalls += r.Recalls
		s.Compactions += r.Compactions
		s.ChecksCaught += r.ChecksCaught
		s.PeakPrompt = max(s.PeakPrompt, r.PeakPrompt)
	}
	if s.Tasks == 0 {
		return s
	}
	n := float64(s.Tasks)
	s.PassRate = float64(s.Passed) / n
	s.MeanCalls = float64(calls) / n
	s.MeanToolCalls = float64(tools) / n
	s.MeanPrompt = float64(prompt) / n
	s.MeanGenerated = float64(gen) / n
	s.MeanResultBytes = float64(bytes) / n
	s.MeanWallSeconds = wall / n
	if prompt > 0 {
		s.CacheHitRate = float64(cached) / float64(prompt)
	}
	return s
}

// WriteDirectMarkdown renders a direct-mode report.
func WriteDirectMarkdown(w io.Writer, r *DirectReport) error {
	s := r.Summary
	fmt.Fprintf(w, "# Direct-mode suite: %s\n\nModel: %s. Started %s, finished %s.\n\n", r.ID, r.Model, r.Started.Format(time.RFC3339), r.Finished.Format(time.RFC3339))
	if len(r.Settings) > 0 {
		keys := make([]string, 0, len(r.Settings))
		for k := range r.Settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, r.Settings[k]))
		}
		fmt.Fprintf(w, "Settings: %s.\n\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(w, "**%d/%d passed (%.0f%%)**, %.1f model calls and %.1f tool calls per task, %.0f prompt tokens per task with a %.0f%% prompt-cache hit rate, %.0f generated tokens per task, %.0f KB of tool results per task, %.0f s per task, %d tokens in the largest single prompt; %d results set aside, %d recalled, %d summaries, %d broken edits caught by the check, %d errors.\n\n",
		s.Passed, s.Tasks, 100*s.PassRate, s.MeanCalls, s.MeanToolCalls, s.MeanPrompt, 100*s.CacheHitRate, s.MeanGenerated, s.MeanResultBytes/1024, s.MeanWallSeconds, s.PeakPrompt, s.Elided, s.Recalls, s.Compactions, s.ChecksCaught, s.Errors)
	if len(r.ByClass) > 1 {
		fmt.Fprintf(w, "| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |\n|---|---|---|---|---|---|---|---|\n")
		for _, c := range r.ByClass {
			fmt.Fprintf(w, "| %s | %d/%d | %.1f | %.1f | %.0f | %.0f%% | %.0f | %.0f |\n", c.Category, c.Passed, c.Tasks, c.MeanCalls, c.MeanToolCalls, c.MeanPrompt, 100*c.CacheHitRate, c.MeanGenerated, c.MeanWallSeconds)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range r.Results {
		verdict := "pass"
		if !t.Success {
			verdict = "FAIL"
		}
		note := ""
		switch {
		case t.Error != "":
			note = t.Error
		case len(t.FailedChecks) > 0:
			note = strings.Join(t.FailedChecks, "; ")
		}
		if t.Switched != "" {
			note = strings.TrimSpace("switched to " + t.Switched + ". " + note)
		}
		if t.Nudged {
			note = strings.TrimSpace("nudged. " + note)
		}
		mode := t.Mode
		if len(t.TurnModes) > 0 {
			mode = strings.Join(t.TurnModes, ", ")
		}
		note = strings.ReplaceAll(tailStr(note, 160), "|", "\\|")
		note = strings.ReplaceAll(note, "\n", " ")
		fmt.Fprintf(w, "| %s | %s | %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %.0f | %s |\n",
			t.ID, t.Category, mode, verdict, t.Calls, t.ToolCalls, t.PromptTokens, t.CachedTokens, t.PeakPrompt, t.GeneratedTokens, t.Elided, t.Recalls, t.ChecksCaught, t.WallSeconds, note)
	}
	return nil
}

// LoadDirectReport reads a saved direct-mode report.
func LoadDirectReport(path string) (*DirectReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r DirectReport
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// CompareDirect renders two runs side by side: the summary figures and,
// per task, what changed. Tasks present in only one run are listed.
func CompareDirect(w io.Writer, a, b *DirectReport) {
	fmt.Fprintf(w, "# %s vs %s\n\n", a.ID, b.ID)
	fmt.Fprintf(w, "| | %s | %s |\n|---|---|---|\n", a.ID, b.ID)
	row := func(name string, fa, fb float64, format string) {
		fmt.Fprintf(w, "| %s | "+format+" | "+format+" |\n", name, fa, fb)
	}
	sa, sb := a.Summary, b.Summary
	row("passed", float64(sa.Passed), float64(sb.Passed), "%.0f")
	row("pass rate", 100*sa.PassRate, 100*sb.PassRate, "%.0f%%")
	row("model calls per task", sa.MeanCalls, sb.MeanCalls, "%.1f")
	row("tool calls per task", sa.MeanToolCalls, sb.MeanToolCalls, "%.1f")
	row("prompt tokens per task", sa.MeanPrompt, sb.MeanPrompt, "%.0f")
	row("prompt-cache hit rate", 100*sa.CacheHitRate, 100*sb.CacheHitRate, "%.0f%%")
	row("generated tokens per task", sa.MeanGenerated, sb.MeanGenerated, "%.0f")
	row("tool-result KB per task", sa.MeanResultBytes/1024, sb.MeanResultBytes/1024, "%.1f")
	row("seconds per task", sa.MeanWallSeconds, sb.MeanWallSeconds, "%.0f")
	row("largest single prompt", float64(sa.PeakPrompt), float64(sb.PeakPrompt), "%.0f")
	row("results set aside", float64(sa.Elided), float64(sb.Elided), "%.0f")
	row("recalls", float64(sa.Recalls), float64(sb.Recalls), "%.0f")
	row("summaries", float64(sa.Compactions), float64(sb.Compactions), "%.0f")
	row("checks caught", float64(sa.ChecksCaught), float64(sb.ChecksCaught), "%.0f")
	fmt.Fprintln(w)
	byID := map[string]DirectResult{}
	for _, r := range b.Results {
		byID[r.ID] = r
	}
	fmt.Fprintf(w, "| Task | Result | Calls | Tools | Prompt tok | Gen tok | s |\n|---|---|---|---|---|---|---|\n")
	seen := map[string]bool{}
	for _, ra := range a.Results {
		seen[ra.ID] = true
		rb, ok := byID[ra.ID]
		if !ok {
			fmt.Fprintf(w, "| %s | only in %s | | | | | |\n", ra.ID, a.ID)
			continue
		}
		fmt.Fprintf(w, "| %s | %s → %s | %d → %d | %d → %d | %d → %d | %d → %d | %.0f → %.0f |\n",
			ra.ID, verdict(ra), verdict(rb), ra.Calls, rb.Calls, ra.ToolCalls, rb.ToolCalls, ra.PromptTokens, rb.PromptTokens, ra.GeneratedTokens, rb.GeneratedTokens, ra.WallSeconds, rb.WallSeconds)
	}
	for _, rb := range b.Results {
		if !seen[rb.ID] {
			fmt.Fprintf(w, "| %s | only in %s | | | | | |\n", rb.ID, b.ID)
		}
	}
}

func verdict(r DirectResult) string {
	if r.Success {
		return "pass"
	}
	return "FAIL"
}
