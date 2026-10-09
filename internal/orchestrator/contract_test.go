package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

// contractFixture is the ledger fixture with a scripted request-reading
// model; the scripted agent fixes the bug with a reproduction test.
type contractFixture struct {
	r     *Runner
	rt    *scripted.Runtime
	tk    *task.Task
	mu    sync.Mutex
	calls []ContractCall
}

const ledgerRequest = "Fix the unbalanced ledger posting in HandlePaymentCharged"

// newContractFixture creates the task without running it.
func newContractFixture(t *testing.T, policy, request string, model func(ContractCall) string) *contractFixture {
	t.Helper()
	_, w, s, root := setup(t)
	t.Cleanup(func() { s.Close() })
	fix := func(ws, _ string) (string, error) {
		if err := addReproTest(ws); err != nil {
			return "", err
		}
		return "negated", replaceIn(filepath.Join(ws, consumerFile), "AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	f := &contractFixture{rt: &scripted.Runtime{Steps: []scripted.Step{fix}}}
	f.r = newRunner(s, root, f.rt, nil)
	f.r.Cfg.Task.Ambiguity = policy
	f.r.ContractModel = func(_ context.Context, call ContractCall) (string, error) {
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		return model(call), nil
	}
	tk, err := f.r.Create(context.Background(), w, request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.tk = tk
	return f
}

func (f *contractFixture) run(opt RunOptions) (*task.Task, error) {
	return f.r.Run(context.Background(), f.tk.ID, opt)
}

// count is the number of model calls made for a purpose.
func (f *contractFixture) count(purpose string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Purpose == purpose {
			n++
		}
	}
	return n
}

// eventData is the data of the task's events of a kind, oldest first.
func (f *contractFixture) eventData(t *testing.T, kind string) []string {
	t.Helper()
	rows, err := f.r.DB.Query(`SELECT data FROM events WHERE task_id = ? AND kind = ? ORDER BY id`, f.tk.ID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

// contractRun runs the ledger request; the derivation returns contracts(request).
// A grounding call, which these contracts should not need, gets an unusable reply.
func contractRun(t *testing.T, policy string, contracts func(request string) string, clarify string) (*task.Task, *Runner, *scripted.Runtime, error) {
	t.Helper()
	f := newContractFixture(t, policy, ledgerRequest, deriveOnly(contracts))
	got, err := f.run(RunOptions{})
	if err == nil && clarify != "" && got.Status == task.StatusBlocked {
		got, err = f.run(RunOptions{Clarification: clarify})
	}
	return got, f.r, f.rt, err
}

func deriveOnly(contracts func(request string) string) func(ContractCall) string {
	return func(c ContractCall) string {
		if c.Purpose == ContractCallDerive {
			return contracts(c.Request)
		}
		return "no grounding expected"
	}
}

const (
	clearContract  = `{"required":["each charge posts a credit to the customer and a matching debit to settlement"],"acceptance_evidence":["a charge of 500 leaves the customer at 500 and settlement at -500"]}`
	twoBehaviours  = `{"required":["postings balance"],"acceptable_alternatives":[{"point":"an unbalanced event","options":["reject it with an error","post a correcting entry"]}]}`
	materialAmbig  = `{"required":["postings balance"],"unknown_or_ambiguous":[{"question":"Should refunds of partial charges be supported?","interpretations":["only full refunds","partial refunds too"],"affects":"API behaviour and data model","material":true}]}`
	wordingAmbig   = `{"required":["postings balance"],"unknown_or_ambiguous":[{"question":"Does 'matching' mean equal or opposite sign?","interpretations":["equal","opposite"],"affects":"wording only","material":false}]}`
	clarifiedClean = `{"required":["postings balance","full refunds only"]}`

	// ambiguousRequest genuinely supports both readings of materialAmbig.
	ambiguousRequest = ledgerRequest + ". Refunds must post too: support says we should accept only full refunds of a charge, finance says partial refunds of a charge as well."
	bothGrounded     = `{"points":[{"id":1,"settled":false,"settled_by":"","readings":["accept only full refunds of a charge","partial refunds of a charge as well"]}]}`
)

// ambiguousModel derives materialAmbig (clarifiedClean once clarified) and
// answers the grounding check with grounding.
func ambiguousModel(grounding string) func(ContractCall) string {
	return func(c ContractCall) string {
		switch {
		case c.Purpose == ContractCallGround:
			return grounding
		case strings.Contains(c.Request, "Clarification from the user"):
			return clarifiedClean
		}
		return materialAmbig
	}
}

func TestUnambiguousTaskProceedsWithContract(t *testing.T) {
	f := newContractFixture(t, AmbiguityAsk, ledgerRequest, deriveOnly(func(string) string { return clearContract }))
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(f.rt.Messages[0], "Task contract") || !strings.Contains(f.rt.Messages[0], "matching debit to settlement") {
		t.Fatalf("contract missing from the pack:\n%s", f.rt.Messages[0])
	}
	if f.r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatal("unambiguous task flagged")
	}
	if n := f.count(ContractCallGround); n != 0 {
		t.Fatalf("a clear task cost %d grounding calls", n)
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

// TestMaterialAmbiguityAsksThenProceedsAfterClarification: under "ask" a
// request whose two readings are both in its own words stops before any
// implementation with SPEC_AMBIGUOUS and the question; the grounding result
// is kept, so running again does not re-check; a clarification re-derives
// the contract and the task completes.
func TestMaterialAmbiguityAsksThenProceedsAfterClarification(t *testing.T) {
	f := newContractFixture(t, AmbiguityAsk, ambiguousRequest, ambiguousModel(bothGrounded))
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusBlocked || len(f.rt.Messages) != 0 {
		t.Fatalf("expected a block before implementation: status=%v messages=%d err=%v", got.Status, len(f.rt.Messages), err)
	}
	last := got.Decisions[len(got.Decisions)-1].Text
	if !strings.Contains(last, "SPEC_AMBIGUOUS") || !strings.Contains(last, "partial charges") {
		t.Fatalf("block reason = %q", last)
	}
	c, ok := f.r.loadContract(got.ID)
	if !ok || c.Ambiguities[0].Grounding == nil || c.Ambiguities[0].Grounding.Grounded != 2 {
		t.Fatalf("grounding not persisted: %+v", c)
	}
	got, err = f.run(RunOptions{})
	if err != nil || got.Status != task.StatusBlocked || f.count(ContractCallDerive) != 1 || f.count(ContractCallGround) != 1 {
		t.Fatalf("rerun: status=%v err=%v derive=%d ground=%d", got.Status, err, f.count(ContractCallDerive), f.count(ContractCallGround))
	}
	got, err = f.run(RunOptions{Clarification: "Only full refunds."})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("after clarification: status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(f.rt.Messages[0], "full refunds only") {
		t.Fatalf("re-derived contract not used:\n%s", f.rt.Messages[0])
	}
}

// TestSettledAmbiguityDoesNotBlock reconstructs the vue false positive of
// the second validation: the request gives one exact expected result, the
// model still raised a "material" question, and its own acceptance evidence
// settled it. The grounding check finds the request's answer and the task
// proceeds under "ask", with the demotion recorded.
func TestSettledAmbiguityDoesNotBlock(t *testing.T) {
	const request = "Array items rendered in v-for are converted differently from direct access.\n\n" +
		"Reproduction: items = [{}]; render `{{ String(item) }}` inside v-for and `{{ String(items[0]) }}` outside it.\n\n" +
		"Expected: the v-for loop output matches the direct access output; both return 'object object'."
	const derived = `{"required":["v-for item output matches direct access output"],
"unknown_or_ambiguous":[{"question":"Does the fix apply to nested properties or only direct array items?",
 "interpretations":["Fix only the immediate item reference in v-for loop.","Fix all nested reactive conversions within the array items."],
 "affects":"behavior","material":true}],
"acceptance_evidence":["v-for loop output matches direct access output: both return 'object object'"]}`
	const grounding = `{"points":[{"id":1,"settled":true,"settled_by":"the v-for loop output matches the direct access output; both return 'object object'","readings":["render {{ String(item) }} inside v-for",""]}]}`
	f := newContractFixture(t, AmbiguityAsk, request, func(c ContractCall) string {
		if c.Purpose == ContractCallGround {
			if !strings.Contains(c.Messages[0].Content, "nested reactive conversions") || !strings.Contains(c.Messages[0].Content, "object object") {
				t.Errorf("grounding prompt lacks the point or the request:\n%s", c.Messages[0].Content)
			}
			return grounding
		}
		return derived
	})
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if f.r.hasEvent(context.Background(), got.ID, "task.ambiguous") || strings.Contains(f.rt.Messages[0], "materially ambiguous") {
		t.Fatal("a settled point was raised")
	}
	if !hasDecision(got, "settled by the request") || len(f.eventData(t, "task.ambiguity_grounded")) != 1 {
		t.Fatalf("demotion not recorded: %+v", got.Decisions)
	}
}

// TestInventedReadingDoesNotBlock reconstructs the axios case: one of the
// two readings (an HTTP 400) is not supported by the request at all.
func TestInventedReadingDoesNotBlock(t *testing.T) {
	const request = "When the response body is larger than maxContentLength the request should be rejected with an error: \"maxContentLength size of 2000 exceeded\"."
	const derived = `{"required":["reject responses larger than maxContentLength with an error"],
"unknown_or_ambiguous":[{"question":"Should exceeding the limit throw an error or return HTTP 400?","interpretations":["throw an error","return HTTP 400"],"affects":"error behaviour","material":true}]}`
	// The model even claims a quote for the invented reading; it is not in the request.
	const grounding = `{"points":[{"id":1,"settled":false,"settled_by":"","readings":["the request should be rejected with an error","respond with HTTP status 400 Bad Request"]}]}`
	f := newContractFixture(t, AmbiguityAsk, request, func(c ContractCall) string {
		if c.Purpose == ContractCallGround {
			return grounding
		}
		return derived
	})
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted || f.r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if !hasDecision(got, "ungrounded readings: 1 of 2") {
		t.Fatalf("demotion not recorded: %+v", got.Decisions)
	}
}

// TestGroundingFailureKeepsAmbiguityMaterial: an unusable grounding reply
// leaves the ambiguity material (the gate blocks as before) and unchecked,
// so the next run checks it again.
func TestGroundingFailureKeepsAmbiguityMaterial(t *testing.T) {
	grounding := "I cannot answer that."
	f := newContractFixture(t, AmbiguityAsk, ledgerRequest, func(c ContractCall) string {
		if c.Purpose == ContractCallGround {
			return grounding
		}
		return materialAmbig
	})
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusBlocked {
		t.Fatalf("expected a block: status=%v err=%v", got.Status, err)
	}
	ev := f.eventData(t, "task.grounding_failed")
	if len(ev) != 1 || !strings.Contains(ev[0], "no JSON object") || !strings.Contains(ev[0], "I cannot answer that.") {
		t.Fatalf("grounding_failed events = %q", ev)
	}
	if c, _ := f.r.loadContract(got.ID); c == nil || c.Ambiguities[0].Grounding != nil {
		t.Fatalf("failed check recorded as a result: %+v", c)
	}
	// The readings are not in the ledger request: the retried check demotes it.
	grounding = `{"points":[{"id":1,"settled":false,"readings":["only full refunds",""]}]}`
	got, err = f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted || f.count(ContractCallGround) != 2 || f.count(ContractCallDerive) != 1 {
		t.Fatalf("rerun: status=%v err=%v ground=%d derive=%d", got.Status, err, f.count(ContractCallGround), f.count(ContractCallDerive))
	}
}

// TestContractWithoutGroundingLoads: a contract.json written before the
// grounding check loads; its material ambiguity is checked once, and the
// contract is not derived again.
func TestContractWithoutGroundingLoads(t *testing.T) {
	f := newContractFixture(t, AmbiguityAsk, ambiguousRequest, ambiguousModel(bothGrounded))
	old := `{
 "required": ["postings balance"],
 "acceptable_alternatives": null,
 "constraints": null,
 "explicitly_not_required": null,
 "unknown_or_ambiguous": [{"question": "Should refunds of partial charges be supported?", "interpretations": ["only full refunds", "partial refunds too"], "affects": "API behaviour and data model", "material": true}],
 "acceptance_evidence": null
}`
	if err := os.MkdirAll(filepath.Dir(f.r.contractPath(f.tk.ID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.r.contractPath(f.tk.ID), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusBlocked {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if f.count(ContractCallDerive) != 0 || f.count(ContractCallGround) != 1 {
		t.Fatalf("derive=%d ground=%d", f.count(ContractCallDerive), f.count(ContractCallGround))
	}
	if c, _ := f.r.loadContract(got.ID); c == nil || c.Ambiguities[0].Grounding == nil {
		t.Fatalf("grounding not added to the old contract: %+v", c)
	}
}

// TestEmptyContractRetriesOnce: a contract with nothing required is asked
// for once more with a corrective follow-up; a usable second answer is used.
func TestEmptyContractRetriesOnce(t *testing.T) {
	f := newContractFixture(t, AmbiguityAsk, ledgerRequest, func(c ContractCall) string {
		if len(c.Messages) == 1 {
			return `{"required":[],"acceptance_evidence":["the ledger balances"]}`
		}
		return clearContract
	})
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if f.count(ContractCallDerive) != 2 {
		t.Fatalf("derive calls = %d", f.count(ContractCallDerive))
	}
	retry := f.calls[1].Messages
	if len(retry) != 3 || retry[1].Role != "assistant" || !strings.Contains(retry[2].Content, `nothing under "required"`) {
		t.Fatalf("retry messages = %+v", retry)
	}
	if !strings.Contains(f.rt.Messages[0], "matching debit to settlement") {
		t.Fatalf("retried contract not used:\n%s", f.rt.Messages[0])
	}
}

// TestEmptyContractFailureRecordsReply: when the retry is empty too, the
// task runs without a contract and the failure event carries the redacted
// raw reply.
func TestEmptyContractFailureRecordsReply(t *testing.T) {
	const secret = "sk-proj-abcdefghijklmnopqrstuvwxyz0123"
	f := newContractFixture(t, AmbiguityAsk, ledgerRequest, func(ContractCall) string {
		return `{"required":[],"constraints":["use key ` + secret + `"]}`
	})
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if f.count(ContractCallDerive) != 2 {
		t.Fatalf("derive calls = %d, want exactly one retry", f.count(ContractCallDerive))
	}
	ev := f.eventData(t, "task.contract_failed")
	if len(ev) != 1 || !strings.Contains(ev[0], "nothing required") || !strings.Contains(ev[0], `"retried":true`) ||
		!strings.Contains(ev[0], "use key [REDACTED]") || strings.Contains(ev[0], secret) {
		t.Fatalf("contract_failed events = %q", ev)
	}
}

// TestContractRequestDropsHTMLComments: an issue template's comments are
// not sent to the model.
func TestContractRequestDropsHTMLComments(t *testing.T) {
	request := "<!-- Please search existing issues first.\nDescribe the bug. -->\n" + ledgerRequest + "\n<!-- Thanks! -->"
	f := newContractFixture(t, AmbiguityAsk, request, deriveOnly(func(string) string { return clearContract }))
	if _, err := f.run(RunOptions{}); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.Request != ledgerRequest || strings.Contains(c.Messages[0].Content, "search existing issues") || !strings.Contains(c.Messages[0].Content, ledgerRequest) {
		t.Fatalf("request sent = %q", c.Messages[0].Content)
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
	f := newContractFixture(t, AmbiguityProceed, ambiguousRequest, ambiguousModel(bothGrounded))
	got, err := f.run(RunOptions{})
	if err != nil || got.Status != task.StatusCompleted || !f.r.hasEvent(context.Background(), got.ID, "task.ambiguous") {
		t.Fatalf("status=%v err=%v", got.Status, err)
	}
	if !strings.Contains(f.rt.Messages[0], "materially ambiguous") {
		t.Fatalf("agent not told about the ambiguity:\n%s", f.rt.Messages[0])
	}
	if !hasDecision(got, "SPEC_AMBIGUOUS") {
		t.Fatal("SPEC_AMBIGUOUS not recorded")
	}
}

func hasDecision(t *task.Task, sub string) bool {
	for _, d := range t.Decisions {
		if strings.Contains(d.Text, sub) {
			return true
		}
	}
	return false
}

// TestIncompleteGroundingKeepsAmbiguityMaterial: an answer without one
// reading per interpretation, or numbered differently from the points sent,
// decides nothing; the task still asks (code review of the grounding check).
func TestIncompleteGroundingKeepsAmbiguityMaterial(t *testing.T) {
	for name, reply := range map[string]string{
		"no readings":      `{"points":[{"id":1,"settled":false}]}`,
		"numbered from 0":  `{"points":[{"id":0,"settled":false,"readings":["",""]}]}`,
		"one reading only": `{"points":[{"id":1,"settled":false,"readings":["only full refunds"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newContractFixture(t, AmbiguityAsk, ledgerRequest, func(c ContractCall) string {
				if c.Purpose == ContractCallGround {
					return reply
				}
				return materialAmbig
			})
			got, err := f.run(RunOptions{})
			if err != nil || got.Status != task.StatusBlocked {
				t.Fatalf("expected a block: status=%v err=%v", got.Status, err)
			}
			if c, _ := f.r.loadContract(got.ID); c == nil || c.Ambiguities[0].Grounding != nil {
				t.Fatalf("incomplete answer recorded as a result: %+v", c)
			}
		})
	}
}

// fakeUpstream is a cloud provider's upstream that answers every call with
// one chat message.
type fakeUpstream struct{ reply string }

func (f fakeUpstream) Complete(context.Context, map[string]any) (int, []byte, error) {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": f.reply}}}})
	return 200, b, nil
}

func (fakeUpstream) Provider() string { return "openai-compatible" }

// TestContractUsesCloudUpstream: with a cloud provider the gateway has an
// upstream and no local client; the request-reading calls must use it (they
// used to fail with "no model client", silently skipping the contract and
// its ambiguity check).
func TestContractUsesCloudUpstream(t *testing.T) {
	r := &Runner{NewGateway: func(string, int) *inference.Gateway {
		return &inference.Gateway{Model: "m", Upstream: fakeUpstream{reply: `{"required":["x"]}`}}
	}}
	got, err := r.chatReader(context.Background(), &task.Task{ID: "t"}, ContractCall{Purpose: ContractCallDerive}, 100, 0)
	if err != nil || got != `{"required":["x"]}` {
		t.Fatalf("chatReader = %q, %v", got, err)
	}
}

// TestAgentEnvironment: the environment shared with the benchmark baseline
// carries the secret masks, installed dependencies and offline caches.
func TestAgentEnvironment(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("K=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Verify: &verify.Engine{GoModCache: "/gomod"}, Paths: config.Paths{Data: t.TempDir()}}
	masks, deps, _, tc, err := r.AgentEnvironment("t1", wt, []task.Worktree{{RepoName: "r", RepoPath: src, Path: wt}})
	if err != nil {
		t.Fatal(err)
	}
	if len(masks) != 1 || masks[0] != ".env" {
		t.Fatalf("masks = %v", masks)
	}
	if len(deps) != 1 || filepath.Base(deps[0].Host) != "node_modules" {
		t.Fatalf("deps = %+v", deps)
	}
	if tc.GoModCache != "/gomod" || tc.Work == "" {
		t.Fatalf("toolchain = %+v", tc)
	}
}
