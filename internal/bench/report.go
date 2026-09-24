package bench

import (
	"context"
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
	EvaluatorTimeout     Rate           `json:"evaluator_timeout"`
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
	Comparable            bool     `json:"comparable"`
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
	OfficialResults     bool              `json:"official_results"`
	SmokeResults        bool              `json:"smoke_results"`
	MixedProvenance     bool              `json:"mixed_provenance"`
	PerformanceEvidence bool              `json:"performance_evidence_eligible"`
	ScheduleVerified    bool              `json:"schedule_verified"`
	ManifestHashes      []string          `json:"manifest_hashes,omitempty"`
	Modes               []ModeSummary     `json:"modes"`
	PairedStatistics    []PairedStatistic `json:"paired_statistics"`
	PairedRuns          []PairedSummary   `json:"paired_runs"`
	Tasks               []TaskSummary     `json:"tasks"`
	IncomparablePairs   int               `json:"incomparable_pairs"`
	IncompletePairs     int               `json:"incomplete_pairs"`
	Warnings            []string          `json:"warnings"`
	Results             []Result          `json:"results,omitempty"`
	scheduleEligible    bool
}

const ReportSchemaVersion = 1

// BuildReport computes only from durable Result files. It never reruns a
// worker and never treats a missing metric as zero.
func BuildReport(results []Result) Report {
	safeResults := make([]Result, 0, len(results))
	for _, result := range results {
		safeResults = append(safeResults, RedactResult(result))
	}
	results = safeResults
	for i := range results {
		results[i].Derive()
	}
	rep := Report{ReportSchemaVersion: ReportSchemaVersion, GeneratedAt: time.Now().UTC(), Results: append([]Result(nil), results...)}
	analysisResults := make([]Result, 0, len(results))
	seenAnalysisCell := map[string]bool{}
	for _, result := range results {
		cell := result.PairID + "\x00" + string(result.Mode)
		if seenAnalysisCell[cell] {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("duplicate result for pair/mode %q/%q was excluded from aggregate rates", result.PairID, result.Mode))
			continue
		}
		seenAnalysisCell[cell] = true
		analysisResults = append(analysisResults, result)
	}
	suiteSet := map[string]bool{}
	hashSet := map[string]bool{}
	provenanceKinds := map[string]map[string]bool{}
	for _, r := range results {
		suiteSet[r.BenchmarkSuite] = true
		rep.OfficialResults = rep.OfficialResults || r.Official
		rep.SmokeResults = rep.SmokeResults || r.Smoke
		kind := "ordinary"
		if r.Official {
			kind = "official"
		} else if r.Smoke {
			kind = "smoke"
		}
		if provenanceKinds[r.BenchmarkSuite] == nil {
			provenanceKinds[r.BenchmarkSuite] = map[string]bool{}
		}
		provenanceKinds[r.BenchmarkSuite][kind] = true
		identity := r.ManifestHash
		if identity == "" {
			identity = r.SuiteHash
		}
		if identity != "" {
			hashSet[identity] = true
		}
	}
	for _, kinds := range provenanceKinds {
		if len(kinds) > 1 {
			rep.MixedProvenance = true
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
	rep.MixedProvenance = rep.MixedProvenance || (rep.OfficialResults && rep.SmokeResults) || len(rep.SuiteIDs) > 1
	rep.PerformanceEvidence = len(results) > 0 && !rep.SmokeResults && !rep.MixedProvenance && rep.OfficialResults
	for _, result := range results {
		if !result.Evaluator.Independent {
			rep.PerformanceEvidence = false
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("result %s used an unconfined or non-independent evaluator", result.RunID))
		}
		if result.Evaluator.BaselineStatus != "" && result.Evaluator.BaselineStatus != EvaluatorFail {
			rep.PerformanceEvidence = false
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("result %s was not qualified by a failing evaluator baseline", result.RunID))
		}
	}
	if rep.SmokeResults {
		rep.Warnings = append(rep.Warnings, "this report contains NON-OFFICIAL smoke results and is not performance evidence")
	}
	if rep.OfficialResults {
		for _, result := range results {
			if result.Official && strings.TrimSpace(result.ManifestHash) == "" {
				rep.PerformanceEvidence = false
				rep.Warnings = append(rep.Warnings, "an official result has no frozen manifest identity")
				break
			}
		}
	}
	if rep.MixedProvenance {
		rep.Warnings = append(rep.Warnings, "this report mixes provenance or suite identities and is not a single official experiment")
	}
	if len(rep.ManifestHashes) > 1 {
		rep.PerformanceEvidence = false
		rep.Warnings = append(rep.Warnings, "results were generated against different manifest/suite identities and are not a single frozen experiment")
	}
	for _, mode := range []Mode{Raw, Bounded} {
		rep.Modes = append(rep.Modes, summarizeMode(mode, analysisResults))
	}
	rep.PairedRuns, rep.PairedStatistics = paired(analysisResults)
	byPair := map[string]map[Mode]Result{}
	duplicateCells := map[string]bool{}
	for _, r := range results {
		if byPair[r.PairID] == nil {
			byPair[r.PairID] = map[Mode]Result{}
		}
		cell := r.PairID + "\x00" + string(r.Mode)
		if _, exists := byPair[r.PairID][r.Mode]; exists {
			duplicateCells[cell] = true
			continue
		}
		byPair[r.PairID][r.Mode] = r
	}
	for pair, arms := range byPair {
		if len(arms) < 2 {
			rep.IncompletePairs++
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("pair %s is incomplete; no paired statistic is reported", pair))
		}
	}
	for i := range rep.PairedRuns {
		pair := &rep.PairedRuns[i]
		if duplicateCells[pair.PairID+"\x00"+string(Raw)] || duplicateCells[pair.PairID+"\x00"+string(Bounded)] {
			pair.Comparable = false
			pair.ComparabilityWarnings = append(pair.ComparabilityWarnings, "duplicate result for a paired arm")
		}
		cmp := CompareResultPair(byPair[pair.PairID][Raw], byPair[pair.PairID][Bounded])
		if !cmp.OK || !pair.Comparable {
			rep.IncomparablePairs++
			if len(cmp.Reasons) > 0 {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("pair %s is incomparable: %s", pair.PairID, strings.Join(cmp.Reasons, "; ")))
			}
		}
		for _, warning := range cmp.Warnings {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("pair %s: %s", pair.PairID, warning))
		}
	}
	if rep.IncomparablePairs > 0 || rep.IncompletePairs > 0 || len(rep.PairedRuns) == 0 {
		rep.PerformanceEvidence = false
		rep.Warnings = append(rep.Warnings, "performance evidence is ineligible because the result set has incomplete or incomparable pairs")
	}
	rep.Tasks = taskSummaries(analysisResults)
	rep.scheduleEligible = rep.PerformanceEvidence
	rep.ScheduleVerified = false
	rep.PerformanceEvidence = false
	return rep
}

