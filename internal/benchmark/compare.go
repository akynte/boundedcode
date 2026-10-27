package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
)

// Comparing a change with a baseline: each side is one or more runs of
// the same suite. A metric's change is only called a difference when the
// candidate's mean lies outside the range the baseline's own runs
// spanned; with one baseline run the noise is unknown and nothing is
// called a difference. Per task, the passes are counted on each side.

// Run is one saved report, reduced to what a comparison needs.
type Run struct {
	ID      string
	Kind    string             // "direct" or "bounded"
	Metrics map[string]float64 // by metric name
	Passed  map[string]bool    // task id -> passed (skipped tasks absent)
	Seconds map[string]float64 // task id -> wall seconds
}

// metric order and how each is shown; lower is better unless noted.
var metricOrder = []struct {
	name, format string
	higherBetter bool
}{
	{"pass rate", "%.0f%%", true},
	{"self-verified rate", "%.0f%%", true},
	{"false verifications", "%.0f", false},
	{"missed verifications", "%.0f", false},
	{"seconds per task", "%.1f", false},
	{"agent seconds per task", "%.1f", false},
	{"model seconds per task", "%.1f", false},
	{"tool seconds per task", "%.1f", false},
	{"harness seconds per task", "%.2f", false},
	{"verification seconds per task", "%.1f", false},
	{"time to first token, median ms", "%.0f", false},
	{"model calls per task", "%.2f", false},
	{"tool calls per task", "%.2f", false},
	{"attempts per task", "%.2f", false},
	{"prompt tokens per task", "%.0f", false},
	{"processed tokens per task", "%.0f", false},
	{"generated tokens per task", "%.0f", false},
	{"reasoning tokens per task", "%.0f", false},
	{"prompt-cache hit rate", "%.1f%%", true},
	{"cache invalidations", "%.0f", false},
	{"largest prompt", "%.0f", false},
	{"processed tokens per passed task", "%.0f", false},
	{"processed tokens per failed task", "%.0f", false},
	{"escalations", "%.0f", false},
	{"stalls", "%.0f", false},
	{"peak harness RSS MiB", "%.0f", false},
	{"peak model server RSS MiB", "%.0f", false},
	{"peak GPU memory MiB", "%.0f", false},
	{"peak sandbox memory MiB", "%.0f", false},
}

// LoadRun reads a direct-mode or bounded-suite report.
func LoadRun(path string) (*Run, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	switch {
	case strings.HasPrefix(probe.ID, "direct-"):
		var r DirectReport
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return directRun(&r), nil
	case strings.HasPrefix(probe.ID, "suite-"):
		var r SuiteReport
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return suiteRun(&r), nil
	}
	return nil, fmt.Errorf("%s: not a direct-mode or bounded-suite report", path)
}

func directRun(r *DirectReport) *Run {
	s := r.Summary
	run := &Run{ID: r.ID, Kind: "direct", Passed: map[string]bool{}, Seconds: map[string]float64{}, Metrics: map[string]float64{
		"pass rate":                        100 * s.PassRate,
		"seconds per task":                 s.MeanWallSeconds,
		"agent seconds per task":           s.MeanAgentSeconds,
		"model seconds per task":           s.MeanInferenceSeconds,
		"tool seconds per task":            s.MeanToolSeconds,
		"harness seconds per task":         s.MeanHarnessSeconds,
		"time to first token, median ms":   s.TTFTMedianMS,
		"model calls per task":             s.MeanCalls,
		"tool calls per task":              s.MeanToolCalls,
		"prompt tokens per task":           s.MeanPrompt,
		"generated tokens per task":        s.MeanGenerated,
		"reasoning tokens per task":        s.MeanReasoning,
		"prompt-cache hit rate":            100 * s.CacheHitRate,
		"cache invalidations":              float64(s.Invalidations),
		"largest prompt":                   float64(s.PeakPrompt),
		"processed tokens per passed task": s.PassedTokens,
		"processed tokens per failed task": s.FailedTokens,
		"peak harness RSS MiB":             s.PeakHarnessRSSMiB,
		"peak model server RSS MiB":        s.PeakServerRSSMiB,
		"peak GPU memory MiB":              s.PeakGPUMemMiB,
	}}
	stalls, processed := 0, 0.0
	for _, n := range s.Stalls {
		stalls += n
	}
	run.Metrics["stalls"] = float64(stalls)
	for _, t := range r.Results {
		if t.Skipped != "" {
			continue
		}
		run.Passed[t.ID] = t.Success
		run.Seconds[t.ID] = t.WallSeconds
		processed += float64(t.PromptTokens - t.CachedTokens + t.GeneratedTokens)
	}
	if s.Tasks > 0 {
		run.Metrics["processed tokens per task"] = processed / float64(s.Tasks)
	}
	return run
}

