package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReportMDFile and ReportJSONFile are the canonical, generated campaign
// report destination under the guarded evidence run root — regenerated
// after every completed or skipped planned run by RunCampaign (design
// instruction, 3rd finalization pass, CHECK 2). Never
// docs/evidence/evidence-v1.md, which report.go's own RenderMarkdown doc
// comment already establishes as hand-maintained prose the automatic
// numbers are meant to feed into, not a file this package overwrites.
const (
	ReportMDFile   = "report.md"
	ReportJSONFile = "report.json"
)

// RefreshCampaignReport is the real
// LoadResultsForSuite -> BuildCampaignReportFromResults -> RenderMarkdown
// pipeline, run from persisted data on disk every time — never from an
// in-memory Result a caller happens to be holding. It is idempotent and
// safe to call after every single planned run, which is exactly how
// RunCampaign uses it: the report always reflects PARTIAL CAMPAIGN —
// N/16 completed until every non-invalid planned run has a terminal
// persisted result, at which point Partial becomes false automatically —
// no separate "final report" code path exists or is needed.
func RefreshCampaignReport(runRoot, suiteHash string, plans []RunPlan, led *Ledger) (ResultsCampaignReport, error) {
	current, err := led.Current()
	if err != nil {
		return ResultsCampaignReport{}, err
	}
	results, corrupted, err := LoadResultsForSuite(runRoot, suiteHash, plans, current)
	if err != nil {
		return ResultsCampaignReport{}, err
	}
	report := BuildCampaignReportFromResults(plans, results, corrupted)

	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		return report, err
	}
	if err := atomicWrite(filepath.Join(runRoot, ReportMDFile), []byte(report.RenderMarkdown())); err != nil {
		return report, err
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, err
	}
	if err := atomicWrite(filepath.Join(runRoot, ReportJSONFile), body); err != nil {
		return report, err
	}
	return report, nil
}

// LoadResultsForSuite discovers every valid, persisted terminal result for
// the frozen suite — design instruction GAP 4 §19/§20/§21: the run ledger
// (current) already resolves "which run ID is the valid terminal attempt
// for this task/arm" (an infrastructure retry's abandoned attempt is
// superseded the moment a later record for the same key is appended, and
// Ledger.Current always returns the latest), so this function does not
// re-implement that policy — it only loads and verifies the result
// Current() already says is the real one. A result that fails integrity
// verification is reported in corrupted rather than silently included or
// silently dropped.
func LoadResultsForSuite(runRoot, suiteHash string, plans []RunPlan, current map[string]Record) (results map[string]Result, corrupted []string, err error) {
	results = map[string]Result{}
	for _, p := range plans {
		key := p.Key()
		rec, ok := current[key]
		if !ok || !rec.Status.terminal() {
			continue // PENDING/RUNNING/INFRA_ERROR: nothing valid to load yet
		}
		res, lerr := LoadAndVerifyResult(runRoot, suiteHash, p.TaskID, p.Arm, rec.RunID)
		if lerr != nil {
			corrupted = append(corrupted, fmt.Sprintf("%s (run %s): %v", key, rec.RunID, lerr))
			continue
		}
		results[key] = res
	}
	return results, corrupted, nil
}

// CampaignReport is what automatic report generation (design instruction
// §26-§29) computes from the ledger alone — never invented, never
// hand-typed. Every number is either a real count from completed Record
// values or explicitly "N/A" because no completed run supports it yet.
type CampaignReport struct {
	Planned             int                    `json:"planned"`
	Complete            int                    `json:"complete"`
	Partial             bool                   `json:"partial_campaign"`
	ByStatus            map[Status]int         `json:"by_status"`
	SolvedBothArms      []string               `json:"solved_by_both,omitempty"`
	SolvedControlOnly   []string               `json:"solved_control_only,omitempty"`
	SolvedTreatmentOnly []string               `json:"solved_jev_treatment_only,omitempty"`
	SolvedNeither       []string               `json:"solved_by_neither,omitempty"`
	ByBenchmark         map[Benchmark]ArmTally `json:"by_benchmark"`
}

// ArmTally is a solved-count pair for one benchmark family or the overall
// suite — design instruction §25's per-family and overall summaries.
type ArmTally struct {
	Total           int `json:"total"`
	ControlSolved   int `json:"control_solved"`
	TreatmentSolved int `json:"treatment_solved"`
}

