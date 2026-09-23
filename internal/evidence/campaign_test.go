package evidence

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/llm"
)

type fakeProvider struct{ llmProviderStub }

func (fakeProvider) Name() string { return "pipeline-test-fake-provider" }

func TestRunCampaignSkipsTerminalRunsAndDoesNotRerunAFailedRun(t *testing.T) {
	runRoot := t.TempDir()
	ledgerPath := filepath.Join(runRoot, "ledger", "v1.jsonl")
	led, err := OpenLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}

	plans := []RunPlan{
		{SuiteHash: "s1", TaskID: "t1", Arm: ArmControl, Benchmark: BenchmarkSWEBenchProVerified,
			ImageRef: "does-not-matter", ImageDir: "/app", RunOrder: 0},
	}
	// Seed a terminal FAILED record for t1|CONTROL, as a prior campaign run
	// would have left it — design instruction §5/§20: the next invocation
	// must skip it, never call the generator or grader again.
	if err := led.Append(Record{SuiteHash: "s1", TaskID: "t1", Arm: ArmControl, RunID: "prior-run-1",
		Status: StatusFailed, Detail: "official grader reported a legitimate failure"}); err != nil {
		t.Fatal(err)
	}

	rf := &RunnerFactory{
		Provider: fakeProvider{},
		EngineFactory: func() (engine.Engine, error) {
			t.Fatal("generator must not be called for a terminal run")
			return nil, nil
		},
	}

	steps, err := RunCampaign(context.Background(), runRoot, led, rf, plans, nil)
	if err != nil {
		t.Fatalf("RunCampaign: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Action != ActionSkip {
		t.Fatalf("expected ActionSkip for a FAILED run, got %v", steps[0].Action)
	}
	if steps[0].Ran {
		t.Fatal("a skipped run must not execute")
	}

	current, err := led.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current["t1|CONTROL"].RunID != "prior-run-1" {
		t.Fatalf("expected the ledger's current record to remain the prior FAILED run, got %+v",
			current["t1|CONTROL"])
	}
}

// llmProviderStub satisfies llm.Provider with panics on every method that
// isn't exercised — RunCampaign's skip path above never reaches any of
// them, which is exactly what this test asserts.
type llmProviderStub struct{}

func (llmProviderStub) Name() string                   { panic("not called in this test") }
func (llmProviderStub) Capabilities() llm.Capabilities { panic("not called in this test") }
func (llmProviderStub) Health(context.Context) error   { panic("not called in this test") }
func (llmProviderStub) Close() error                   { panic("not called in this test") }
func (llmProviderStub) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("not called in this test")
}
func (llmProviderStub) ChatStructured(context.Context, llm.ChatRequest, json.RawMessage) (*llm.ChatResponse, error) {
	panic("not called in this test")
}
func (llmProviderStub) Embed(context.Context, llm.EmbedRequest) (*llm.EmbedResponse, error) {
	panic("not called in this test")
}
func (llmProviderStub) Infill(context.Context, llm.InfillRequest) (*llm.ChatResponse, error) {
	panic("not called in this test")
}
