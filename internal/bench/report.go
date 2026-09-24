package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Rate is a denominator-bearing proportion. A nil Value means unavailable;
// it is never rendered as a successful zero.
type Rate struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
	Low         *float64 `json:"ci_low"`
	High        *float64 `json:"ci_high"`
}

func rate(successes, total int) Rate {
	r := Rate{Numerator: successes, Denominator: total}
	if total <= 0 {
		return r
	}
	p := float64(successes) / float64(total)
	r.Value = &p
	// Wilson interval, conservative at small n.
	const z = 1.96
	n := float64(total)
	denom := 1 + z*z/n
	centre := (p + z*z/(2*n)) / denom
	margin := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom
	lo, hi := math.Max(0, centre-margin), math.Min(1, centre+margin)
	r.Low, r.High = &lo, &hi
	return r
}

type NumericSummary struct {
	Median *float64 `json:"median"`
	P90    *float64 `json:"p90"`
	N      int      `json:"n"`
}

type ModeSummary struct {
	Mode                 Mode           `json:"mode"`
	Runs                 int            `json:"runs"`
	EvidenceRuns         int            `json:"evidence_runs"`
	VerifiedCompletion   Rate           `json:"independently_verified_completion"`
	ReportedSuccess      Rate           `json:"reported_success"`
	FalseSuccess         Rate           `json:"false_success"`
	FalseFailure         Rate           `json:"false_failure"`
	Timeout              Rate           `json:"timeout"`
	InfrastructureError  Rate           `json:"infrastructure_error"`
	WallClock            NumericSummary `json:"wall_clock_seconds"`
	InputTokens          NumericSummary `json:"input_tokens"`
	OutputTokens         NumericSummary `json:"output_tokens"`
	TotalTokens          NumericSummary `json:"total_tokens"`
	GenerationRequests   NumericSummary `json:"generation_requests"`
	ToolCalls            NumericSummary `json:"tool_calls"`
	Retries              NumericSummary `json:"retries"`
	VerificationAttempts NumericSummary `json:"verification_attempts"`
	RecoveryAttempts     Rate           `json:"recovery_attempts"`
	RecoverySuccesses    Rate           `json:"recovery_successes"`
	RepeatedFailures     NumericSummary `json:"repeated_failure_count"`
	Warnings             []string       `json:"warnings,omitempty"`
}

type PairedSummary struct {
	PairID                string   `json:"pair_id"`
	TaskID                string   `json:"task_id"`
	Repetition            int      `json:"repetition"`
	RawResult             string   `json:"raw_result"`
	BoundedResult         string   `json:"bounded_result"`
	RawVerified           bool     `json:"raw_verified"`
	BoundedVerified       bool     `json:"bounded_verified"`
	RawFalseSuccess       bool     `json:"raw_false_success"`
	BoundedFalseSuccess   bool     `json:"bounded_false_success"`
	RawWallClock          *float64 `json:"raw_wall_clock_seconds"`
	BoundedWallClock      *float64 `json:"bounded_wall_clock_seconds"`
	ComparabilityWarnings []string `json:"comparability_warnings,omitempty"`
}

type PairedStatistic struct {
	Metric             string   `json:"metric"`
	RawRate            Rate     `json:"raw_rate"`
	BoundedRate        Rate     `json:"bounded_rate"`
	AbsoluteDifference *float64 `json:"absolute_difference"`
	CI95Low            *float64 `json:"ci95_low"`
	CI95High           *float64 `json:"ci95_high"`
	Tasks              int      `json:"tasks"`
	PairedRuns         int      `json:"paired_runs"`
	Verdict            string   `json:"verdict"`
}

type TaskSummary struct {
	TaskID                string `json:"task_id"`
	RawSuccesses          int    `json:"raw_successes"`
	BoundedSuccesses      int    `json:"bounded_successes"`
	Repetitions           int    `json:"repetitions"`
	RawFalseSuccesses     int    `json:"raw_false_successes"`
	BoundedFalseSuccesses int    `json:"bounded_false_successes"`
}

type Report struct {
	ReportSchemaVersion int               `json:"report_schema_version"`
	GeneratedAt         time.Time         `json:"generated_at"`
	SuiteIDs            []string          `json:"suite_ids"`
	ManifestHashes      []string          `json:"manifest_hashes,omitempty"`
	Modes               []ModeSummary     `json:"modes"`
	PairedStatistics    []PairedStatistic `json:"paired_statistics"`
	PairedRuns          []PairedSummary   `json:"paired_runs"`
	Tasks               []TaskSummary     `json:"tasks"`
	Warnings            []string          `json:"warnings"`
	Results             []Result          `json:"results,omitempty"`
}

const ReportSchemaVersion = 1

