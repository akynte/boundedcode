// Package workflow defines the persisted architecture-review phase contract.
package workflow

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/akynte/boundedcode/internal/contextpack"
	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
)

type Phase string

const (
	Intake   Phase = "INTAKE"
	Localize Phase = "LOCALIZE"
	Impact   Phase = "IMPACT"
	Planning Phase = "PLAN"
	Edit     Phase = "EDIT"
	Verify   Phase = "VERIFY"
	Review   Phase = "REVIEW"
	Finalize Phase = "FINALIZE"
)

func Transition(from, to Phase) error {
	allowed := map[Phase][]Phase{
		Intake: {Localize}, Localize: {Impact}, Impact: {Planning}, Planning: {Edit},
		Edit: {Verify, Planning, Localize}, Verify: {Edit, Review, Localize, Planning},
		Review: {Edit, Localize, Finalize}, Finalize: {},
	}
	for _, next := range allowed[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("workflow: forbidden transition %s -> %s", from, to)
}

type Obligation struct {
	Symbol     string     `json:"symbol"`
	Path       string     `json:"path"`
	Reason     string     `json:"reason"`
	Resolution Resolution `json:"resolution,omitzero"`
}

// ResolutionAction is the closed set of things a plan may say about a
// consumer it affects.
//
// It was free text with the rule written in a schema description, and the
// validator matched `"edit"` exactly and `"no change needed:"` by prefix.
// Across six recorded plans the model wrote `fix`, `keep_passing`, `verify`
// and prose, and every one of them was refused by a contract it had never
// been given. Control semantics carried in a prose prefix is a contract only
// the validator can read.
type ResolutionAction string

const (
	// ActionEdit: the consumer is changed, and its file must be writable.
	ActionEdit ResolutionAction = "edit"
	// ActionNoChange: the consumer keeps working unchanged, and the reason
	// says concretely why.
	ActionNoChange ResolutionAction = "no_change_needed"
)

// Resolution is what the plan does about one affected consumer.
type Resolution struct {
	Action ResolutionAction `json:"action"`
	Reason string           `json:"reason,omitempty"`
}

// Valid reports a resolution the validator and the schema both accept. The
// two now describe the same domain, which is the whole point of the type.
func (r Resolution) Valid() bool {
	switch r.Action {
	case ActionEdit:
		return true
	case ActionNoChange:
		return strings.TrimSpace(r.Reason) != ""
	}
	return false
}

// Target is a repository path with the planner's rationale beside it rather
// than inside it.
//
// The recorded failure: a plan named
//
//	src/_pytest/compat.py (num_mock_patch_args, lines 62-73: replace …)
//
// which is the correct file with an explanation appended, and the whole plan
// was refused because that string is not a path. One field cannot hold two
// things. A bare string is still accepted when decoding so that existing
// task files and fixtures keep working; what the model is asked for is the
// object.
type Target struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

func (t *Target) UnmarshalJSON(b []byte) error {
	var asString string
	if err := json.Unmarshal(b, &asString); err == nil {
		t.Path, t.Reason = asString, ""
		return nil
	}
	type plain Target
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*t = Target(v)
	return nil
}

// Targets is a list of repository paths.
type Targets []Target

// Paths renders the list as the plain paths the rest of the system uses.
func (ts Targets) Paths() []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Path)
	}
	return out
}

type Plan struct {
	RootCause      string       `json:"root_cause"`
	Files          Targets      `json:"files"`
	Symbols        []string     `json:"symbols"`
	Tests          []string     `json:"tests"`
	Contracts      []string     `json:"contracts"`
	WriteAllowlist []string     `json:"write_allowlist"`
	Risks          []string     `json:"risks"`
	Obligations    []Obligation `json:"obligations"`
	// Regenerate names frozen presets of kind "generate" that this change
	// makes stale. Declaring one is how a plan changes generated code: the
	// supervisor runs the generator, so what lands is what the generator
	// produces rather than what a model believed it would produce.
	Regenerate []string `json:"regenerate"`
}

