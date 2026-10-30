package lab

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// Experiment is a pre-registered comparison: the hypothesis, the metric it
// is decided on, the tasks, the trials and the arms, written down before
// anything runs (lab/<id>.yaml).
type Experiment struct {
	ID         string  `yaml:"id"`
	Hypothesis string  `yaml:"hypothesis"`
	Pattern    Pattern `yaml:"pattern,omitempty"`
	// Suite is direct (bench direct) or bounded (bench tasks).
	Suite string `yaml:"suite"`
	Tasks struct {
		// Dirs are task definition directories (bench --tasks), relative to
		// the repository.
		Dirs []string `yaml:"dirs"`
		// Only selects task ids or categories (bench --only); empty: all.
		Only     []string `yaml:"only,omitempty"`
		Fixtures string   `yaml:"fixtures,omitempty"`
	} `yaml:"tasks"`
	// Primary is the metric the decision is made on, on held-out tasks.
	Primary Metric `yaml:"primary"`
	// DecideOn is the split decided on: heldout (the default), or dev to
	// re-read past experiments that had no held-out tasks. A dev decision is
	// not independent of the tuning and never becomes a proposal.
	DecideOn string `yaml:"decide_on,omitempty"`
	// Trials per arm (at least 2 for a run; a retrospective arm's are its
	// reports).
	Trials int `yaml:"trials,omitempty"`
	// Model is the model profile (default: the configured one).
	Model string `yaml:"model,omitempty"`
	// Env is passed to every arm; only server tuning variables (GGML_*,
	// CUDA_VISIBLE_DEVICES, OMP_NUM_THREADS) are allowed.
	Env map[string]string `yaml:"env,omitempty"`
	// Policy is the gate policy file (default benchmarks/gate-policy.yaml).
	Policy string `yaml:"policy,omitempty"`
	// MinLanguages is how many languages the held-out tasks must cover for
	// a change to be accepted for every workload (default 3); with fewer,
	// at most a workload-specific proposal follows.
	MinLanguages int        `yaml:"min_languages,omitempty"`
	EditBudget   EditBudget `yaml:"edit_budget,omitempty"`
	// AllowCost lets a candidate cost more on a resource metric, up to a
	// ratio of the baseline (tokens_per_success: 1.2), when the hypothesis
	// is that the spending buys success.
	AllowCost map[string]float64 `yaml:"allow_cost,omitempty"`
	Arms      []Arm              `yaml:"arms"`
}

// Arm is one side of the comparison.
type Arm struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	// Role: baseline (exactly one, unchanged), simpler (an alternative a
	// candidate must beat) or candidate.
	Role string `yaml:"role"`
	// Against names the simpler arms a candidate must beat (default: all).
	Against []string `yaml:"against,omitempty"`
	// Set is a configuration change; Patch a code change (a git diff
	// against the base revision, relative to the experiment file).
	Set   config.ExperimentSettings `yaml:"set,omitempty"`
	Patch string                    `yaml:"patch,omitempty"`
	// Reports are recorded runs to evaluate instead of running the arm (a
	// retrospective experiment).
	Reports []string `yaml:"reports,omitempty"`
	// Primary overrides the experiment's primary metric for this arm (two
	// hypotheses sharing a baseline); a simpler arm is compared with a
	// candidate on the candidate's.
	Primary *Metric `yaml:"primary,omitempty"`
	// Predicts are tasks the change should fix and tasks it puts at risk,
	// written before the run and checked after it.
	Predicts struct {
		Fixes  []string `yaml:"fixes,omitempty" json:"fixes,omitempty"`
		AtRisk []string `yaml:"at_risk,omitempty" json:"at_risk,omitempty"`
	} `yaml:"predicts,omitempty"`
}

var (
	slug     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)
	allowEnv = regexp.MustCompile(`^(GGML_[A-Z0-9_]+|CUDA_VISIBLE_DEVICES|OMP_NUM_THREADS)$`)
)

