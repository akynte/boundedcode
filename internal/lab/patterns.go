package lab

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Pattern is a recurring failure pattern. Every detector is deterministic
// and reads only what the run recorded; none asks a model.
type Pattern string

// The patterns the laboratory looks for.
const (
	RetrievalMiss    Pattern = "retrieval_miss"         // the files the task is about were never opened, or late
	RepeatedCalls    Pattern = "repeated_calls"         // the same call again with nothing changed, or failing calls in a row
	WeakVerification Pattern = "weak_verification"      // a change never checked, or checked by tests that missed what the hidden ones caught
	ToolSelection    Pattern = "tool_selection"         // a shell command for what a tool does, or a tool used wrongly
	ContextExcess    Pattern = "context_excess"         // a prompt far above its group's, a huge result, files read again and again
	Escalation       Pattern = "escalation"             // escalation that did not help
	Retries          Pattern = "unnecessary_retries"    // tests re-run with no change between, attempts exhausted
	Compaction       Pattern = "ineffective_compaction" // files read again after a compaction, failed recalls
	Budget           Pattern = "budget_exhausted"       // the turn limit reached, the project left broken
)

// Patterns lists them in report order.
var Patterns = []Pattern{RetrievalMiss, RepeatedCalls, WeakVerification, ToolSelection, ContextExcess, Escalation, Retries, Compaction, Budget}

// Finding is one pattern in one run.
type Finding struct {
	Pattern Pattern `json:"pattern"`
	Kind    string  `json:"kind"`   // the variant, such as "unverified-change"
	Detail  string  `json:"detail"` // what happened, with figures
	Steps   []int   `json:"steps,omitempty"`
	// Waste is the tool calls the pattern cost, when it can be counted.
	Waste int `json:"waste,omitempty"`
	// Classifies marks a kind defined by the run's failure (its tests
	// passed but the hidden ones failed, say): it explains a failure and
	// says nothing about whether the pattern predicts one.
	Classifies bool `json:"classifies,omitempty"`
	Run        *Run `json:"-"`
}

// classifying are the kinds that exist only in failed runs.
var classifying = map[string]bool{"false-confidence": true, "false-pass": true, "partly-opened": true, "escalated-failed": true,
	"attempts-failed": true, "left-broken": true}

// Thresholds are the figures detectors compare against, computed from the
// runs analysed (Calibrate) so that "large" means large for this suite.
type Thresholds struct {
	// PeakByGroup is the 90th percentile of peak prompt tokens per group.
	PeakByGroup map[string]int
	// BigResult is a single tool result size that is large (bytes).
	BigResult int
}

// Calibrate computes thresholds from runs.
func Calibrate(runs []*Run) Thresholds {
	peaks := map[string][]int{}
	for _, r := range runs {
		if r.PeakPrompt > 0 && !r.Skipped {
			peaks[r.Group] = append(peaks[r.Group], r.PeakPrompt)
		}
	}
	t := Thresholds{PeakByGroup: map[string]int{}, BigResult: 32 << 10}
	for g, v := range peaks {
		sort.Ints(v)
		t.PeakByGroup[g] = v[(len(v)*9)/10]
	}
	return t
}

// Detect runs every detector over runs.
func Detect(runs []*Run, th Thresholds) []Finding {
	var out []Finding
	for _, r := range runs {
		if r.Skipped {
			continue
		}
		for _, d := range []func(*Run, Thresholds) []Finding{retrievalMiss, repeatedCalls, weakVerification, toolSelection, contextExcess,
			escalation, retries, compaction, budget} {
			for _, f := range d(r, th) {
				f.Run, f.Classifies = r, classifying[f.Kind]
				out = append(out, f)
			}
		}
	}
	return out
}

// touches reports whether a step names a file matching target (either is
// a path suffix of the other: steps may give absolute or relative paths,
// and a task's targets are relative to one of its repositories).
func touches(s Step, target string) bool {
	for _, p := range s.Paths {
		if p == target || strings.HasSuffix(p, "/"+target) || strings.HasSuffix(target, "/"+p) {
			return true
		}
	}
	return false
}

