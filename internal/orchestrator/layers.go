package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/compat"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

// ContractCallCounterexample is the independent verifier's call: it sees
// the request and the diff, never the author's conversation.
const ContractCallCounterexample = "counterexample"

// repoEvidence is one repository's behavioural evidence.
type repoEvidence struct {
	ev     verify.Evidence
	needed bool
}

// layerAssessment is what the optional layers and the classification add
// to a passing candidate.
type layerAssessment struct {
	layers   []task.EvidenceLayer
	withheld []string // reasons beyond behavioural evidence and contracts
	strength string
	// advice is a request to the agent (once per kind), adviceKind what it is.
	advice, adviceKind string
}

// assessLayers runs the integrity review, mutation assessment and
// counterexample checks on each changed repository and classifies every
// layer. A weakness the agent can address (a test-runner change, surviving
// mutants, a failing counterexample) is asked about once; afterwards it is
// recorded, and withholds task_verified only where a layer is required or
// the change touches how its tests run.
func (r *Runner) assessLayers(ctx context.Context, t *task.Task, wts []task.Worktree, changedRepos []string, full map[string]verify.Result,
	evidence map[string]repoEvidence, gate *compat.Report, roundsLeft bool) layerAssessment {
	vc := r.Cfg.Task.Verification
	strict := vc.MutationMode() == "strict"
	req := verify.Requirements{Mutation: vc.MutationMode() == "required" || strict, Boundaries: strict, Counterexamples: vc.CounterexampleMode() == "required"}
	contracts := ""
	if gate != nil {
		contracts = gate.State()
	}
	var la layerAssessment
	var strengths []string
	multi := len(changedRepos) > 1
	for _, w := range wts {
		if !slices.Contains(changedRepos, w.RepoName) {
			continue
		}
		res, ok := full[w.RepoName]
		if !ok {
			continue
		}
		target := verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID, Source: w.RepoPath}
		in := verify.LayerInputs{Full: res, Contracts: contracts, Require: req}
		if e, ok := evidence[w.RepoName]; ok {
			ev := e.ev
			in.Evidence, in.EvidenceNeeded = &ev, e.needed
		} else {
			files, err := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit)
			in.EvidenceNeeded = err != nil || verify.NeedsEvidence(files)
		}
		// Test integrity: a change to how the tests run is asked about once.
		var review []string
		for _, f := range res.Integrity {
			if f.Severity == verify.IntegrityReview {
				review = append(review, f.String())
			}
		}
		if len(review) > 0 && roundsLeft && !r.hasEvent(ctx, t.ID, "verify.integrity_requested") {
			r.Rec.Emit(ctx, t.ID, "verify.integrity_requested", map[string]any{"repo": w.RepoName, "findings": review})
			r.say("verification passed, but the change touches how its tests run; asking the agent to revert or justify it")
			la.advice, la.adviceKind = integrityRequest(review), "test-integrity review"
			return la
		}
		if in.EvidenceNeeded && vc.MutationMode() != "off" {
			budget := vc.MutationBudget.D()
			if budget <= 0 {
				budget = 5 * time.Minute
			}
			rep, err := r.Verify.Mutation(ctx, target, verify.MutationOptions{MaxMutants: vc.MutationMutants, Budget: budget})
			if err != nil {
				r.Log.Warn("mutation assessment", "repo", w.RepoName, "err", err)
			} else {
				in.Mutation = &rep
				r.Rec.Emit(ctx, t.ID, "verify.mutation", map[string]any{"repo": w.RepoName, "status": rep.Status, "score": rep.Score,
					"killed": rep.Killed, "survived": rep.Survived, "invalid": rep.Invalid, "timed_out": rep.TimedOut, "boundary": rep.Boundary,
					"reason": rep.Reason, "ms": rep.DurationMS})
				if rep.Status == verify.MutationWeak && roundsLeft && !r.hasEvent(ctx, t.ID, "verify.mutation_requested") {
					r.Rec.Emit(ctx, t.ID, "verify.mutation_requested", map[string]any{"repo": w.RepoName, "survived": rep.Survived})
					r.say("verification passed, but mutants of the change survive the tests; asking the agent to strengthen them")
					la.advice, la.adviceKind = mutationRequest(rep), "stronger tests"
					return la
				}
			}
		}
		if in.EvidenceNeeded && vc.CounterexampleMode() != "off" {
			rep := r.counterexamples(ctx, t, w, target)
			in.Counter = &rep
			if rep.Status == verify.CounterFound && roundsLeft && !r.hasEvent(ctx, t.ID, "verify.counterexample_requested") {
				r.Rec.Emit(ctx, t.ID, "verify.counterexample_requested", map[string]any{"repo": w.RepoName})
				r.say("an independent check wrote a test that fails on the change; asking the agent to examine it")
				la.advice, la.adviceKind = counterexampleRequest(rep), "counterexample review"
				return la
			}
		}
		cls := verify.Classify(in)
		r.Rec.Emit(ctx, t.ID, "verify.layers", map[string]any{"repo": w.RepoName, "label": cls.Label, "strength": cls.Strength, "withheld": cls.Withheld,
			"layers": cls.Layers})
		for _, l := range cls.Layers {
			la.layers = append(la.layers, task.EvidenceLayer{Repo: w.RepoName, Layer: l.Layer, Name: l.Name, Status: l.Status, Detail: l.Detail, Required: l.Required})
		}
		for _, wh := range cls.Withheld {
			// Behavioural evidence and contracts are judged by the runner
			// already (behaviourEvidence, the compat gate).
			if strings.HasPrefix(wh, "no test shows the change") || strings.HasPrefix(wh, "cross-repository contracts") {
				continue
			}
			if multi {
				wh = w.RepoName + ": " + wh
			}
			la.withheld = append(la.withheld, wh)
		}
		for _, s := range cls.Strength {
			if multi {
				s = w.RepoName + ": " + s
			}
			strengths = append(strengths, s)
		}
	}
	la.strength = strings.Join(strengths, ", ")
	return la
}

