package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/task"
)

func evidenceRow(tk *task.Task, layer int, name string) (task.EvidenceLayer, bool) {
	for _, l := range tk.Evidence {
		if l.Layer == layer && (name == "" || l.Name == name) {
			return l, true
		}
	}
	return task.EvidenceLayer{}, false
}

// A change that fixes the bug but also skips an existing test is asked
// about once, and is not task_verified when it keeps the skip.
func TestIntegrityReviewWithholdsVerification(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()
	fixAndSkip := func(ws, _ string) (string, error) {
		if err := fixLedger(ws); err != nil {
			return "", err
		}
		f := filepath.Join(ws, "ledger-service/internal/consumer/consumer_test.go")
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		s := strings.Replace(string(b), "(t *testing.T) {\n", "(t *testing.T) {\n\tt.Skip(\"flaky\")\n", 1)
		return "fixed; skipped a flaky test", os.WriteFile(f, []byte(s), 0o644)
	}
	noop := func(string, string) (string, error) { return "the skip is needed", nil }
	rt := &scripted.Runtime{Steps: []scripted.Step{fixAndSkip, noop}}
	r := newRunner(s, root, rt, nil)
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting", []string{"ledger-service"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTestsGreen {
		t.Fatalf("status=%s verify=%s", got.Status, got.VerificationState)
	}
	if len(rt.Messages) < 2 || !strings.Contains(rt.Messages[1], "touches how the tests run") || !strings.Contains(rt.Messages[1], "t.Skip") {
		t.Fatalf("no integrity request:\n%s", strings.Join(rt.Messages, "\n----\n"))
	}
	if l, ok := evidenceRow(got, 3, "test integrity"); !ok || l.Status != "weak" {
		t.Fatalf("integrity layer %+v", got.Evidence)
	}
	if !strings.Contains(lastDecision(got), "touches how its tests run") {
		t.Fatalf("decision: %s", lastDecision(got))
	}
}

func lastDecision(tk *task.Task) string {
	if len(tk.Decisions) == 0 {
		return ""
	}
	return tk.Decisions[len(tk.Decisions)-1].Text
}

// Counterexamples: the verifier's tests are run, a failing one is put to
// the agent once, and by default it does not lower the label; every layer
// is recorded.
func TestCounterexampleLayer(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()
	fix := func(ws, _ string) (string, error) { return "fixed the posting", fixLedger(ws) }
	noop := func(string, string) (string, error) { return "the generated test expects the wrong sign", nil }
	rt := &scripted.Runtime{Steps: []scripted.Step{fix, noop}}
	r := newRunner(s, root, rt, nil)
	r.Cfg.Task.Verification.Counterexamples = "on"
	var calls []ContractCall
	r.ContractModel = func(_ context.Context, c ContractCall) (string, error) {
		if c.Purpose != ContractCallCounterexample {
			return "", context.Canceled // no contract in this test
		}
		calls = append(calls, c)
		return `{"tests": [{"path": "internal/consumer/independent_test.go", "content": "package consumer\n\nimport \"testing\"\n\nfunc TestIndependent(t *testing.T) {\n\tt.Fatal(\"the verifier expects something else\")\n}\n"}]}`, nil
	}
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting", []string{"ledger-service"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTaskVerified {
		t.Fatalf("status=%s verify=%s; %s", got.Status, got.VerificationState, lastDecision(got))
	}
	if len(calls) == 0 || strings.Contains(calls[0].Messages[0].Content, "fixed the posting") {
		t.Fatal("the verifier saw the author's conversation, or was not called")
	}
	if len(rt.Messages) < 2 || !strings.Contains(rt.Messages[1], "independent check") {
		t.Fatalf("no counterexample request:\n%s", strings.Join(rt.Messages, "\n----\n"))
	}
	for n, want := range map[int]string{5: "established", 7: "found", 8: "not applicable"} {
		if l, ok := evidenceRow(got, n, ""); !ok || l.Status != want {
			t.Errorf("layer %d: %+v", n, l)
		}
	}
	if l, ok := evidenceRow(got, 6, ""); !ok || l.Status == "skipped" {
		t.Errorf("mutation did not run: %+v", l)
	}
}

// Required counterexamples withhold task_verified when one is found.
func TestCounterexamplesRequired(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()
	fix := func(ws, _ string) (string, error) { return "fixed the posting", fixLedger(ws) }
	noop := func(string, string) (string, error) { return "done", nil }
	rt := &scripted.Runtime{Steps: []scripted.Step{fix, noop}}
	r := newRunner(s, root, rt, nil)
	r.Cfg.Task.Verification.Counterexamples = "required"
	r.ContractModel = func(_ context.Context, c ContractCall) (string, error) {
		if c.Purpose != ContractCallCounterexample {
			return "", context.Canceled
		}
		return `{"tests": [{"path": "internal/consumer/independent_test.go", "content": "package consumer\n\nimport \"testing\"\n\nfunc TestIndependent(t *testing.T) { t.Fatal(\"x\") }\n"}]}`, nil
	}
	tk, _ := r.Create(ctx, w, "Fix the unbalanced ledger posting", []string{"ledger-service"}, nil)
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.VerificationState != task.VerificationTestsGreen || !strings.Contains(lastDecision(got), "counterexample checks are required") {
		t.Fatalf("verify=%s; %s", got.VerificationState, lastDecision(got))
	}
}
