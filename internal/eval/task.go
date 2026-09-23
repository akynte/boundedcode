// Package eval is the evaluation harness of design v3 §10.2.
//
// The design commits to publishing only what this measures: "for each task
// category, the accepted-task rate within a fixed budget for (a) the same
// local model … (b) the supervised system, (c) a frontier agent", with the
// task set, hardware, model manifest and scripts published so anyone can
// reproduce or dispute the numbers.
//
// Three properties make the difference between a measurement and a story.
//
// # Hidden acceptance
//
// A task's acceptance tests are never in the worktree while the task runs.
// They are applied afterwards, to a copy. A model that can read the test can
// satisfy it without solving the problem — and that failure looks exactly like
// success in the results.
//
// # Ground truth separate from the system's own verdict
//
// The supervised system decides acceptance from the evidence it gathered. The
// harness decides it from the hidden tests. When those disagree in the
// system's favour, that is a *false acceptance*: the system claimed success
// and was wrong. It is the single most damaging failure mode a tool like this
// has, and it is reported as its own number rather than averaged away.
//
// # Leak disclosure
//
// A task the model has seen in training gives an inflated number. Every task
// declares its leak risk, and a report that mixes risks says so.
package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Category groups tasks so results can be reported per kind of work. Averaging
// a bug fix with a refactor hides which one the system is bad at.
type Category string

const (
	CategoryBugFix    Category = "bug_fix"
	CategoryFeature   Category = "feature"
	CategoryRefactor  Category = "refactor"
	CategorySchema    Category = "schema_change"
	CategoryAPIChange Category = "api_change"
	CategoryTestGap   Category = "test_gap"
)

// Categories lists every category, for reporting.
func Categories() []Category {
	return []Category{CategoryBugFix, CategoryFeature, CategoryRefactor,
		CategorySchema, CategoryAPIChange, CategoryTestGap}
}

// LeakRisk records how likely it is that a model has seen this task before.
//
// This is disclosed rather than assumed away. A result computed over tasks
// drawn from a public dataset means something different from one computed over
// tasks written for this repository, and a report that does not say which is
// not a result anyone should act on.
type LeakRisk string

const (
	// LeakNone: the task was written for this repository and has never been
	// published. Not a guarantee — nothing is — but the strongest claim
	// available.
	LeakNone LeakRisk = "none"
	// LeakSynthetic: the fixture is generated, so the specific code cannot
	// have been trained on, though the pattern may have been.
	LeakSynthetic LeakRisk = "synthetic"
	// LeakPublic: drawn from a public dataset or a public repository. Results
	// over these are an upper bound, not an estimate.
	LeakPublic LeakRisk = "public"
	// LeakUnknown: provenance not established. Reported separately; never
	// silently folded into a headline number.
	LeakUnknown LeakRisk = "unknown"
)

