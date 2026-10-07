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
	"github.com/akynte/boundedcode/internal/telemetry"
)

// Ambiguity policies (config task.ambiguity).
const (
	AmbiguityAsk     = "ask"     // block a materially ambiguous task until the user clarifies it
	AmbiguityProceed = "proceed" // record SPEC_AMBIGUOUS, have the agent state and demonstrate its reading
)

// Purposes of the local-model calls that read the request (ContractCall).
const (
	ContractCallDerive = "contract"  // derive the contract (and its one corrective retry)
	ContractCallGround = "grounding" // check material ambiguities against the request text
)

// ContractCall is one local-model chat call made while reading the request
// before implementation.
type ContractCall struct {
	Purpose  string
	Request  string        // the request text the call reads (HTML comments removed)
	Messages []ChatMessage // the chat as sent, in order
}

// ChatMessage is one chat turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const contractPrompt = `Extract a compact task contract from the software change request below.
Use only what the request says: the request stays authoritative, and the contract must not add requirements or readings the request does not support.
Reply with one JSON object and nothing else:
{"required": [], "acceptable_alternatives": [{"point": "", "options": []}], "constraints": [],
 "explicitly_not_required": [], "unknown_or_ambiguous": [{"question": "", "interpretations": [], "affects": "", "material": false}],
 "acceptance_evidence": []}
Rules:
- required: what the request asks for; never empty. For a bug report, the expected (correct) behaviour it describes; for a question or a proposal, the change it asks about or proposes; otherwise the changes or behaviours it explicitly asks for.
- acceptable_alternatives: ONLY where the request itself says more than one outcome is acceptable (for example "either X or Y", "X, or alternatively Y"). Each option is a complete outcome.
- constraints: limits the request states (compatibility, performance, APIs that must not change).
- explicitly_not_required: what the request puts out of scope.
- unknown_or_ambiguous: points the request leaves open. Do not list a point its examples, expected outputs, error messages or reproduction steps already settle. Each interpretation must be a reading the request's own words support. material=true only when plausible readings would change observable behaviour, an API, data, security, compatibility or tests; "affects" names which. Do not list wording vagueness or implementation details an engineer may choose freely.
- acceptance_evidence: concrete checks, using the request's own inputs and expected results where it gives them.
Keep each list to at most 5 items of at most 25 words.

Request:
`

// contractRetryPrompt follows a contract that names nothing required (2 of
// 6 derivations in the second validation: a bug report ending in a
// question, and a one-line proposal under an issue template).
const contractRetryPrompt = `Your contract lists nothing under "required". Every change request asks for something: for a bug report, the expected (correct) behaviour it describes; for a question or a proposal, the change it asks about or proposes. Reply with the complete JSON object again, with "required" filled in from the request.`

const groundingPrompt = `Below are open questions raised about a software change request. Check each one against the request text alone.
For each point:
- settled: true only if the request already answers the question, including through its examples, expected outputs, error messages or reproduction steps.
- settled_by: when settled, the exact words from the request that answer it; otherwise "".
- readings: one entry per interpretation, in order: the exact words from the request that support that interpretation, or "" if the request does not support it.
Copy quotes from the request character for character; do not paraphrase, shorten with "..." or combine passages.
Reply with one JSON object and nothing else:
{"points": [{"id": 1, "settled": false, "settled_by": "", "readings": ["", ""]}]}

