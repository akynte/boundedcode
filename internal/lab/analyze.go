package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// AnalysisSchema is the version of the analysis format.
const AnalysisSchema = 1

// Analysis is the patterns found in a set of runs.
type Analysis struct {
	Schema  int       `json:"schema"`
	Created time.Time `json:"created"`
	Reports []FileRef `json:"reports"`
	Scope   string    `json:"scope"` // which splits were analysed: "dev" (held-out runs left out) or "all"
	Runs    int       `json:"runs"`
	// WithTranscript counts the direct runs whose tool calls were read.
	WithTranscript int `json:"with_transcript"`
	// LeftOut counts runs not analysed: held out, or skipped.
	LeftOut  map[string]int   `json:"left_out,omitempty"`
	Patterns []PatternSummary `json:"patterns"`
	// Findings are every finding, for inspection.
	Findings []FindingRef `json:"findings"`
}

// FileRef pins a file by content.
type FileRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// FindingRef is a finding with its run named.
type FindingRef struct {
	Pattern   Pattern  `json:"pattern"`
	Kind      string   `json:"kind"`
	Task      string   `json:"task"`
	Report    string   `json:"report"`
	Languages []string `json:"languages,omitempty"`
	Success   bool     `json:"success"`
	Detail    string   `json:"detail"`
	Steps     []int    `json:"steps,omitempty"`
	Waste     int      `json:"waste,omitempty"`
	// Classifies: the kind exists only in failed runs.
	Classifies bool `json:"classifies,omitempty"`
	// Transcript is the session file the steps index into, relative to the
	// sessions directory.
	Transcript string `json:"transcript,omitempty"`
}

// PatternSummary is one pattern over the runs.
type PatternSummary struct {
	Pattern Pattern `json:"pattern"`
	// Eligible are the runs that could show the pattern (some need a
	// transcript, some are bounded mode only); Runs those that did.
	Eligible int `json:"eligible"`
	Runs     int `json:"runs"`
	// Classified are failed runs the pattern explains (kinds defined by
	// the failure); they are left out of FailWith and FailWithout.
	Classified int            `json:"classified_failures"`
	Tasks      int            `json:"tasks"`
	Kinds      map[string]int `json:"kinds"`
	// FailWith and FailWithout are the failure rates of eligible runs with
	// and without the pattern's other kinds, with 90% Wilson intervals: an
	// association, not a cause.
	FailWith      float64    `json:"fail_with"`
	FailWithCI    [2]float64 `json:"fail_with_ci90"`
	FailWithout   float64    `json:"fail_without"`
	FailWithoutCI [2]float64 `json:"fail_without_ci90"`
	// TokensWith and TokensWithout are mean processed tokens per run.
	TokensWith    float64        `json:"tokens_with"`
	TokensWithout float64        `json:"tokens_without"`
	Waste         int            `json:"waste_calls"`
	ByLanguage    map[string]int `json:"by_language"`
	// Recurring: in at least two tasks and three runs.
	Recurring bool `json:"recurring"`
	// Associated: the runs with it fail more often than those without,
	// beyond chance (the intervals do not overlap).
	Associated bool `json:"associated"`
}

// eligible says which runs can show a pattern.
func eligible(p Pattern, r *Run) bool {
	switch p {
	case Escalation:
		return r.Suite == "bounded"
	case Retries:
		return r.Suite == "bounded" || len(r.Steps) > 0
	case WeakVerification:
		return r.Suite == "bounded" || len(r.Steps) > 0 && r.Changes()
	case RetrievalMiss:
		return len(r.Steps) > 0 && len(r.Targets) > 0
	case RepeatedCalls, ToolSelection, Compaction:
		return len(r.Steps) > 0
	case ContextExcess:
		return r.Suite == "direct"
	case Budget:
		return r.Suite == "direct" && r.Turns <= 1
	}
	return false
}