func retrievalMiss(r *Run, _ Thresholds) []Finding {
	if len(r.Targets) == 0 || len(r.Steps) == 0 {
		return nil
	}
	var missed []string
	first := -1
	for _, t := range r.Targets {
		at := -1
		for i, s := range r.Steps {
			if touches(s, t) {
				at = i
				break
			}
		}
		if at < 0 {
			missed = append(missed, t)
			continue
		}
		if first < 0 || at < first {
			first = at
		}
	}
	switch {
	case len(missed) == len(r.Targets):
		return []Finding{{Pattern: RetrievalMiss, Kind: "never-opened", Detail: fmt.Sprintf("never opened %s in %d calls", strings.Join(missed, ", "), len(r.Steps)), Waste: len(r.Steps)}}
	case len(missed) > 0 && !r.Success:
		return []Finding{{Pattern: RetrievalMiss, Kind: "partly-opened", Detail: "never opened " + strings.Join(missed, ", ")}}
	case first >= 10 && first*2 >= len(r.Steps):
		return []Finding{{Pattern: RetrievalMiss, Kind: "found-late", Detail: fmt.Sprintf("first opened a target at call %d of %d", first+1, len(r.Steps)), Steps: []int{first}, Waste: first}}
	}
	return nil
}

// mutatingCommand reports whether a shell command may change files.
var mutatingCommand = regexp.MustCompile(`(^|[;&|]\s*)(sed\s+-i|perl\s+-[a-z]*i|tee\b|mv\b|cp\b|rm\b|git\s+(apply|checkout|restore|am)|patch\b|python3?\s+-c)|>{1,2}\s*[^&\s]`)

func changes(s Step) bool {
	if s.Mutate {
		return true
	}
	if s.Tool == "bash" {
		c, _ := s.Args["command"].(string)
		return mutatingCommand.MatchString(c)
	}
	return false
}

func repeatedCalls(r *Run, _ Thresholds) []Finding {
	var out []Finding
	// Identical calls within a window with no change: a change (an edit)
	// starts a new window, since reading or testing again after one is
	// what should happen.
	count := map[string][]int{}
	flushWindow := func() {
		for _, key := range sortedKeys(count) {
			if idx := count[key]; len(idx) >= 3 {
				out = append(out, Finding{Pattern: RepeatedCalls, Kind: "identical-call", Detail: fmt.Sprintf("%s called %d times with the same arguments and no change between", r.Steps[idx[0]].Tool, len(idx)), Steps: idx, Waste: len(idx) - 1})
			}
		}
		count = map[string][]int{}
	}
	for i, s := range r.Steps {
		if changes(s) {
			flushWindow()
			continue
		}
		count[s.Key] = append(count[s.Key], i)
	}
	flushWindow()
	// Failing calls of one tool in a row.
	run, start := 0, 0
	flush := func(end int) {
		if run >= 4 {
			out = append(out, Finding{Pattern: RepeatedCalls, Kind: "failing-streak", Detail: fmt.Sprintf("%d failing %s calls in a row (%s)", run, r.Steps[start].Tool, r.Steps[start].Error), Steps: []int{start, end - 1}, Waste: run - 1})
		}
	}
	for i, s := range r.Steps {
		if s.Error != "" && run > 0 && s.Tool == r.Steps[start].Tool {
			run++
			continue
		}
		flush(i)
		run, start = 0, i
		if s.Error != "" {
			run = 1
		}
	}
	flush(len(r.Steps))
	return out
}

// verifyCommand matches a command that builds or tests a project, in any
// ecosystem.
var verifyCommand = regexp.MustCompile(`\b(test|tests|pytest|unittest|jest|vitest|mocha|tsc|ctest|rspec|minitest|phpunit|rake|check|vet|build|compile|lint)\b|go\s+(test|build|vet)|cargo\s+(test|build|check)|npm\s+(test|run)|node\s+--test|mvn|gradle|make\b|dotnet\s+(test|build)`)

func weakVerification(r *Run, _ Thresholds) []Finding {
	if r.Suite == "bounded" {
		if r.SelfVerified && !r.Success {
			return []Finding{{Pattern: WeakVerification, Kind: "false-pass", Detail: "verified by its own checks; the hidden checks failed: " + firstLine(r.FailedChecks)}}
		}
		return nil
	}
	if !r.Changes() || len(r.FilesChanged) == 0 || len(r.Steps) == 0 {
		return nil
	}
	last := -1
	for i, s := range r.Steps {
		if changes(s) {
			last = i
		}
	}
	checked, clean := false, false
	for _, s := range r.Steps[last+1:] {
		if s.Tool != "bash" {
			continue
		}
		c, _ := s.Args["command"].(string)
		if verifyCommand.MatchString(c) {
			checked, clean = true, s.Error == ""
		}
	}
	switch {
	case !checked:
		return []Finding{{Pattern: WeakVerification, Kind: "unverified-change", Detail: fmt.Sprintf("changed %s and never built or tested after the last change", strings.Join(r.FilesChanged, ", "))}}
	case clean && !r.Success && len(r.FailedChecks) > 0:
		return []Finding{{Pattern: WeakVerification, Kind: "false-confidence", Detail: "its own build or tests passed after the last change; the hidden checks failed: " + firstLine(r.FailedChecks)}}
	}
	return nil
}

