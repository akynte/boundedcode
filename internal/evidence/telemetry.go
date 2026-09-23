package evidence

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
)

// evidenceJournal is judgment.Recorder, backed by the exact same
// internal/ledger.Ledger + ledger.KindJudgment every ordinary BoundedCode
// task already journals judgment requests through
// (internal/supervisor/judgment.go's unexported `journal` does the
// identical thing for a normal workspace) — never a second, parallel
// judgment-logging path. It exists in this package only because
// supervisor's own adapter is unexported and this package cannot import
// internal/supervisor without a cycle (supervisor already depends on
// internal/task, which internal/evidence's ExecuteRun also constructs).
type evidenceJournal struct{ l *ledger.Ledger }

func newEvidenceJournal(st *store.Store) *evidenceJournal {
	return &evidenceJournal{l: ledger.New(st)}
}

func (j *evidenceJournal) BeginJudgment(ctx context.Context, in judgment.JournalIntent) (func(judgment.JournalOutcome), error) {
	taskID := judgment.TaskIDFrom(ctx)
	if taskID == "" {
		return func(judgment.JournalOutcome) {}, nil
	}
	h, err := j.l.Begin(ctx, taskID, ledger.KindJudgment, in, "")
	if err != nil {
		return nil, err
	}
	return func(out judgment.JournalOutcome) {
		if out.Err != "" {
			_ = h.Interrupted(ctx, errString(out.Err))
			return
		}
		_ = h.Complete(ctx, out, "", "")
	}, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// ExtractJudgmentTelemetry reads back every judgment operation the real
// ledger recorded for taskID and maps it into JudgmentTelemetry rows —
// design instruction GAP 3 §12/§13/§14: reads already-recorded data only,
// never calls Jev again, never recomputes a probability.
//
// "Site" is populated from JournalIntent.Phase — the closest concept the
// judgment ledger's own schema records to a call site name (every Site
// registration wraps its Consult call in judgment.WithPhase(ctx, name)).
// Fields the ledger schema does not carry at all (proposed/applied
// effect, tier, intervened, paired outcome) are left at their zero value
// rather than fabricated — design instruction §12's "if a field is not
// available: persist null. Do not fabricate it." A future ledger/workflow
// change that records those explicitly should populate them here instead
// of this package inventing a second source for them.
func ExtractJudgmentTelemetry(ctx context.Context, st *store.Store, taskID string) ([]JudgmentTelemetry, error) {
	ops, err := ledger.New(st).Operations(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []JudgmentTelemetry
	for _, op := range ops {
		if op.Kind != ledger.KindJudgment {
			continue
		}
		var in judgment.JournalIntent
		if err := json.Unmarshal(op.Intent, &in); err != nil {
			continue
		}
		row := JudgmentTelemetry{
			Site:        in.Phase,
			SiteVersion: judgment.SiteVersion(in.Phase),
			Model:       in.Model,
		}
		if len(in.QuestionIDs) > 0 {
			sort.Strings(in.QuestionIDs)
			row.QuestionID = in.QuestionIDs[0]
		}
		if len(op.Outcome) > 0 {
			var out2 judgment.JournalOutcome
			if err := json.Unmarshal(op.Outcome, &out2); err == nil {
				row.LatencyMS = out2.DurationMS
				row.InputTokens = out2.Usage.InputTokens
				for _, c := range out2.Confidence {
					if c > row.Confidence {
						row.Confidence = c
					}
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// SummarizeJudgmentTelemetry aggregates the real, already-extracted rows
// into telemetry.json's jev_* fields — design instruction §15. It never
// estimates: a field with nothing to aggregate over is left at its zero
// value, which for `Cost` (unavailable from the judgment ledger's own
// Usage struct today — see Usage's fields) means always nil/omitted
// rather than a guessed number.
func SummarizeJudgmentTelemetry(rows []JudgmentTelemetry) JevSummary {
	s := JevSummary{SitesCalled: []string{}}
	if len(rows) == 0 {
		return s
	}
	s.JevCalls = len(rows)
	sites := map[string]bool{}
	latencies := make([]int64, 0, len(rows))
	inputTokens := 0
	for _, r := range rows {
		s.JevTotalLatencyMS += r.LatencyMS
		inputTokens += r.InputTokens
		latencies = append(latencies, r.LatencyMS)
		if r.Site != "" {
			sites[r.Site] = true
		}
		if r.ProposedEffect != "" {
			s.EffectsProposed++
		}
		if r.AppliedEffect != "" {
			s.EffectsApplied++
		}
	}
	for site := range sites {
		s.SitesCalled = append(s.SitesCalled, site)
	}
	sort.Strings(s.SitesCalled)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	s.JevP50LatencyMS = percentile(latencies, 0.50)
	s.JevP95LatencyMS = percentile(latencies, 0.95)
	s.JevInputTokens = &inputTokens
	return s
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// JevSummary is telemetry.json's aggregate Jev section for a treatment
// run — design instruction §15/§16. CostUnavailableReason documents why
// JevCost stays nil: internal/judgment.Usage does not currently expose a
// dollar figure, only token counts by kind (see internal/judgment/typesafe.go).
type JevSummary struct {
	JevCalls          int      `json:"jev_calls"`
	JevInputTokens    *int     `json:"jev_input_tokens"`
	JevTotalLatencyMS int64    `json:"jev_total_latency_ms"`
	JevP50LatencyMS   int64    `json:"jev_p50_latency_ms"`
	JevP95LatencyMS   int64    `json:"jev_p95_latency_ms"`
	JevCost           *float64 `json:"jev_cost"`
	SitesCalled       []string `json:"sites_called"`
	EffectsProposed   int      `json:"effects_proposed"`
	EffectsApplied    int      `json:"effects_applied"`
}