// BuildReport computes only from durable Result files. It never reruns a
// worker and never treats a missing metric as zero.
func BuildReport(results []Result) Report {
	rep := Report{ReportSchemaVersion: ReportSchemaVersion, GeneratedAt: time.Now().UTC(), Results: append([]Result(nil), results...)}
	suiteSet := map[string]bool{}
	hashSet := map[string]bool{}
	for _, r := range results {
		suiteSet[r.BenchmarkSuite] = true
		if r.SuiteHash != "" {
			hashSet[r.SuiteHash] = true
		}
	}
	for s := range suiteSet {
		rep.SuiteIDs = append(rep.SuiteIDs, s)
	}
	for h := range hashSet {
		rep.ManifestHashes = append(rep.ManifestHashes, h)
	}
	sort.Strings(rep.SuiteIDs)
	sort.Strings(rep.ManifestHashes)
	rep.Warnings = append(rep.Warnings, "Rates are descriptive; a smoke or single-task run is not evidence of comparative performance.")
	if len(rep.ManifestHashes) > 1 {
		rep.Warnings = append(rep.Warnings, "results were generated against different suite hashes and are not a single frozen experiment")
	}
	for _, mode := range []Mode{Raw, Bounded} {
		rep.Modes = append(rep.Modes, summarizeMode(mode, results))
	}
	rep.PairedRuns, rep.PairedStatistics = paired(results)
	rep.Tasks = taskSummaries(results)
	return rep
}

func summarizeMode(mode Mode, results []Result) ModeSummary {
	s := ModeSummary{Mode: mode}
	var evidence, verified, reported, falseSuccess, falseFailure, timeout, infra int
	var recoveryAttempts, recoverySuccesses int
	values := map[string][]float64{}
	for _, r := range results {
		if r.Mode != mode {
			continue
		}
		s.Runs++
		if r.Status == RunTimeout {
			timeout++
		}
		if !r.Status.Evidence() {
			infra++
			continue
		}
		evidence++
		if r.Derived.IndependentVerifiedSuccess {
			verified++
		}
		if r.System.ReportedSuccess {
			reported++
		}
		if r.Derived.FalseSuccess {
			falseSuccess++
		}
		if r.Derived.FalseFailure {
			falseFailure++
		}
		addNumber(values, "wall", r.Execution.WallClockSeconds)
		addPtr(values, "input", r.Execution.InputTokens)
		addPtr(values, "output", r.Execution.OutputTokens)
		if r.Execution.TotalTokens != nil {
			addNumber(values, "total", float64(*r.Execution.TotalTokens))
		} else if r.Execution.InputTokens != nil && r.Execution.OutputTokens != nil {
			addNumber(values, "total", float64(*r.Execution.InputTokens+*r.Execution.OutputTokens))
		}
		addPtr(values, "generation", r.Execution.GenerationRequests)
		addPtr(values, "tools", r.Execution.ToolCalls)
		addPtr(values, "retries", r.Execution.Retries)
		addPtr(values, "verification", r.Execution.VerificationAttempts)
		addPtr(values, "repeated", r.Execution.RepeatedFailureCount)
		for _, e := range r.Events {
			if e.Kind == "recovery_attempted" {
				recoveryAttempts++
			}
			if e.Kind == "recovery_succeeded" {
				recoverySuccesses++
			}
		}
	}
	s.EvidenceRuns, s.VerifiedCompletion = evidence, rate(verified, evidence)
	s.ReportedSuccess, s.FalseSuccess, s.FalseFailure = rate(reported, evidence), rate(falseSuccess, evidence), rate(falseFailure, evidence)
	s.Timeout, s.InfrastructureError = rate(timeout, s.Runs), rate(infra, s.Runs)
	s.RecoveryAttempts, s.RecoverySuccesses = rate(recoveryAttempts, evidence), rate(recoverySuccesses, evidence)
	s.WallClock, s.InputTokens, s.OutputTokens = numeric(values["wall"]), numeric(values["input"]), numeric(values["output"])
	s.TotalTokens, s.GenerationRequests, s.ToolCalls = numeric(values["total"]), numeric(values["generation"]), numeric(values["tools"])
	s.Retries, s.VerificationAttempts, s.RepeatedFailures = numeric(values["retries"]), numeric(values["verification"]), numeric(values["repeated"])
	return s
}

func addNumber(m map[string][]float64, key string, v float64) { m[key] = append(m[key], v) }
func addPtr(m map[string][]float64, key string, value any) {
	switch v := value.(type) {
	case *int:
		if v != nil {
			m[key] = append(m[key], float64(*v))
		}
	case *int64:
		if v != nil {
			m[key] = append(m[key], float64(*v))
		}
	}
}

