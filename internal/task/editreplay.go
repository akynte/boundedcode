package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/worktree"
)

// Development-only: start a task at EDIT from a recorded pre-EDIT state.
//
// This exists to compare engines, not to run work. Two models asked to solve
// the same task from scratch are not comparable — LOCALIZE and PLAN are model
// calls too, so each one edits against evidence it selected for itself, and
// the EDIT comparison is confounded by everything upstream of it. Seeding the
// same recorded state into both makes the tool loop the only variable.
//
// It is not a benchmark path and nothing in the evaluation uses it: the
// seed is written by hand, so a number produced through here describes the
// seed's author as much as the model.

// EditReplaySeed is the model-independent state EDIT starts from.
//
// Every field is something EDIT legitimately receives at the start of a real
// run. What is deliberately absent matters as much: no gold patch, no test
// patch, no acceptance material, no transcript or tried-call history from a
// previous engine. A seed carrying any of those would be measuring recall.
type EditReplaySeed struct {
	// Hypothesis, Files, Symbols and Bodies are LOCALIZE's outputs.
	Hypothesis string            `json:"hypothesis"`
	Files      []string          `json:"files"`
	Symbols    []string          `json:"symbols"`
	Bodies     map[string]string `json:"bodies,omitempty"`
	// Impact is IMPACT's output.
	Impact *graph.Impact `json:"impact,omitempty"`
	// Plan is the accepted plan, as PLAN would have produced it.
	Plan workflow.Plan `json:"plan"`
}

// LoadEditReplaySeed reads a seed from disk. The file is the unit of
// reproducibility: the same bytes give every engine the same starting point.
func LoadEditReplaySeed(path string) (*EditReplaySeed, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seed EditReplaySeed
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seed); err != nil {
		return nil, fmt.Errorf("edit replay seed %s: %w", path, err)
	}
	return &seed, nil
}

// SeedEditReplay advances a fresh workflow state to the start of EDIT.
//
// It performs INTAKE's deterministic work — preset discovery and the frozen
// context prefix — and then supplies LOCALIZE's, IMPACT's and PLAN's outputs
// from the seed rather than calling a model for them. The phase chain is
// walked through SaveWorkflow so the recorded transitions are the ordinary
// legal ones and a later run resumes exactly as any interrupted task does.
//
// The baseline is deliberately not measured here. It is VERIFY's input, not
// EDIT's — nothing in the EDIT prompt carries it — and running the presets
// would make seeding depend on the host toolchain being able to build a
// repository pinned to a 2019 compiler.
func (r *Runner) SeedEditReplay(ctx context.Context, t *Task, wt *worktree.Worktree,
	s *workflow.State, seed *EditReplaySeed) error {

	if seed == nil {
		return nil
	}
	if s.Phase != workflow.Intake {
		// Already seeded, or genuinely mid-task. Either way this is a resume
		// and re-seeding would overwrite the state being resumed.
		return nil
	}
	if err := seed.Plan.Validate(t.Budget.Scope); err != nil {
		return fmt.Errorf("edit replay seed: the plan is not one PLAN could have produced: %w", err)
	}

	if len(s.Presets) == 0 {
		presets, err := recipe.DiscoverPresets(wt.Path, t.Verification)
		if err != nil {
			return fmt.Errorf("edit replay seed: discovering presets: %w", err)
		}
		s.Presets = presets
	}
	if err := r.ensurePrefix(ctx, t, wt, s); err != nil {
		return fmt.Errorf("edit replay seed: context prefix: %w", err)
	}

	// LOCALIZE's outputs.
	s.Hypothesis, s.Files, s.Symbols = seed.Hypothesis, seed.Files, seed.Symbols
	s.Bodies = seed.Bodies
	if err := r.advance(ctx, t, s, workflow.Localize); err != nil {
		return err
	}

	// IMPACT's output.
	s.Impact = seed.Impact
	if err := r.advance(ctx, t, s, workflow.Impact); err != nil {
		return err
	}
	if err := r.advance(ctx, t, s, workflow.Planning); err != nil {
		return err
	}

	// PLAN's output, and the task card it rewrites — §7.1's one legitimate
	// change to the frozen prefix, made here exactly as the Planning phase
	// makes it so the engine sees the prefix a real run would have.
	s.Plan = seed.Plan
	s.Prefix.TaskCard = r.taskCard(ctx, t, s).Render()
	s.Edit = workflow.Transcript{}
	return r.advance(ctx, t, s, workflow.Edit)
}

// advance records one legal phase transition.
func (r *Runner) advance(ctx context.Context, t *Task, s *workflow.State, next workflow.Phase) error {
	if err := workflow.Transition(s.Phase, next); err != nil {
		return err
	}
	previous := s.Phase
	s.Phase = next
	if err := r.Store.SaveWorkflow(ctx, t.ID, s); err != nil {
		s.Phase = previous
		return err
	}
	r.logf("task %s: replay seeded %s", t.ID, next)
	return nil
}
