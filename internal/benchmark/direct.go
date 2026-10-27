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
	"github.com/akynte/boundedcode/internal/inference"
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
	// Tokenize counts tokens with the model's own tokenizer (llama-server's
	// /tokenize); nil for a provider without one, and counts are then
	// labelled estimates.
	Tokenize func(context.Context, string) (int, error)
	// Model, ServerBinary, TaskDirs and FixturesDir pin the run's
	// environment in the report (captureEnvironment).
	Model        map[string]any
	ServerBinary string
	TaskDirs     []string
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
	// CheckNotes counts notes about code that compiles but is probably
	// wrong (a pointer to a range loop's copy).
	CheckNotes int `json:"check_notes,omitempty"`
	// GuardRefusals counts sensitive operations the guard refused (no one
	// is asked in a benchmark); on ordinary tasks it should stay 0.
	GuardRefusals int `json:"guard_refusals,omitempty"`
	// Sandbox is the containment commands ran with; NetRefused counts
	// connections its proxy refused.
	Sandbox    string `json:"sandbox,omitempty"`
	NetRefused int    `json:"net_refused,omitempty"`
	// HistoryNotes counts notes naming files the repository's history
	// changes together with a changed file.
	HistoryNotes int `json:"history_notes,omitempty"`
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

	// Language is the task's language (from the task definition).
	Language string `json:"language,omitempty"`
	// Skipped says why the task was not run (a toolchain its checks need
	// is not installed); a skipped task is not counted as run.
	Skipped string `json:"skipped,omitempty"`
	// FailureClass separates why a task failed: "model" (it ran and the
	// result is wrong), "timeout", "interrupted" (the run was stopped),
	// "routing" (handed off to verification),
	// "environment" (the model server, a toolchain, the fixture),
	// "harness" (BoundedCode itself).
	FailureClass string `json:"failure_class,omitempty"`
	// Stalls: "max_turns" (stopped at direct.max_turns), "repeated_failure"
	// (the same call failed three times), "nudged", "empty_answer" (asked
	// for an answer after a turn ended without one), "timeout".
	Stalls []string `json:"stalls,omitempty"`
	// Where the agent's time went: AgentSeconds is the turns themselves
	// (no fixture set-up or hidden checks); Model is the calls' sum
	// (Calls.InferenceSeconds), ToolSeconds the tools' (with the checks
	// after edits), HarnessSeconds the rest: routing-independent work of
	// the loop such as the project map, the seed pack and the working set
	// (PhaseSeconds breaks those down).
	AgentSeconds   float64              `json:"agent_seconds"`
	ToolSeconds    float64              `json:"tool_seconds"`
	HarnessSeconds float64              `json:"harness_seconds"`
	PhaseSeconds   map[string]float64   `json:"phase_seconds,omitempty"`
	ToolTimes      map[string]ToolTime  `json:"tool_times,omitempty"`
	CallStats      CallSummary          `json:"call_stats"`
	CallLog        []inference.CallStat `json:"call_log,omitempty"`
	// Context is the largest request of the task, by section.
	Context   *ContextBreakdown `json:"context,omitempty"`
	Resources ResourcePeaks     `json:"resources"`
}

