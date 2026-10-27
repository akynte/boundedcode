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
// the same suite. Whole-run means move a lot between runs of the same
// build (one long task dominates them), so verdicts rest on paired,
// per-task figures: for each task, the candidate's mean over its runs
// divided by the baseline's, and the median of those ratios over the
// tasks. The noise band is the baseline's own: each baseline run compared
// with the others the same way (leave one out), widened by its width on
// both sides and to at least minBand around 1. A ratio outside it is
// called better or worse; with fewer than three baseline runs there is no
// band and no verdict. Pass and fail are compared per task, and a task is
// called regressed or fixed only when it was consistent across every run
// on each side, with at least two candidate runs.

// minBand is the smallest change a verdict can be given for. An A/A check
// of the 2026-10 baseline (a fourth run of the same build against the
// first three) put two of eight paired metrics just outside a band without
// this floor (1.09 against 1.07, 1.03 against 1.02);
// docs/benchmarks/baseline-2026-10.md.
const minBand = 0.10

// Run is one saved report, reduced to what a comparison needs.
type Run struct {
	ID      string
	Kind    string             // "direct" or "bounded"
	Metrics map[string]float64 // whole-run figures, descriptive
	Passed  map[string]bool    // task id -> passed (skipped tasks absent)
	// Task holds each task's paired figures, by metric name.
	Task map[string]map[string]float64
}

// summaryOrder lists the whole-run figures shown, in order.
var summaryOrder = []struct{ name, format string }{
	{"pass rate", "%.0f%%"},
	{"self-verified rate", "%.0f%%"},
	{"false verifications", "%.0f"},
	{"missed verifications", "%.0f"},
	{"seconds per task", "%.1f"},
	{"model seconds per task", "%.1f"},
	{"tool seconds per task", "%.2f"},
	{"harness seconds per task", "%.3f"},
	{"verification seconds per task", "%.1f"},
	{"time to first token, median ms", "%.0f"},
	{"model calls per task", "%.2f"},
	{"tool calls per task", "%.2f"},
	{"attempts per task", "%.2f"},
	{"prompt tokens per task", "%.0f"},
	{"processed tokens per task", "%.0f"},
	{"generated tokens per task", "%.0f"},
	{"reasoning tokens per task", "%.0f"},
	{"prompt-cache hit rate", "%.1f%%"},
	{"cache invalidations", "%.0f"},
	{"largest prompt", "%.0f"},
	{"processed tokens per passed task", "%.0f"},
	{"processed tokens per failed task", "%.0f"},
	{"escalations", "%.0f"},
	{"stalls", "%.0f"},
	{"peak harness RSS MiB", "%.0f"},
	{"peak model server RSS MiB", "%.0f"},
	{"peak GPU memory MiB", "%.0f"},
	{"peak sandbox memory MiB", "%.0f"},
}

// pairedOrder lists the per-task figures compared with a verdict (lower
// is better for all of them).
var pairedOrder = []string{"seconds", "model seconds", "model calls", "tool calls", "attempts", "verification seconds",
	"prompt tokens", "processed tokens", "generated tokens", "reasoning tokens"}

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
	run := &Run{ID: r.ID, Kind: "direct", Passed: map[string]bool{}, Task: map[string]map[string]float64{}, Metrics: map[string]float64{
		"pass rate":                        100 * s.PassRate,
		"seconds per task":                 s.MeanWallSeconds,
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
		p := float64(t.PromptTokens - t.CachedTokens + t.GeneratedTokens)
		processed += p
		run.Task[t.ID] = map[string]float64{
			"seconds": t.WallSeconds, "model seconds": t.CallStats.InferenceSeconds, "model calls": float64(t.Calls),
			"tool calls": float64(t.ToolCalls), "prompt tokens": float64(t.PromptTokens), "processed tokens": p,
			"generated tokens": float64(t.GeneratedTokens), "reasoning tokens": float64(t.CallStats.ReasoningTokens),
		}
	}
	if s.Tasks > 0 {
		run.Metrics["processed tokens per task"] = processed / float64(s.Tasks)
	}
	return run
}

