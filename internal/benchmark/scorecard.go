package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// A scorecard is the machine-readable result of one or more trials of a
// suite under one configuration: what was run (model, build, task set,
// budgets), how often, and the figures with their uncertainty, overall and
// per language, group and split. Baselines and candidates are compared as
// scorecards (gate.go).

// ScorecardSchema is the version of the format.
const ScorecardSchema = 1

// Scorecard is the result.
type Scorecard struct {
	Schema  int       `json:"schema"`
	Suite   string    `json:"suite"` // direct, bounded, verify
	Created time.Time `json:"created"`
	// Model is the model as the reports pinned it (profile, file, sha256,
	// server); Build the bcode build; TaskSet the task definitions' hash.
	Model    map[string]any `json:"model,omitempty"`
	Server   string         `json:"server,omitempty"`
	Build    string         `json:"build"`
	TaskSet  string         `json:"task_set,omitempty"`
	Machine  string         `json:"machine,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
	// Trials is the number of reports, Reports their ids.
	Trials  int      `json:"trials"`
	Reports []string `json:"reports"`
	Overall Metrics  `json:"overall"`
	// ByLanguage, ByGroup and BySplit are the same figures per key.
	ByLanguage map[string]Metrics `json:"by_language"`
	ByGroup    map[string]Metrics `json:"by_group,omitempty"`
	BySplit    map[string]Metrics `json:"by_split,omitempty"`
	// Tasks are per-task pass counts over the trials (a task passing in
	// some trials and failing in others is the suite's noise).
	Tasks map[string]TaskTally `json:"tasks"`
	// PerTrial are each trial's overall figures, for the run-to-run noise
	// the gate allows for.
	PerTrial    []Metrics `json:"per_trial"`
	Limitations []string  `json:"limitations"`
}

// TaskTally is one task over the trials.
type TaskTally struct {
	Runs   int `json:"runs"`
	Passed int `json:"passed"`
}

// Metrics are a set of runs' figures. Rates come with a 90% Wilson
// interval over the task runs (runs of one task are not independent, so
// the interval is narrower than the truth when trials repeat tasks).
type Metrics struct {
	// Passed counts runs the hidden checks passed; in the verify suite,
	// cases whose change is correct.
	Runs        int        `json:"runs"`
	Passed      int        `json:"passed"`
	SuccessRate float64    `json:"success_rate"`
	SuccessCI90 [2]float64 `json:"success_ci90"`
	// Verification (bounded and verify suites): a claim of verification
	// against the hidden checks' verdict.
	Verified          int     `json:"verified_claims,omitempty"`
	VerifyPrecision   float64 `json:"verify_precision,omitempty"` // verified and correct / verified
	VerifyRecall      float64 `json:"verify_recall,omitempty"`    // verified and correct / correct
	FalseAcceptance   int     `json:"false_acceptance"`           // verified, hidden checks failed
	FalseAcceptRate   float64 `json:"false_acceptance_rate"`      // over the incorrect
	FalseRejection    int     `json:"false_rejection"`            // not verified, hidden checks passed
	FalseRejectRate   float64 `json:"false_rejection_rate"`       // over the correct
	TokensPerSuccess  float64 `json:"tokens_per_success,omitempty"`
	TokensPerFailure  float64 `json:"tokens_per_failure,omitempty"`
	MedianSeconds     float64 `json:"median_seconds,omitempty"`
	P90Seconds        float64 `json:"p90_seconds,omitempty"`
	P95Seconds        float64 `json:"p95_seconds,omitempty"`
	ModelCalls        float64 `json:"model_calls_per_run,omitempty"`
	ToolCalls         float64 `json:"tool_calls_per_run,omitempty"`
	PeakHarnessMiB    float64 `json:"peak_harness_mib,omitempty"`
	PeakServerMiB     float64 `json:"peak_server_mib,omitempty"`
	PeakGPUMiB        float64 `json:"peak_gpu_mib,omitempty"`
	SecurityRuns      int     `json:"security_runs,omitempty"`
	SecurityFailures  int     `json:"security_failures"`
	SecurityAttempts  int     `json:"security_attempts,omitempty"`
	TamperedRuns      int     `json:"tampered_runs,omitempty"`
	TasksSkipped      int     `json:"skipped,omitempty"`
	successTokens     []float64
	failureTokens     []float64
	seconds           []float64
	modelCalls, tools float64
}

// run is one task run as the scorecard counts it.
type run struct {
	id, group, split string
	langs            []string
	passed           bool
	claimed, graded  bool // a verification claim was made; the run was graded against hidden checks
	tokens, seconds  float64
	modelCalls       float64
	toolCalls        float64
	harness, server  float64
	gpu              float64
	security         *SecurityOutcome
	tampered         bool
	skipped          bool
}

func (m *Metrics) add(r run) {
	if r.skipped {
		m.TasksSkipped++
		return
	}
	m.Runs++
	if r.passed {
		m.Passed++
		m.successTokens = append(m.successTokens, r.tokens)
	} else {
		m.failureTokens = append(m.failureTokens, r.tokens)
	}
	if r.graded {
		switch {
		case r.claimed && r.passed:
			m.Verified++
		case r.claimed && !r.passed:
			m.Verified++
			m.FalseAcceptance++
		case !r.claimed && r.passed:
			m.FalseRejection++
		}
	}
	if r.seconds > 0 {
		m.seconds = append(m.seconds, r.seconds)
	}
	m.modelCalls += r.modelCalls
	m.tools += r.toolCalls
	m.PeakHarnessMiB = math.Max(m.PeakHarnessMiB, r.harness)
	m.PeakServerMiB = math.Max(m.PeakServerMiB, r.server)
	m.PeakGPUMiB = math.Max(m.PeakGPUMiB, r.gpu)
	if r.security != nil {
		m.SecurityRuns++
		m.SecurityFailures += len(r.security.Failures)
		m.SecurityAttempts += len(r.security.Attempts)
	}
	if r.tampered {
		m.TamperedRuns++
	}
}

func (m *Metrics) finish() {
	if m.Runs == 0 {
		return
	}
	n := float64(m.Runs)
	m.SuccessRate = float64(m.Passed) / n
	lo, hi := wilson90(m.Passed, m.Runs)
	m.SuccessCI90 = [2]float64{round3(lo), round3(hi)}
	correct, incorrect := m.Passed, m.Runs-m.Passed
	if m.Verified > 0 {
		m.VerifyPrecision = round3(float64(m.Verified-m.FalseAcceptance) / float64(m.Verified))
	}
	if correct > 0 && m.Verified+m.FalseRejection > 0 {
		m.VerifyRecall = round3(float64(m.Verified-m.FalseAcceptance) / float64(correct))
	}
	if incorrect > 0 {
		m.FalseAcceptRate = round3(float64(m.FalseAcceptance) / float64(incorrect))
	}
	if correct > 0 {
		m.FalseRejectRate = round3(float64(m.FalseRejection) / float64(correct))
	}
	m.TokensPerSuccess = meanOf(m.successTokens)
	m.TokensPerFailure = meanOf(m.failureTokens)
	sort.Float64s(m.seconds)
	m.MedianSeconds, m.P90Seconds, m.P95Seconds = rankQuantile(m.seconds, 0.5), rankQuantile(m.seconds, 0.9), rankQuantile(m.seconds, 0.95)
	m.ModelCalls = round3(m.modelCalls / n)
	m.ToolCalls = round3(m.tools / n)
	m.SuccessRate = round3(m.SuccessRate)
}

func meanOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return math.Round(s / float64(len(v)))
}

// rankQuantile is the nearest-rank quantile of sorted v.
func rankQuantile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(v)))) - 1
	return math.Round(v[max(i, 0)]*10) / 10
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// wilson90 is the 90% Wilson score interval of k successes in n.
func wilson90(k, n int) (float64, float64) {
	if n == 0 {
		return 0, 1
	}
	const z = 1.645
	p, nf := float64(k)/float64(n), float64(n)
	d := 1 + z*z/nf
	c := p + z*z/(2*nf)
	w := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf))
	return math.Max(0, (c-w)/d), math.Min(1, (c+w)/d)
}

// BuildScorecard reads the reports at paths (all of one suite) into a
// scorecard. meta supplies a task's group, split and languages when an
// older report lacks them (nil: none).
func BuildScorecard(paths []string, meta map[string]CatalogEntry) (*Scorecard, error) {
	sc := &Scorecard{Schema: ScorecardSchema, Created: time.Now().UTC(), ByLanguage: map[string]Metrics{}, ByGroup: map[string]Metrics{},
		BySplit: map[string]Metrics{}, Tasks: map[string]TaskTally{}}
	var runs []run
	builds, models, sets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, p := range paths {
		rs, suite, env, settings, id, err := readRuns(p, meta)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if sc.Suite != "" && sc.Suite != suite {
			return nil, fmt.Errorf("%s is a %s report; the others are %s", p, suite, sc.Suite)
		}
		sc.Suite = suite
		sc.Trials++
		sc.Reports = append(sc.Reports, id)
		if env != nil {
			builds[env.Build] = true
			sets[env.TasksHash] = true
			if sha, _ := env.Model["sha256"].(string); sha != "" {
				models[sha] = true
			}
			sc.Model, sc.Server, sc.Build, sc.TaskSet = env.Model, env.Server, env.Build, env.TasksHash
			sc.Machine = strings.TrimSpace(env.CPU + " " + env.GPU)
		}
		if sc.Settings == nil {
			sc.Settings = settings
		}
		var trial Metrics
		for _, r := range rs {
			trial.add(r)
		}
		trial.finish()
		sc.PerTrial = append(sc.PerTrial, trial)
		runs = append(runs, rs...)
	}
	for _, r := range runs {
		sc.add(r)
	}
	sc.Overall.finish()
	for _, m := range []map[string]Metrics{sc.ByLanguage, sc.ByGroup, sc.BySplit} {
		for k, v := range m {
			v.finish()
			m[k] = v
		}
	}
	sc.Limitations = limitations(sc, builds, models, sets)
	return sc, nil
}

func (sc *Scorecard) add(r run) {
	sc.Overall.add(r)
	for _, l := range r.langs {
		m := sc.ByLanguage[l]
		m.add(r)
		sc.ByLanguage[l] = m
	}
	if r.group != "" {
		m := sc.ByGroup[r.group]
		m.add(r)
		sc.ByGroup[r.group] = m
	}
	if r.split != "" {
		m := sc.BySplit[r.split]
		m.add(r)
		sc.BySplit[r.split] = m
	}
	if !r.skipped {
		t := sc.Tasks[r.id]
		t.Runs++
		if r.passed {
			t.Passed++
		}
		sc.Tasks[r.id] = t
	}
}

// limitations are what the scorecard cannot show, stated with it.
func limitations(sc *Scorecard, builds, models, sets map[string]bool) []string {
	var out []string
	if sc.Trials < 3 && sc.Suite != "verify" {
		out = append(out, fmt.Sprintf("%d trial(s): fewer than three, so run-to-run noise is barely estimated", sc.Trials))
	}
	if len(builds) > 1 {
		out = append(out, fmt.Sprintf("the trials come from %d builds", len(builds)))
	}
	if len(models) > 1 {
		out = append(out, fmt.Sprintf("the trials used %d different model files", len(models)))
	}
	if len(sets) > 1 {
		out = append(out, fmt.Sprintf("the trials ran %d different task sets", len(sets)))
	}
	flaky := 0
	for _, t := range sc.Tasks {
		if t.Passed > 0 && t.Passed < t.Runs {
			flaky++
		}
	}
	if flaky > 0 {
		out = append(out, fmt.Sprintf("%d task(s) passed in some trials and failed in others", flaky))
	}
	if sc.Overall.TasksSkipped > 0 {
		out = append(out, fmt.Sprintf("%d task run(s) skipped (a toolchain is missing)", sc.Overall.TasksSkipped))
	}
	if sc.Suite == "verify" {
		out = append(out, "verification alone: the changes are fixed in advance and no model ran")
	}
	out = append(out, "one machine; the confidence intervals treat repeated runs of a task as independent")
	return out
}

// readRuns reads one report of any suite.
func readRuns(path string, meta map[string]CatalogEntry) (runs []run, suite string, env *Environment, settings map[string]any, id string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", nil, nil, "", err
	}
	var probe struct {
		ID       string            `json:"id"`
		Variants []json.RawMessage `json:"variants"`
		Results  []json.RawMessage `json:"results"`
		Summary  map[string]any    `json:"summary"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, "", nil, nil, "", err
	}
	fill := func(r *run, group, split, lang string, langs []string) {
		r.group, r.split, r.langs = group, split, langs
		if m, ok := meta[r.id]; ok {
			if r.group == "" {
				r.group = m.Group
			}
			if r.split == "" {
				r.split = m.Split
			}
			if len(r.langs) == 0 {
				r.langs = m.Languages
			}
		}
		if len(r.langs) == 0 && lang != "" {
			r.langs = []string{lang}
		}
	}
	switch {
	case probe.Variants != nil:
		var rep VerifyReport
		if err := json.Unmarshal(b, &rep); err != nil {
			return nil, "", nil, nil, "", err
		}
		for _, c := range rep.Results {
			if !c.TruthAgrees || c.Error != "" {
				continue
			}
			r := run{id: c.ID, passed: c.Correct, claimed: c.New == "task_verified", graded: true}
			fill(&r, "verification", verifySplit(VerifyCase{Language: c.Language, Kind: c.Kind}), c.Language, nil)
			if c.Language == "mixed" {
				r.langs = []string{"go", "javascript"}
			}
			runs = append(runs, r)
		}
		return runs, "verify", nil, map[string]any{"sandbox": rep.Sandbox}, "verify-" + rep.Started.Format("20060102T150405Z"), nil
	case probe.Summary != nil && probe.Summary["self_verified_but_hidden_failed"] != nil:
		var rep SuiteReport
		if err := json.Unmarshal(b, &rep); err != nil {
			return nil, "", nil, nil, "", err
		}
		for _, t := range rep.Results {
			r := run{id: t.ID, passed: t.Success, claimed: t.SelfVerified, graded: true, tokens: float64(t.LocalTokens),
				seconds: t.WallSeconds, modelCalls: float64(t.Calls.Calls), harness: t.Resources.HarnessRSSMiB,
				server: t.Resources.ServerRSSMiB, gpu: t.Resources.GPUMemMiB, tampered: t.Tamper != nil && t.Tamper.Any(), skipped: t.Skipped != ""}
			r.toolCalls = float64(t.Intel.AgentToolCalls)
			fill(&r, t.Group, t.Split, t.Language, t.Languages)
			runs = append(runs, r)
		}
		return runs, "bounded", &rep.Environment, rep.Settings, rep.ID, nil
	default:
		var rep DirectReport
		if err := json.Unmarshal(b, &rep); err != nil {
			return nil, "", nil, nil, "", err
		}
		for _, t := range rep.Results {
			r := run{id: t.ID, passed: t.Success, tokens: float64(t.PromptTokens - t.CachedTokens + t.GeneratedTokens), seconds: t.WallSeconds,
				modelCalls: float64(t.Calls), toolCalls: float64(t.ToolCalls), harness: t.Resources.HarnessRSSMiB, server: t.Resources.ServerRSSMiB,
				gpu: t.Resources.GPUMemMiB, security: t.Security, tampered: t.Tamper != nil && t.Tamper.Any(), skipped: t.Skipped != ""}
			fill(&r, t.Group, t.Split, t.Language, t.Languages)
			runs = append(runs, r)
		}
		return runs, "direct", &rep.Environment, rep.Settings, rep.ID, nil
	}
}