// Task is one evaluation case.
type Task struct {
	ID       string   `yaml:"id"`
	Category Category `yaml:"category"`
	// Objective is what the system is asked to do, in the words a user would
	// use. It must not name the fix: an objective that says "change x - y to
	// x + y" measures nothing.
	Objective string `yaml:"objective"`
	// Fixture is the directory, relative to the task file, copied in as the
	// starting state.
	Fixture string `yaml:"fixture"`
	// Scope restricts which paths a solution may change, as a real task would.
	Scope []string `yaml:"scope,omitempty"`
	// Acceptance is the ground truth, applied only after the run.
	Acceptance Acceptance `yaml:"acceptance"`
	// Budget bounds the attempt.
	Budget Budget `yaml:"budget"`
	// Verification is the level the supervised arm runs at.
	Verification string `yaml:"verification"`
	// LeakRisk is disclosed per task, never assumed.
	LeakRisk LeakRisk `yaml:"leak_risk"`
	// Set decides whether this task may be tuned against. Ranking weights,
	// thresholds and retrieval constants may be fitted on the dev set; a task
	// in the held-out set is judged on and never fitted to, because a
	// threshold chosen because it scored well on a task is no longer measured
	// by that task. Tasks written before the split default to dev, which is
	// the conservative reading: it keeps them out of held-out headline numbers.
	Set Set `yaml:"set,omitempty"`
	// Expected records the ground truth for localization scoring where it is
	// known — the files and symbols the real fixing commit touched. It is
	// applied only when scoring, never shown to the system.
	// It never serialises to JSON. A task is handed to a solver as a struct
	// and rendered into artifacts as JSON, and an `expected` key appearing in
	// either is one prompt-assembly mistake away from being read. The
	// evaluator holds it in memory and writes it back through the annotation
	// files, which are YAML and live outside every fixture.
	Expected Expected `yaml:"expected,omitempty" json:"-"`
	// Notes record anything a reader of the results would need to interpret
	// them: why the task is hard, what a wrong-but-passing solution looks like.
	Notes string `yaml:"notes,omitempty"`
	// Origin records where this task came from in real project history.
	//
	// It is the evidence the admission protocol checks. A task with no origin
	// is one nobody can show is a real engineering problem rather than one
	// written to be solvable, and `bcode eval admit` refuses it for a benchmark.
	Origin TaskOrigin `yaml:"origin,omitempty"`
	// Traits are the facets the split protocol balances on and the duplicate
	// audit groups by. They are descriptive, not scored.
	Traits TaskTraits `yaml:"traits,omitempty"`

	// path is where the task was loaded from, for resolving the fixture.
	path string
}

// TaskOrigin is where a benchmark task came from.
//
// The point of recording it is that a benchmark of engineering assistance is
// only as good as its claim to contain engineering problems. A task invented
// to be solvable measures whether the system can do what its author imagined;
// a task lifted from a real fix measures whether it can do what somebody
// actually needed doing. The two are easy to confuse after the fact and
// impossible to tell apart without this.
//
// The gold revision lives here, and it is evaluator-only in exactly the same
// way the labels are: the annotation workflow may read it, and it must never
// be reachable from the worktree the solver edits.
type TaskOrigin struct {
	// Repository identifies the project, when the set draws on more than one.
	Repository string `yaml:"repository,omitempty"`
	// BaseRevision is the pre-fix state the solver executes against. Required
	// for a historical task: it is what makes the starting point reproducible
	// rather than a directory somebody assembled.
	BaseRevision string `yaml:"base_revision,omitempty"`
	// GoldRevision is the fixing commit. Evaluator-only. It is evidence for
	// an annotator and for whoever reviews the task's admission; it is never
	// the ground truth by itself.
	GoldRevision string `yaml:"gold_revision,omitempty"`
	// Date is when the fix landed, for ordering a split by time and for
	// judging training-data leakage.
	Date string `yaml:"date,omitempty"`
	// Reference is a link or identifier a reader can follow: an issue, a pull
	// request, a commit URL.
	Reference string `yaml:"reference,omitempty"`
	// Derived says the objective was written from real history rather than
	// invented for the benchmark. It is a claim the task author makes and a
	// reviewer checks, not something this code can verify — which is why
	// `bcode eval admit` prints the evidence beside it rather than just the flag.
	Derived bool `yaml:"derived_from_history,omitempty"`
	// Synthetic marks a task written for the benchmark. Such tasks are
	// allowed in dev and refused in a held-out publication set.
	Synthetic bool `yaml:"synthetic,omitempty"`
	// ExternalDependencies names anything the acceptance check needs that is
	// not in the fixture: a network service, a licensed toolchain. A
	// mandatory one makes the task unreproducible for anybody else.
	ExternalDependencies []string `yaml:"external_dependencies,omitempty"`
	// Digest pins the content of the base state.
	//
	// The base revision names a commit in another repository, which this
	// harness cannot resolve. The digest is what makes the claim checkable
	// here: a fixture edited after the task was admitted stops matching, and
	// the integrity check says so instead of the run quietly measuring a
	// different starting state. `bcode eval integrity --print-digest` computes it.
	Digest string `yaml:"base_digest,omitempty"`

	// RuntimeImage pins the environment this task's own verification must run
	// in, as an immutable reference — name@sha256:….
	//
	// A task written for this repository leaves it empty and verifies on the
	// host, which is correct: the host is the environment it targets. An
	// imported task does not target this host. Its checks are the upstream
	// project's, written against an interpreter, a compiler and a set of
	// installed packages that are part of the task, not of the operator. Run
	// against whatever the machine has, they fail for reasons that say
	// nothing about the change under test.
	RuntimeImage string `yaml:"runtime_image,omitempty"`
	// RuntimeWorkdir is where the worktree is mounted inside RuntimeImage.
	// An image that installed the project in development mode has recorded
	// an absolute path, and the tree has to arrive there for its own imports
	// to resolve. Empty means the worktree's own path.
	RuntimeWorkdir string `yaml:"runtime_workdir,omitempty"`
	// RuntimeEnv are KEY=VALUE variables the runtime needs before the
	// repository's own checks will run — typically the PATH of the
	// environment the project was installed into, which the image records
	// in its activation script rather than in its default PATH.
	RuntimeEnv []string `yaml:"runtime_env,omitempty"`
	// RuntimePreparedImage is a runtime derived from RuntimeImage with the
	// repository's dependencies already fetched, so the measured run needs
	// no network. `bcode eval runtime --prepare` builds it and records it here;
	// RuntimeImage stays the official one, because preparation must always
	// start from the published environment rather than from the last thing
	// this harness happened to build.
	RuntimePreparedImage string `yaml:"runtime_prepared_image,omitempty"`
	// RuntimePrelude runs inside the runtime before each verification
	// command, offline.
	//
	// Mounting the solver's worktree over the image's project directory
	// replaces files the image generated when it built the project — a
	// version module written at install time, package metadata — and the
	// project then cannot import itself. The upstream evaluation script
	// reinstalls for the same reason; this is that step, and nothing more.
	RuntimePrelude string `yaml:"runtime_prelude,omitempty"`
}

