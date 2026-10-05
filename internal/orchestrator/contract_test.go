package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/task"
)

// contractRun runs the ledger fixture task with a fake contract model; the
// scripted agent fixes the bug with a reproduction test.
func contractRun(t *testing.T, policy string, contracts func(request string) string, clarify string) (*task.Task, *Runner, *scripted.Runtime, error) {
	t.Helper()
	_, w, s, root := setup(t)
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	fix := func(ws, _ string) (string, error) {
		if err := addReproTest(ws); err != nil {
			return "", err
		}
		return "negated", replaceIn(filepath.Join(ws, consumerFile), "AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	rt := &scripted.Runtime{Steps: []scripted.Step{fix}}
	r := newRunner(s, root, rt, nil)
	r.Cfg.Task.Ambiguity = policy
	r.ContractModel = func(_ context.Context, request string) (string, error) { return contracts(request), nil }
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting in HandlePaymentCharged", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err == nil && clarify != "" && got.Status == task.StatusBlocked {
		got, err = r.Run(ctx, tk.ID, RunOptions{Clarification: clarify})
	}
	return got, r, rt, err
}

const (
	clearContract  = `{"required":["each charge posts a credit to the customer and a matching debit to settlement"],"acceptance_evidence":["a charge of 500 leaves the customer at 500 and settlement at -500"]}`
	twoBehaviours  = `{"required":["postings balance"],"acceptable_alternatives":[{"point":"an unbalanced event","options":["reject it with an error","post a correcting entry"]}]}`
	materialAmbig  = `{"required":["postings balance"],"unknown_or_ambiguous":[{"question":"Should refunds of partial charges be supported?","interpretations":["only full refunds","partial refunds too"],"affects":"API behaviour and data model","material":true}]}`
	wordingAmbig   = `{"required":["postings balance"],"unknown_or_ambiguous":[{"question":"Does 'matching' mean equal or opposite sign?","interpretations":["equal","opposite"],"affects":"wording only","material":false}]}`
	clarifiedClean = `{"required":["postings balance","full refunds only"]}`
)

func TestUnambiguousTaskProceedsWithContract(t *testing.T) {
	got, r, rt, err := contractRun(t, AmbiguityAsk, func(string) string { return clearContract }, "")
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(rt.Messages[0], "Task contract") || !strings.Contains(rt.Messages[0], "matching debit to settlement") {
		t.Fatalf("contract missing from the pack:\n%s", rt.Messages[0])
	}
	if r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatal("unambiguous task flagged")
	}
}

// TestAllowedAlternativesDoNotBlock: a request that explicitly accepts two
// outcomes keeps both; implementing either is fine and nothing blocks.
func TestAllowedAlternativesDoNotBlock(t *testing.T) {
	got, _, rt, err := contractRun(t, AmbiguityAsk, func(string) string { return twoBehaviours }, "")
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	m := rt.Messages[0]
	if !strings.Contains(m, "reject it with an error | post a correcting entry") || !strings.Contains(m, "implement one option, say which") {
		t.Fatalf("alternatives not preserved:\n%s", m)
	}
}

// TestMaterialAmbiguityAsksThenProceedsAfterClarification: under "ask" the
// task stops before any implementation with SPEC_AMBIGUOUS and the question;
// a clarification re-derives the contract and the task completes.
func TestMaterialAmbiguityAsksThenProceedsAfterClarification(t *testing.T) {
	model := func(req string) string {
		if strings.Contains(req, "Clarification from the user") {
			return clarifiedClean
		}
		return materialAmbig
	}
	got, r, rt, err := contractRun(t, AmbiguityAsk, model, "")
	if err != nil || got.Status != task.StatusBlocked || len(rt.Messages) != 0 {
		t.Fatalf("expected a block before implementation: status=%v messages=%d err=%v", got.Status, len(rt.Messages), err)
	}
	last := got.Decisions[len(got.Decisions)-1].Text
	if !strings.Contains(last, "SPEC_AMBIGUOUS") || !strings.Contains(last, "partial charges") {
		t.Fatalf("block reason = %q", last)
	}
	got, err = r.Run(context.Background(), got.ID, RunOptions{Clarification: "Only full refunds."})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("after clarification: status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(rt.Messages[0], "full refunds only") {
		t.Fatalf("re-derived contract not used:\n%s", rt.Messages[0])
	}
}

// TestWordingAmbiguityDoesNotBlock: vagueness that changes nothing material
// is not raised.
func TestWordingAmbiguityDoesNotBlock(t *testing.T) {
	got, r, _, err := contractRun(t, AmbiguityAsk, func(string) string { return wordingAmbig }, "")
	if err != nil || got.Status != task.StatusCompleted || r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
}

// TestProceedPolicyRecordsAmbiguity: benchmarks record SPEC_AMBIGUOUS and go
// on; the agent is told to state and demonstrate its reading.
func TestProceedPolicyRecordsAmbiguity(t *testing.T) {
	got, r, rt, err := contractRun(t, AmbiguityProceed, func(string) string { return materialAmbig }, "")
	if err != nil || got.Status != task.StatusCompleted || !r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(rt.Messages[0], "materially ambiguous") {
		t.Fatalf("agent not told about the ambiguity:\n%s", rt.Messages[0])
	}
	found := false
	for _, d := range got.Decisions {
		found = found || strings.Contains(d.Text, "SPEC_AMBIGUOUS")
	}
	if !found {
		t.Fatal("SPEC_AMBIGUOUS not recorded")
	}
}