func numeric(v []float64) NumericSummary {
	if len(v) == 0 {
		return NumericSummary{}
	}
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	median := percentile(x, .5)
	p90 := percentile(x, .9)
	return NumericSummary{Median: &median, P90: &p90, N: len(x)}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	lo, hi := int(math.Floor(pos)), int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

func paired(results []Result) ([]PairedSummary, []PairedStatistic) {
	byPair := map[string]map[Mode]Result{}
	for _, r := range results {
		if byPair[r.PairID] == nil {
			byPair[r.PairID] = map[Mode]Result{}
		}
		byPair[r.PairID][r.Mode] = r
	}
	keys := make([]string, 0, len(byPair))
	for k := range byPair {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []PairedSummary
	for _, key := range keys {
		arms := byPair[key]
		raw, rb := arms[Raw]
		bounded, bb := arms[Bounded]
		if !rb || !bb {
			continue
		}
		p := PairedSummary{PairID: key, TaskID: raw.TaskID, Repetition: raw.Repetition}
		p.RawResult, p.BoundedResult = string(raw.Status), string(bounded.Status)
		p.RawVerified, p.BoundedVerified = raw.Derived.IndependentVerifiedSuccess, bounded.Derived.IndependentVerifiedSuccess
		p.RawFalseSuccess, p.BoundedFalseSuccess = raw.Derived.FalseSuccess, bounded.Derived.FalseSuccess
		p.RawWallClock, p.BoundedWallClock = floatPtr(raw.Execution.WallClockSeconds), floatPtr(bounded.Execution.WallClockSeconds)
		if raw.Fingerprints.Common != bounded.Fingerprints.Common {
			p.ComparabilityWarnings = append(p.ComparabilityWarnings, "common comparability fingerprints differ")
		}
		pairs = append(pairs, p)
	}
	stats := []PairedStatistic{pairedRate("independently_verified_completion", pairs, func(p PairedSummary) (bool, bool) { return p.RawVerified, p.BoundedVerified })}
	return pairs, stats
}

func floatPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func pairedRate(metric string, pairs []PairedSummary, get func(PairedSummary) (bool, bool)) PairedStatistic {
	st := PairedStatistic{Metric: metric, Verdict: "insufficient sample size for meaningful statistical conclusion"}
	raw, bounded := 0, 0
	for _, p := range pairs {
		a, b := get(p)
		if a {
			raw++
		}
		if b {
			bounded++
		}
	}
	st.RawRate, st.BoundedRate, st.PairedRuns = rate(raw, len(pairs)), rate(bounded, len(pairs)), len(pairs)
	if len(pairs) == 0 {
		return st
	}
	diff := float64(bounded-raw) / float64(len(pairs))
	st.AbsoluteDifference = &diff
	// A task-clustered paired bootstrap. Each task contributes its mean paired
	// difference; repetitions never masquerade as independent tasks.
	byTask := map[string][]float64{}
	for _, p := range pairs {
		a, b := get(p)
		d := 0.0
		if b {
			d++
		}
		if a {
			d--
		}
		byTask[p.TaskID] = append(byTask[p.TaskID], d)
	}
	tasks := make([]string, 0, len(byTask))
	for id := range byTask {
		tasks = append(tasks, id)
	}
	sort.Strings(tasks)
	st.Tasks = len(tasks)
	if len(tasks) < 2 {
		return st
	}
	boot := make([]float64, 0, 2000)
	rng := uint64(20260924)
	for i := 0; i < 2000; i++ {
		var sum float64
		for range tasks {
			id := tasks[int(rng%uint64(len(tasks)))]
			vals := byTask[id]
			sum += vals[int(rng%uint64(len(vals)))]
		}
		boot = append(boot, sum/float64(len(tasks)))
	}
	sort.Float64s(boot)
	lo, hi := percentile(boot, .025), percentile(boot, .975)
	st.CI95Low, st.CI95High = &lo, &hi
	if lo > 0 || hi < 0 {
		st.Verdict = "paired interval excludes zero; descriptive, not causal proof"
	} else {
		st.Verdict = "no statistically distinguishable difference at this sample size"
	}
	return st
}

func taskSummaries(results []Result) []TaskSummary {
	byTask := map[string]*TaskSummary{}
	for _, r := range results {
		if r.Status != RunCompleted && r.Status != RunTaskFailed && r.Status != RunTimeout {
			continue
		}
		t := byTask[r.TaskID]
		if t == nil {
			t = &TaskSummary{TaskID: r.TaskID}
			byTask[r.TaskID] = t
		}
		switch r.Mode {
		case Raw:
			if r.Derived.IndependentVerifiedSuccess {
				t.RawSuccesses++
			}
			if r.Derived.FalseSuccess {
				t.RawFalseSuccesses++
			}
		case Bounded:
			if r.Derived.IndependentVerifiedSuccess {
				t.BoundedSuccesses++
			}
			if r.Derived.FalseSuccess {
				t.BoundedFalseSuccesses++
			}
		}
	}
	out := make([]TaskSummary, 0, len(byTask))
	for _, t := range byTask {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// LoadResults reads result.json files below root. A missing result directory is
// an empty report, not a synthetic all-zero result.
func LoadResults(root string) ([]Result, error) {
	var out []Result
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "result.json" {
			return nil
		}
		r, err := LoadResult(path)
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# BoundedCode benchmark report\n\n")
	b.WriteString("This report describes infrastructure measurements. It does not, by itself, establish that one control architecture is better.\n\n")
	if len(r.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Aggregate modes\n\n")
	b.WriteString("| Mode | Evidence | Independently verified | Reported success | False success | False failure | Timeout | Infrastructure error |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, m := range r.Modes {
		fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s | %s | %s | %s |\n", m.Mode, m.EvidenceRuns, formatRate(m.VerifiedCompletion), formatRate(m.ReportedSuccess), formatRate(m.FalseSuccess), formatRate(m.FalseFailure), formatRate(m.Timeout), formatRate(m.InfrastructureError))
	}
	b.WriteString("\n## Secondary metrics\n\n| Mode | Median wall | p90 wall | Median input | Median output | Median total | Median generations | Median tool calls | Median retries | Median verification attempts |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, m := range r.Modes {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", m.Mode, formatNumber(m.WallClock.Median), formatNumber(m.WallClock.P90), formatNumber(m.InputTokens.Median), formatNumber(m.OutputTokens.Median), formatNumber(m.TotalTokens.Median), formatNumber(m.GenerationRequests.Median), formatNumber(m.ToolCalls.Median), formatNumber(m.Retries.Median), formatNumber(m.VerificationAttempts.Median))
	}
	b.WriteString("\n## Paired comparison\n\n")
	for _, p := range r.PairedStatistics {
		fmt.Fprintf(&b, "- **%s**: raw %s; bounded %s; bounded − raw %s; 95%% CI [%s, %s]; %s (%d tasks, %d pairs).\n", p.Metric, formatRate(p.RawRate), formatRate(p.BoundedRate), formatSigned(p.AbsoluteDifference), formatSigned(p.CI95Low), formatSigned(p.CI95High), p.Verdict, p.Tasks, p.PairedRuns)
	}
	b.WriteString("\n## Per-task / per-repetition\n\n| Pair | Task | Repetition | RAW | BOUNDED | RAW false success | BOUNDED false success | RAW wall | BOUNDED wall |\n|---|---|---:|---|---|---|---|---:|---:|\n")
	for _, p := range r.PairedRuns {
		fmt.Fprintf(&b, "| `%s` | `%s` | %d | %s | %s | %t | %t | %s | %s |\n", p.PairID, p.TaskID, p.Repetition, p.RawResult, p.BoundedResult, p.RawFalseSuccess, p.BoundedFalseSuccess, formatNumber(p.RawWallClock), formatNumber(p.BoundedWallClock))
	}
	if len(r.Tasks) > 0 {
		b.WriteString("\n## Task summary\n\n| Task | RAW successes | BOUNDED successes | RAW false successes | BOUNDED false successes |\n|---|---:|---:|---:|---:|\n")
		for _, t := range r.Tasks {
			fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d |\n", t.TaskID, t.RawSuccesses, t.BoundedSuccesses, t.RawFalseSuccesses, t.BoundedFalseSuccesses)
		}
	}
	b.WriteString("\n## Statistical interpretation\n\n")
	b.WriteString("Paired intervals resample tasks, not individual attempts. Small samples, infrastructure errors, timeouts, and incomparable fingerprints remain visible; they are not silently discarded or treated as performance evidence.\n")
	return b.String()
}

func formatRate(r Rate) string {
	if r.Value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", *r.Value*100, r.Numerator, r.Denominator)
}
func formatNumber(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *v)
}
func formatSigned(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%+.3f", *v)
}

// WriteReport writes machine and human reports beside one another.
func WriteReport(root string, report Report) (jsonPath, markdownPath string, err error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", "", err
	}
	body, err := report.JSON()
	if err != nil {
		return "", "", err
	}
	jsonPath, markdownPath = filepath.Join(root, "report.json"), filepath.Join(root, "report.md")
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0o600); err != nil {
		return "", "", err
	} //nolint:gosec // published report
	if err := os.WriteFile(markdownPath, []byte(report.Markdown()), 0o600); err != nil {
		return "", "", err
	} //nolint:gosec // published report
	return jsonPath, markdownPath, nil
}