// Runtime is the environment this task's own verification runs in: the
// prepared one when there is one, the official one otherwise.
func (o TaskOrigin) Runtime() string {
	if o.RuntimePreparedImage != "" {
		return o.RuntimePreparedImage
	}
	return o.RuntimeImage
}

// PinnedRuntime reports whether this task names an immutable runtime for its
// own verification.
func (o TaskOrigin) PinnedRuntime() bool { return o.RuntimeImage != "" }

// BaseDigest reports the pinned content digest of the base state.
func (o TaskOrigin) BaseDigest() string { return o.Digest }

// Empty reports an origin nobody recorded.
func (o TaskOrigin) Empty() bool {
	return o.Repository == "" && o.BaseRevision == "" && o.GoldRevision == "" &&
		o.Date == "" && o.Reference == "" && !o.Derived && !o.Synthetic &&
		len(o.ExternalDependencies) == 0 && o.Digest == ""
}

// TaskTraits are the facets a split is balanced on and a duplicate audit
// groups by.
type TaskTraits struct {
	// Subsystem is the area of the project the work is in — "billing",
	// "retrieval", "auth". Two tasks in one subsystem are the likeliest pair
	// to leak across a split.
	Subsystem string `yaml:"subsystem,omitempty"`
	// Language is the primary language of the change.
	Language string `yaml:"language,omitempty"`
	// Family groups tasks a human judged to be variations of one problem.
	// The audit surfaces candidates; this records the decision.
	Family string `yaml:"family,omitempty"`
}

// Empty reports traits nobody recorded.
func (t TaskTraits) Empty() bool {
	return t.Subsystem == "" && t.Language == "" && t.Family == ""
}