// BuildCampaignReport computes a CampaignReport from the frozen plan and
// the ledger's current state. It never presents a partial result as
// complete — design instruction §40: fewer than all non-invalid planned
// runs complete means Partial is true, and callers (the Markdown
// generator, the CLI) must label the output PARTIAL CAMPAIGN rather than
// a final Evidence Suite v1 result.
func BuildCampaignReport(plans []RunPlan, current map[string]Record) CampaignReport {
	r := CampaignReport{Planned: len(plans), ByStatus: map[Status]int{}, ByBenchmark: map[Benchmark]ArmTally{}}

	solvedByTaskArm := map[string]map[Arm]bool{}
	nonInvalidComplete := 0
	nonInvalidTotal := 0

	for _, p := range plans {
		rec, ok := current[p.Key()]
		status := StatusPending
		if ok {
			status = rec.Status
		}
		r.ByStatus[status]++

		if status != StatusInvalid {
			nonInvalidTotal++
			if status == StatusSolved || status == StatusFailed {
				nonInvalidComplete++
			}
		}

		if solvedByTaskArm[p.TaskID] == nil {
			solvedByTaskArm[p.TaskID] = map[Arm]bool{}
		}
		solvedByTaskArm[p.TaskID][p.Arm] = status == StatusSolved

		tally := r.ByBenchmark[p.Benchmark]
		if p.Arm == ArmControl {
			tally.Total++
		}
		if status == StatusSolved {
			if p.Arm == ArmControl {
				tally.ControlSolved++
			} else {
				tally.TreatmentSolved++
			}
		}
		r.ByBenchmark[p.Benchmark] = tally
	}

	r.Complete = nonInvalidComplete
	r.Partial = nonInvalidTotal == 0 || nonInvalidComplete < nonInvalidTotal

	taskIDs := uniqueTaskIDsInOrder(plans)
	for _, id := range taskIDs {
		arms := solvedByTaskArm[id]
		control, treatment := arms[ArmControl], arms[ArmExperimentalJevFull]
		switch {
		case control && treatment:
			r.SolvedBothArms = append(r.SolvedBothArms, id)
		case control && !treatment:
			r.SolvedControlOnly = append(r.SolvedControlOnly, id)
		case !control && treatment:
			r.SolvedTreatmentOnly = append(r.SolvedTreatmentOnly, id)
		default:
			r.SolvedNeither = append(r.SolvedNeither, id)
		}
	}
	return r
}

func uniqueTaskIDsInOrder(plans []RunPlan) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range Ordered(plans) {
		if !seen[p.TaskID] {
			seen[p.TaskID] = true
			out = append(out, p.TaskID)
		}
	}
	return out
}

// RenderMarkdown produces the GitHub-ready headline paragraph and
// aggregate table design instructions §26-28 ask for. It is deliberately
// small: the full docs/evidence/evidence-v1.md narrative remains
// hand-maintained prose around this table, since design instruction §26
// asks the *numbers* to be automatic, not the whole document's editorial
// voice.
func (r CampaignReport) RenderMarkdown() string {
	status := "PARTIAL CAMPAIGN"
	if !r.Partial {
		status = "COMPLETE"
	}
	out := fmt.Sprintf("**%s** — %d of %d planned runs complete.\n\n", status, r.Complete, r.Planned)
	out += "| Status | Count |\n|---|---:|\n"
	for _, s := range []Status{StatusPending, StatusRunning, StatusSolved, StatusFailed,
		StatusInfraErr, StatusInvalid} {
		out += fmt.Sprintf("| %s | %d |\n", s, r.ByStatus[s])
	}
	out += "\n| Category | Tasks |\n|---|---:|\n"
	out += fmt.Sprintf("| Solved by both | %d |\n", len(r.SolvedBothArms))
	out += fmt.Sprintf("| Solved control only | %d |\n", len(r.SolvedControlOnly))
	out += fmt.Sprintf("| Solved Jev treatment only | %d |\n", len(r.SolvedTreatmentOnly))
	out += fmt.Sprintf("| Solved by neither | %d |\n", len(r.SolvedNeither))
	return out
}