// AddScheduleCoverage makes missing cells visible when a report is generated
// beside a durable schedule. BuildReport remains useful for ad-hoc result
// directories, but a scheduled experiment must not silently omit an entire
// pair because no result file was written for it.
func AddScheduleCoverage(report Report, schedule Schedule) Report {
	// BuildReport defers the intrinsic evidence decision until a durable
	// schedule is available. Capture it before clearing the public flags so a
	// report cannot accidentally retain a value from an earlier coverage pass.
	baseEligible := report.scheduleEligible || report.PerformanceEvidence
	report.ScheduleVerified = false
	report.PerformanceEvidence = false
	if err := schedule.Validate(); err != nil {
		report.Warnings = append(report.Warnings, "schedule coverage unavailable: "+err.Error())
		return report
	}

	// A cell is the same identity used by Schedule.Validate: a pair and a mode.
	// Keep the complete RunSpec rather than a subset of its fields so a result
	// with the right pair but a stale run/index/task/seed cannot satisfy it.
	expected := make(map[string]RunSpec, len(schedule.Entries))
	expectedPairs := make(map[string]map[Mode]bool)
	cellKey := func(pair string, mode Mode) string { return pair + "\x00" + string(mode) }
	for _, entry := range schedule.Entries {
		expected[cellKey(entry.PairID, entry.Mode)] = entry
		if expectedPairs[entry.PairID] == nil {
			expectedPairs[entry.PairID] = make(map[Mode]bool)
		}
		expectedPairs[entry.PairID][entry.Mode] = true
	}

	seen := make(map[string]bool, len(expected))
	covered := make(map[string]bool, len(expected))
	coverageOK := true
	actualPairs := make(map[string]map[Mode]bool)
	for _, result := range report.Results {
		if result.PairID != "" {
			if actualPairs[result.PairID] == nil {
				actualPairs[result.PairID] = make(map[Mode]bool)
			}
			actualPairs[result.PairID][result.Mode] = true
		}
		key := cellKey(result.PairID, result.Mode)
		entry, scheduled := expected[key]
		if !scheduled {
			coverageOK = false
			report.Warnings = append(report.Warnings, fmt.Sprintf("result %s/%s is not a cell in the durable schedule", result.PairID, result.Mode))
			continue
		}
		if seen[key] {
			coverageOK = false
			report.Warnings = append(report.Warnings, fmt.Sprintf("scheduled cell %s/%s has more than one result", result.PairID, result.Mode))
			continue
		}
		seen[key] = true
		// resultMatchesSpec is the canonical durable identity check used by
		// resume. The suite-level fields are checked separately because a
		// schedule can be syntactically valid while belonging to another
		// suite/version than the result set.
		if !resultMatchesSpec(result, entry) || result.BenchmarkSuite != schedule.SuiteID || result.SuiteVersion != schedule.SuiteVersion {
			coverageOK = false
			report.Warnings = append(report.Warnings, fmt.Sprintf("scheduled cell %s/%s has inconsistent identity fields", result.PairID, result.Mode))
			continue
		}
		covered[key] = true
	}

	// Recompute incomplete pairs as a set. BuildReport already counts an
	// incomplete pair that has one arm; adding to that integer here would
	// double-count it. The set also includes pairs that are missing entirely
	// from a durable result directory.
	incompletePairs := make(map[string]bool)
	for pair, modes := range actualPairs {
		if len(modes) < 2 {
			incompletePairs[pair] = true
		}
	}
	expectedPairIDs := make([]string, 0, len(expectedPairs))
	for pair := range expectedPairs {
		expectedPairIDs = append(expectedPairIDs, pair)
	}
	sort.Strings(expectedPairIDs)
	for _, pair := range expectedPairIDs {
		for mode := range expectedPairs[pair] {
			if !covered[cellKey(pair, mode)] {
				incompletePairs[pair] = true
				break
			}
		}
	}
	report.IncompletePairs = len(incompletePairs)
	for _, pair := range expectedPairIDs {
		complete := true
		for mode := range expectedPairs[pair] {
			if !covered[cellKey(pair, mode)] {
				complete = false
				break
			}
		}
		if !complete {
			coverageOK = false
			report.Warnings = append(report.Warnings, fmt.Sprintf("scheduled pair %s is missing one or more exact result cells", pair))
		}
	}

	report.ScheduleVerified = coverageOK
	report.PerformanceEvidence = coverageOK && baseEligible
	return report
}