// Expected is localization ground truth, used to score retrieval rather than
// to decide the task. It is optional: a task with no known fixing commit still
// measures success, it just cannot contribute to recall.
type Expected struct {
	// Files the real fix touched, repository-relative.
	Files []string `yaml:"files,omitempty"`
	// Symbols the real fix changed.
	Symbols []string `yaml:"symbols,omitempty"`
	// Callers that had to change because of the fix, where known.
	Callers []string `yaml:"callers,omitempty"`
	// Tests that cover the change, where known.
	Tests []string `yaml:"tests,omitempty"`
	// Useful lists files an annotator marked USEFUL: reading them helps, and
	// a correct solution is reachable without them. They are held separately
	// from Files so that metrics can decline to treat a packet carrying
	// helpful context as having made an error, without inflating the
	// denominator of recall with things retrieval is not failing by missing.
	Useful []string `yaml:"useful,omitempty"`
	// RubricVersion and Annotator record where these labels came from. They
	// are filled by ApplyAnnotations from the annotation file, never written
	// into a task definition by hand: ground truth has provenance or it has
	// no standing.
	RubricVersion string `yaml:"rubric_version,omitempty"`
	Annotator     string `yaml:"annotator,omitempty"`
}

// Empty reports an Expected carrying no ground truth at all.
func (e Expected) Empty() bool {
	return len(e.Files) == 0 && len(e.Symbols) == 0 && len(e.Callers) == 0 &&
		len(e.Tests) == 0 && len(e.Useful) == 0 && e.RubricVersion == "" && e.Annotator == ""
}

// WithoutGroundTruth returns the task as the system under test may see it.
//
// It is a copy rather than a mutation, because the evaluator needs the labels
// it is removing: the same task is scored against them after the run. This is
// the single place a label crosses from the harness to the thing being
// measured, and the crossing is that it does not.
func (t Task) WithoutGroundTruth() Task {
	t.Expected = Expected{}
	return t
}

// Known reports whether this task can contribute to localization scoring.
func (e Expected) Known() bool { return len(e.Files) > 0 || len(e.Symbols) > 0 }

// Acceptance is the hidden ground truth for a task.
type Acceptance struct {
	// Files are written into the worktree copy *after* the run, before the
	// command executes. They are never present while the task is being solved.
	Files map[string]string `yaml:"files"`
	// Argv decides the task. A non-zero exit means the task was not solved.
	Argv []string `yaml:"argv"`
	// Timeout bounds the check.
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// MustNotChange lists paths a solution must leave alone. A task that
	// "passes" by deleting the failing test has not been solved, and without
	// this the harness would score it as a success.
	MustNotChange []string `yaml:"must_not_change,omitempty"`
}

// Budget bounds one attempt.
type Budget struct {
	MaxAttempts    int `yaml:"max_attempts"`
	MaxWallSeconds int `yaml:"max_wall_seconds"`
	MaxTokens      int `yaml:"max_tokens,omitempty"`
}

// FixturePath resolves the fixture directory.
func (t Task) FixturePath() string {
	if filepath.IsAbs(t.Fixture) {
		return t.Fixture
	}
	return filepath.Join(filepath.Dir(t.path), t.Fixture)
}