func suiteRun(r *SuiteReport) *Run {
	run := &Run{ID: r.ID, Kind: "bounded", Passed: map[string]bool{}, Seconds: map[string]float64{}, Metrics: map[string]float64{}}
	n := 0.0
	var selfVerified, falseV, missed, attempts, processed, generated, prompt, cached, verifySec, modelSec, esc, stalls float64
	var ttft []float64
	var peak ResourcePeaks
	for _, t := range r.Results {
		n++
		run.Passed[t.ID] = t.Success
		run.Seconds[t.ID] = t.WallSeconds
		if t.SelfVerified {
			selfVerified++
		}
		if t.SelfVerified && !t.Success {
			falseV++
		}
		if t.Success && !t.SelfVerified {
			missed++
		}
		attempts += float64(t.Attempts)
		processed += float64(t.LocalTokens)
		generated += float64(t.GeneratedTokens)
		prompt += float64(t.Calls.PromptTokens)
		cached += float64(t.CachedTokens)
		verifySec += t.VerifySeconds
		modelSec += t.Calls.InferenceSeconds
		esc += float64(t.Escalations)
		stalls += float64(len(t.Stalls))
		if t.Calls.TTFTMedianMS > 0 {
			ttft = append(ttft, t.Calls.TTFTMedianMS)
		}
		peak.HarnessRSSMiB = max(peak.HarnessRSSMiB, t.Resources.HarnessRSSMiB)
		peak.ServerRSSMiB = max(peak.ServerRSSMiB, t.Resources.ServerRSSMiB)
		peak.GPUMemMiB = max(peak.GPUMemMiB, t.Resources.GPUMemMiB)
		peak.ContainerMemMiB = max(peak.ContainerMemMiB, t.Resources.ContainerMemMiB)
	}
	if n == 0 {
		return run
	}
	m := run.Metrics
	m["pass rate"] = 100 * r.Summary.SuccessRate
	m["self-verified rate"] = 100 * selfVerified / n
	m["false verifications"], m["missed verifications"] = falseV, missed
	m["seconds per task"] = r.Summary.MeanWallSeconds
	m["model seconds per task"] = modelSec / n
	m["verification seconds per task"] = verifySec / n
	m["attempts per task"] = attempts / n
	m["processed tokens per task"] = processed / n
	m["generated tokens per task"] = generated / n
	if prompt > 0 {
		m["prompt tokens per task"] = prompt / n
		m["prompt-cache hit rate"] = 100 * cached / prompt
	}
	m["escalations"], m["stalls"] = esc, stalls
	if len(ttft) > 0 {
		sort.Float64s(ttft)
		m["time to first token, median ms"] = ttft[len(ttft)/2]
	}
	m["peak harness RSS MiB"], m["peak model server RSS MiB"] = peak.HarnessRSSMiB, peak.ServerRSSMiB
	m["peak GPU memory MiB"], m["peak sandbox memory MiB"] = peak.GPUMemMiB, peak.ContainerMemMiB
	return run
}

// Compare renders a candidate (one or more runs) against a baseline.
func Compare(w io.Writer, base, cand []*Run) {
	fmt.Fprintf(w, "# Comparison\n\nBaseline: %s. Candidate: %s.\n\n", runIDs(base), runIDs(cand))
	if len(base) < 2 {
		fmt.Fprintf(w, "With one baseline run its noise is unknown: no change below is called a difference. Run the baseline at least twice.\n\n")
	}
	fmt.Fprintf(w, "| Metric | Baseline mean (range) | Candidate mean (range) | Change | Verdict |\n|---|---|---|---|---|\n")
	for _, m := range metricOrder {
		bv, bok := values(base, m.name)
		cv, cok := values(cand, m.name)
		if !bok && !cok {
			continue
		}
		verdict, change := "", ""
		if bok && cok {
			bm, cm := mean(bv), mean(cv)
			if bm != 0 {
				change = fmt.Sprintf("%+.0f%%", 100*(cm-bm)/math.Abs(bm))
			} else if cm != 0 {
				change = "from 0"
			}
			lo, hi := minMax(bv)
			switch {
			case len(bv) < 2:
				verdict = "noise unknown"
			case cm >= lo && cm <= hi:
				verdict = "within baseline range"
			case (cm > hi) == m.higherBetter:
				verdict = "**better**, outside baseline range"
			default:
				verdict = "**worse**, outside baseline range"
			}
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", m.name, spread(bv, m.format), spread(cv, m.format), change, verdict)
	}
	fmt.Fprintf(w, "\n| Task | Baseline passed | Candidate passed | Baseline s | Candidate s |\n|---|---|---|---|---|\n")
	ids := map[string]bool{}
	for _, r := range append(append([]*Run(nil), base...), cand...) {
		for id := range r.Passed {
			ids[id] = true
		}
	}
	var sorted []string
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		bp, bn, bs := taskFigures(base, id)
		cp, cn, cs := taskFigures(cand, id)
		mark := ""
		if bn > 0 && cn > 0 && (bp == 0) != (cp == 0) {
			mark = " ←"
		}
		fmt.Fprintf(w, "| %s | %d/%d | %d/%d%s | %.0f | %.0f |\n", id, bp, bn, cp, cn, mark, bs, cs)
	}
}

func runIDs(rs []*Run) string {
	var ids []string
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ", ")
}

func values(rs []*Run, name string) ([]float64, bool) {
	var out []float64
	for _, r := range rs {
		if v, ok := r.Metrics[name]; ok {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func minMax(v []float64) (float64, float64) {
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = min(lo, x), max(hi, x)
	}
	return lo, hi
}

func spread(v []float64, format string) string {
	if len(v) == 0 {
		return "–"
	}
	s := fmt.Sprintf(format, mean(v))
	if len(v) > 1 {
		lo, hi := minMax(v)
		s += fmt.Sprintf(" ("+format+"–"+format+", n=%d)", lo, hi, len(v))
	}
	return s
}

// taskFigures: passes, runs that ran the task, mean seconds.
func taskFigures(rs []*Run, id string) (passed, ran int, seconds float64) {
	for _, r := range rs {
		if p, ok := r.Passed[id]; ok {
			ran++
			if p {
				passed++
			}
			seconds += r.Seconds[id]
		}
	}
	if ran > 0 {
		seconds /= float64(ran)
	}
	return passed, ran, seconds
}