// AggregateTelemetry is §23's real, non-synthesized performance rollup —
// every field is a straight sum/count over loaded Results; a field with
// nothing behind it is left at its zero value rather than estimated.
type AggregateTelemetry struct {
	Attempts        int   `json:"attempts"`
	GeneratorTokens int   `json:"generator_tokens"`
	WallTimeMS      int64 `json:"wall_time_ms"`
}

// ResultsCampaignReport is CampaignReport's real-results counterpart —
// design instruction GAP 4 §19-§25/§27/§28: sourced from persisted
// Result.OfficialBenchmarkStatus (never Record.Status, which only
// controls resume), with real aggregate telemetry and an explicit
// corruption list rather than a silent skip.
type ResultsCampaignReport struct {
	CampaignReport
	Corrupted []string           `json:"corrupted,omitempty"`
	Aggregate AggregateTelemetry `json:"aggregate"`
}

// BuildCampaignReportFromResults is CampaignReport's real-results
// counterpart — design instruction GAP 4 §22/§23: "solved by both/
// control-only/treatment-only/neither" from official benchmark statuses,
// plus real, non-synthesized aggregate telemetry summed only over the
// Results that were actually loaded and verified.
func BuildCampaignReportFromResults(plans []RunPlan, results map[string]Result, corrupted []string) ResultsCampaignReport {
	r := ResultsCampaignReport{
		CampaignReport: CampaignReport{
			Planned: len(plans), ByStatus: map[Status]int{}, ByBenchmark: map[Benchmark]ArmTally{},
		},
		Corrupted: corrupted,
	}

	solvedByTaskArm := map[string]map[Arm]bool{}
	nonInvalidComplete, nonInvalidTotal := 0, 0

	for _, p := range plans {
		res, ok := results[p.Key()]
		status := StatusPending
		if ok {
			status = res.OfficialBenchmarkStatus
			r.Aggregate.Attempts += res.Attempts
			r.Aggregate.GeneratorTokens += res.TokensUsed
			r.Aggregate.WallTimeMS += res.WallTimeMS
		}
		r.ByStatus[status]++

		if status != StatusInvalid {
			nonInvalidTotal++
			if status == StatusSolved || status == StatusFailed {
				nonInvalidComplete++
			}
		}

		if solvedByTaskArm[p.TaskID] == nil {
			solvedByTaskArm[p.TaskID] = map[Arm]bool{}
		}
		solvedByTaskArm[p.TaskID][p.Arm] = status == StatusSolved

		tally := r.ByBenchmark[p.Benchmark]
		if p.Arm == ArmControl {
			tally.Total++
		}
		if status == StatusSolved {
			if p.Arm == ArmControl {
				tally.ControlSolved++
			} else {
				tally.TreatmentSolved++
			}
		}
		r.ByBenchmark[p.Benchmark] = tally
	}

	r.Complete = nonInvalidComplete
	r.Partial = nonInvalidTotal == 0 || nonInvalidComplete < nonInvalidTotal || len(corrupted) > 0

	for _, id := range uniqueTaskIDsInOrder(plans) {
		arms := solvedByTaskArm[id]
		control, treatment := arms[ArmControl], arms[ArmExperimentalJevFull]
		switch {
		case control && treatment:
			r.SolvedBothArms = append(r.SolvedBothArms, id)
		case control && !treatment:
			r.SolvedControlOnly = append(r.SolvedControlOnly, id)
		case !control && treatment:
			r.SolvedTreatmentOnly = append(r.SolvedTreatmentOnly, id)
		default:
			r.SolvedNeither = append(r.SolvedNeither, id)
		}
	}
	return r
}

// RenderMarkdown extends CampaignReport's rendering with the corruption
// and real-aggregate sections design instruction §21/§27/§28 ask for.
func (r ResultsCampaignReport) RenderMarkdown() string {
	out := r.CampaignReport.RenderMarkdown()
	if len(r.Corrupted) > 0 {
		out += fmt.Sprintf("\n**%d result(s) failed integrity verification and were excluded:**\n",
			len(r.Corrupted))
		for _, c := range r.Corrupted {
			out += "- " + c + "\n"
		}
	}
	out += fmt.Sprintf("\nAggregate (loaded results only): %d attempts, %d generator tokens, %dms wall time.\n",
		r.Aggregate.Attempts, r.Aggregate.GeneratorTokens, r.Aggregate.WallTimeMS)
	return out
}