// ToolTime is one tool's calls in a task.
type ToolTime struct {
	Calls       int     `json:"calls"`
	Seconds     float64 `json:"seconds"`
	MaxSeconds  float64 `json:"max_seconds"`
	ResultBytes int     `json:"result_bytes"`
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
	// ByLanguage summarises the tasks per language.
	ByLanguage  []DirectSummary `json:"by_language,omitempty"`
	Environment Environment     `json:"environment"`
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
	CheckNotes      int     `json:"check_notes"`
	GuardRefusals   int     `json:"guard_refusals,omitempty"`
	HistoryNotes    int     `json:"history_notes"`
	Errors          int     `json:"errors"`

	Language string `json:"language,omitempty"`
	Skipped  int    `json:"skipped,omitempty"` // not run (environment); not in Tasks
	// Means per task of the time attribution and the call figures.
	MeanAgentSeconds     float64 `json:"agent_seconds_per_task"`
	MeanInferenceSeconds float64 `json:"inference_seconds_per_task"`
	MeanToolSeconds      float64 `json:"tool_seconds_per_task"`
	MeanHarnessSeconds   float64 `json:"harness_seconds_per_task"`
	MeanReasoning        float64 `json:"reasoning_tokens_per_task"`
	// TTFTMedianMS is the median over tasks of each task's median.
	TTFTMedianMS     float64 `json:"ttft_median_ms"`
	Invalidations    int     `json:"cache_invalidations"`
	AfterRewrite     int     `json:"cache_invalidations_after_rewrite"`
	RecomputedTokens int     `json:"recomputed_prompt_tokens"`
	// The cost of passed and failed tasks, apart: processed tokens
	// (uncached prompt + generated) and seconds per task.
	PassedTokens  float64        `json:"processed_tokens_per_passed_task"`
	PassedSeconds float64        `json:"seconds_per_passed_task"`
	FailedTokens  float64        `json:"processed_tokens_per_failed_task"`
	FailedSeconds float64        `json:"seconds_per_failed_task"`
	Failures      map[string]int `json:"failures,omitempty"` // by FailureClass
	Stalls        map[string]int `json:"stalls,omitempty"`
	// Peaks over the run's tasks.
	PeakHarnessRSSMiB float64 `json:"peak_harness_rss_mib"`
	PeakServerRSSMiB  float64 `json:"peak_model_server_rss_mib,omitempty"`
	PeakGPUMemMiB     float64 `json:"peak_gpu_mem_used_mib,omitempty"`
}

func (d *DirectRunner) progress(format string, args ...any) {
	if d.Progress != nil {
		d.Progress(fmt.Sprintf(format, args...))
	}
}