func integrityRequest(findings []string) string {
	return "Verification passed, but your change touches how the tests run, which makes their result untrustworthy:\n- " +
		strings.Join(findings, "\n- ") + "\nRevert these changes unless the task explicitly asks for them. Do not skip, disable, " +
		"delete or weaken tests, and do not change the test runner's configuration to make a check pass. If the task does require one of " +
		"these changes, keep it and say why in your final message."
}

func mutationRequest(rep verify.MutationReport) string {
	var b strings.Builder
	b.WriteString("Verification passed, but the tests may not check the code you changed: when the code is broken in these ways, every test still passes:\n")
	n := 0
	for _, m := range rep.Mutants {
		if m.Status == verify.MutantSurvived && m.Operator != "boundary" && n < 6 {
			fmt.Fprintf(&b, "- %s line %d: `%s` changed to `%s`\n", m.File, m.Line, m.Orig, m.Repl)
			n++
		}
	}
	b.WriteString("Add assertions to the tests of this behaviour that would fail if the code were changed like that, using the expected results " +
		"the task gives. Do not change the code to suit the tests, and do not add tests that only check the code runs.")
	return b.String()
}

func counterexampleRequest(rep verify.CounterexampleReport) string {
	var b strings.Builder
	b.WriteString("An independent check (not the task's author) proposed tests for the requested behaviour; run under containment, these fail on your change:\n")
	for _, ct := range rep.Tests {
		if ct.Outcome == verify.CounterRegression || ct.Outcome == verify.CounterFailing {
			fmt.Fprintf(&b, "- %s: %s\n", ct.File, ct.Detail)
		}
	}
	b.WriteString("Generated tests can be wrong. Decide from the task's request whether each one is: if it shows a real defect, fix the code and add an " +
		"equivalent test of your own; if its expectation is wrong, leave the code and explain why in your final message.")
	return b.String()
}

const counterexamplePrompt = `You are an independent verifier of a code change. You did not write it. Find inputs or scenarios where the change does not do what the request asks, or breaks behaviour that worked before.

Write at most 2 new test files, in the project's own test framework, language and conventions, placed where the project keeps its tests (a new file name that does not exist yet). Each test must assert the behaviour the request asks for, or behaviour that must not change, with concrete expected values from the request. Do not test implementation details. Do not modify existing files.

Answer with one JSON object and nothing else:
{"tests": [{"path": "repository-relative/path/to/new_test_file", "content": "the whole file"}]}
`

var jsonObjectRE = regexp.MustCompile(`(?s)\{.*\}`)

// counterexamples asks the task's model, in a context of its own, for tests
// meant to expose the change, and runs them (verify.Counterexamples).
func (r *Runner) counterexamples(ctx context.Context, t *task.Task, w task.Worktree, target verify.RepoTarget) verify.CounterexampleReport {
	diff, err := gitops.Diff(ctx, w.Path, w.BaseCommit, false)
	if err != nil {
		return verify.CounterexampleReport{Status: verify.CounterUnavailable, Reason: "no diff: " + err.Error()}
	}
	example := exampleTest(w.Path, t.ChangedFiles)
	prompt := counterexamplePrompt + "\nRequest:\n" + contractInput(t) + "\n\nThe change (diff against the base):\n" + trunc(diff, 12000)
	if example != "" {
		prompt += "\n\nAn existing test of this project, for its conventions:\n" + example
	}
	reply, err := r.chatReader(ctx, t, ContractCall{Purpose: ContractCallCounterexample, Request: contractInput(t),
		Messages: []ChatMessage{{Role: "user", Content: prompt}}}, 3000, 0.2)
	if err != nil {
		return verify.CounterexampleReport{Status: verify.CounterUnavailable, Reason: "the verifier model is unavailable: " + trunc(err.Error(), 200)}
	}
	var out struct {
		Tests []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"tests"`
	}
	if m := jsonObjectRE.FindString(reply); m == "" || json.Unmarshal([]byte(m), &out) != nil || len(out.Tests) == 0 {
		return verify.CounterexampleReport{Status: verify.CounterInconclusive, Reason: "the verifier proposed no usable test", Generator: "task model (independent context)"}
	}
	files := map[string]string{}
	for i, tc := range out.Tests {
		if i == 2 {
			break
		}
		files[tc.Path] = tc.Content
	}
	rep, err := r.Verify.Counterexamples(ctx, target, files)
	if err != nil {
		return verify.CounterexampleReport{Status: verify.CounterUnavailable, Reason: trunc(err.Error(), 200)}
	}
	rep.Generator = "task model (independent context)"
	r.Rec.Emit(ctx, t.ID, "verify.counterexamples", map[string]any{"repo": w.RepoName, "status": rep.Status, "tests": rep.Tests, "ms": rep.DurationMS})
	return rep
}

// exampleTest is the start of an existing test file next to the change.
func exampleTest(root string, changed []string) string {
	for _, f := range changed {
		if _, rel, ok := strings.Cut(f, ":"); ok {
			f = rel
		}
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(f)))
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			rel, _ := filepath.Rel(root, filepath.Join(dir, e.Name()))
			if !e.IsDir() && verify.IsTestFile(filepath.ToSlash(rel)) {
				b, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err == nil {
					return filepath.ToSlash(rel) + ":\n" + trunc(string(b), 3000)
				}
			}
		}
	}
	return ""
}