Request:
`

// chatReader sends one request-reading call to the local model, or to the
// ContractModel test hook when set.
func (r *Runner) chatReader(ctx context.Context, t *task.Task, call ContractCall, maxTokens int, temperature float64) (string, error) {
	if r.ContractModel != nil {
		return r.ContractModel(ctx, call)
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
		"messages":             call.Messages,
		"max_tokens":           maxTokens,
		"temperature":          temperature,
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
		return "", fmt.Errorf("%s call: status %d", call.Purpose, status)
	}
	return resp.Choices[0].Message.Content, nil
}

// deriveContract asks the local model for the task contract. A contract
// that names nothing required is asked for once more with a corrective
// follow-up. It returns the last raw reply (for diagnosis) and whether it
// retried.
func (r *Runner) deriveContract(ctx context.Context, t *task.Task, req string) (c task.Contract, reply string, retried bool, err error) {
	msgs := []ChatMessage{{Role: "user", Content: contractPrompt + req}}
	reply, err = r.chatReader(ctx, t, ContractCall{Purpose: ContractCallDerive, Request: req, Messages: msgs}, 1500, 0.2)
	if err != nil {
		return c, "", false, err
	}
	c, err = task.ParseContract(reply)
	if !errors.Is(err, task.ErrNothingRequired) {
		return c, reply, false, err
	}
	msgs = append(msgs, ChatMessage{Role: "assistant", Content: reply}, ChatMessage{Role: "user", Content: contractRetryPrompt})
	again, cerr := r.chatReader(ctx, t, ContractCall{Purpose: ContractCallDerive, Request: req, Messages: msgs}, 1500, 0.2)
	if cerr != nil {
		return c, reply, true, fmt.Errorf("%w; retry: %w", err, cerr)
	}
	c, err = task.ParseContract(again)
	return c, again, true, err
}

// contractRequest is the request with any clarification, as the task
// records it.
func contractRequest(t *task.Task) string {
	req := t.OriginalRequest
	if t.Goal != "" && t.Goal != t.OriginalRequest {
		req += "\n\n" + t.Goal
	}
	return req
}

// contractInput is the request text the contract calls read: issue-template
// comments are not part of what is asked.
func contractInput(t *task.Task) string {
	return task.StripHTMLComments(contractRequest(t))
}

// rawReply is a model reply as an event carries it: redacted and bounded.
func rawReply(s string) string {
	return strings.ToValidUTF8(trunc(telemetry.Redact(s), 2000), "")
}

func (r *Runner) contractPath(taskID string) string {
	return filepath.Join(r.Paths.TaskDir(taskID), "contract.json")
}

// loadContract returns the task's persisted contract, if any. Contracts
// written before the grounding check load too (their ambiguities have no
// grounding record).
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

func (r *Runner) saveContract(taskID string, c *task.Contract) {
	b, _ := json.MarshalIndent(c, "", " ")
	if err := os.MkdirAll(filepath.Dir(r.contractPath(taskID)), 0o700); err == nil {
		_ = config.WriteFileAtomic(r.contractPath(taskID), b, 0o600)
	}
}

// ensureContract derives and persists the contract once per task (again
// after a clarification), then checks its material ambiguities against the
// request (groundAmbiguities). It returns nil when contracts are disabled or
// the model's answer is unusable: the task then runs without one.
func (r *Runner) ensureContract(ctx context.Context, t *task.Task) *task.Contract {
	if !r.Cfg.Task.Contract {
		return nil
	}
	req := contractInput(t)
	if c, ok := r.loadContract(t.ID); ok {
		// Ambiguities checked before are not checked again; unchecked ones
		// (an earlier check failed, or the contract predates it) are.
		if r.groundAmbiguities(ctx, t, c, req) {
			r.saveContract(t.ID, c)
		}
		return c
	}
	c, reply, retried, err := r.deriveContract(ctx, t, req)
	if err != nil {
		ev := map[string]any{"error": trunc(err.Error(), 300), "retried": retried}
		if reply != "" {
			ev["reply"] = rawReply(reply)
		}
		r.Rec.Emit(ctx, t.ID, "task.contract_failed", ev)
		r.Log.Warn("task contract unavailable; continuing without one", "err", err)
		return nil
	}
	r.groundAmbiguities(ctx, t, &c, req)
	r.saveContract(t.ID, &c)
	r.Rec.Emit(ctx, t.ID, "task.contract", map[string]any{"required": len(c.Required), "alternatives": len(c.Alternatives),
		"ambiguities": len(c.Ambiguities), "material": len(c.Material()), "retried": retried})
	return &c
}

// groundAmbiguities checks the contract's unchecked material ambiguities
// against the request text before the ambiguity gate can block on them. The
// model is asked (one call, only when there is something to check) whether
// the request already settles each one and which of its words support each
// interpretation; task.Ground then decides from quotes that occur in the
// request. A demotion is recorded as a decision with its reason. If the
// call fails, the ambiguities stay material (fail safe) and stay unchecked,
// so a later run checks them again. It reports whether c changed.
func (r *Runner) groundAmbiguities(ctx context.Context, t *task.Task, c *task.Contract, req string) bool {
	var idx []int
	for i, a := range c.Ambiguities {
		if a.IsMaterial() && a.Grounding == nil {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return false
	}
	var b strings.Builder
	b.WriteString(groundingPrompt + req + "\n\nPoints:\n")
	for k, i := range idx {
		a := c.Ambiguities[i]
		fmt.Fprintf(&b, "%d. question: %s\n", k+1, a.Question)
		for j, in := range a.Interpretations {
			fmt.Fprintf(&b, "   interpretation %d: %s\n", j+1, in)
		}
	}
	call := ContractCall{Purpose: ContractCallGround, Request: req, Messages: []ChatMessage{{Role: "user", Content: b.String()}}}
	reply, err := r.chatReader(ctx, t, call, 1000, 0)
	var answers []task.GroundingAnswer
	if err == nil {
		answers, err = task.ParseGrounding(reply)
	}
	if err != nil {
		ev := map[string]any{"error": trunc(err.Error(), 300), "unchecked": len(idx)}
		if reply != "" {
			ev["reply"] = rawReply(reply)
		}
		r.Rec.Emit(ctx, t.ID, "task.grounding_failed", ev)
		r.Log.Warn("ambiguity grounding unavailable; the ambiguities stay material", "err", err)
		return false
	}
	byID := map[int]task.GroundingAnswer{}
	for _, a := range answers {
		if a.ID < 1 || a.ID > len(idx) {
			// Numbered differently from the points sent: answers cannot be
			// matched to questions, so none is used.
			r.Rec.Emit(ctx, t.ID, "task.grounding_failed", map[string]any{"error": fmt.Sprintf("answer id %d outside 1..%d", a.ID, len(idx)),
				"unchecked": len(idx), "reply": rawReply(reply)})
			r.Log.Warn("ambiguity grounding answers do not match the points; the ambiguities stay material")
			return false
		}
		if _, dup := byID[a.ID]; !dup {
			byID[a.ID] = a
		}
	}
	var demoted []map[string]any
	kept, unanswered := 0, 0
	for k, i := range idx {
		ans, ok := byID[k+1]
		a := &c.Ambiguities[i]
		if !ok || !ans.Complete(len(a.Interpretations)) {
			unanswered++ // stays material and unchecked
			continue
		}
		g := task.Ground(req, *a, ans)
		a.Grounding = &g
		if g.Demoted == "" {
			kept++
			continue
		}
		t.Decide("policy", "ambiguity not raised: "+oneLineStr(a.Question, 200)+": "+g.Demoted)
		demoted = append(demoted, map[string]any{"question": oneLineStr(a.Question, 200), "reason": g.Demoted})
	}
	r.Rec.Emit(ctx, t.ID, "task.ambiguity_grounded", map[string]any{"checked": len(idx) - unanswered, "kept": kept,
		"unanswered": unanswered, "demoted": demoted})
	return len(idx) > unanswered
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