// Run executes the tasks sequentially, saving the report after each one.
func (d *DirectRunner) Run(ctx context.Context, model string, tasks []TaskSpec, out string) (*DirectReport, error) {
	rep := &DirectReport{ID: "direct-" + time.Now().UTC().Format("20060102T150405Z"), Model: model, Started: time.Now().UTC(), Settings: d.Settings,
		Environment: captureEnvironment(ctx, d.Model, d.ServerBinary, tasks, d.TaskDirs, d.FixturesDir)}
	save := func() error {
		rep.Finished = time.Now().UTC()
		rep.Summary = summarizeDirect("", rep.Results)
		rep.ByClass, rep.ByLanguage = nil, nil
		seen, langs := map[string]bool{}, map[string]bool{}
		for _, r := range rep.Results {
			if !seen[r.Category] {
				seen[r.Category] = true
				rep.ByClass = append(rep.ByClass, summarizeDirect(r.Category, rep.Results))
			}
			if r.Language != "" && !langs[r.Language] {
				langs[r.Language] = true
				ls := summarizeDirect("", languageOnly(rep.Results, r.Language))
				ls.Language = r.Language
				rep.ByLanguage = append(rep.ByLanguage, ls)
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
	missing := missingCommands(tasks)
	if len(missing) == len(tasks) {
		return nil, fmt.Errorf("no task can run here: %s", strings.Join(uniqStrings(mapValues(missing)), "; "))
	}
	for i, t := range tasks {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		d.progress("task %d/%d %s", i+1, len(tasks), t.ID)
		var res DirectResult
		if why, ok := missing[t.ID]; ok {
			res = DirectResult{ID: t.ID, Category: t.Category, Language: t.Language, Model: model, Skipped: why, FailureClass: "environment"}
			d.progress("task %s: skipped (%s)", t.ID, why)
			rep.Results = append(rep.Results, res)
			if err := save(); err != nil {
				return rep, err
			}
			continue
		}
		res = d.runOne(ctx, model, t)
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

// missingCommands finds the tasks whose acceptance commands are not
// installed: they would fail for a reason that has nothing to do with the
// agent (which could not run them either), so they are skipped and
// reported as such.
func missingCommands(tasks []TaskSpec) map[string]string {
	out := map[string]string{}
	for _, t := range tasks {
		var bins []string
		need := append([]string(nil), t.Requires...)
		for _, c := range t.Checks {
			if len(c.Run) > 0 {
				need = append(need, c.Run[0])
			}
		}
		for _, bin := range need {
			if _, err := exec.LookPath(bin); err != nil {
				bins = append(bins, bin)
			}
		}
		if len(bins) > 0 {
			out[t.ID] = strings.Join(uniqStrings(bins), ", ") + " not on PATH"
		}
	}
	return out
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func languageOnly(rs []DirectResult, lang string) []DirectResult {
	var out []DirectResult
	for _, r := range rs {
		if r.Language == lang {
			out = append(out, r)
		}
	}
	return out
}

func uniqStrings(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
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
	res = DirectResult{ID: spec.ID, Category: spec.Category, Language: spec.Language, Model: model, Tools: map[string]int{}, ToolTimes: map[string]ToolTime{}}
	start := time.Now()
	defer func() {
		res.WallSeconds = time.Since(start).Seconds()
		res.FailureClass = failureClass(res)
	}()
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
	r.Gateway.KeepLog = true
	sections := map[int]map[string]string{} // call index -> what the request was made of
	rewrites := map[int]bool{}              // call indexes that follow a history rewrite
	r.OnRequest = func(s map[string]string) { sections[len(r.Gateway.CallLog())] = s }
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
			tt := res.ToolTimes[e.Tool]
			tt.Calls++
			tt.Seconds += e.Elapsed.Seconds()
			tt.MaxSeconds = max(tt.MaxSeconds, e.Elapsed.Seconds())
			tt.ResultBytes += len(e.Output)
			res.ToolTimes[e.Tool] = tt
			res.ToolSeconds += e.Elapsed.Seconds()
			if strings.Contains(e.Output, "Fix this before going on") {
				res.ChecksCaught++
			}
		case direct.EventGuard:
			if e.Args["decision"] == "refused" {
				res.GuardRefusals++
			}
		case direct.EventSandbox:
			if lv, ok := e.Args["level"].(string); ok {
				res.Sandbox = fmt.Sprintf("%s (%v)", lv, e.Args["backend"])
			}
			if e.Args["allowed"] == false {
				res.NetRefused++
			}
			if strings.Contains(e.Output, "check note (") {
				res.CheckNotes++
			}
			if strings.Contains(e.Output, "history note: ") {
				res.HistoryNotes++
			}
		case direct.EventDenied:
			res.Denied++
		case direct.EventCompact:
			rewrites[len(r.Gateway.CallLog())] = true
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
				res.Stalls = append(res.Stalls, "nudged")
			}
			if strings.HasPrefix(e.Text, "Stopped after") {
				res.Stalls = append(res.Stalls, "max_turns")
			}
			if strings.Contains(e.Text, "ended without an answer") {
				res.Stalls = append(res.Stalls, "empty_answer")
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
	agentStart := time.Now()
	stopSampling := sampleResources(ctx, false)
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
	res.AgentSeconds = time.Since(agentStart).Seconds()
	res.Resources = stopSampling()
	res.Answer = tailStr(answer, 4000)
	d.attribute(ctx, r, &res, sections, rewrites)
	p, g, c := r.Gateway.Stats()
	res.Calls = r.Gateway.Calls()
	res.GeneratedTokens, res.CachedTokens = g, c
	res.PromptTokens = p - g + c
	res.PeakPrompt = r.Gateway.PeakPrompt()
	var handoff *direct.Handoff
	switch {
	case errors.As(sendErr, &handoff):
		res.Error = "handed off to verification: " + handoff.Reason
	case sendErr != nil && ctx.Err() != nil:
		res.Error = "interrupted: the run was stopped"
	case errors.Is(sendErr, context.DeadlineExceeded) || (sendErr != nil && tctx.Err() != nil):
		res.Error = fmt.Sprintf("timed out after %s", timeout)
		res.Stalls = append(res.Stalls, "timeout")
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

// attribute fills in where the task's time and tokens went, from the
// gateway's call log, the requests' sections and the runner's phases.
func (d *DirectRunner) attribute(ctx context.Context, r *direct.Runner, res *DirectResult, sections map[int]map[string]string, rewrites map[int]bool) {
	log := r.Gateway.CallLog()
	countReasoning(ctx, log, d.Tokenize)
	res.CallStats = summarizeCalls(log, rewrites)
	res.CallLog = log
	res.HarnessSeconds = max(res.AgentSeconds-res.CallStats.InferenceSeconds-res.ToolSeconds, 0)
	res.PhaseSeconds = map[string]float64{}
	for k, v := range r.Phases() {
		res.PhaseSeconds[k] = v.Seconds()
	}
	// The largest request whose sections were seen (the loop's own
	// requests; summaries and retries have none).
	peak := -1
	for i := range sections {
		if i < len(log) && (peak < 0 || log[i].Prompt > log[peak].Prompt) {
			peak = i
		}
	}
	if peak >= 0 {
		res.Context = breakdown(ctx, peak, log[peak].Prompt, sections[peak], d.Tokenize)
	}
	for _, m := range r.Session.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "This exact call has now failed") {
			res.Stalls = append(res.Stalls, "repeated_failure")
			break
		}
	}
}

// failureClass says why a task that did not pass failed ("" when it
// passed or was skipped, which has its own class).
func failureClass(r DirectResult) string {
	switch {
	case r.Skipped != "":
		return "environment"
	case r.Success:
		return ""
	case strings.HasPrefix(r.Error, "interrupted"):
		return "interrupted"
	case strings.HasPrefix(r.Error, "timed out"):
		return "timeout"
	case strings.HasPrefix(r.Error, "handed off to verification"):
		return "routing"
	case strings.HasPrefix(r.Error, "materialize"), strings.Contains(r.Error, "model request failed (http 5"),
		strings.Contains(r.Error, "connection refused"), strings.Contains(r.Error, "not on PATH"), r.CallStats.Failed > 0 && r.CallStats.Failed == r.CallStats.Calls:
		return "environment"
	case r.Error != "":
		return "harness"
	}
	return "model"
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
	var wall, agent, inf, tool, harness, reasoning float64
	var passTok, passSec, failTok, failSec float64
	var ttfts []float64
	for _, r := range rs {
		if category != "" && r.Category != category {
			continue
		}
		if r.Skipped != "" {
			s.Skipped++
			if s.Failures == nil {
				s.Failures = map[string]int{}
			}
			s.Failures["environment (skipped)"]++
			continue
		}
		s.Tasks++
		agent += r.AgentSeconds
		inf += r.CallStats.InferenceSeconds
		tool += r.ToolSeconds
		harness += r.HarnessSeconds
		reasoning += float64(r.CallStats.ReasoningTokens)
		if r.CallStats.TTFTMedianMS > 0 {
			ttfts = append(ttfts, r.CallStats.TTFTMedianMS)
		}
		s.Invalidations += r.CallStats.Invalidations
		s.AfterRewrite += r.CallStats.AfterRewrite
		s.RecomputedTokens += r.CallStats.RecomputedTokens
		processed := float64(r.PromptTokens - r.CachedTokens + r.GeneratedTokens)
		if r.Success {
			passTok += processed
			passSec += r.WallSeconds
		} else {
			failTok += processed
			failSec += r.WallSeconds
			if s.Failures == nil {
				s.Failures = map[string]int{}
			}
			s.Failures[r.FailureClass]++
		}
		for _, st := range r.Stalls {
			if s.Stalls == nil {
				s.Stalls = map[string]int{}
			}
			s.Stalls[st]++
		}
		s.PeakHarnessRSSMiB = max(s.PeakHarnessRSSMiB, r.Resources.HarnessRSSMiB)
		s.PeakServerRSSMiB = max(s.PeakServerRSSMiB, r.Resources.ServerRSSMiB)
		s.PeakGPUMemMiB = max(s.PeakGPUMemMiB, r.Resources.GPUMemMiB)
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
		s.CheckNotes += r.CheckNotes
		s.GuardRefusals += r.GuardRefusals
		s.HistoryNotes += r.HistoryNotes
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
	s.MeanAgentSeconds, s.MeanInferenceSeconds, s.MeanToolSeconds, s.MeanHarnessSeconds = agent/n, inf/n, tool/n, harness/n
	s.MeanReasoning = reasoning / n
	if len(ttfts) > 0 {
		sort.Float64s(ttfts)
		s.TTFTMedianMS = ttfts[len(ttfts)/2]
	}
	if s.Passed > 0 {
		s.PassedTokens, s.PassedSeconds = passTok/float64(s.Passed), passSec/float64(s.Passed)
	}
	if failed := s.Tasks - s.Passed; failed > 0 {
		s.FailedTokens, s.FailedSeconds = failTok/float64(failed), failSec/float64(failed)
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
	fmt.Fprintf(w, "**%d/%d passed (%.0f%%)**, %.1f model calls and %.1f tool calls per task, %.0f prompt tokens per task with a %.0f%% prompt-cache hit rate, %.0f generated tokens per task, %.0f KB of tool results per task, %.0f s per task, %d tokens in the largest single prompt; %d results set aside, %d recalled, %d summaries, %d broken edits caught by the check, %d check notes, %d history notes, %d errors.\n\n",
		s.Passed, s.Tasks, 100*s.PassRate, s.MeanCalls, s.MeanToolCalls, s.MeanPrompt, 100*s.CacheHitRate, s.MeanGenerated, s.MeanResultBytes/1024, s.MeanWallSeconds, s.PeakPrompt, s.Elided, s.Recalls, s.Compactions, s.ChecksCaught, s.CheckNotes, s.HistoryNotes, s.Errors)
	if s.MeanAgentSeconds > 0 {
		fmt.Fprintf(w, "Where the agent's time went, per task: %.1f s in model calls, %.1f s in tools (with the checks after edits), %.2f s in the harness itself; time to first token %.0f ms (median). %.0f reasoning tokens per task. %d prompt-cache invalidations (%d after the history was rewritten), %d prompt tokens recomputed. Cost of a passed task: %.0f processed tokens and %.0f s; of a failed one: %.0f and %.0f s. Peak memory: harness %.0f MiB, model server %.0f MiB, GPU %.0f MiB.\n\n",
			s.MeanInferenceSeconds, s.MeanToolSeconds, s.MeanHarnessSeconds, s.TTFTMedianMS, s.MeanReasoning, s.Invalidations, s.AfterRewrite, s.RecomputedTokens,
			s.PassedTokens, s.PassedSeconds, s.FailedTokens, s.FailedSeconds, s.PeakHarnessRSSMiB, s.PeakServerRSSMiB, s.PeakGPUMemMiB)
	}
	if len(s.Failures) > 0 || len(s.Stalls) > 0 {
		fmt.Fprintf(w, "Failures by cause: %s. Stalls: %s.\n\n", countList(s.Failures), countList(s.Stalls))
	}
	if len(r.ByLanguage) > 1 {
		fmt.Fprintf(w, "| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, c := range r.ByLanguage {
			fmt.Fprintf(w, "| %s | %d/%d | %d | %.1f | %.1f | %.0f | %.1f | %.1f | %.0f |\n", c.Language, c.Passed, c.Tasks, c.Skipped, c.MeanCalls, c.MeanToolCalls, c.MeanPrompt, c.MeanInferenceSeconds, c.MeanToolSeconds, c.MeanWallSeconds)
		}
		fmt.Fprintln(w)
	}
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
		if t.Skipped != "" {
			verdict, note = "skipped", t.Skipped
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %.0f | %s |\n",
			t.ID, t.Category, mode, verdict, t.Calls, t.ToolCalls, t.PromptTokens, t.CachedTokens, t.PeakPrompt, t.GeneratedTokens, t.Elided, t.Recalls, t.ChecksCaught, t.WallSeconds, note)
	}
	// What the largest request of each task was made of.
	var secNames []string
	seen := map[string]bool{}
	for _, t := range r.Results {
		if t.Context == nil {
			continue
		}
		for k := range t.Context.Sections {
			if !seen[k] {
				seen[k] = true
				secNames = append(secNames, k)
			}
		}
	}
	if len(secNames) > 0 {
		sort.Strings(secNames)
		fmt.Fprintf(w, "\nThe largest request of each task, in tokens by section (counting: %s):\n\n| Task | Prompt | %s | framing |\n|---|---|%s---|\n",
			r.Results[0].contextCounting(r.Results), strings.Join(secNames, " | "), strings.Repeat("---|", len(secNames)))
		for _, t := range r.Results {
			if t.Context == nil {
				continue
			}
			row := []string{t.ID, fmt.Sprint(t.Context.PromptTokens)}
			for _, k := range secNames {
				row = append(row, fmt.Sprint(t.Context.Sections[k].Tokens))
			}
			row = append(row, fmt.Sprint(t.Context.Framing))
			fmt.Fprintf(w, "| %s |\n", strings.Join(row, " | "))
		}
	}
	if e := r.Environment; e.OS != "" {
		fmt.Fprintf(w, "\n%s\n", environmentLine(e))
	}
	return nil
}

// contextCounting names how the reports' sections were counted.
func (DirectResult) contextCounting(rs []DirectResult) string {
	kinds := map[string]bool{}
	for _, r := range rs {
		if r.Context != nil {
			kinds[r.Context.Counting] = true
		}
	}
	switch {
	case len(kinds) > 1:
		return "mixed"
	case kinds["estimate"]:
		return "estimate, bytes × 10 / 32"
	}
	return "the model's tokenizer"
}

func countList(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s %d", orDash(k), v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
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