// LoadExperiment reads and checks an experiment file.
func LoadExperiment(path string) (*Experiment, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Experiment
	if err := config.DecodeStrict(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &e, nil
}

// MetricFor is an arm's primary metric.
func (e *Experiment) MetricFor(a Arm) Metric {
	if a.Primary != nil {
		return *a.Primary
	}
	return e.Primary
}

// Retrospective reports whether every arm is recorded runs.
func (e *Experiment) Retrospective() bool {
	for _, a := range e.Arms {
		if len(a.Reports) == 0 {
			return false
		}
	}
	return true
}

// Baseline returns the baseline arm.
func (e *Experiment) Baseline() Arm {
	for _, a := range e.Arms {
		if a.Role == "baseline" {
			return a
		}
	}
	return Arm{}
}

// Validate checks the experiment.
func (e *Experiment) Validate() error {
	var errs []error
	bad := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }
	if !slug.MatchString(e.ID) {
		bad("id: %q is not a lowercase slug", e.ID)
	}
	if strings.TrimSpace(e.Hypothesis) == "" {
		bad("hypothesis: required (write it down before the run)")
	}
	if e.DecideOn != "" && e.DecideOn != "heldout" && e.DecideOn != "dev" {
		bad("decide_on: %q is not heldout or dev", e.DecideOn)
	}
	if e.Suite != "direct" && e.Suite != "bounded" {
		bad("suite: %q is not direct or bounded", e.Suite)
	}
	if e.Primary.Name == "" || e.Primary.Better != "higher" && e.Primary.Better != "lower" {
		bad("primary: a metric and better: higher|lower")
	}
	if len(e.Tasks.Dirs) == 0 && !e.Retrospective() {
		bad("tasks.dirs: required")
	}
	if e.Suite == "bounded" && len(e.Tasks.Dirs) > 1 {
		bad("tasks.dirs: the bounded suite reads one directory")
	}
	if !e.Retrospective() && e.Trials < 2 {
		bad("trials: at least 2 (one trial cannot show run-to-run noise)")
	}
	for k := range e.Env {
		if !allowEnv.MatchString(k) {
			bad("env: %s is not a server tuning variable (GGML_*, CUDA_VISIBLE_DEVICES, OMP_NUM_THREADS); credentials and paths never reach an arm", k)
		}
	}
	budget := e.EditBudget.withDefaults()
	names := map[string]bool{}
	baselines, candidates := 0, 0
	for _, a := range e.Arms {
		if !slug.MatchString(a.Name) || names[a.Name] {
			bad("arm %q: names are unique lowercase slugs", a.Name)
		}
		names[a.Name] = true
		changes := !a.Set.IsZero() || a.Patch != ""
		switch a.Role {
		case "baseline":
			baselines++
			if changes {
				bad("arm %s: the baseline changes nothing", a.Name)
			}
		case "candidate", "simpler":
			if a.Role == "candidate" {
				candidates++
			}
			if !changes && len(a.Reports) == 0 {
				bad("arm %s: set, patch or reports required", a.Name)
			}
			if !a.Set.IsZero() && a.Patch != "" {
				bad("arm %s: one change per arm: a setting or a patch, not both", a.Name)
			}
		default:
			bad("arm %s: role %q is not baseline, simpler or candidate", a.Name, a.Role)
		}
		if a.Primary != nil && (a.Primary.Name == "" || a.Primary.Better != "higher" && a.Primary.Better != "lower") {
			bad("arm %s: primary: a metric and better: higher|lower", a.Name)
		}
		if err := a.Set.Validate(); err != nil {
			bad("arm %s: %v", a.Name, err)
		}
		if n := len(a.Set.Pairs()); n > budget.Settings {
			bad("arm %s: %d settings changed; the edit budget is %d (one isolated change per candidate)", a.Name, n, budget.Settings)
		}
		if e.Suite == "direct" && (a.Set.Budgets != config.ExperimentSettings{}.Budgets || a.Set.Escalation != config.ExperimentSettings{}.Escalation) {
			bad("arm %s: budgets and escalation apply to the bounded suite", a.Name)
		}
		if e.Suite == "bounded" && !a.Set.Direct.IsZero() {
			bad("arm %s: direct settings apply to the direct suite", a.Name)
		}
		if a.Patch != "" && e.Suite == "direct" && !e.coversSecurity() {
			bad("arm %s: a code change must also run the security tasks (tasks.only should include the security group, or leave only empty)", a.Name)
		}
	}
	for _, a := range e.Arms {
		for _, s := range a.Against {
			if !names[s] {
				bad("arm %s: against %q: no such arm", a.Name, s)
			}
		}
	}
	if baselines != 1 {
		bad("arms: exactly one baseline (got %d)", baselines)
	}
	if candidates == 0 {
		bad("arms: at least one candidate")
	}
	return errors.Join(errs...)
}

// coversSecurity reports whether the task selection includes the security
// tasks (all tasks, or the security category).
func (e *Experiment) coversSecurity() bool {
	return len(e.Tasks.Only) == 0 || contains(e.Tasks.Only, "security")
}

// budgetKeys are the report settings an arm's change alters, so that the
// gate's equal-budget check does not refuse the comparison it was made
// for.
func budgetKeys(s config.ExperimentSettings) []string {
	var out []string
	if s.Direct.MaxTurns != 0 {
		out = append(out, "max_turns")
	}
	if s.Budgets.MaxAttempts != 0 {
		out = append(out, "max_attempts")
	}
	return out
}
