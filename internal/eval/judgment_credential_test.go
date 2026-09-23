package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/task"
)

// Which arms need the Jev credential, and which are entitled to ignore it.
//
// The defect these guard against made every arm need it: judgment.yaml with
// enabled set and no key exported failed configuration loading, so a
// `supervised` run — which never consults the judge — could not start. The
// dependency belongs to the arm, not to the file.

// armByName finds a named arm in the shipped set, so these tests describe the
// arms operators actually select rather than ones invented here.
func armByName(t *testing.T, name string) Arm {
	t.Helper()
	for _, a := range Arms() {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no arm named %q in the shipped set", name)
	return Arm{}
}

// Cases 1 and 2. Neither arm reranks with the judge, so neither may be stopped
// by a judge that has no credential. A keyless judge is what
// supervisor.Judge hands the solver in that situation.
func TestNonJudgedArmsIgnoreAMissingCredential(t *testing.T) {
	for _, name := range []string{"supervised", "supervised-rerank-local"} {
		t.Run(name, func(t *testing.T) {
			arm := armByName(t, name)
			if arm.Rerank == RerankJudged {
				t.Fatalf("arm %q reranks with the judge; this test assumes it does not", name)
			}

			// The gate under test is the Rerank switch in Solve. Reaching it
			// with no store root configured makes Solve return the supervised
			// precondition error instead, which is the signal that the judge
			// gate let this arm through.
			s := &SystemSolver{Judge: keylessJudge(t)}
			_, err := s.Solve(context.Background(), SolveRequest{Arm: arm})
			if err == nil {
				t.Fatal("Solve returned no error at all; the test can no longer tell " +
					"which gate it passed")
			}
			if strings.Contains(err.Error(), judgment.ConfigFile) {
				t.Fatalf("arm %q was refused over the judgment configuration: %v", name, err)
			}
		})
	}
}

// Case 3. The arm that names the judge is stopped before anything is
// measured. Preflight is where that has to happen: by the time Solve runs, a
// cell of the batch has already been spent.
func TestJudgedArmIsBlockedInPreflightWithoutCredential(t *testing.T) {
	arm := armByName(t, "supervised-rerank")
	if arm.Rerank != RerankJudged {
		t.Fatal("supervised-rerank no longer reranks with the judge")
	}

	in := preflightInput()
	in.Arms = []Arm{arm}
	in.Benchmark = true
	// What a keyless judge reports. supervisor.Judge builds one and hands it
	// over; Available() is false because the key is not exported.
	in.JudgeAvailable = false

	p := RunPreflight(context.Background(), in)
	if !p.Blocked() {
		t.Fatal("the judged arm was allowed to run with no credential; it would have " +
			"published the baseline's ordering under the reranker's name")
	}
	if !strings.Contains(strings.Join(p.Blockers(), " "), "external judge") {
		t.Fatalf("the blockers do not say the judge is what is missing: %v", p.Blockers())
	}

	// And the same preflight passes once the judge is usable, so the block is
	// attributable to the credential rather than to something else in the
	// fixture.
	// SmokeServed is what a live round trip reports back; with the credential
	// present preflight also insists the service named the model it was asked
	// for, which is a separate check from this one.
	in.JudgeAvailable = true
	in.SmokeServed = in.Provenance.JudgmentModelRequested
	if p := RunPreflight(context.Background(), in); p.Blocked() {
		t.Fatalf("blocked with the judge available: %v", p.Blockers())
	}
}

// The second half of the invariant, at the solver. Even if a batch reached
// Solve with an unusable judge, the judged arm refuses rather than running
// unreranked, and says so.
func TestJudgedArmRefusesAtSolveRatherThanDowngrading(t *testing.T) {
	arm := armByName(t, "supervised-rerank")
	// Root is left nil, so Solve's supervised precondition fires first. That
	// ordering is itself fine — both are refusals — but it means this test
	// has to assert on the judge gate directly rather than through Solve.
	s := &SystemSolver{Judge: keylessJudge(t)}
	if s.Judge.Available() {
		t.Fatal("a keyless judge reported itself available; the gate would let the arm run")
	}

	_, err := s.Solve(context.Background(), SolveRequest{Arm: arm})
	if err == nil {
		t.Fatal("the judged arm ran to completion with no credential")
	}
	// Whichever precondition fired, the one thing that must never happen is a
	// result produced under this arm's name without the reranker.
	if !strings.Contains(err.Error(), "supervised-rerank") {
		t.Fatalf("the refusal does not name the arm it refused: %v", err)
	}
}

// keylessJudge is what New returns for an enabled configuration whose key is
// not exported: a real judge that reports itself unavailable.
func keylessJudge(t *testing.T) judgment.Judge {
	t.Helper()
	cfg := judgment.DefaultConfig()
	cfg.Enabled = true
	cfg.Endpoint = "https://example.invalid/v1"
	cfg.Model = "jev-1.13.0"
	cfg.APIKeyEnv = "BC_TEST_EVAL_JUDGMENT_KEY"
	t.Setenv(cfg.APIKeyEnv, "")

	j, err := judgment.New(cfg, judgment.Deps{})
	if err != nil {
		t.Fatalf("building a keyless judge: %v", err)
	}
	if j.Available() {
		t.Fatal("a keyless judge reported itself available")
	}
	return j
}

// Every supervised arm hands the task runner the decision plane.
//
// The recorded failure: after the runner started requiring Jev, `bcode eval
// run --arms supervised` failed every task in about a second with
// JEV_NOT_CONFIGURED — with Jev configured and its smoke call passing —
// because only the judged-rerank arm gave the runner its judge.
func TestEverySupervisedArmGetsTheDecisionPlane(t *testing.T) {
	judge := keylessJudge(t)
	for _, arm := range Arms() {
		if !arm.Supervised {
			continue
		}
		t.Run(arm.Name, func(t *testing.T) {
			runner := &task.Runner{Retriever: &retrieval.Retriever{}}
			(&SystemSolver{Judge: judge}).wireJudgment(runner, arm)
			if runner.Judge != judge {
				t.Fatalf("arm %q ran without the decision plane", arm.Name)
			}
		})
	}
}
