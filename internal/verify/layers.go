package verify

import (
	"fmt"
	"strings"
)

// The verification layers, each capability-driven (what a stage is, which
// languages have a provider, which tools are installed) rather than written
// for one language:
//
//	1 syntax and structure      stages of kind syntax (gofmt), and the per-file checks of internal/lang during the change
//	2 build and types           stages of kind build (go build, go vet, tsc, cargo build, mvn compile)
//	3 the repository's tests    stages of kind test, with the test-integrity check of the change
//	4 targeted tests            the tests the change adds or modifies, run with the rest
//	5 behavioural difference    those tests fail on the base and pass on the change (delta.go)
//	6 mutation                  the tests fail on mutants of the changed lines (mutation.go)
//	7 counterexamples           independent tests, run on the change and the base (counterexample.go; optional)
//	8 contracts                 cross-repository links hold (internal/compat)
//
// Each layer reports passed, failed, established, weak, not established,
// skipped, unavailable or not applicable, and the label a change gets
// never claims more than the layers show.

// Layer statuses.
const (
	LayerPassed         = "passed"
	LayerFailed         = "failed"
	LayerEstablished    = "established"
	LayerWeak           = "weak"
	LayerNotEstablished = "not established"
	LayerSkipped        = "skipped"
	LayerUnavailable    = "unavailable"
	LayerNotApplicable  = "not applicable"
	LayerCompleted      = "completed"
	LayerFound          = "found"
	LayerIncomplete     = "incomplete"
)

// Labels, from weakest: the verification states of a task (backward
// compatible with task.VerificationTestsGreen and VerificationTaskVerified).
const (
	LabelFailing      = "failing"
	LabelIncomplete   = "incomplete"
	LabelTestsGreen   = "tests_green"
	LabelTaskVerified = "task_verified"
)

