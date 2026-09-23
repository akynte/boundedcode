package eval

import (
	"testing"
	"time"
)

func metricRun(task, arm string, rep int, solved bool, recall float64, tokens int, secs float64) Outcome {
	o := Outcome{
		TaskID: task, Arm: arm, Repetition: rep, Solved: solved,
		Status: StatusCompleted, PacketTokens: tokens,
		Duration: time.Duration(secs * float64(time.Second)),
	}
	if recall >= 0 {
		o.Localization = &LocalizationScore{
			Status: LocalizationScored, Recall: recall, Precision: recall,
			GeneratorRecall: recall, PrecisionDenominator: 1, Expected: 1,
		}
	}
	return o
}

// Wins, losses and ties count tasks, not runs: a task with five passes is one
// problem, and counting it five times would inflate every sample size.
func TestWinsCountTasksNotRuns(t *testing.T) {
	var outcomes []Outcome
	for rep := 1; rep <= 5; rep++ {
		outcomes = append(outcomes,
			metricRun("a", "base", rep, false, 0.2, 100, 1),
			metricRun("a", "variant", rep, true, 0.8, 100, 1),
			metricRun("b", "base", rep, true, 0.9, 100, 1),
			metricRun("b", "variant", rep, true, 0.9, 100, 1))
	}
	d := CompareMetric(outcomes, "base", "variant", BenchmarkMetrics()[1], 1, 200)
	if d.Tasks != 2 {
		t.Fatalf("tasks = %d, want 2", d.Tasks)
	}
	if d.Runs != 20 {
		t.Fatalf("runs = %d, want 20", d.Runs)
	}
	if d.Wins != 1 || d.Ties != 1 || d.Losses != 0 {
		t.Fatalf("W/L/T = %d/%d/%d, want 1/0/1", d.Wins, d.Losses, d.Ties)
	}
}

// Packet tokens and duration are costs. Less is better, and a comparison that
// got that backwards would publish an efficiency gain as a regression.
func TestCostMetricsCountLessAsAWin(t *testing.T) {
	outcomes := []Outcome{
		metricRun("a", "base", 1, true, 0.5, 6000, 100),
		metricRun("a", "variant", 1, true, 0.5, 4000, 80),
	}
	for _, spec := range BenchmarkMetrics() {
		if spec.Name != "packet tokens" && spec.Name != "duration" {
			continue
		}
		d := CompareMetric(outcomes, "base", "variant", spec, 1, 200)
		if d.Wins != 1 {
			t.Errorf("%s: spending less was not a win (W/L/T %d/%d/%d)",
				spec.Name, d.Wins, d.Losses, d.Ties)
		}
		if d.Delta >= 0 {
			t.Errorf("%s: delta = %v, want negative", spec.Name, d.Delta)
		}
	}
}

// A run on an unannotated task has no recall. Treating the missing value as
// zero would report retrieval as having found nothing rather than as not
// having been measured.
func TestUnscorableRunsAreExcludedNotCountedAsZero(t *testing.T) {
	outcomes := []Outcome{
		metricRun("scored", "base", 1, true, 0.4, 100, 1),
		metricRun("scored", "variant", 1, true, 0.9, 100, 1),
		metricRun("unscored", "base", 1, true, -1, 100, 1),
		metricRun("unscored", "variant", 1, true, -1, 100, 1),
	}
	d := CompareMetric(outcomes, "base", "variant", BenchmarkMetrics()[1], 1, 200)
	if d.Tasks != 1 {
		t.Fatalf("tasks = %d; the unannotated task must be excluded, not scored as zero", d.Tasks)
	}
	if d.Delta < 0.49 || d.Delta > 0.51 {
		t.Fatalf("delta = %v, want ~0.5", d.Delta)
	}
}

// A one-task sample cannot support a claim, whatever the point estimate says.
func TestTinyDifferenceIsNotAnImprovement(t *testing.T) {
	outcomes := []Outcome{
		metricRun("a", "base", 1, true, 0.50, 100, 1),
		metricRun("a", "variant", 1, true, 0.51, 100, 1),
	}
	d := CompareMetric(outcomes, "base", "variant", BenchmarkMetrics()[1], 1, 500)
	if d.Decided() {
		t.Fatalf("a one-point gap over one task was called decided: %+v", d)
	}
	if d.Verdict == VerdictPositive || d.Verdict == VerdictNegative {
		t.Fatalf("verdict = %q on one task", d.Verdict)
	}
}

// Rendering the same result twice must give the same interval.
func TestBootstrapIsReproducible(t *testing.T) {
	var outcomes []Outcome
	for i, task := range []string{"a", "b", "c", "d"} {
		outcomes = append(outcomes,
			metricRun(task, "base", 1, i%2 == 0, 0.3+0.1*float64(i), 100, 1),
			metricRun(task, "variant", 1, true, 0.6+0.1*float64(i), 100, 1))
	}
	first := CompareMetric(outcomes, "base", "variant", BenchmarkMetrics()[1], 42, 1000)
	second := CompareMetric(outcomes, "base", "variant", BenchmarkMetrics()[1], 42, 1000)
	if first.Low != second.Low || first.High != second.High {
		t.Fatalf("two readings disagreed: [%v,%v] vs [%v,%v]",
			first.Low, first.High, second.Low, second.High)
	}
	if first.Seed != 42 || first.Resamples != 1000 {
		t.Fatalf("the procedure was not recorded: seed=%d resamples=%d",
			first.Seed, first.Resamples)
	}
}

// Every metric the brief asks for is reported.
func TestEveryRequiredMetricIsCompared(t *testing.T) {
	want := map[string]bool{
		"solve rate": false, "localization recall": false, "localization precision": false,
		"generator recall": false, "packet tokens": false, "duration": false,
	}
	for _, spec := range BenchmarkMetrics() {
		if _, ok := want[spec.Name]; !ok {
			t.Errorf("unexpected metric %q", spec.Name)
		}
		want[spec.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%q is not compared", name)
		}
	}
}
