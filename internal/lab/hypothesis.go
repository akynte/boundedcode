package lab

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// Metric is what an experiment is decided on.
type Metric struct {
	Name   string `yaml:"metric" json:"metric"` // a scorecard metric, e.g. success_rate
	Better string `yaml:"better" json:"better"` // higher or lower
}

// Alternative is a simpler change a candidate must beat.
type Alternative struct {
	Name     string                    `json:"name"`
	Settings config.ExperimentSettings `json:"settings"`
	Why      string                    `json:"why"`
}

// Intervention is a candidate change for a pattern, from a fixed catalog:
// the laboratory proposes only changes it can express and test.
type Intervention struct {
	ID      string  `json:"id"`
	Pattern Pattern `json:"pattern"`
	// Kinds are the finding kinds it addresses (none: any).
	Kinds   []string `json:"kinds,omitempty"`
	Summary string   `json:"summary"`
	// Settings is a configuration candidate; zero when the change needs
	// code, which Code then describes (a patch to write and test).
	Settings config.ExperimentSettings `json:"settings"`
	Code     string                    `json:"code,omitempty"`
	Simpler  []Alternative             `json:"simpler,omitempty"`
	Primary  Metric                    `json:"primary"`
}

// Catalog is every intervention the laboratory knows. Values are chosen
// against direct mode's defaults for a local model, so that each is a
// change: an observation budget of 8 KB, tool results cut at 16 KB, a
// 32K-token history, 80 model calls per message.
func Catalog() []Intervention {
	var cat []Intervention
	add := func(i Intervention) { cat = append(cat, i) }
	add(Intervention{ID: "loop-guard", Pattern: RepeatedCalls,
		Summary: "Refuse a call identical to two earlier ones with no change between, and say why, instead of running it again.",
		Code:    "internal/direct: count identical calls since the last change; refuse the third with a message naming the earlier results.",
		Simpler: []Alternative{{Name: "max-turns-40", Settings: config.ExperimentSettings{Direct: config.Tunables{MaxTurns: 40}},
			Why: "a lower turn limit bounds what a loop can waste, without any new code"}},
		Primary: Metric{"p90_seconds", "lower"}})
	add(Intervention{ID: "observation-budget", Pattern: ContextExcess, Kinds: []string{"large-prompt", "large-result"},
		Summary:  "Prune command output to 4 KB instead of the local default of 8 KB.",
		Settings: config.ExperimentSettings{Direct: config.Tunables{ObservationBytes: 4 << 10}},
		Simpler: []Alternative{{Name: "max-tool-output-8k", Settings: config.ExperimentSettings{Direct: config.Tunables{MaxToolOutput: 8 << 10}},
			Why: "a plain cut of every tool result at 8 KB (the default is 16 KB), without pruning by relevance"}},
		Primary: Metric{"tokens_per_success", "lower"}})
	add(Intervention{ID: "working-set-48k", Pattern: Compaction, Kinds: []string{"re-read-after"},
		Summary:  "Raise the history budget to 48K tokens so long sessions compact less often.",
		Settings: config.ExperimentSettings{Direct: config.Tunables{WorkingSetTokens: 48000}},
		Simpler: []Alternative{{Name: "legacy-compaction", Settings: config.ExperimentSettings{Direct: config.Tunables{Compaction: "legacy"}},
			Why: "the older summary-only compaction, with no session state"}},
		Primary: Metric{"success_rate", "higher"}})
	add(Intervention{ID: "working-set-24k", Pattern: ContextExcess, Kinds: []string{"large-prompt", "re-read"},
		Summary:  "Lower the history budget to 24K tokens: smaller prompts, more compaction.",
		Settings: config.ExperimentSettings{Direct: config.Tunables{WorkingSetTokens: 24000}},
		Primary:  Metric{"tokens_per_success", "lower"}})
	add(Intervention{ID: "verify-examples", Pattern: WeakVerification,
		Summary: "Before a change is reported done, ask for the request's own examples (inputs and expected outputs it states) to be run as tests.",
		Code:    "internal/direct: after the last change of a turn, when the request states examples and no test exercised them, add a note asking for them to be run. Post-change checks themselves are verification and not tunable.",
		Primary: Metric{"success_rate", "higher"}})
	add(Intervention{ID: "tool-hints", Pattern: ToolSelection,
		Summary: "Answer a misused call (a directory read, a missing file) with the call that would work.",
		Code:    "internal/direct: in the read tool, a directory gets its listing and a missing file the nearest existing paths.",
		Primary: Metric{"model_calls_per_run", "lower"}})
	add(Intervention{ID: "rerun-note", Pattern: Retries, Kinds: []string{"rerun-unchanged"},
		Summary: "Note when a build or test is run again with nothing changed since its last run.",
		Code:    "internal/direct: remember the last result of each verification command and the change counter; on an unchanged rerun, say so.",
		Primary: Metric{"model_calls_per_run", "lower"}})
	add(Intervention{ID: "basic-retrieval", Pattern: RetrievalMiss,
		Summary:  "Look the request's names up by grep instead of the adaptive strategies.",
		Settings: config.ExperimentSettings{Direct: config.Tunables{Retrieval: "basic"}},
		Primary:  Metric{"success_rate", "higher"}})
	add(Intervention{ID: "max-turns-120", Pattern: Budget, Kinds: []string{"turn-limit", "left-broken"},
		Summary:  "Allow 120 model calls per message instead of 80.",
		Settings: config.ExperimentSettings{Direct: config.Tunables{MaxTurns: 120}},
		Primary:  Metric{"success_rate", "higher"}})
	// After Budget Tracker (arXiv 2511.17006): an agent shown its remaining
	// budget used it better; one model, so it needs replicating here.
	add(Intervention{ID: "budget-notice", Pattern: Budget, Kinds: []string{"turn-limit", "left-broken"},
		Summary: "Tell the model how many model calls remain in the message, and near the end to leave the project building.",
		Code:    "internal/direct: add the remaining call budget to the turn's notes from half the limit on, and at the last few calls ask for a building state and an answer.",
		Simpler: []Alternative{{Name: "max-turns-120", Settings: config.ExperimentSettings{Direct: config.Tunables{MaxTurns: 120}},
			Why: "more budget instead of awareness of it"}},
		Primary: Metric{"success_rate", "higher"}})
	// After "The Complexity Trap" (arXiv 2508.21433): masking old
	// observations matched summarising them at about half the cost.
	add(Intervention{ID: "observation-masking", Pattern: Compaction, Kinds: []string{"re-read-after", "recall-failed"},
		Summary: "Replace old tool results with a placeholder instead of summarising the history when it fills.",
		Code:    "internal/direct: in the working set, mask tool results older than the last N turns with a fixed placeholder before compacting.",
		Simpler: []Alternative{{Name: "legacy-compaction", Settings: config.ExperimentSettings{Direct: config.Tunables{Compaction: "legacy"}},
			Why: "the older summary-only compaction"}},
		Primary: Metric{"tokens_per_success", "lower"}})
	add(Intervention{ID: "fewer-attempts", Pattern: Retries, Kinds: []string{"attempts-failed"},
		Summary: "Give a bounded task 3 attempts instead of the configured number.",
		Settings: func() config.ExperimentSettings {
			var s config.ExperimentSettings
			s.Budgets.MaxAttempts = 3
			return s
		}(),
		Primary: Metric{"tokens_per_success", "lower"}})
	add(Intervention{ID: "adaptive-escalation", Pattern: Escalation,
		Summary: "Escalate by the adaptive policy (measured value of escalating) instead of the fixed rules.",
		Settings: func() config.ExperimentSettings {
			var s config.ExperimentSettings
			s.Escalation.Policy = "adaptive"
			return s
		}(),
		Primary: Metric{"success_rate", "higher"}})
	return cat
}