func suiteRun(r *SuiteReport) *Run {
	run := &Run{ID: r.ID, Kind: "bounded", Passed: map[string]bool{}, Task: map[string]map[string]float64{}, Metrics: map[string]float64{}}
	n := 0.0
	var selfVerified, falseV, missed, attempts, processed, generated, prompt, cached, verifySec, modelSec, esc, stalls float64
	var ttft []float64
	var peak ResourcePeaks
	for _, t := range r.Results {
		n++
		run.Passed[t.ID] = t.Success
		run.Task[t.ID] = map[string]float64{
			"seconds": t.WallSeconds, "model seconds": t.Calls.InferenceSeconds, "model calls": float64(t.Calls.Calls),
			"attempts": float64(t.Attempts), "verification seconds": t.VerifySeconds, "prompt tokens": float64(t.Calls.PromptTokens),
			"processed tokens": float64(t.LocalTokens), "generated tokens": float64(t.GeneratedTokens),
		}
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

	fmt.Fprintf(w, "## Per task (paired)\n\nFor each task, the candidate's mean over its runs divided by the baseline's; the median of those ratios over the tasks both sides ran. ")
	if len(base) < 3 {
		fmt.Fprintf(w, "The baseline has %d run(s): its noise band needs at least 3, so no change is called a difference.\n\n", len(base))
	} else {
		fmt.Fprintf(w, "The noise band is the baseline's own (each of its runs against the others), widened by its width on both sides and to at least ±%.0f%%.\n\n", 100*minBand)
	}
	fmt.Fprintf(w, "| Metric | Median ratio | Tasks | Noise band | Verdict |\n|---|---|---|---|---|\n")
	for _, m := range pairedOrder {
		ratio, n := medianRatio(base, cand, m)
		if n == 0 {
			continue
		}
		band, verdict := "–", "noise unknown"
		if lo, hi, ok := noiseBand(base, m); ok {
			band = fmt.Sprintf("%.2f–%.2f", lo, hi)
			switch {
			case ratio < lo:
				verdict = "**better**"
			case ratio > hi:
				verdict = "**worse**"
			default:
				verdict = "within noise"
			}
		}
		fmt.Fprintf(w, "| %s | %.2f | %d | %s | %s |\n", m, ratio, n, band, verdict)
	}

	fmt.Fprintf(w, "\n## Pass and fail per task\n\n")
	var regressed, fixed []string
	fmt.Fprintf(w, "| Task | Baseline passed | Candidate passed | Note |\n|---|---|---|---|\n")
	for _, id := range taskIDs(base, cand) {
		bp, bn := passes(base, id)
		cp, cn := passes(cand, id)
		note := ""
		switch {
		case bn > 0 && cn > 1 && bp == bn && cp == 0:
			note = "regressed"
			regressed = append(regressed, id)
		case bn > 0 && cn > 1 && bp == 0 && cp == cn:
			note = "fixed"
			fixed = append(fixed, id)
		case bn > 0 && cn == 1 && bp == bn && cp == 0:
			note = "failed in the only candidate run (baseline always passed)"
		case bn > 0 && cn == 1 && bp == 0 && cp == 1:
			note = "passed in the only candidate run (baseline always failed)"
		}
		fmt.Fprintf(w, "| %s | %d/%d | %d/%d | %s |\n", id, bp, bn, cp, cn, note)
	}
	if len(cand) < 2 {
		fmt.Fprintf(w, "\nWith one candidate run no task is called regressed or fixed.\n")
	} else {
		fmt.Fprintf(w, "\nTasks that always passed in the baseline and always failed in the candidate: %s. The reverse: %s.\n",
			orNone(regressed), orNone(fixed))
	}

	fmt.Fprintf(w, "\n## Whole-run figures (descriptive)\n\nMeans over each side's runs, with their range. They are not judged: one long task moves them.\n\n")
	fmt.Fprintf(w, "| Figure | Baseline mean (range) | Candidate mean (range) | Change |\n|---|---|---|---|\n")
	for _, m := range summaryOrder {
		bv, bok := values(base, m.name)
		cv, cok := values(cand, m.name)
		if !bok && !cok {
			continue
		}
		change := ""
		if bok && cok {
			if bm, cm := mean(bv), mean(cv); bm != 0 {
				change = fmt.Sprintf("%+.0f%%", 100*(cm-bm)/math.Abs(bm))
			}
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s |\n", m.name, spread(bv, m.format), spread(cv, m.format), change)
	}
}

// medianRatio is the median over tasks of mean(cand)/mean(base) for a
// metric, over the tasks both sides ran whose baseline mean is positive.
func medianRatio(base, cand []*Run, metric string) (float64, int) {
	var ratios []float64
	for _, id := range taskIDs(base, cand) {
		b, bok := taskMean(base, id, metric)
		c, cok := taskMean(cand, id, metric)
		if bok && cok && b > 0 {
			ratios = append(ratios, c/b)
		}
	}
	if len(ratios) == 0 {
		return 0, 0
	}
	sort.Float64s(ratios)
	mid := len(ratios) / 2
	if len(ratios)%2 == 0 {
		return (ratios[mid-1] + ratios[mid]) / 2, len(ratios)
	}
	return ratios[mid], len(ratios)
}

// noiseBand compares each baseline run with the others and widens the
// range of those ratios by its width on both sides (at least 3 runs).
func noiseBand(base []*Run, metric string) (lo, hi float64, ok bool) {
	if len(base) < 3 {
		return 0, 0, false
	}
	var rs []float64
	for i := range base {
		rest := append(append([]*Run(nil), base[:i]...), base[i+1:]...)
		if r, n := medianRatio(rest, base[i:i+1], metric); n > 0 {
			rs = append(rs, r)
		}
	}
	if len(rs) < 3 {
		return 0, 0, false
	}
	lo, hi = minMax(rs)
	width := hi - lo
	return min(lo-width, 1-minBand), max(hi+width, 1+minBand), true
}

func taskMean(rs []*Run, id, metric string) (float64, bool) {
	sum, n := 0.0, 0
	for _, r := range rs {
		if t, ok := r.Task[id]; ok {
			if v, ok := t[metric]; ok {
				sum += v
				n++
			}
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func taskIDs(sides ...[]*Run) []string {
	seen := map[string]bool{}
	var ids []string
	for _, rs := range sides {
		for _, r := range rs {
			for id := range r.Passed {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func passes(rs []*Run, id string) (passed, ran int) {
	for _, r := range rs {
		if p, ok := r.Passed[id]; ok {
			ran++
			if p {
				passed++
			}
		}
	}
	return passed, ran
}

func orNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
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