// LayerResult is one layer's outcome.
type LayerResult struct {
	Layer    int    `json:"layer"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Requirements say which optional layers a change must pass.
type Requirements struct {
	Mutation bool // Layer 6 must be established
	// Boundaries additionally requires every boundary mutant killed
	// (mutation: strict).
	Boundaries      bool
	Counterexamples bool // Layer 7 must complete with none found
}

// LayerInputs are the results the classification reads.
type LayerInputs struct {
	Full Result // the full gate
	// EvidenceNeeded: the change goes beyond tests and documentation.
	EvidenceNeeded bool
	Evidence       *Evidence // nil: not computed
	Mutation       *MutationReport
	Counter        *CounterexampleReport
	// Contracts is the cross-repository gate's state (compat.Report.State):
	// "" when it did not run, "none", "compatible", "broken", "untested".
	Contracts string
	Require   Requirements
}

// Classification is the layers and the label they support.
type Classification struct {
	Layers []LayerResult `json:"layers"`
	Label  string        `json:"label"`
	// Strength qualifies a label with the optional layers that held
	// ("mutation established"); Withheld says why task_verified is not
	// granted.
	Strength []string `json:"strength,omitempty"`
	Withheld []string `json:"withheld,omitempty"`
}

// Summary is one line: the label, its strength and what withheld more.
func (c Classification) Summary() string {
	s := c.Label
	if len(c.Strength) > 0 {
		s += " (" + strings.Join(c.Strength, ", ") + ")"
	}
	if len(c.Withheld) > 0 {
		s += "; not task_verified: " + strings.Join(c.Withheld, "; ")
	}
	return s
}

// Classify builds the layers and the label from the results.
func Classify(in LayerInputs) Classification {
	var c Classification
	add := func(n int, name, status, detail string, required bool) {
		c.Layers = append(c.Layers, LayerResult{Layer: n, Name: name, Status: status, Detail: detail, Required: required})
	}
	byKind := map[string][]StageResult{}
	failing, incomplete := false, false
	testsRan := false
	for _, s := range in.Full.Stages {
		kind := stageResultKind(s)
		byKind[kind] = append(byKind[kind], s)
		switch {
		case s.Status == "fail":
			failing = true
		case s.Status == "error" && (s.Cause == CauseMissingTool || s.Cause == CauseEnvironment):
			incomplete = true
		case s.Status == "error":
			failing = true
		}
		if kind == KindTest && s.Status == "pass" {
			testsRan = true
		}
	}
	add(1, "syntax and structure", kindStatus(byKind[KindSyntax]), layerStageNames(byKind[KindSyntax]), false)
	add(2, "build and types", kindStatus(byKind[KindBuild]), layerStageNames(byKind[KindBuild]), false)
	testStatus, testDetail := kindStatus(byKind[KindTest]), layerStageNames(byKind[KindTest])
	if !testsRan && testStatus != LayerFailed {
		testStatus, testDetail = LayerUnavailable, "no test runner ran"
	}
	add(3, "repository tests", testStatus, testDetail, true)
	var review []string
	for _, f := range in.Full.Integrity {
		if f.Severity == IntegrityReview {
			review = append(review, f.String())
		}
	}
	integ := LayerPassed
	if len(review) > 0 {
		integ = LayerWeak
	}
	for _, s := range in.Full.Stages {
		if s.Name == "test-integrity" && s.Status == "fail" {
			integ = LayerFailed
		}
	}
	add(3, "test integrity", integ, trunc(strings.Join(review, "; "), 400), true)
	// Layers 4 and 5: the change's own tests, and their difference.
	switch {
	case !in.EvidenceNeeded:
		add(4, "targeted tests", LayerNotApplicable, "the change is tests or documentation only", false)
		add(5, "behavioural difference", LayerNotApplicable, "", false)
	case in.Evidence == nil:
		add(4, "targeted tests", LayerSkipped, "not checked", false)
		add(5, "behavioural difference", LayerSkipped, "not checked", true)
	default:
		if len(in.Evidence.TestFiles) > 0 {
			add(4, "targeted tests", LayerPassed, fmt.Sprintf("%d changed test file(s)", len(in.Evidence.TestFiles)), false)
		} else {
			add(4, "targeted tests", LayerNotEstablished, "the change adds or modifies no tests", false)
		}
		if in.Evidence.Verified {
			add(5, "behavioural difference", LayerEstablished, in.Evidence.Reason, true)
		} else {
			add(5, "behavioural difference", LayerNotEstablished, in.Evidence.Reason, true)
		}
	}
	// Layer 6.
	switch m := in.Mutation; {
	case m == nil:
		add(6, "mutation", LayerSkipped, "not run", in.Require.Mutation)
	default:
		st := map[string]string{MutationEstablished: LayerEstablished, MutationWeak: LayerWeak, MutationInconclusive: LayerIncomplete,
			MutationUnavailable: LayerUnavailable, MutationNotRequired: LayerNotApplicable}[m.Status]
		detail := m.Reason
		if m.Boundary > 0 {
			detail += fmt.Sprintf("; boundary mutants: %d of %d killed (not scored)", m.BoundaryKilled, m.Boundary)
		}
		add(6, "mutation", st, detail, in.Require.Mutation)
	}
	// Layer 7.
	switch x := in.Counter; {
	case x == nil || x.Status == CounterNotRun:
		add(7, "counterexamples", LayerSkipped, "not run (optional)", in.Require.Counterexamples)
	case x.Status == CounterCompleted:
		add(7, "counterexamples", LayerCompleted, x.Reason, in.Require.Counterexamples)
	case x.Status == CounterFound:
		var found []string
		for _, t := range x.Tests {
			if t.Outcome == CounterRegression || t.Outcome == CounterFailing {
				found = append(found, t.File+": "+t.Detail)
			}
		}
		add(7, "counterexamples", LayerFound, trunc(strings.Join(found, "; "), 400), in.Require.Counterexamples)
	case x.Status == CounterUnavailable:
		add(7, "counterexamples", LayerUnavailable, x.Reason, in.Require.Counterexamples)
	default:
		add(7, "counterexamples", LayerIncomplete, x.Reason, in.Require.Counterexamples)
	}
	// Layer 8.
	switch in.Contracts {
	case "", "none":
		add(8, "contracts", LayerNotApplicable, "no cross-repository link is affected", false)
	case "compatible":
		add(8, "contracts", LayerPassed, "", true)
	case "broken":
		add(8, "contracts", LayerFailed, "", true)
	default:
		add(8, "contracts", LayerNotEstablished, "affected links are "+in.Contracts, true)
	}

	// The label.
	switch {
	case failing:
		c.Label = LabelFailing
		return c
	case incomplete:
		c.Label = LabelIncomplete
		return c
	}
	c.Label = LabelTestsGreen
	for _, l := range c.Layers {
		switch {
		case l.Layer == 3 && l.Name == "repository tests" && l.Status != LayerPassed:
			c.Withheld = append(c.Withheld, "no tests ran")
		case l.Name == "test integrity" && l.Status != LayerPassed:
			c.Withheld = append(c.Withheld, "the change touches how its tests run ("+trunc(l.Detail, 200)+")")
		case l.Layer == 5 && l.Status != LayerEstablished && l.Status != LayerNotApplicable:
			c.Withheld = append(c.Withheld, "no test shows the change ("+trunc(l.Detail, 200)+")")
		case l.Layer == 6 && l.Required && l.Status != LayerEstablished:
			c.Withheld = append(c.Withheld, "mutation evidence is required and is "+l.Status)
		case l.Layer == 6 && in.Require.Boundaries && in.Mutation != nil && in.Mutation.BoundarySurvived > 0:
			c.Withheld = append(c.Withheld, fmt.Sprintf("strict mutation evidence is required, and %d boundary mutant(s) survive", in.Mutation.BoundarySurvived))
		case l.Layer == 7 && l.Required && l.Status != LayerCompleted:
			c.Withheld = append(c.Withheld, "counterexample checks are required and are "+l.Status)
		case l.Layer == 8 && l.Status != LayerPassed && l.Status != LayerNotApplicable:
			c.Withheld = append(c.Withheld, "cross-repository contracts are "+l.Status)
		}
		switch {
		case l.Layer == 6 && l.Status == LayerEstablished && in.Mutation != nil && in.Mutation.BoundarySurvived > 0:
			c.Strength = append(c.Strength, "mutation established", fmt.Sprintf("%d boundary untested", in.Mutation.BoundarySurvived))
		case l.Layer == 6 && l.Status == LayerEstablished:
			c.Strength = append(c.Strength, "mutation established")
		case l.Layer == 6 && l.Status == LayerWeak:
			c.Strength = append(c.Strength, "mutation weak")
		case l.Layer == 7 && l.Status == LayerCompleted:
			c.Strength = append(c.Strength, "counterexamples checked")
		case l.Layer == 7 && l.Status == LayerFound:
			c.Strength = append(c.Strength, "counterexample found")
		}
	}
	if len(c.Withheld) == 0 {
		c.Label = LabelTaskVerified
	}
	return c
}

// stageResultKind is the layer of a stage result (built-in stages by name).
func stageResultKind(s StageResult) string {
	switch s.Name {
	case "diff-scope", "secret-scan", "test-integrity", "side-effects", "build-tags":
		return KindOther
	case "tests":
		return KindTest // the "no test runner" note
	}
	return StageKind(Stage{Name: s.Name, Run: strings.Fields(s.Command)})
}

func kindStatus(rs []StageResult) string {
	if len(rs) == 0 {
		return LayerNotApplicable
	}
	ran := false
	for _, r := range rs {
		switch r.Status {
		case "fail", "error":
			return LayerFailed
		case "pass":
			ran = true
		}
	}
	if !ran {
		return LayerSkipped
	}
	return LayerPassed
}

func layerStageNames(rs []StageResult) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name+" "+r.Status)
	}
	return strings.Join(out, ", ")
}