// WriteScorecardMarkdown writes a scorecard as tables.
func WriteScorecardMarkdown(w io.Writer, sc *Scorecard) {
	model, _ := sc.Model["profile"].(string)
	sha, _ := sc.Model["sha256"].(string)
	if len(sha) > 12 {
		sha = sha[:12]
	}
	fmt.Fprintf(w, "# Scorecard: %s suite\n\n%d trial(s) (%s). Model %s (sha256 %s), server %s; build %s; task set %s; %s.\n\n",
		sc.Suite, sc.Trials, strings.Join(sc.Reports, ", "), dash(model), dash(sha), dash(sc.Server), dash(sc.Build), dash(sc.TaskSet), dash(sc.Machine))
	head := "| | Runs | Passed | Success (90% CI) | False acceptance | False rejection | Tokens per success / failure | Median / p90 / p95 s | Model / tool calls | Security failures (attempts) | Tampered |\n|---|---|---|---|---|---|---|---|---|---|---|\n"
	row := func(name string, m Metrics) string {
		return fmt.Sprintf("| %s | %d | %d | %.0f%% (%.0f–%.0f%%) | %d | %d | %.0f / %.0f | %.0f / %.0f / %.0f | %.1f / %.1f | %d (%d) | %d |\n", name, m.Runs, m.Passed,
			100*m.SuccessRate, 100*m.SuccessCI90[0], 100*m.SuccessCI90[1], m.FalseAcceptance, m.FalseRejection, m.TokensPerSuccess, m.TokensPerFailure,
			m.MedianSeconds, m.P90Seconds, m.P95Seconds, m.ModelCalls, m.ToolCalls, m.SecurityFailures, m.SecurityAttempts, m.TamperedRuns)
	}
	if sc.Suite == "verify" {
		// A verify case passes when its change is correct, so a success rate
		// says nothing about verification; its own table shows the verdicts.
		head = "| | Cases | Correct changes | Accepted | False acceptance (rate) | False rejection (rate) | Precision | Recall |\n|---|---|---|---|---|---|---|---|\n"
		row = func(name string, m Metrics) string {
			return fmt.Sprintf("| %s | %d | %d | %d | %d (%.0f%%) | %d (%.0f%%) | %.2f | %.2f |\n", name, m.Runs, m.Passed, m.Verified,
				m.FalseAcceptance, 100*m.FalseAcceptRate, m.FalseRejection, 100*m.FalseRejectRate, m.VerifyPrecision, m.VerifyRecall)
		}
	}
	fmt.Fprint(w, head+row("**all**", sc.Overall))
	for _, sec := range []struct {
		title string
		m     map[string]Metrics
	}{{"split", sc.BySplit}, {"group", sc.ByGroup}, {"language", sc.ByLanguage}} {
		var keys []string
		for k, m := range sec.m {
			if m.Runs > 0 {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprint(w, row(sec.title+": "+k, sec.m[k]))
		}
	}
	fmt.Fprintln(w, "\nLimitations:")
	for _, l := range sc.Limitations {
		fmt.Fprintln(w, "- "+l)
	}
}
