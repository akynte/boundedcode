package task

import (
	"context"
	"fmt"
	"sort"

	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/worktree"
)

// Hidden acceptance checks are the operator's, applied in the verification
// snapshot after the candidate and never shown to the model. Everything here
// that could reach model context — a result's headline, its error, its
// findings — says which check and whether it passed, and nothing about what it
// asserted. A failure message that quoted the hidden test would hand the model
// the test, one retry at a time.

// hiddenApplicable lists the checks a change must satisfy.
func hiddenApplicable(suite *oracle.Suite, changed []string) []oracle.Check {
	if suite.Empty() {
		return nil
	}
	var out []oracle.Check
	for _, c := range suite.Checks {
		if c.Applies(changed) {
			out = append(out, c)
		}
	}
	return out
}

// runHidden runs the applicable checks in the snapshot. compiled is false when
// a build recipe already failed, in which case each check is recorded as
// skipped rather than run against code that does not compile.
func runHidden(ctx context.Context, runner *recipe.Runner, snap *worktree.Snapshot,
	checks []oracle.Check, candidate string, compiled bool) []recipe.Result {

	out := make([]recipe.Result, 0, len(checks))
	for _, c := range checks {
		res := recipe.Result{Recipe: hiddenName(c.ID), Kind: recipe.KindHidden, Candidate: candidate}
		switch {
		case ctx.Err() != nil:
			res.Status = recipe.Error
		case !compiled:
			res.Status = recipe.Skipped
		case len(c.Tampered(snap.Changed)) > 0:
			res.Status = recipe.Fail
			res.Summary.Headline = fmt.Sprintf(
				"hidden acceptance check %s did not pass: the change modifies a path it protects", c.ID)
			out = append(out, res)
			continue
		default:
			if err := installHidden(snap.Path, c); err != nil {
				res.Status = recipe.Error
			} else {
				res = runner.Run(ctx, recipe.Recipe{
					Name: res.Recipe, Kind: recipe.KindHidden, Argv: c.Argv,
					Timeout: c.Timeout(), Summarize: hiddenSummary,
				}, snap.Path, candidate)
			}
		}
		out = append(out, redactHidden(res, c.ID))
	}
	return out
}

func hiddenName(id string) string { return "hidden: " + id }

// installHidden writes a check's files into the snapshot, confined to it.
func installHidden(dir string, c oracle.Check) error {
	paths := make([]string, 0, len(c.Files))
	for rel := range c.Files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if err := worktree.WriteWithin(dir, rel, []byte(c.Files[rel])); err != nil {
			return err
		}
	}
	return nil
}

// hiddenSummary decides on the exit code alone and keeps none of the output.
func hiddenSummary(exitCode int, _, _ string) (recipe.Status, recipe.Summary) {
	if exitCode == 0 {
		return recipe.Pass, recipe.Summary{}
	}
	return recipe.Fail, recipe.Summary{}
}

// redactHidden replaces everything a reader of the result could learn the
// check's content from. The status, duration and artifact hash survive: the
// artifact is where a person reads the full output.
func redactHidden(res recipe.Result, id string) recipe.Result {
	res.Err = ""
	res.Summary = recipe.Summary{}
	switch res.Status {
	case recipe.Pass:
		res.Summary.Headline = fmt.Sprintf("hidden acceptance check %s passed", id)
	case recipe.Skipped:
		res.Summary.Headline = fmt.Sprintf("hidden acceptance check %s was skipped: the code does not compile", id)
	case recipe.Error:
		// A check that could not run or timed out has not passed. The
		// reason is in the artifact, not here: a timeout is itself a hint
		// about what the check exercises.
		res.Err = fmt.Sprintf("hidden acceptance check %s could not complete", id)
		res.Summary.Headline = res.Err
	default:
		res.Summary.Headline = fmt.Sprintf("hidden acceptance check %s did not pass", id)
	}
	return res
}

// CheckHidden is the completion contract's rule for hidden acceptance: every
// hidden check that ran must have passed, against this candidate. The reasons
// are the failures only.
//
// Unlike the other kinds, an Error does not get the benefit of the doubt. A
// visible check that could not run says nothing about the code; a hidden one
// that could not run is an acceptance criterion nobody has shown is met, and
// a check that times out on the candidate is usually a check the candidate
// fails. Nor is there a baseline allowance: a hidden check is the operator's
// statement of what done means, not a pre-existing condition of the tree.
func CheckHidden(results []recipe.Result, candidate string) (bool, []string) {
	ok := true
	var reasons []string
	for _, res := range results {
		if res.Kind != recipe.KindHidden {
			continue
		}
		switch {
		case res.Status != recipe.Pass:
			ok = false
			reasons = append(reasons, res.Summary.Headline)
		case candidate == "" || res.Candidate == "":
			ok = false
			reasons = append(reasons, fmt.Sprintf(
				"%s passed without candidate identity; rerun hidden acceptance on the current checkout", res.Recipe))
		case res.Candidate != candidate:
			ok = false
			reasons = append(reasons, fmt.Sprintf(
				"%s passed, but against an older candidate (%s)", res.Recipe, shortHash(res.Candidate)))
		}
	}
	return ok, reasons
}

// DefaultHiddenFeedbackRounds is the default bound on failing hidden verdicts
// a task's model may see.
const DefaultHiddenFeedbackRounds = 3

// spendHiddenFeedback records a failing hidden verdict against the task's
// budget and reports whether the task must stop instead of retrying.
//
// Only distinct candidates count: an unchanged rerun asks the suite nothing
// new. The candidate that exceeds the budget is not reported to the model at
// all — the task ends there — so the model learns about at most the budgeted
// number of candidates, and at most one bit per applicable check for each.
// A candidate that passes every hidden check ends the task by acceptance, so
// passing verdicts need no budget.
func (r *Runner) spendHiddenFeedback(rejected *[]string, results []recipe.Result, candidate string) (bool, string) {
	failed := false
	for _, res := range results {
		if res.Kind == recipe.KindHidden && res.Status != recipe.Pass {
			failed = true
			break
		}
	}
	if !failed {
		return false, ""
	}
	seen := false
	for _, c := range *rejected {
		seen = seen || c == candidate
	}
	if !seen {
		*rejected = append(*rejected, candidate)
	}
	budget := r.HiddenFeedbackRounds
	if budget <= 0 {
		budget = DefaultHiddenFeedbackRounds
	}
	if len(*rejected) <= budget {
		return false, ""
	}
	return true, fmt.Sprintf("hidden acceptance checks failed on %d distinct candidates, more than the "+
		"feedback budget of %d; the task stops rather than let the model search the hidden suite",
		len(*rejected), budget)
}
