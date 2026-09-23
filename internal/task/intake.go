package task

// Intake profiling: a judgment site that reads the objective before
// localization starts (design.md mechanism M1).
//
// Every task runs the same shape today — the same budgets, the same single
// candidate — and a task that needed a dependency, a migration, or a
// decision the user never made is discovered only after minutes of work.
// This asks a bounded set of questions about the objective itself, using
// nothing but what INTAKE already has: the task title, and the verification
// commands this repository is known to run.
//
// It is speculative fan-out in TypeSafe's own sense: every question below is
// independent, asked in one request, and code decides afterwards which
// answers apply.
//
// This site can change control flow, so it is one of the six whose decision
// may not be defaulted. What that means in practice depends on its configured
// tier, and judgment.Required is where that is decided: at the default logged
// tier a failure here is recorded and the task proceeds, because nothing was
// going to read the answer; at routing, where a finding would have stopped the
// task, an undecided question stops it instead of being assumed benign.

import (
	"context"
	"strings"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/recipe"
)

// IntakeSite names this judgment site in judgment.yaml's `sites` map.
const IntakeSite = "intake_profile"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            IntakeSite,
		Description:     "profiles the objective at INTAKE for ambiguity, missing dependencies or migrations, and blast radius, before localization starts",
		Mechanism:       "M1",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactStrict,
		Outcome:         "",
		EffectThreshold: 0.65,
	})
}

// IntakeBlastRadius is the Score this site asks.
type IntakeBlastRadius string

const (
	BlastFunction IntakeBlastRadius = "function"
	BlastPackage  IntakeBlastRadius = "package"
	BlastSeveral  IntakeBlastRadius = "several_packages"
	BlastContract IntakeBlastRadius = "public_contract"
)

var blastRadiusLevels = []string{
	"the change is confined to one function or method",
	"the change is confined to one package, touching more than one declaration in it",
	"the change spans several packages that are not bound by a public contract",
	"the change touches a public contract: an exported API, a schema, or an interface other services consume",
}

func blastRadiusFromScore(score float64) IntakeBlastRadius {
	idx := int(score + 0.5)
	radii := []IntakeBlastRadius{BlastFunction, BlastPackage, BlastSeveral, BlastContract}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(radii) {
		idx = len(radii) - 1
	}
	return radii[idx]
}

// IntakeProfile is what INTAKE profiling found.
type IntakeProfile struct {
	Attempted, Applied bool
	Tier               judgment.Tier
	SkipReason         string

	Ambiguous         bool
	AmbiguousP        float64
	Underspecified    bool
	UnderspecifiedP   float64
	NeedsDependency   bool
	NeedsDependencyP  float64
	NeedsMigration    bool
	NeedsMigrationP   float64
	ChangesInterface  bool
	ChangesInterfaceP float64
	TestsOnly         bool
	TestsOnlyP        float64
	PureRefactor      bool
	PureRefactorP     float64
	NeedsNetwork      bool
	NeedsNetworkP     float64

	BlastRadius         IntakeBlastRadius
	BlastRadiusAnswered bool

	Requests, InputTokens, OutputTokens int
}

// IntakeTuning names the probability floor above which a Noul counts as a
// positive finding. A Noul near 0.5 means the judge is unsure, not "a little
// true" — see design.md's own jaggedness notes — so this is deliberately a
// floor well above the midpoint.
type IntakeTuning struct {
	Floor float64
}

const DefaultIntakeFloor = 0.65

func (t IntakeTuning) withDefaults() IntakeTuning {
	if t.Floor <= 0 {
		t.Floor = DefaultIntakeFloor
	}
	return t
}

var intakeNouls = map[string]string{
	"ambiguous": "the objective admits two or more materially different implementations " +
		"that would each satisfy its exact words",
	"underspecified": "the objective names an outcome but not where in the repository or how " +
		"to achieve it",
	"needs_dependency": "the objective requires adding or upgrading a third-party dependency",
	"needs_migration": "the objective requires a database schema or migration change — a " +
		"change to a table, column, index, or stored schema file",
	"changes_interface": "the objective changes an interface, exported API, or contract that " +
		"other packages or services consume",
	"tests_only": "the objective is confined to tests or fixtures, with no production code " +
		"change implied",
	"pure_refactor": "the objective is a pure refactor with no intended behaviour change",
	"needs_network": "the objective requires network access at verification time to confirm " +
		"the change works",
}

func intakeProposition(key, phrase string) string {
	return "Considering `objective`, and the verification commands this repository runs in " +
		"`verification`: does the objective request that " + phrase + "?"
}

func blastRadiusProposition() string {
	return "Considering `objective` and `verification`: how large a blast radius does carrying " +
		"it out have?"
}

// CheckIntakeProfile profiles objective before localization begins.
//
// The error is non-nil when the profile could not be obtained — no judge, a
// state this site may not send, or a service that did not answer. It is not a
// verdict on the objective, and it is not, on its own, a reason to stop:
// judgment.FailClosed decides that from the site's configured tier. An empty
// objective is not an error, because there is then nothing to profile.
func CheckIntakeProfile(ctx context.Context, j judgment.Judge, objective string, presets []recipe.Preset,
	tuning IntakeTuning, logf func(string, ...any)) (IntakeProfile, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := IntakeProfile{Tier: judgment.SiteTier(j, IntakeSite)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(IntakeSite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case strings.TrimSpace(objective) == "":
		// Nothing to decide rather than a decision not made.
		res.SkipReason = "no_objective"
		return res, nil
	}
	res.Attempted = true

	st := judgment.NewState(judgment.RedactStrict)
	if err := st.Objective(objective); err != nil {
		logf("intake: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(IntakeSite, judgment.FailureRefused, err.Error())
	}
	names := make([]string, 0, len(presets))
	for _, p := range presets {
		names = append(names, string(p.Kind)+":"+p.Name)
	}
	if err := st.TrustedFact("verification", strings.Join(names, ", ")); err != nil {
		logf("intake: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(IntakeSite, judgment.FailureRefused, err.Error())
	}

	qs := make(map[string]judgment.Question, len(intakeNouls)+1)
	for key, phrase := range intakeNouls {
		qs[key] = judgment.Noul(intakeProposition(key, phrase))
	}
	qs["blast_radius"] = judgment.Score(blastRadiusProposition(), blastRadiusLevels)

	answers, note, err := judgment.RequireAll(ctx, j, IntakeSite, st, qs)
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens

	get := func(key string) (bool, float64) {
		a := answers[key]
		if !a.Answered {
			return false, 0
		}
		return a.Noul >= tn.Floor, a.Noul
	}
	res.Ambiguous, res.AmbiguousP = get("ambiguous")
	res.Underspecified, res.UnderspecifiedP = get("underspecified")
	res.NeedsDependency, res.NeedsDependencyP = get("needs_dependency")
	res.NeedsMigration, res.NeedsMigrationP = get("needs_migration")
	res.ChangesInterface, res.ChangesInterfaceP = get("changes_interface")
	res.TestsOnly, res.TestsOnlyP = get("tests_only")
	res.PureRefactor, res.PureRefactorP = get("pure_refactor")
	res.NeedsNetwork, res.NeedsNetworkP = get("needs_network")

	if a := answers["blast_radius"]; a.Answered {
		res.BlastRadius = blastRadiusFromScore(a.Score)
		res.BlastRadiusAnswered = true
	}
	if err != nil {
		return res, err
	}
	res.Applied = true
	return res, nil
}