// shellForTool maps a command's first word to the tool that does it.
var shellForTool = map[string]string{"cat": "read", "head": "read", "tail": "read", "less": "read", "grep": "grep", "rg": "grep", "egrep": "grep",
	"find": "glob", "ls": "ls", "tree": "ls"}

// misuse classifies a tool error as a use of the tool the runner rejected.
func misuse(e string) string {
	switch l := strings.ToLower(e); {
	case strings.Contains(l, "unknown tool"):
		return "unknown tool"
	case strings.Contains(l, "arguments are not a json object"), strings.Contains(l, "missing required"), strings.Contains(l, "is required"):
		return "bad arguments"
	case strings.Contains(l, "is a directory"):
		return "read a directory"
	case strings.Contains(l, "not found in"), strings.Contains(l, "old_string"), strings.Contains(l, "copy the text exactly"):
		return "edit text not found"
	case strings.Contains(l, "no such file"), strings.Contains(l, "does not exist"):
		return "missing file"
	case strings.Contains(l, "recall"):
		return "recall failed"
	}
	return ""
}

func toolSelection(r *Run, _ Thresholds) []Finding {
	shell := map[string]int{}
	var shellSteps []int
	errs := map[string]int{}
	var errSteps []int
	for i, s := range r.Steps {
		if s.Tool == "bash" {
			c, _ := s.Args["command"].(string)
			w := firstWord(c)
			if t, ok := shellForTool[w]; ok && !strings.Contains(c, "|") && !mutatingCommand.MatchString(c) {
				shell[w+"→"+t]++
				shellSteps = append(shellSteps, i)
			}
		}
		if k := misuse(s.Error); k != "" {
			errs[s.Tool+": "+k]++
			errSteps = append(errSteps, i)
		}
	}
	var out []Finding
	if len(shellSteps) >= 3 {
		out = append(out, Finding{Pattern: ToolSelection, Kind: "shell-for-tool", Detail: "shell commands instead of tools: " + tally(shell), Steps: shellSteps, Waste: 0})
	}
	if len(errSteps) >= 2 {
		out = append(out, Finding{Pattern: ToolSelection, Kind: "tool-misuse", Detail: "tool calls rejected as misused: " + tally(errs), Steps: errSteps, Waste: len(errSteps)})
	}
	return out
}

func contextExcess(r *Run, th Thresholds) []Finding {
	var out []Finding
	if p90 := th.PeakByGroup[r.Group]; p90 > 0 && r.PeakPrompt > p90 && r.PeakPrompt >= 2*medianish(p90) {
		out = append(out, Finding{Pattern: ContextExcess, Kind: "large-prompt", Detail: fmt.Sprintf("peak prompt %d tokens, above the %s group's 90th percentile (%d)", r.PeakPrompt, r.Group, p90)})
	}
	big := 0
	var bigSteps []int
	for i, s := range r.Steps {
		if s.Bytes > th.BigResult {
			big += s.Bytes
			bigSteps = append(bigSteps, i)
		}
	}
	if len(bigSteps) > 0 {
		out = append(out, Finding{Pattern: ContextExcess, Kind: "large-result", Detail: fmt.Sprintf("%d tool results over %d KB (%d KB in all)", len(bigSteps), th.BigResult>>10, big>>10), Steps: bigSteps})
	}
	if r.RedundantReads >= 3 {
		out = append(out, Finding{Pattern: ContextExcess, Kind: "re-read", Detail: fmt.Sprintf("%d reads of files already read and unchanged", r.RedundantReads), Waste: r.RedundantReads})
	}
	return out
}

// medianish is a floor under "large": half the 90th percentile, at least
// 8K tokens, so a suite of small prompts does not flag a 6K one.
func medianish(p90 int) int { return max(p90/2, 8000) }

func escalation(r *Run, _ Thresholds) []Finding {
	if r.Suite != "bounded" || r.Escalations == 0 || r.Success {
		return nil
	}
	return []Finding{{Pattern: Escalation, Kind: "escalated-failed", Detail: fmt.Sprintf("escalated %d times and still failed (%s)", r.Escalations, r.FailureClass)}}
}