// Analyze loads runs from reports and summarises their patterns. With
// includeHeldout false, held-out runs are left out, so that the held-out
// tasks stay independent of the hypotheses drawn from the analysis.
func Analyze(runs []*Run, reports []string, includeHeldout bool) (*Analysis, error) {
	a := &Analysis{Schema: AnalysisSchema, Created: time.Now().UTC(), Scope: "dev", LeftOut: map[string]int{}}
	if includeHeldout {
		a.Scope = "all"
	}
	for _, p := range reports {
		ref, err := pin(p)
		if err != nil {
			return nil, err
		}
		a.Reports = append(a.Reports, ref)
	}
	var kept []*Run
	for _, r := range runs {
		switch {
		case r.Skipped:
			a.LeftOut["skipped"]++
		case r.Split == "heldout" && !includeHeldout:
			a.LeftOut["heldout"]++
		default:
			kept = append(kept, r)
			if len(r.Steps) > 0 {
				a.WithTranscript++
			}
		}
	}
	a.Runs = len(kept)
	findings := Detect(kept, Calibrate(kept))
	byRun := map[*Run]map[Pattern][]Finding{}
	for _, f := range findings {
		if byRun[f.Run] == nil {
			byRun[f.Run] = map[Pattern][]Finding{}
		}
		byRun[f.Run][f.Pattern] = append(byRun[f.Run][f.Pattern], f)
		a.Findings = append(a.Findings, FindingRef{Pattern: f.Pattern, Kind: f.Kind, Task: f.Run.Task, Report: f.Run.Report, Languages: f.Run.Languages,
			Success: f.Run.Success, Detail: f.Detail, Steps: f.Steps, Waste: f.Waste, Classifies: f.Classifies, Transcript: f.Run.TranscriptRef})
	}
	for _, p := range Patterns {
		s := PatternSummary{Pattern: p, Kinds: map[string]int{}, ByLanguage: map[string]int{}}
		tasks := map[string]bool{}
		var failW, nW, failWo, nWo int
		var tokW, tokWo []float64
		for _, r := range kept {
			if !eligible(p, r) {
				continue
			}
			s.Eligible++
			fs := byRun[r][p]
			predictive := false
			for _, f := range fs {
				s.Kinds[f.Kind]++
				s.Waste += f.Waste
				if f.Classifies {
					s.Classified++
				} else {
					predictive = true
				}
			}
			if len(fs) > 0 {
				s.Runs++
				tasks[r.Task] = true
				for _, l := range r.Languages {
					s.ByLanguage[l]++
				}
			}
			if !predictive {
				nWo++
				tokWo = append(tokWo, float64(r.Tokens()))
				if !r.Success {
					failWo++
				}
				continue
			}
			nW++
			tokW = append(tokW, float64(r.Tokens()))
			if !r.Success {
				failW++
			}
		}
		s.Tasks = len(tasks)
		s.FailWith, s.FailWithCI = rate(failW, nW)
		s.FailWithout, s.FailWithoutCI = rate(failWo, nWo)
		s.TokensWith, s.TokensWithout = mean(tokW), mean(tokWo)
		s.Recurring = s.Tasks >= 2 && s.Runs >= 3
		s.Associated = nW > 0 && nWo > 0 && s.FailWithCI[0] > s.FailWithoutCI[1]
		a.Patterns = append(a.Patterns, s)
	}
	return a, nil
}

func rate(k, n int) (float64, [2]float64) {
	if n == 0 {
		return 0, [2]float64{0, 1}
	}
	lo, hi := wilson90(k, n)
	return round3(float64(k) / float64(n)), [2]float64{round3(lo), round3(hi)}
}

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

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return math.Round(s / float64(len(v)))
}

// pin hashes a file.
func pin(path string) (FileRef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FileRef{}, err
	}
	sum := sha256.Sum256(b)
	return FileRef{Path: path, SHA256: hex.EncodeToString(sum[:])}, nil
}

// Summary returns a pattern's summary.
func (a *Analysis) Summary(p Pattern) (PatternSummary, bool) {
	for _, s := range a.Patterns {
		if s.Pattern == p {
			return s, true
		}
	}
	return PatternSummary{}, false
}

// WriteAnalysisMarkdown writes the analysis as tables.
func WriteAnalysisMarkdown(w io.Writer, a *Analysis, examples int) {
	fmt.Fprintf(w, "# Trajectory analysis\n\n%d runs from %d reports (scope: %s", a.Runs, len(a.Reports), a.Scope)
	if n := a.LeftOut["heldout"]; n > 0 {
		fmt.Fprintf(w, "; %d held-out runs left out", n)
	}
	if n := a.LeftOut["skipped"]; n > 0 {
		fmt.Fprintf(w, "; %d skipped runs left out", n)
	}
	fmt.Fprintf(w, "). Tool calls were read from the transcripts of %d direct-mode runs.\n\n", a.WithTranscript)
	fmt.Fprintln(w, "*Classified failures* are failed runs a pattern explains by definition (its own tests passed and the hidden ones failed, say). The failure rates with and without a pattern use its other kinds only, and are an association over these runs, not a cause. A pattern is *recurring* in two or more tasks and three or more runs, and *associated* when the runs with it fail more often beyond chance (90% intervals apart).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Pattern | Runs (of eligible) | Tasks | Classified failures | Fail with | Fail without | Tokens with / without | Wasted calls | Recurring | Associated | Kinds | Languages |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, s := range a.Patterns {
		fmt.Fprintf(w, "| %s | %d (%d) | %d | %d | %.0f%% (%.0f–%.0f) | %.0f%% (%.0f–%.0f) | %.0f / %.0f | %d | %s | %s | %s | %s |\n", s.Pattern, s.Runs, s.Eligible, s.Tasks, s.Classified,
			100*s.FailWith, 100*s.FailWithCI[0], 100*s.FailWithCI[1], 100*s.FailWithout, 100*s.FailWithoutCI[0], 100*s.FailWithoutCI[1],
			s.TokensWith, s.TokensWithout, s.Waste, yes(s.Recurring), yes(s.Associated), tally(s.Kinds), tally(s.ByLanguage))
	}
	if examples <= 0 {
		return
	}
	fmt.Fprintln(w, "\n## Examples")
	for _, p := range Patterns {
		var fs []FindingRef
		for _, f := range a.Findings {
			if f.Pattern == p {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			continue
		}
		// Failures first, then the costliest.
		sort.SliceStable(fs, func(i, j int) bool {
			if fs[i].Success != fs[j].Success {
				return !fs[i].Success
			}
			return fs[i].Waste > fs[j].Waste
		})
		fmt.Fprintf(w, "\n### %s\n\n", p)
		seen := map[string]bool{}
		n := 0
		for _, f := range fs {
			if seen[f.Task+f.Kind] || n >= examples {
				continue
			}
			seen[f.Task+f.Kind] = true
			n++
			outcome := "passed"
			if !f.Success {
				outcome = "failed"
			}
			fmt.Fprintf(w, "- `%s` (%s, %s, %s): %s\n", f.Task, strings.Join(f.Languages, ","), f.Kind, outcome, f.Detail)
		}
	}
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "–"
}