// Validate rejects a task that cannot produce a meaningful measurement.
//
// Each rule here corresponds to a way a task set can quietly stop measuring
// what it claims to.
func (t Task) Validate() error {
	var problems []string

	if t.ID == "" {
		problems = append(problems, "no id")
	}
	if t.Objective == "" {
		problems = append(problems, "no objective")
	}
	if t.Category == "" {
		problems = append(problems, "no category; results are reported per category")
	}
	if t.Set != "" && t.Set != SetDev && t.Set != SetHeldout {
		problems = append(problems, fmt.Sprintf("set %q is not dev or heldout", t.Set))
	}
	if t.LeakRisk == "" {
		problems = append(problems, "no leak_risk; provenance is disclosed, never assumed")
	}
	if t.Fixture == "" {
		problems = append(problems, "no fixture")
	}
	if len(t.Acceptance.Argv) == 0 {
		problems = append(problems,
			"no acceptance command; a task the harness cannot decide is not an evaluation task")
	}
	if len(t.Acceptance.Files) == 0 {
		problems = append(problems,
			"no hidden acceptance files; if the test is already in the fixture the model can read it, "+
				"and satisfying a test you can see is not solving the problem")
	}
	// A hidden file that is also in the fixture is not hidden.
	for path := range t.Acceptance.Files {
		full := filepath.Join(t.FixturePath(), filepath.FromSlash(path))
		if _, err := os.Stat(full); err == nil {
			problems = append(problems, fmt.Sprintf(
				"acceptance file %s is also present in the fixture, so it is visible to the model", path))
		}
	}
	if t.Budget.MaxAttempts <= 0 {
		problems = append(problems, "no attempt budget")
	}
	if t.Budget.MaxWallSeconds <= 0 {
		problems = append(problems, "no wall-clock budget; a result without a fixed budget is not comparable")
	}

	if len(problems) > 0 {
		return fmt.Errorf("task %q: %s", t.ID, strings.Join(problems, "; "))
	}
	return nil
}

// Timeout returns the acceptance command's bound.
func (a Acceptance) Timeout() time.Duration {
	if a.TimeoutSeconds > 0 {
		return time.Duration(a.TimeoutSeconds) * time.Second
	}
	return 5 * time.Minute
}

// WallClock returns the attempt's bound.
func (b Budget) WallClock() time.Duration {
	if b.MaxWallSeconds > 0 {
		return time.Duration(b.MaxWallSeconds) * time.Second
	}
	return 10 * time.Minute
}

// LoadTask reads one task file.
// Membership reports the task's set, defaulting an unlabelled task to dev.
//
// Defaulting to dev is the conservative direction: an unlabelled task cannot
// accidentally end up in a held-out headline number, which is the error that
// would matter.
func (t Task) Membership() Set {
	if t.Set == "" {
		return SetDev
	}
	return t.Set
}

// Synthetic reports whether the fixture is generated rather than drawn from
// real history. Synthetic tasks are reported separately and never folded into
// a headline rate over real tasks.
func (t Task) Synthetic() bool { return t.LeakRisk == LeakSynthetic }

func LoadTask(path string) (Task, error) {
	body, err := os.ReadFile(path) //nolint:gosec // an operator-supplied task file
	if err != nil {
		return Task{}, err
	}
	var t Task
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return Task{}, fmt.Errorf("eval: parse %s: %w", path, err)
	}
	t.path = path
	return t, t.Validate()
}

// LoadSet reads every task under a directory, in a stable order.
//
// A malformed task is an error rather than a skip: silently running a smaller
// set than the one named in the results is how a number stops meaning what it
// says.
func LoadSet(dir string) ([]Task, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// A fixture is a snapshot of somebody's repository, and a
			// snapshot of *this* one carries the task files that existed
			// when it was taken. Walking into it loads a historical task
			// definition as though it were current — which surfaced as
			// "duplicate task id" the moment a real pre-fix fixture was
			// materialized. Fixtures are data, not task definitions.
			if d.Name() == AnnotationDir || p != dir && d.Name() == "fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".task.yaml") {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("eval: no .task.yaml files under %s", dir)
	}
	sort.Strings(paths)

	var tasks []Task
	var problems []error
	seen := map[string]string{}
	for _, p := range paths {
		t, err := LoadTask(p)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if prev, dup := seen[t.ID]; dup {
			problems = append(problems, fmt.Errorf("eval: duplicate task id %q in %s and %s", t.ID, prev, p))
			continue
		}
		seen[t.ID] = p
		tasks = append(tasks, t)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return tasks, nil
}
