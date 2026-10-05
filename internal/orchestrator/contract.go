package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/task"
)

// Ambiguity policies (config task.ambiguity).
const (
	AmbiguityAsk     = "ask"     // block a materially ambiguous task until the user clarifies it
	AmbiguityProceed = "proceed" // record SPEC_AMBIGUOUS, have the agent state and demonstrate its reading
)

const contractPrompt = `Extract a compact task contract from the software change request below.
Reply with one JSON object and nothing else:
{"required": [], "acceptable_alternatives": [{"point": "", "options": []}], "constraints": [],
 "explicitly_not_required": [], "unknown_or_ambiguous": [{"question": "", "interpretations": [], "affects": "", "material": false}],
 "acceptance_evidence": []}
Rules:
- required: changes or behaviours the request explicitly asks for.
- acceptable_alternatives: ONLY where the request itself says more than one outcome is acceptable (for example "either X or Y", "X, or alternatively Y"). Each option is a complete outcome.
- constraints: limits the request states (compatibility, performance, APIs that must not change).
- explicitly_not_required: what the request puts out of scope.
- unknown_or_ambiguous: points the request leaves open. material=true only when plausible readings would change observable behaviour, an API, data, security, compatibility or tests; "affects" names which. Do not list wording vagueness or implementation details an engineer may choose freely.
- acceptance_evidence: concrete checks, using the request's own inputs and expected results where it gives them.
Keep each list to at most 5 items of at most 25 words.

Request:
`

// deriveContract asks the local model for the task contract.
func (r *Runner) deriveContract(ctx context.Context, t *task.Task) (string, error) {
	if r.ContractModel != nil {
		return r.ContractModel(ctx, contractRequest(t))
	}
	if r.NewGateway == nil {
		return "", errors.New("no model gateway")
	}
	gw := r.NewGateway(t.ID, 0)
	if gw == nil || gw.Client == nil {
		return "", errors.New("no model client")
	}
	gw.Source = "planner"
	body := map[string]any{
		"messages":             []map[string]any{{"role": "user", "content": contractPrompt + contractRequest(t)}},
		"max_tokens":           1500,
		"temperature":          0.2,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
	status, out, err := gw.Forward(ctx, "/v1/chat/completions", body)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(out)
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if status != 200 || json.Unmarshal(b, &resp) != nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("contract call: status %d", status)
	}
	return resp.Choices[0].Message.Content, nil
}

func contractRequest(t *task.Task) string {
	req := t.OriginalRequest
	if t.Goal != "" && t.Goal != t.OriginalRequest {
		req += "\n\n" + t.Goal
	}
	return req
}

func (r *Runner) contractPath(taskID string) string {
	return filepath.Join(r.Paths.TaskDir(taskID), "contract.json")
}

// loadContract returns the task's persisted contract, if any.
func (r *Runner) loadContract(taskID string) (*task.Contract, bool) {
	b, err := os.ReadFile(r.contractPath(taskID))
	if err != nil {
		return nil, false
	}
	var c task.Contract
	if json.Unmarshal(b, &c) != nil {
		return nil, false
	}
	return &c, true
}

// ensureContract derives and persists the contract once per task (again
// after a clarification). It returns nil when contracts are disabled or the
// model's answer is unusable: the task then runs without one.
func (r *Runner) ensureContract(ctx context.Context, t *task.Task) *task.Contract {
	if !r.Cfg.Task.Contract {
		return nil
	}
	if c, ok := r.loadContract(t.ID); ok {
		return c
	}
	reply, err := r.deriveContract(ctx, t)
	var c task.Contract
	if err == nil {
		c, err = task.ParseContract(reply)
	}
	if err != nil {
		r.Rec.Emit(ctx, t.ID, "task.contract_failed", map[string]any{"error": trunc(err.Error(), 300)})
		r.Log.Warn("task contract unavailable; continuing without one", "err", err)
		return nil
	}
	b, _ := json.MarshalIndent(c, "", " ")
	if err := os.MkdirAll(filepath.Dir(r.contractPath(t.ID)), 0o700); err == nil {
		_ = config.WriteFileAtomic(r.contractPath(t.ID), b, 0o600)
	}
	r.Rec.Emit(ctx, t.ID, "task.contract", map[string]any{"required": len(c.Required), "alternatives": len(c.Alternatives),
		"ambiguities": len(c.Ambiguities), "material": len(c.Material())})
	return &c
}

// ambiguityGate applies the ambiguity policy. It reports whether the task
// must stop for clarification, and the questions.
func (r *Runner) ambiguityGate(ctx context.Context, t *task.Task, c *task.Contract, clarified bool) (bool, string) {
	if c == nil {
		return false, ""
	}
	m := c.Material()
	if len(m) == 0 {
		return false, ""
	}
	var qs []string
	for _, a := range m {
		qs = append(qs, fmt.Sprintf("%s (affects %s): %s", a.Question, a.Affects, strings.Join(a.Interpretations, " / ")))
	}
	questions := strings.Join(qs, "\n- ")
	if r.hasEvent(ctx, t.ID, "task.ambiguous") && (clarified || r.hasEvent(ctx, t.ID, "task.clarified")) {
		return false, "" // the user answered; do not ask again
	}
	r.Rec.Emit(ctx, t.ID, "task.ambiguous", map[string]any{"policy": r.Cfg.Task.Ambiguity, "questions": qs})
	if r.Cfg.Task.Ambiguity == AmbiguityProceed {
		t.Decide("policy", "SPEC_AMBIGUOUS (proceeding by policy): "+oneLineStr(questions, 600))
		return false, ""
	}
	return true, questions
}

func oneLineStr(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// contractPackText is the contract as the context pack shows it.
func contractPackText(c *task.Contract, proceedingAmbiguous bool) string {
	if c == nil {
		return ""
	}
	s := c.Render()
	if len(c.Alternatives) > 0 {
		s += "\nThe request accepts more than one outcome where listed: implement one option, say which in your final message, and make your test demonstrate it."
	}
	if proceedingAmbiguous {
		s += "\nThe request is materially ambiguous on the points above: choose the reading best supported by the request, state it in your final message, and make your test demonstrate it."
	}
	return s
}
