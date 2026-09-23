package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func preflightInput() PreflightInput {
	judged, _ := ArmByName("supervised-rerank")
	return PreflightInput{
		// Marked synthetic so the integrity check has something coherent to
		// verify: a synthetic fixture is its own base state. The integrity
		// rule itself is exercised in integrity_test.go.
		Tasks: []Task{
			{ID: "a", Set: SetDev, Origin: TaskOrigin{Synthetic: true},
				Expected: Expected{Files: []string{"x.go"}}},
			{ID: "b", Set: SetDev, Origin: TaskOrigin{Synthetic: true},
				Expected: Expected{Files: []string{"y.go"}}},
		},
		Arms:            []Arm{judged},
		Set:             "dev",
		JudgeAvailable:  true,
		Annotated:       AnnotationStatus{Total: 2, Annotated: 2},
		HeldoutRequired: 30,
		Provenance: Provenance{
			Commit: "abc123", JudgmentModelRequested: "jev-1.13.0",
			JudgmentModelPinned: true,
		}.Finalise(),
	}
}

// The distinction the whole preflight exists for. Outside a benchmark a
// missing treatment is worth saying; inside one it invalidates the run.
func TestBenchmarkModeBlocksWhereProductionWouldFallBack(t *testing.T) {
	for name, mutate := range map[string]func(PreflightInput) PreflightInput{
		"no judge configured": func(in PreflightInput) PreflightInput {
			in.JudgeAvailable = false
			return in
		},
		"smoke call failed": func(in PreflightInput) PreflightInput {
			in.SmokeErr = errors.New("401 Unauthorized")
			return in
		},
		"unpinned model": func(in PreflightInput) PreflightInput {
			in.Provenance.JudgmentModelPinned = false
			return in
		},
		"served a different model": func(in PreflightInput) PreflightInput {
			in.SmokeServed = "jev-1.14.0"
			return in
		},
		"no ground truth": func(in PreflightInput) PreflightInput {
			in.Annotated = AnnotationStatus{Total: 2}
			return in
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := mutate(preflightInput())

			in.Benchmark = false
			if p := RunPreflight(context.Background(), in); p.Blocked() {
				t.Errorf("production mode blocked on %q; a task must degrade, not stop", name)
			}

			in.Benchmark = true
			p := RunPreflight(context.Background(), in)
			if !p.Blocked() {
				t.Fatalf("benchmark mode did not block on %q; the run would publish the "+
					"baseline's numbers under the treatment's name", name)
			}
			if len(p.Blockers()) == 0 {
				t.Fatal("blocked with no stated reason")
			}
		})
	}
}

// A configuration with everything in place must not be blocked, or the gate
// is useless.
func TestCompleteConfigurationPasses(t *testing.T) {
	in := preflightInput()
	in.Benchmark = true
	in.SmokeServed = "jev-1.13.0"
	p := RunPreflight(context.Background(), in)
	if p.Blocked() {
		t.Fatalf("a complete configuration was blocked: %v", p.Blockers())
	}
	if !strings.Contains(p.Format(), in.Provenance.ExperimentID) {
		t.Error("the preflight does not show the experiment id it would record")
	}
}

// The local control arm has its own dependency, and the same rule applies.
func TestLocalArmNeedsItsEmbeddingProvider(t *testing.T) {
	local, _ := ArmByName("supervised-rerank-local")
	in := preflightInput()
	in.Arms = []Arm{local}
	in.Benchmark = true

	if p := RunPreflight(context.Background(), in); !p.Blocked() {
		t.Fatal("the local arm ran with no embedding provider; it would have produced " +
			"the baseline's ordering under the control arm's name")
	}
	in.EmbeddingAvailable = true
	if p := RunPreflight(context.Background(), in); p.Blocked() {
		t.Fatalf("blocked with a provider available: %v", p.Blockers())
	}
}

// A held-out run below the qualification bar is stopped before it is spent.
func TestHeldoutBelowTheBarIsBlockedInBenchmarkMode(t *testing.T) {
	in := preflightInput()
	in.Set = string(SetHeldout)
	in.Tasks = []Task{{ID: "a", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true},
		Expected: Expected{Files: []string{"x.go"}}}}
	in.Benchmark, in.SmokeServed = true, "jev-1.13.0"

	p := RunPreflight(context.Background(), in)
	if !p.Blocked() {
		t.Fatal("a one-task held-out run was allowed")
	}
	if !strings.Contains(strings.Join(p.Blockers(), " "), "held-out size") {
		t.Fatalf("blockers = %v", p.Blockers())
	}
}

// A dirty tree warns by default and blocks when the numbers will be published.
func TestDirtyTreeBlocksOnlyWithRequireClean(t *testing.T) {
	in := preflightInput()
	in.Provenance.Dirty = true
	in.Provenance = in.Provenance.Finalise()
	in.Benchmark, in.SmokeServed = true, "jev-1.13.0"

	if p := RunPreflight(context.Background(), in); p.Blocked() {
		t.Fatalf("a dirty tree blocked without --require-clean: %v", p.Blockers())
	}
	in.RequireClean = true
	p := RunPreflight(context.Background(), in)
	if !p.Blocked() {
		t.Fatal("--require-clean accepted a dirty tree")
	}
	if !strings.Contains(p.Format(), "uncommitted changes") {
		t.Error("the report does not say what is wrong")
	}
}

// An arm that needs no treatment must not be blocked by a missing one.
func TestBaselineOnlyRunNeedsNothingExternal(t *testing.T) {
	base, _ := ArmByName("supervised")
	in := preflightInput()
	in.Arms = []Arm{base}
	in.Benchmark = true
	in.JudgeAvailable = false
	if p := RunPreflight(context.Background(), in); p.Blocked() {
		t.Fatalf("a baseline-only run was blocked: %v", p.Blockers())
	}
}