// DeclareWriteGrants adds to Files every write grant the plan did not also
// declare there, and reports which it added.
//
// A grant is the plan saying it will write the file, so the declaration it
// left out is not in doubt. The recorded failure: a plan granted itself
// internal/worker/reconcile_test.go — the new regression test the task
// needed — without listing it under files, Validate refused the whole plan
// for it, and the correction that followed talked the planner out of the
// test file altogether. Everything Validate and the target check require of
// a declared file still applies to the ones added here.
func (p *Plan) DeclareWriteGrants() (added []string) {
	declared := map[string]bool{}
	for _, f := range p.Files {
		declared[f.Path] = true
	}
	for _, f := range p.WriteAllowlist {
		if declared[f] {
			continue
		}
		declared[f] = true
		p.Files = append(p.Files, Target{Path: f, Reason: "declared by its write grant"})
		added = append(added, f)
	}
	return added
}

// FillWaiverReasons gives a no_change_needed obligation that left its
// resolution reason empty the reason the obligation itself states, and reports
// how many it filled.
//
// The recorded plan answered every owed consumer with a concrete compatibility
// reason — "calls orders.Service.Place, whose signature is preserved" — in the
// obligation's reason field, left resolution.reason empty, and was refused on
// its last round for three waivers with no reason. It is the planner's own
// statement, in the field beside the one it was checked in.
func (p *Plan) FillWaiverReasons() int {
	filled := 0
	for i := range p.Obligations {
		o := &p.Obligations[i]
		if o.Resolution.Action == ActionNoChange && strings.TrimSpace(o.Resolution.Reason) == "" &&
			strings.TrimSpace(o.Reason) != "" {
			o.Resolution.Reason = o.Reason
			filled++
		}
	}
	return filled
}

// RegeneratesGenerated reports whether the plan declared any generator, which
// is what makes a changed generated file an expected outcome rather than an
// edit outside the allowlist.
func (p Plan) RegeneratesGenerated() bool { return len(p.Regenerate) > 0 }