func retries(r *Run, _ Thresholds) []Finding {
	var out []Finding
	if r.Suite == "bounded" && r.Attempts > 1 && !r.Success {
		out = append(out, Finding{Pattern: Retries, Kind: "attempts-failed", Detail: fmt.Sprintf("%d attempts, none passed (%s)", r.Attempts, r.FailureClass), Waste: r.Attempts - 1})
	}
	last := map[string]int{}
	var steps []int
	for i, s := range r.Steps {
		if changes(s) {
			last = map[string]int{}
			continue
		}
		if s.Tool != "bash" {
			continue
		}
		c, _ := s.Args["command"].(string)
		if !verifyCommand.MatchString(c) {
			continue
		}
		if _, ok := last[c]; ok {
			steps = append(steps, i)
		}
		last[c] = i
	}
	if len(steps) > 0 {
		out = append(out, Finding{Pattern: Retries, Kind: "rerun-unchanged", Detail: fmt.Sprintf("re-ran a build or test %d times with no change between", len(steps)), Steps: steps, Waste: len(steps)})
	}
	return out
}

func compaction(r *Run, _ Thresholds) []Finding {
	var out []Finding
	for _, at := range r.Compacted {
		before := map[string]bool{}
		for _, s := range r.Steps[:min(at, len(r.Steps))] {
			if s.Tool == "read" {
				for _, p := range s.Paths {
					before[p] = true
				}
			}
			if changes(s) {
				for _, p := range s.Paths {
					delete(before, p)
				}
			}
		}
		var again []string
		var steps []int
		for i := at; i < len(r.Steps); i++ {
			s := r.Steps[i]
			if changes(s) {
				for _, p := range s.Paths {
					delete(before, p)
				}
			}
			if s.Tool == "read" {
				for _, p := range s.Paths {
					if before[p] {
						again = appendUnique(again, path.Base(p))
						steps = append(steps, i)
						delete(before, p)
					}
				}
			}
		}
		if len(again) >= 2 {
			out = append(out, Finding{Pattern: Compaction, Kind: "re-read-after", Detail: fmt.Sprintf("after the compaction at call %d, read again %d unchanged files: %s", at+1, len(again), strings.Join(again, ", ")), Steps: steps, Waste: len(steps)})
		}
	}
	failed := 0
	var steps []int
	for i, s := range r.Steps {
		if s.Tool == "recall" && s.Error != "" {
			failed++
			steps = append(steps, i)
		}
	}
	if failed > 0 {
		out = append(out, Finding{Pattern: Compaction, Kind: "recall-failed", Detail: fmt.Sprintf("%d recall calls failed", failed), Steps: steps, Waste: failed})
	}
	return out
}

// buildBroken matches hidden-check output saying the code does not build.
var buildBroken = regexp.MustCompile(`(?i)imported and not used|undefined:|cannot find|syntaxerror|syntax error|compilation failed|build failed|error\[E\d+\]|cannot compile|error TS\d+|ModuleNotFoundError|ImportError`)

func budget(r *Run, _ Thresholds) []Finding {
	if r.Suite != "direct" || r.Turns > 1 {
		return nil
	}
	limit := r.MaxTurns
	if limit == 0 {
		limit = 80
	}
	if r.ModelCalls < limit {
		return nil
	}
	f := Finding{Pattern: Budget, Kind: "turn-limit", Detail: fmt.Sprintf("stopped at the turn limit (%d model calls)", limit)}
	if !r.Success && buildBroken.MatchString(strings.Join(r.FailedChecks, "\n")) {
		f.Kind, f.Detail = "left-broken", f.Detail+" with the project failing to build: "+firstLine(r.FailedChecks)
	}
	if strings.TrimSpace(r.Answer) == "" {
		f.Detail += "; no answer"
	}
	return []Finding{f}
}

func firstLine(s []string) string {
	if len(s) == 0 {
		return ""
	}
	l := s[0]
	if i := strings.Index(l, ": "); i > 0 && i < 120 {
		l = l[i+2:]
	}
	for _, line := range strings.Split(l, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			if len(t) > 140 {
				t = t[:140] + "…"
			}
			return t
		}
	}
	return ""
}

func firstWord(c string) string {
	c = strings.TrimSpace(c)
	for _, p := range []string{"cd ", "(cd "} {
		if strings.HasPrefix(c, p) {
			if i := strings.Index(c, "&&"); i > 0 {
				c = strings.TrimSpace(c[i+2:])
			}
		}
	}
	w, _, _ := strings.Cut(c, " ")
	return path.Base(w)
}

func tally(m map[string]int) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}
