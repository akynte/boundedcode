package task

import (
	"encoding/json"
	"sort"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

// PLAN's evidence has to fit its phase budget by construction.
//
// The recorded failure: IMPACT succeeded on GIN-1805, returned a large
// consumer set, and PLAN then refused its own evidence — twice, with
// "PLAN evidence exceeds phase budget" and "append-only log is full". The
// run that reached EDIT was the one where IMPACT found nothing and fell back
// to six lexical files. Upstream analysis working better made the next phase
// unrunnable, which is backwards: a planner that cannot be called is worse
// than a planner called with less.
//
// LOCALIZE already solved this shape with fitSkeleton and fitSignatures —
// rank, then drop until the real serialization is admitted. This is the same
// method applied to the planner's evidence, against the same admission test.

// planEvidenceSummary is what was kept, so the abbreviation is visible to the
// model reading the evidence and to the operator reading telemetry.
type planEvidenceSummary struct {
	Available int `json:"available"`
	Retained  int `json:"retained"`
	Dropped   int `json:"dropped"`
	Bytes     int `json:"bytes"`
	// The per-kind detail, because "dropped 40" says nothing about whether
	// what went was a breaking consumer or a compatible one.
	ConsumersAvailable  int `json:"consumers_available"`
	ConsumersRetained   int `json:"consumers_retained"`
	ActionableAvailable int `json:"actionable_available"`
	ActionableRetained  int `json:"actionable_retained"`
	BodiesAvailable     int `json:"bodies_available"`
	BodiesRetained      int `json:"bodies_retained"`
	FailuresAvailable   int `json:"failures_available"`
	FailuresRetained    int `json:"failures_retained"`
	// Truncated says the planner is looking at a bounded view, which changes
	// what an absent consumer means.
	Truncated bool `json:"truncated_for_phase_budget,omitempty"`
}

// planBudget is one rung of the reduction ladder.
type planBudget struct {
	actionable int
	peripheral int
	bodies     int
	bodyBytes  int
	failures   int
}

// planLadder is the order in which evidence is given up.
//
// It descends by priority from the bottom of the list upward: peripheral
// graph results first, then bodies, then older corrections, and actionable
// obligations last and never to zero. The objective, the accepted files and
// the symbols never appear here because they are never dropped.
var planLadder = []planBudget{
	{actionable: 200, peripheral: 200, bodies: 64, bodyBytes: 4000, failures: 12},
	{actionable: 200, peripheral: 80, bodies: 48, bodyBytes: 4000, failures: 12},
	{actionable: 160, peripheral: 40, bodies: 32, bodyBytes: 3000, failures: 10},
	{actionable: 120, peripheral: 20, bodies: 24, bodyBytes: 2000, failures: 8},
	{actionable: 96, peripheral: 10, bodies: 16, bodyBytes: 1500, failures: 6},
	{actionable: 64, peripheral: 5, bodies: 12, bodyBytes: 1200, failures: 5},
	{actionable: 48, peripheral: 0, bodies: 8, bodyBytes: 900, failures: 4},
	{actionable: 32, peripheral: 0, bodies: 6, bodyBytes: 700, failures: 3},
	{actionable: 24, peripheral: 0, bodies: 4, bodyBytes: 500, failures: 2},
	{actionable: 16, peripheral: 0, bodies: 2, bodyBytes: 400, failures: 2},
	{actionable: 8, peripheral: 0, bodies: 1, bodyBytes: 300, failures: 1},
	{actionable: 4, peripheral: 0, bodies: 0, bodyBytes: 0, failures: 1},
	// The floor. One obligation and one contradiction is still a planning
	// problem; nothing at all is not.
	{actionable: 1, peripheral: 0, bodies: 0, bodyBytes: 0, failures: 1},
}

// fitPlanEvidence builds the planner's evidence at the largest rung of the
// ladder the phase will actually admit.
//
// fits is the phase's own admission test, the same one LOCALIZE uses: the
// append-only log's token budget, which is tighter than the byte ceiling and
// is the one a large impact report actually hits. A nil fits leaves the byte
// ceiling as the only bound, which is what the unit tests exercise.
func fitPlanEvidence(s *workflow.State, base map[string]any, fits func([]byte) bool) (map[string]any, planEvidenceSummary) {
	actionable, peripheral := splitConsumers(s.Impact)
	bodyKeys := rankBodies(s.Bodies, s.Files)

	sum := planEvidenceSummary{
		ConsumersAvailable:  len(actionable) + len(peripheral),
		ActionableAvailable: len(actionable),
		BodiesAvailable:     len(s.Bodies),
		FailuresAvailable:   len(s.Feedback),
	}
	sum.Available = sum.ConsumersAvailable + sum.BodiesAvailable + sum.FailuresAvailable

	var chosen map[string]any
	var chosenSum planEvidenceSummary
	for i, b := range planLadder {
		trial, ts := buildPlanEvidence(s, base, actionable, peripheral, bodyKeys, b, sum)
		body, err := json.Marshal(trial)
		if err != nil {
			continue
		}
		ts.Bytes = len(body)
		chosen, chosenSum = trial, ts
		if len(body) <= phaseEvidenceLimit && (fits == nil || fits(body)) {
			break
		}
		if i == len(planLadder)-1 {
			// Nothing left to give up. What remains is over budget on its
			// own, which is decide's refusal to make rather than this
			// function's — and it will make it with a clear message.
			break
		}
	}
	return chosen, chosenSum
}

// buildPlanEvidence assembles one rung.
func buildPlanEvidence(s *workflow.State, base map[string]any,
	actionable, peripheral []graph.Consumer, bodyKeys []string,
	b planBudget, sum planEvidenceSummary) (map[string]any, planEvidenceSummary) {

	out := make(map[string]any, len(base)+6)
	for k, v := range base {
		out[k] = v
	}

	// Priority 1-3: never reduced. The objective, the files LOCALIZE accepted
	// and the symbols are what make this a plannable problem at all, and they
	// are small.
	out["hypothesis"] = s.Hypothesis
	out["files"] = s.Files
	out["symbols"] = s.Symbols

	// Priority 4: actionable obligations, with diversity across files so one
	// crowded file cannot fill the whole allowance.
	keptActionable := diverseByFile(actionable, b.actionable)
	keptPeripheral := diverseByFile(peripheral, b.peripheral)
	sum.ActionableRetained = len(keptActionable)
	sum.ConsumersRetained = len(keptActionable) + len(keptPeripheral)
	if s.Impact != nil {
		out["impact"] = boundedImpact(s.Impact, keptActionable, keptPeripheral)
	}

	// Priority 5: bodies, capped in count and in size each.
	if b.bodies > 0 && len(bodyKeys) > 0 {
		bodies := make(map[string]string, b.bodies)
		for _, k := range bodyKeys {
			if len(bodies) >= b.bodies {
				break
			}
			bodies[k] = clipString(s.Bodies[k], b.bodyBytes)
		}
		out["bodies"] = bodies
		sum.BodiesRetained = len(bodies)
	} else {
		sum.BodiesRetained = 0
	}

	// Priority 6: the corrections from previous PLAN attempts.
	//
	// Kept from the end: the contradiction the model has to resolve is the
	// one it was just given, and dropping that to keep an older one would
	// leave it correcting a fault it has already corrected. This is also what
	// stops repeated corrections growing without bound.
	if b.failures > 0 && len(s.Feedback) > 0 {
		kept := s.Feedback
		if len(kept) > b.failures {
			kept = kept[len(kept)-b.failures:]
		}
		out["failures"] = kept
		sum.FailuresRetained = len(kept)
	} else if len(s.Feedback) > 0 {
		// Never zero while any correction exists.
		out["failures"] = s.Feedback[len(s.Feedback)-1:]
		sum.FailuresRetained = 1
	}

	sum.Retained = sum.ConsumersRetained + sum.BodiesRetained + sum.FailuresRetained
	sum.Dropped = sum.Available - sum.Retained
	sum.Truncated = sum.Dropped > 0
	if sum.Truncated {
		out["evidence_truncated"] = sum
	}
	return out, sum
}

// boundedImpact rebuilds the report with only the consumers that survived,
// keeping the fields that change how an absent consumer must be read.
func boundedImpact(in *graph.Impact, actionable, peripheral []graph.Consumer) *graph.Impact {
	out := *in
	out.Consumers = append(append([]graph.Consumer(nil), actionable...), peripheral...)
	// Counts describe the full traversal, not the excerpt, and are left as
	// they were: they are how the planner can tell that what it is reading is
	// a sample. Truncated is raised for the same reason.
	if len(out.Consumers) < len(in.Consumers) {
		out.Truncated = true
	}
	return &out
}

// splitConsumers separates the obligations that need action from the ones
// that are reported only for completeness.
//
// Breaking and undetermined are work: one cannot keep compiling, the other
// cannot be shown to. Compatible and compiles-but-may-differ are context, and
// they are what a bounded planner should give up first.
func splitConsumers(in *graph.Impact) (actionable, peripheral []graph.Consumer) {
	if in == nil {
		return nil, nil
	}
	for _, c := range in.Consumers {
		switch c.Verdict {
		case graph.Breaking, graph.Undetermined:
			actionable = append(actionable, c)
		default:
			peripheral = append(peripheral, c)
		}
	}
	// Deterministic: breaking before undetermined, then shallower first, then
	// by path. Two runs on the same graph must produce the same evidence.
	rank := func(c graph.Consumer) int {
		if c.Verdict == graph.Breaking {
			return 0
		}
		return 1
	}
	less := func(s []graph.Consumer) func(i, j int) bool {
		return func(i, j int) bool {
			if ri, rj := rank(s[i]), rank(s[j]); ri != rj {
				return ri < rj
			}
			if s[i].Depth != s[j].Depth {
				return s[i].Depth < s[j].Depth
			}
			if s[i].Node.Path != s[j].Node.Path {
				return s[i].Node.Path < s[j].Node.Path
			}
			return s[i].Node.Name < s[j].Node.Name
		}
	}
	sort.SliceStable(actionable, less(actionable))
	sort.SliceStable(peripheral, less(peripheral))
	return actionable, peripheral
}

// diverseByFile keeps at most n consumers, spread across files.
//
// Taking the first n of a sorted list would hand the planner forty call sites
// in one file and nothing about the other six. A round over the files takes
// each file's most important consumer before any file's second.
func diverseByFile(in []graph.Consumer, n int) []graph.Consumer {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) <= n {
		return append([]graph.Consumer(nil), in...)
	}
	byFile := map[string][]graph.Consumer{}
	var order []string
	for _, c := range in {
		p := c.Node.Path
		if _, seen := byFile[p]; !seen {
			order = append(order, p)
		}
		byFile[p] = append(byFile[p], c)
	}
	var out []graph.Consumer
	for round := 0; len(out) < n; round++ {
		progressed := false
		for _, p := range order {
			if round >= len(byFile[p]) {
				continue
			}
			out = append(out, byFile[p][round])
			progressed = true
			if len(out) >= n {
				break
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// rankBodies orders the localized bodies by how central they are: a body for
// a file the planner was told to change outranks one for a file that merely
// appeared in retrieval.
func rankBodies(bodies map[string]string, files []string) []string {
	if len(bodies) == 0 {
		return nil
	}
	accepted := map[string]bool{}
	for _, f := range files {
		accepted[f] = true
	}
	keys := make([]string, 0, len(bodies))
	for k := range bodies {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ai, aj := accepted[keyFile(keys[i])], accepted[keyFile(keys[j])]
		if ai != aj {
			return ai
		}
		return keys[i] < keys[j]
	})
	return keys
}

// keyFile takes the path out of a body key, which may be "path" or
// "path:symbol" depending on what LOCALIZE recorded.
func keyFile(k string) string {
	for i := range len(k) {
		if k[i] == ':' {
			return k[:i]
		}
	}
	return k
}

// planFailureCount is used by the tests to assert the contradiction survives.
func planFailureCount(evidence map[string]any) int {
	f, ok := evidence["failures"].([]recipe.Result)
	if !ok {
		return 0
	}
	return len(f)
}