// ValidateRegeneration resolves each declared generator against the presets
// frozen during INTAKE. A model cannot name a command; it can only select one
// the operator already froze, and only one whose kind is generation.
func (p Plan) ValidateRegeneration(presets []recipe.Preset) error {
	for _, name := range p.Regenerate {
		var found *recipe.Preset
		for i := range presets {
			if presets[i].Name == name {
				found = &presets[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("plan declares regeneration by %q, which is not a frozen verification preset", name)
		}
		if found.Kind != recipe.KindGenerate {
			return fmt.Errorf("plan declares regeneration by %q, whose kind is %q rather than generate", name, found.Kind)
		}
	}
	return nil
}

// ScopeError is a plan file outside the operator's scope. It carries the
// scope so a correction can say what is allowed: the bare "exceeds operator
// scope" named only the offending file, and a planner that had not been told
// the boundary named the same out-of-scope file in its next plan too.
type ScopeError struct {
	File  string
	Scope []string
}

func (e *ScopeError) Error() string {
	return fmt.Sprintf("plan file %q exceeds operator scope", e.File)
}

// Correction is the instruction a planner can act on.
func (e *ScopeError) Correction() string {
	return fmt.Sprintf("The plan names %q, which is outside the operator scope. Every file in "+
		"files and write_allowlist must be inside: %s. Remove that file from the plan; if the "+
		"fix seems to need it, make the change inside the scope instead.",
		e.File, strings.Join(e.Scope, ", "))
}

func (p Plan) Validate(operatorScope []string) error {
	if strings.TrimSpace(p.RootCause) == "" || len(p.Files) == 0 || len(p.WriteAllowlist) == 0 || len(p.Tests) == 0 {
		return fmt.Errorf("plan requires a hypothesis, files, write allowlist and tests")
	}
	// Exact file grants avoid an untrusted planner broadening a glob beyond
	// operator authority. New files must be declared explicitly too.
	for _, f := range append(p.Files.Paths(), p.WriteAllowlist...) {
		if f == "." || path.IsAbs(f) || path.Clean(f) != f || strings.ContainsAny(f, "*?[\\") ||
			f == ".." || strings.HasPrefix(f, "../") || policy.Sensitive(f) || strings.HasPrefix(f, ".git/") {
			return fmt.Errorf("plan contains invalid file grant %q", f)
		}
		// Granting a generated file would let the model hand-edit output its
		// generator owns. The plan says which generator to run instead.
		if firewall.Generated(f) {
			return fmt.Errorf("plan grants writes to generated file %q; declare its generator under regenerate instead", f)
		}
		if len(operatorScope) > 0 && !policy.Covers(operatorScope, f) {
			return &ScopeError{File: f, Scope: operatorScope}
		}
	}
	for _, f := range p.WriteAllowlist {
		found := false
		for _, declared := range p.Files {
			found = found || declared.Path == f
		}
		if !found {
			return fmt.Errorf("write grant %q is not a declared file", f)
		}
	}
	return nil
}

// Transcript is append-only during an EDIT phase. Pending records the crash
// window around a tool side effect; it must be reconciled, never replayed.
type Transcript struct {
	Closed          bool          `json:"closed"`
	Summary         string        `json:"summary"`
	ClaimsDone      bool          `json:"claims_done"`
	BudgetExhausted bool          `json:"budget_exhausted"`
	Truncated       bool          `json:"truncated"`
	FenceToken      string        `json:"fence_token"`
	Messages        []llm.Message `json:"messages"`
	Tokens          int           `json:"tokens"`
	Steps           int           `json:"steps"`
	Tools           int           `json:"tools"`
	// VerifyRuns counts verification rounds, not calls: see
	// RecordVerification.
	VerifyRuns int `json:"verify_runs"`
	// VerifiedSinceEdit is set once the current worktree state has been
	// verified at least once, so further checks of it join that round.
	VerifiedSinceEdit bool   `json:"verified_since_edit,omitempty"`
	InvalidCalls      int    `json:"invalid_calls"`
	Pending           bool   `json:"pending"`
	Candidate         string `json:"candidate"`
	// Continuation is the supervisor's state summary for a transcript that
	// resumes after a context boundary. It is seeded into the fresh
	// conversation instead of the discarded one.
	Continuation string `json:"continuation,omitempty"`
	// Resumed marks a transcript that continues an EDIT attempt across a
	// supervisor context boundary rather than beginning a new one.
	//
	// The EDIT loop recognises a new attempt by an empty message list, and a
	// continuation empties the message list — so without this flag every
	// context boundary spent an edit attempt. On a large repository that
	// coupled two budgets that measure different things: a task whose
	// continuation budget and attempt budget were both three died after
	// three boundaries having never made, let alone failed, a single edit.
	Resumed bool `json:"resumed,omitempty"`
}

// MaxVerifyRounds bounds how many times one EDIT transcript may go back to
// verification after changing the code.
const MaxVerifyRounds = 3

// RecordVerification counts a verification call. A round is the checks run
// against one state of the worktree: build, vet, test and format of the same
// edit are one round, and the next round starts only after another edit.
//
// It counted calls. The tool asks for one check per call, so a model that
// verified one edit the way it was told — build, then vet, then test — had
// spent the whole budget, and the format check after it was refused as
// "EDIT phase budget exhausted". The phase read that as a full context, threw
// away a transcript whose worktree had just passed every check, and started a
// continuation that ran the checks again and hit the same wall.
func (t *Transcript) RecordVerification() {
	if !t.VerifiedSinceEdit {
		t.VerifyRuns++
		t.VerifiedSinceEdit = true
	}
}

// RecordEdit starts a new state of the worktree, so the next verification
// begins a new round.
func (t *Transcript) RecordEdit() { t.VerifiedSinceEdit = false }

// VerificationExhausted reports whether a verification call now would start a
// round past MaxVerifyRounds. Further checks of an already verified state are
// never refused here; the tool budget and the loop guard bound those.
func (t *Transcript) VerificationExhausted() bool {
	return !t.VerifiedSinceEdit && t.VerifyRuns >= MaxVerifyRounds
}

// ReadEvidence is what one inspection call found, in the bounded form that
// survives a context boundary.
//
// A continuation discards the conversation, and the conversation is where the
// contents of every file the model read were sitting. Carrying nothing in
// their place means the model's only move after a boundary is to read them
// all again, which refills the context and reaches the next boundary — the
// loop this record exists to break. It is built from tool results by the
// engine, never written by a model.
type ReadEvidence struct {
	// Key is the call's canonical fingerprint — the same one the loop guard
	// uses in TriedCall. Stored rather than rebuilt from the fields below,
	// because a record restored across a boundary has to land on exactly the
	// key the next identical call will compute, and a second derivation is a
	// second thing to keep in agreement.
	Key string `json:"key"`
	// Tool and Path name the call. Path is empty for a call that is not
	// about one file, such as a search.
	Tool string `json:"tool"`
	Path string `json:"path,omitempty"`
	// Detail is the part of the arguments that distinguishes one call from
	// another on the same path — a line range, a query — so "read lines
	// 1-240" and "read lines 400-600" are separate evidence.
	Detail string `json:"detail,omitempty"`
	// Digest is the answer's digest, the same one the loop guard compares.
	// Evidence whose digest changed describes a file that changed.
	Digest string `json:"digest"`
	// Decls are declaration names found in the result, extracted by a fixed
	// scan rather than by a model.
	Decls []string `json:"decls,omitempty"`
	// Excerpt is a short verbatim opening of the result.
	Excerpt string `json:"excerpt,omitempty"`
	// Bytes is the size of the full result, so the summary can say what a
	// repeat would cost.
	Bytes int `json:"bytes,omitempty"`
	// Empty records a call that returned nothing useful, which is evidence
	// too: it says where not to look again.
	Empty bool `json:"empty,omitempty"`
	// Seq orders evidence by recency. It counts calls, not steps.
	Seq int `json:"seq"`
}

type Verdict struct {
	Accept   bool     `json:"accept"`
	Findings []string `json:"findings"`
}

type State struct {
	// Prefix is §7.1's frozen region, built once and persisted. It is stored
	// rather than recomputed because a repository map rebuilt per call would
	// rank identically only by luck, and one reordered row invalidates every
	// cached token after it.
	Prefix contextpack.Prefix `json:"prefix"`
	// FenceToken is the task's fence. One per task, not one per call: the
	// token is part of the policy preamble, so a fresh one each time changed
	// the first message of every request.
	FenceToken            string            `json:"fence_token,omitempty"`
	Bodies                map[string]string `json:"localized_bodies,omitempty"`
	FinalizationCandidate string            `json:"finalization_candidate,omitempty"`
	Presets               []recipe.Preset   `json:"presets"`
	Phase                 Phase             `json:"phase"`
	Base                  string            `json:"base"`
	Candidate             string            `json:"candidate"`
	StartedAt             int64             `json:"started_at"`
	Tokens                int               `json:"tokens"`
	Attempts              int               `json:"attempts"`
	Replans               int               `json:"replans"`
	VerifyLoops           int               `json:"verify_loops"`
	Regressions           int               `json:"regressions"`
	Files                 []string          `json:"files"`
	Symbols               []string          `json:"symbols"`
	Hypothesis            string            `json:"hypothesis"`
	Plan                  Plan              `json:"plan"`
	Impact                *graph.Impact     `json:"impact,omitempty"`
	Baseline              []recipe.Result   `json:"baseline"`
	Results               []recipe.Result   `json:"results"`
	Feedback              []recipe.Result   `json:"feedback"`
	Failures              map[string]int    `json:"failures"`
	FailureHistory        []FailureRecord   `json:"failure_history"`
	Edit                  Transcript        `json:"edit"`
	Verdict               *Verdict          `json:"verdict,omitempty"`
	// StructuredRetries counts targeted retries of the current phase's
	// structured call. It is reset when a phase completes, so a task that
	// recovered twice in two phases is not one attempt from failing.
	StructuredRetries int `json:"structured_retries,omitempty"`
	// ConformanceReroutes counts how many times a judged diff-to-plan
	// conformance check (design.md mechanism M5) sent VERIFY back to EDIT
	// before running a verify recipe, capped independently of every other
	// budget: routing on a judgment is a ROUTE effect and R1 requires it stay
	// within counters the ladder already permits, but a diff that keeps
	// judging as "does not address the objective" needs its own small bound
	// rather than borrowing one that means something else.
	ConformanceReroutes int `json:"conformance_reroutes,omitempty"`
	// Structured is the running tally a reader of the results needs to tell
	// "the model could not produce the object" from "the phase never asked".
	Structured StructuredOutcome `json:"structured,omitzero"`
	// Repetition and Obligations record the other two recovery paths, for
	// the same reason.
	Repetition  RepetitionOutcome `json:"repetition,omitzero"`
	Obligations ObligationOutcome `json:"obligation_resolution,omitzero"`
	// PlanTargets records what deterministic plan validation refused.
	PlanTargets PlanTargetOutcome `json:"plan_targets,omitzero"`
	// Settled is what earlier plan rounds established; see PlanMemory.
	Settled PlanMemory `json:"plan_settled,omitzero"`
	// DecodeTPS and PrefillTPS are this task's measured model throughput in
	// tokens per second, which size calls to the time a phase has left.
	DecodeTPS  float64 `json:"decode_tps,omitempty"`
	PrefillTPS float64 `json:"prefill_tps,omitempty"`
	// Rescue records the one bounded context-rescue attempt.
	Rescue RescueOutcome `json:"context_rescue,omitzero"`
	// Budgeted says the edit loop ran out of attempts with work in the
	// worktree, so verification is judging what is there rather than what
	// the model said it had finished. It stops further model execution and
	// changes nothing about how the verifier decides.
	Budgeted bool `json:"edit_budget_exhausted,omitempty"`
	// Verification records how each preset compared with its baseline, so a
	// reader can tell "already failing" from "this broke it".
	Verification []recipe.PresetComparison `json:"verification_comparison,omitempty"`
	// EditContinuations counts how many times the EDIT phase was restarted
	// at a supervisor boundary after exhausting its context.
	EditContinuations int `json:"edit_continuations,omitempty"`
	// Tried carries the loop guard across those boundaries. It is the
	// complete canonical set: what the summary shows the model is a bounded
	// view of it, never a replacement for it.
	Tried []TriedCall `json:"tried_calls,omitempty"`
	// Reads carries what inspection already found across those boundaries.
	Reads []ReadEvidence `json:"read_evidence,omitempty"`

	// HiddenRejected lists the distinct candidates hidden acceptance checks
	// have failed, each a verdict the model was told. Persisted so a restart
	// does not reset the bound on what the model may learn about the suite.
	HiddenRejected []string `json:"hidden_rejected,omitempty"`
}

// StructuredOutcome counts what happened to this task's structured calls.
type StructuredOutcome struct {
	Attempts      int    `json:"structured_output_attempts,omitempty"`
	Truncated     int    `json:"truncated,omitempty"`
	SchemaInvalid int    `json:"schema_invalid,omitempty"`
	Recovered     bool   `json:"recovered,omitempty"`
	FinalFailure  string `json:"final_failure_reason,omitempty"`
}

// RepetitionOutcome counts the loop guard's recoveries.
type RepetitionOutcome struct {
	Exact      int `json:"exact_repetitions,omitempty"`
	Canonical  int `json:"canonical_equivalent_repetitions,omitempty"`
	Recoveries int `json:"recoveries,omitempty"`
	Terminated int `json:"terminated_after_recovery,omitempty"`
}

// ObligationOutcome counts the bounded obligation-resolution stage.
type ObligationOutcome struct {
	Total      int `json:"total,omitempty"`
	Resolved   int `json:"resolved,omitempty"`
	Searchable int `json:"unresolved_but_searchable,omitempty"`
	Expanded   int `json:"evidence_expansions,omitempty"`
	Unresolved int `json:"unresolvable,omitempty"`
}

// RescueOutcome records the bounded context rescue, so "the planner had
// nothing to work with" is a fact in the record rather than an inference
// from a bad plan.
type RescueOutcome struct {
	Attempted  bool `json:"context_rescue_attempted,omitempty"`
	Candidates int  `json:"context_rescue_candidates,omitempty"`
	Added      int  `json:"context_rescue_added,omitempty"`
	// OutOfScope counts files retrieval found and the rescue refused because
	// the operator's scope does not cover them. It is recorded rather than
	// dropped: a task whose evidence is mostly out of scope is a scope that
	// is wrong, and that is an operator decision nobody can make from a
	// reason string alone.
	OutOfScope int `json:"context_rescue_out_of_scope,omitempty"`
	// AlreadyLocalized counts retrieval hits that were already in s.Files.
	// It is the difference between "the rescue found nothing" and "the
	// rescue found exactly what localization had already found", which are
	// opposite statements about whether the task can be planned.
	AlreadyLocalized int    `json:"context_rescue_already_localized,omitempty"`
	Reason           string `json:"context_rescue_reason,omitempty"`
}

// PlanTargetOutcome counts deterministic plan-target validation.
type PlanTargetOutcome struct {
	Checked       int `json:"checked,omitempty"`
	Invalid       int `json:"invalid_targets_detected,omitempty"`
	Regenerations int `json:"planner_regenerations,omitempty"`
	// Refused names the targets that were rejected and why, most recent last
	// and bounded. A count alone says a plan failed validation; the name
	// alone still does not say whether the planner invented a path, named a
	// symbol the graph lacks, or wrote a malformed field — those have
	// different causes and different fixes. Without both a failed task
	// cannot be diagnosed after the fact, only re-run.
	Refused []string `json:"refused_targets,omitempty"`
}

// MaxRefusedTargets bounds what PlanTargetOutcome keeps. Two correction
// rounds over a handful of targets is the shape this records; a plan that
// produced more than this has a problem the first few already show.
const MaxRefusedTargets = 12

// Refuse records a rejected target, keeping the most recent ones.
func (o *PlanTargetOutcome) Refuse(target string) {
	if target == "" {
		return
	}
	for _, seen := range o.Refused {
		if seen == target {
			return
		}
	}
	o.Refused = append(o.Refused, target)
	if len(o.Refused) > MaxRefusedTargets {
		o.Refused = o.Refused[len(o.Refused)-MaxRefusedTargets:]
	}
}

// TriedCall is one tool call the loop guard has already seen. It is persisted
// state rather than engine state because it has to outlive a phase boundary:
// the conversation is discarded there, and the record of what was already
// tried and found unhelpful must not be.
type TriedCall struct {
	// Fingerprint identifies the call: its name and canonicalised arguments.
	Fingerprint string `json:"fingerprint"`
	// Digest is the answer it last gave, so the same call with a different
	// answer still counts as new information.
	Digest string `json:"digest"`
	Count  int    `json:"count"`
	// Corrected records that the supervisor already told the model to stop
	// making this call.
	Corrected bool `json:"corrected,omitempty"`
}

// PlanMemory is what earlier plan rounds of a task established, kept so a
// rewritten plan cannot silently undo it.
//
// A planner answers a correction by writing the whole plan again, and the
// recorded rewrites undid earlier fixes: one dropped an obligation the round
// before had satisfied, another named an out-of-scope file the round before
// had removed. Each cost a correction, and the task ran out of them with the
// diagnosis right. The supervisor keeps the model's own settled statements and
// its refusals; it never writes a plan's content itself.
type PlanMemory struct {
	// Obligations are obligations a plan wrote that discharged an owed
	// consumer. A later plan that drops one has it restored while it is still
	// valid for that plan.
	Obligations []Obligation `json:"obligations,omitempty"`
	// Refused maps a target — a file or a symbol — to why it was refused, so
	// naming it again is answered as a repeat.
	Refused map[string]string `json:"refused,omitempty"`
	// FreeRoundUsed records that the one correction round that only repeated
	// earlier refusals has been given without spending the budget.
	FreeRoundUsed bool `json:"free_round_used,omitempty"`
}

// MaxSettledObligations bounds PlanMemory.Obligations.
const MaxSettledObligations = 64

// Settle records an obligation that discharged a consumer.
func (m *PlanMemory) Settle(o Obligation) {
	for i, have := range m.Obligations {
		if have.Path == o.Path && have.Symbol == o.Symbol {
			m.Obligations[i] = o
			return
		}
	}
	if len(m.Obligations) < MaxSettledObligations {
		m.Obligations = append(m.Obligations, o)
	}
}

// Refuse records why a target was refused.
func (m *PlanMemory) Refuse(target, why string) {
	if target == "" {
		return
	}
	if m.Refused == nil {
		m.Refused = map[string]string{}
	}
	if _, seen := m.Refused[target]; !seen && len(m.Refused) < MaxRefusedTargets*4 {
		m.Refused[target] = why
	}
}

// Restore gives plan every settled obligation it dropped, or answered in a way
// that no longer counts, while the settled one is still valid for it, and
// reports how many it restored. An edit resolution is valid only while its
// file is in the plan's write_allowlist; a no-change resolution only while it
// carries its reason.
//
// A dropped answer was the first recorded regression; the second kept the
// consumer and changed a valid no_change_needed into an edit of a file the
// plan could not write, and a present-but-invalid answer was left alone.
func (m PlanMemory) Restore(plan *Plan) int {
	counts := func(o Obligation) bool {
		return o.Resolution.Valid() &&
			(o.Resolution.Action != ActionEdit || policy.Covers(plan.WriteAllowlist, o.Path))
	}
	restored := 0
	for _, o := range m.Obligations {
		if !counts(o) {
			continue
		}
		present, answered := -1, false
		for i, have := range plan.Obligations {
			if have.Path == o.Path && have.Symbol == o.Symbol {
				if counts(have) {
					answered = true
				} else if present < 0 {
					present = i
				}
			}
		}
		switch {
		case answered:
		case present >= 0:
			plan.Obligations[present] = o
			restored++
		default:
			plan.Obligations = append(plan.Obligations, o)
			restored++
		}
	}
	return restored
}