// Hypothesis is a testable claim drawn from an analysis.
type Hypothesis struct {
	ID           string       `json:"id"`
	Intervention Intervention `json:"intervention"`
	Statement    string       `json:"statement"`
	Evidence     string       `json:"evidence"`
	// Tasks are the tasks the pattern occurred in (development tasks, to
	// include in the experiment beside the held-out ones).
	Tasks []string `json:"tasks"`
	// Languages the pattern occurred in.
	Languages []string `json:"languages"`
	// Score orders hypotheses: association with failure first, then the
	// calls and runs affected.
	Score float64 `json:"score"`
}

// Hypothesize turns an analysis's recurring patterns into hypotheses, one
// per intervention that addresses the kinds found.
func Hypothesize(a *Analysis) []Hypothesis {
	var out []Hypothesis
	for _, iv := range Catalog() {
		s, ok := a.Summary(iv.Pattern)
		if !ok || !s.Recurring && !s.Associated {
			continue
		}
		kinds := 0
		for _, k := range iv.Kinds {
			kinds += s.Kinds[k]
		}
		if len(iv.Kinds) > 0 && kinds == 0 {
			continue
		}
		tasks, langs := map[string]bool{}, map[string]bool{}
		for _, f := range a.Findings {
			if f.Pattern != iv.Pattern || len(iv.Kinds) > 0 && !contains(iv.Kinds, f.Kind) {
				continue
			}
			tasks[f.Task] = true
			for _, l := range f.Languages {
				langs[l] = true
			}
		}
		h := Hypothesis{ID: iv.ID, Intervention: iv, Tasks: sortedKeys(tasks), Languages: sortedKeys(langs)}
		h.Statement = fmt.Sprintf("%s This improves %s (%s is better) on held-out tasks without a regression in any language", iv.Summary, iv.Primary.Name, iv.Primary.Better)
		if len(iv.Simpler) > 0 {
			var names []string
			for _, alt := range iv.Simpler {
				names = append(names, alt.Name)
			}
			h.Statement += ", and by more than the simpler " + strings.Join(names, ", ")
		}
		h.Statement += "."
		h.Evidence = fmt.Sprintf("%s in %d of %d eligible runs (%d tasks; %s); fails %.0f%% with, %.0f%% without; %d calls wasted",
			iv.Pattern, s.Runs, s.Eligible, s.Tasks, tally(s.Kinds), 100*s.FailWith, 100*s.FailWithout, s.Waste)
		if s.Classified > 0 {
			h.Evidence += fmt.Sprintf("; explains %d failed runs", s.Classified)
		}
		h.Score = float64(s.Classified) + float64(s.Waste)/10 + float64(s.Runs)
		if s.Associated {
			h.Score += 100
		}
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// MinDetectable is the smallest difference in a rate the experiment can
// call beyond chance: the smallest d for which the 90% Wilson intervals
// of p and p+d (or p−d, whichever is in range) at n runs each do not
// overlap. It is what a pre-registered experiment can hope to show.
func MinDetectable(p float64, n int) float64 {
	if n <= 0 {
		return 1
	}
	k := int(p*float64(n) + 0.5)
	lo, hi := wilson90(k, n)
	for d := 1; d <= n; d++ {
		if up := k + d; up <= n {
			if l2, _ := wilson90(up, n); l2 > hi {
				return round3(float64(d) / float64(n))
			}
		}
		if down := k - d; down >= 0 {
			if _, h2 := wilson90(down, n); h2 < lo {
				return round3(float64(d) / float64(n))
			}
		}
	}
	return 1
}

// WriteHypothesesMarkdown writes hypotheses as a list.
func WriteHypothesesMarkdown(w io.Writer, hs []Hypothesis) {
	fmt.Fprintf(w, "# Hypotheses\n\n%d hypotheses from recurring or failure-associated patterns, strongest evidence first. Each names its candidate change, the simpler alternative it must beat and the metric it is decided on.\n", len(hs))
	for _, h := range hs {
		kind := "configuration: " + strings.Join(h.Intervention.Settings.Pairs(), " ")
		if h.Intervention.Code != "" {
			kind = "code (write a patch and test it as an arm): " + h.Intervention.Code
		}
		fmt.Fprintf(w, "\n## %s (%s)\n\n%s\n\n- Change: %s\n- Evidence: %s\n- Tasks: %s\n- Languages: %s\n", h.ID, h.Intervention.Pattern, h.Statement, kind, h.Evidence,
			strings.Join(h.Tasks, ", "), strings.Join(h.Languages, ", "))
		for _, alt := range h.Intervention.Simpler {
			fmt.Fprintf(w, "- Simpler alternative `%s` (%s): %s\n", alt.Name, strings.Join(alt.Settings.Pairs(), " "), alt.Why)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