func summarizeMode(mode Mode, results []Result) ModeSummary {
	s := ModeSummary{Mode: mode}
	var evidence, verified, reported, falseSuccess, falseFailure, timeout, infra, evaluatorTimeout int
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
		if r.Status == RunEvaluatorTimeout {
			evaluatorTimeout++
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
	s.Timeout, s.InfrastructureError, s.EvaluatorTimeout = rate(timeout, s.Runs), rate(infra, s.Runs), rate(evaluatorTimeout, s.Runs)
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
		p := PairedSummary{PairID: key, TaskID: raw.TaskID, Repetition: raw.Repetition, Comparable: true}
		p.RawResult, p.BoundedResult = string(raw.Status), string(bounded.Status)
		p.RawVerified, p.BoundedVerified = raw.Derived.IndependentVerifiedSuccess, bounded.Derived.IndependentVerifiedSuccess
		p.RawFalseSuccess, p.BoundedFalseSuccess = raw.Derived.FalseSuccess, bounded.Derived.FalseSuccess
		p.RawWallClock, p.BoundedWallClock = floatPtr(raw.Execution.WallClockSeconds), floatPtr(bounded.Execution.WallClockSeconds)
		cmp := CompareResultPair(raw, bounded)
		p.Comparable = cmp.OK
		p.ComparabilityWarnings = append(p.ComparabilityWarnings, cmp.Reasons...)
		p.ComparabilityWarnings = append(p.ComparabilityWarnings, cmp.Warnings...)
		if !isEvidenceStatus(raw.Status) || !isEvidenceStatus(bounded.Status) {
			p.Comparable = false
			p.ComparabilityWarnings = append(p.ComparabilityWarnings, "one or both arms are not eligible evidence")
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

func isEvidenceStatus(status RunStatus) bool {
	return status == RunCompleted || status == RunTaskFailed || status == RunTimeout
}

func pairedRate(metric string, pairs []PairedSummary, get func(PairedSummary) (bool, bool)) PairedStatistic {
	st := PairedStatistic{Metric: metric, Verdict: "insufficient sample size for meaningful statistical conclusion"}
	valid := make([]PairedSummary, 0, len(pairs))
	for _, p := range pairs {
		if p.Comparable && (p.RawResult == string(RunCompleted) || p.RawResult == string(RunTaskFailed) || p.RawResult == string(RunTimeout)) &&
			(p.BoundedResult == string(RunCompleted) || p.BoundedResult == string(RunTaskFailed) || p.BoundedResult == string(RunTimeout)) {
			valid = append(valid, p)
		}
	}
	raw, bounded := 0, 0
	for _, p := range valid {
		a, b := get(p)
		if a {
			raw++
		}
		if b {
			bounded++
		}
	}
	st.RawRate, st.BoundedRate, st.PairedRuns = rate(raw, len(valid)), rate(bounded, len(valid)), len(valid)
	if len(valid) == 0 {
		return st
	}
	diff := float64(bounded-raw) / float64(len(valid))
	st.AbsoluteDifference = &diff
	// A task-clustered paired bootstrap. Each draw samples a task and one of
	// its repetitions; repetitions never masquerade as independent tasks.
	byTask := map[string][]float64{}
	for _, p := range valid {
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
	next := func() uint64 {
		// xorshift64* is tiny, deterministic, and unlike the old fixed modulo
		// sequence it gives each bootstrap draw an independent stream position.
		rng ^= rng >> 12
		rng ^= rng << 25
		rng ^= rng >> 27
		return rng * 2685821657736338717
	}
	for i := 0; i < 2000; i++ {
		var sum float64
		for range tasks {
			id := tasks[int(next()%uint64(len(tasks)))] //nolint:gosec // modulo is bounded by len(tasks)
			vals := byTask[id]
			sum += vals[int(next()%uint64(len(vals)))] //nolint:gosec // modulo is bounded by len(vals)
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
	repetitions := map[string]map[string]bool{}
	for _, r := range results {
		if r.Status != RunCompleted && r.Status != RunTaskFailed && r.Status != RunTimeout {
			continue
		}
		t := byTask[r.TaskID]
		if t == nil {
			t = &TaskSummary{TaskID: r.TaskID}
			byTask[r.TaskID] = t
			repetitions[r.TaskID] = map[string]bool{}
		}
		repetitions[r.TaskID][r.PairID] = true
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
	for id, t := range byTask {
		t.Repetitions = len(repetitions[id])
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// LoadResults reads result.json files below root. A missing result directory is
// an empty report, not a synthetic all-zero result.
func LoadResults(root string) ([]Result, error) {
	var out []Result
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.Contains(d.Name(), ".interrupted-") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "result.json" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 || parts[3] != "result.json" {
			// A worker can create arbitrary result.json files below its
			// workspace. Only the harness's suite/task/mode/run layout is a
			// benchmark result.
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
	fmt.Fprintf(&b, "Performance-evidence eligible: **%t**\n\n", r.PerformanceEvidence)
	fmt.Fprintf(&b, "Provenance: official=%t smoke=%t mixed=%t schedule_verified=%t; suites=%s; manifests=%s; incomplete_pairs=%d incomparable_pairs=%d\n\n", r.OfficialResults, r.SmokeResults, r.MixedProvenance, r.ScheduleVerified, formatStrings(r.SuiteIDs), formatStrings(r.ManifestHashes), r.IncompletePairs, r.IncomparablePairs)
	if len(r.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Aggregate modes\n\n")
	b.WriteString("| Mode | Evidence | Independently verified | Reported success | False success | False failure | Timeout | Evaluator timeout | Infrastructure error |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, m := range r.Modes {
		fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s | %s | %s | %s | %s |\n", m.Mode, m.EvidenceRuns, formatRate(m.VerifiedCompletion), formatRate(m.ReportedSuccess), formatRate(m.FalseSuccess), formatRate(m.FalseFailure), formatRate(m.Timeout), formatRate(m.EvaluatorTimeout), formatRate(m.InfrastructureError))
	}
	b.WriteString("\n## Secondary metrics\n\n| Mode | Median wall | p90 wall | Median input | Median output | Median total | Median generations | Median tool calls | Median retries | Median verification attempts |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, m := range r.Modes {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", m.Mode, formatNumber(m.WallClock.Median), formatNumber(m.WallClock.P90), formatNumber(m.InputTokens.Median), formatNumber(m.OutputTokens.Median), formatNumber(m.TotalTokens.Median), formatNumber(m.GenerationRequests.Median), formatNumber(m.ToolCalls.Median), formatNumber(m.Retries.Median), formatNumber(m.VerificationAttempts.Median))
	}
	b.WriteString("\n## Paired comparison\n\n")
	for _, p := range r.PairedStatistics {
		fmt.Fprintf(&b, "- **%s**: raw %s; bounded %s; bounded − raw %s; 95%% CI [%s, %s]; %s (%d tasks, %d pairs).\n", p.Metric, formatRate(p.RawRate), formatRate(p.BoundedRate), formatSigned(p.AbsoluteDifference), formatSigned(p.CI95Low), formatSigned(p.CI95High), p.Verdict, p.Tasks, p.PairedRuns)
	}
	b.WriteString("\n## Per-task / per-repetition\n\n| Pair | Task | Repetition | Comparable | RAW | BOUNDED | RAW false success | BOUNDED false success | RAW wall | BOUNDED wall |\n|---|---|---:|---|---|---|---|---|---:|---:|\n")
	for _, p := range r.PairedRuns {
		fmt.Fprintf(&b, "| `%s` | `%s` | %d | %t | %s | %s | %t | %t | %s | %s |\n", p.PairID, p.TaskID, p.Repetition, p.Comparable, p.RawResult, p.BoundedResult, p.RawFalseSuccess, p.BoundedFalseSuccess, formatNumber(p.RawWallClock), formatNumber(p.BoundedWallClock))
	}
	if len(r.Tasks) > 0 {
		b.WriteString("\n## Task summary\n\n| Task | Repetitions | RAW successes | BOUNDED successes | RAW false successes | BOUNDED false successes |\n|---|---:|---:|---:|---:|---:|\n")
		for _, t := range r.Tasks {
			fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d |\n", t.TaskID, t.Repetitions, t.RawSuccesses, t.BoundedSuccesses, t.RawFalseSuccesses, t.BoundedFalseSuccesses)
		}
	}
	b.WriteString("\n## Statistical interpretation\n\n")
	b.WriteString("Paired intervals resample tasks, not individual attempts. Small samples, infrastructure errors, timeouts, and incomparable fingerprints remain visible; they are not silently discarded or treated as performance evidence.\n")
	return b.String()
}

func formatStrings(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
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
	return jsonPath, markdownPath, withOutputLock(context.Background(), root, func() error {
		var err error
		jsonPath, markdownPath, err = writeReportUnlocked(root, report)
		return err
	})
}

func writeReportUnlocked(root string, report Report) (jsonPath, markdownPath string, err error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", "", err
	}
	body, err := report.JSON()
	if err != nil {
		return "", "", err
	}
	jsonPath, markdownPath = filepath.Join(root, "report.json"), filepath.Join(root, "report.md")
	if err := writeAtomicFile(jsonPath, append(body, '\n'), 0o600); err != nil {
		return "", "", err
	}
	if err := writeAtomicFile(markdownPath, []byte(report.Markdown()), 0o600); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

// ReportDirectory takes a locked snapshot of a results tree and optionally
// writes the human/machine reports before releasing the lock.
func ReportDirectory(root string, write bool) (Report, error) {
	var report Report
	err := withOutputLock(context.Background(), root, func() error {
		results, err := LoadResults(root)
		if err != nil {
			return err
		}
		report = BuildReport(results)
		schedulePath := filepath.Join(root, "schedule.json")
		if _, statErr := os.Stat(schedulePath); statErr == nil {
			schedule, err := LoadSchedule(schedulePath)
			if err != nil {
				return err
			}
			report = AddScheduleCoverage(report, schedule)
		} else if !os.IsNotExist(statErr) {
			return statErr
		} else {
			report.PerformanceEvidence = false
			report.Warnings = append(report.Warnings, "no durable schedule was found; result-set completeness cannot be proven")
		}
		if write {
			_, _, err = writeReportUnlocked(root, report)
		}
		return err
	})
	return report, err
}
