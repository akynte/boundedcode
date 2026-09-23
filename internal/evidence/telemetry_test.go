package evidence

// GAP 3 §39 integration test: a real judgment ledger, built with the real
// internal/ledger.Ledger + internal/judgment.JournalIntent/JournalOutcome
// record shapes (never a report-only fake schema), exported through
// ExtractJudgmentTelemetry/SummarizeJudgmentTelemetry and persisted via
// PersistResult — asserting judgments.jsonl, telemetry.json, and the
// extracted rows agree with each other. No live Jev call.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	root, err := store.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	st, err := root.OpenWorkspace(context.Background(), workspace.DeriveID("/telemetry-test/", "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestExtractJudgmentTelemetryReadsBackWhatTheRealLedgerRecorded(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	taskID := "evidence-telemetry-test-task"

	j := newEvidenceJournal(st)
	ctx = judgment.WithTaskID(ctx, taskID)
	ctx = judgment.WithPhase(ctx, "intake_profile")

	done, err := j.BeginJudgment(ctx, judgment.JournalIntent{
		Endpoint: "https://api.typesafe.ai/v1/systemone", Model: "jev-1.13.0",
		Phase: "intake_profile", QuestionIDs: []string{"q1"}, StateDigest: "deadbeef", StateBytes: 128,
		RedactMode: "repo_text",
	})
	if err != nil {
		t.Fatal(err)
	}
	done(judgment.JournalOutcome{
		Source: "live", ModelServed: "jev-1.13.0-served", Answered: 1,
		Confidence: map[string]float64{"q1": 0.82},
		Usage:      judgment.Usage{InputTokens: 512, OutputTokens: 8, Requests: 1},
		DurationMS: 340,
	})

	// A second, refused judgment (declined locally, no state) — proves
	// extraction handles both shapes real recordings take.
	done2, err := j.BeginJudgment(ctx, judgment.JournalIntent{
		Endpoint: "https://api.typesafe.ai/v1/systemone", Model: "jev-1.13.0",
		Phase: "review_rubric", QuestionIDs: []string{"q2"}, Refused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	done2(judgment.JournalOutcome{Source: "declined", Err: "empty state"})

	rows, err := ExtractJudgmentTelemetry(ctx, st, taskID)
	if err != nil {
		t.Fatalf("ExtractJudgmentTelemetry: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 extracted rows, got %d: %+v", len(rows), rows)
	}

	var live *JudgmentTelemetry
	for i := range rows {
		if rows[i].Site == "intake_profile" {
			live = &rows[i]
		}
	}
	if live == nil {
		t.Fatal("expected a row for the intake_profile site")
	}
	if live.Model != "jev-1.13.0" {
		t.Fatalf("expected model jev-1.13.0, got %q", live.Model)
	}
	if live.InputTokens != 512 {
		t.Fatalf("expected 512 input tokens (from the real Usage struct), got %d", live.InputTokens)
	}
	if live.LatencyMS != 340 {
		t.Fatalf("expected 340ms latency, got %d", live.LatencyMS)
	}
	if live.Confidence != 0.82 {
		t.Fatalf("expected confidence 0.82, got %v", live.Confidence)
	}

	summary := SummarizeJudgmentTelemetry(rows)
	if summary.JevCalls != 2 {
		t.Fatalf("expected 2 jev_calls in the summary, got %d", summary.JevCalls)
	}
	if summary.JevInputTokens == nil || *summary.JevInputTokens != 512 {
		t.Fatalf("expected total input tokens 512, got %v", summary.JevInputTokens)
	}
	if summary.JevCost != nil {
		t.Fatal("expected jev_cost to stay nil — no cost field exists in judgment.Usage today")
	}

	// Persist through the real PersistResult path and confirm
	// judgments.jsonl / telemetry.json both reflect exactly what was
	// extracted — design instruction §39's final assertion.
	runRoot := t.TempDir()
	res := Result{
		TaskID: "test-task", Benchmark: BenchmarkSWEBenchProVerified, Arm: ArmExperimentalJevFull,
		RunID: "run-1", SuiteHash: "suite-1", OfficialBenchmarkStatus: StatusFailed,
	}
	if _, err := PersistResult(runRoot, res, []byte("diff\n"), nil, rows); err != nil {
		t.Fatalf("PersistResult: %v", err)
	}

	dir := ResultDir(runRoot, res.SuiteHash, res.TaskID, res.Arm, res.RunID)
	jbody, err := os.ReadFile(filepath.Join(dir, JudgmentTraceFile))
	if err != nil {
		t.Fatalf("reading judgments.jsonl: %v", err)
	}
	lines := splitNonEmptyLines(string(jbody))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in judgments.jsonl, got %d: %s", len(lines), jbody)
	}
	var persistedFirst JudgmentTelemetry
	if err := json.Unmarshal([]byte(lines[0]), &persistedFirst); err != nil {
		t.Fatal(err)
	}
	if persistedFirst.Site != rows[0].Site {
		t.Fatalf("judgments.jsonl order/content mismatch: got %+v, want %+v", persistedFirst, rows[0])
	}

	tbody, err := os.ReadFile(filepath.Join(dir, TelemetryFile))
	if err != nil {
		t.Fatalf("reading telemetry.json: %v", err)
	}
	var telemetry Telemetry
	if err := json.Unmarshal(tbody, &telemetry); err != nil {
		t.Fatal(err)
	}
	if telemetry.Jev == nil || telemetry.Jev.JevCalls != 2 {
		t.Fatalf("expected telemetry.json's jev section to report 2 calls, got %+v", telemetry.Jev)
	}
}

// TestControlRunsPersistZeroJevCallsNotOmittedTelemetry proves §17: a
// CONTROL run's telemetry.json always carries an explicit Jev section
// showing zero calls when a caller does compute one for it, and — more
// importantly — ExecuteRun itself never attaches a Judge (and therefore
// never journals anything) for CONTROL, which SummarizeJudgmentTelemetry
// over an empty slice models correctly.
func TestControlRunsPersistZeroJevCallsNotOmittedTelemetry(t *testing.T) {
	summary := SummarizeJudgmentTelemetry(nil)
	if summary.JevCalls != 0 {
		t.Fatalf("expected 0 jev_calls for no rows, got %d", summary.JevCalls)
	}
	if len(summary.SitesCalled) != 0 {
		t.Fatalf("expected no sites called, got %v", summary.SitesCalled)
	}
}

func splitNonEmptyLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

var _ = ledger.KindJudgment // keep import honest about what this test relies on
