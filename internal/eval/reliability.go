package eval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Independent annotation, and what agreement between annotators is worth.
//
// One person's labels are one person's reading. For a dev set that is
// proportionate; for a held-out number somebody will quote, it is a single
// point of failure that nothing in the pipeline can detect — a systematically
// generous annotator produces a recall figure that is wrong in one direction
// for every task, and every test still passes.
//
// So held-out labels are written twice, independently, and the disagreements
// are adjudicated by a third decision that is recorded as such. The word
// independently is doing real work: an annotator who can see the other's
// labels is not a second reading, and LoadForAnnotator enforces that by
// refusing to hand over anybody else's.

// annotationSuffix distinguishes one annotator's file from another's.
func annotatorPath(taskDir, taskID, annotator string) string {
	return filepath.Join(taskDir, AnnotationDir,
		fmt.Sprintf("%s.%s.annotation.yaml", taskID, slug(annotator)))
}

// adjudicationPath is where the resolved labels live.
func adjudicationPath(taskDir, taskID string) string {
	return filepath.Join(taskDir, AnnotationDir, taskID+".adjudicated.yaml")
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '@' || r == '.' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "anonymous"
	}
	return out
}

// Adjudication is the resolved ground truth for a task annotated twice.
type Adjudication struct {
	TaskID        string `yaml:"task_id"`
	RubricVersion string `yaml:"rubric_version"`
	// Annotators are whose independent labels were compared, in order.
	Annotators []string `yaml:"annotators"`
	// Adjudicator decided the disagreements. Required, and required to be a
	// name: "resolved automatically" is not adjudication.
	Adjudicator   string `yaml:"adjudicator"`
	AdjudicatedAt string `yaml:"adjudicated_at"`
	// Note explains the calls that were not obvious. A disagreement resolved
	// without a reason is one nobody can revisit.
	Note string `yaml:"note,omitempty"`
	// Files are the final labels.
	Files map[string]Label `yaml:"files"`
	// Disagreements records what the annotators differed on and how it went,
	// so the resolution is auditable rather than just the result.
	Disagreements []Disagreement `yaml:"disagreements,omitempty"`
}

// Disagreement is one file the annotators labelled differently.
type Disagreement struct {
	Path  string           `yaml:"path"`
	Given map[string]Label `yaml:"given"`
	Final Label            `yaml:"final"`
}

// Validate refuses an adjudication that cannot be trusted.
func (a Adjudication) Validate() []string {
	var problems []string
	if strings.TrimSpace(a.TaskID) == "" {
		problems = append(problems, "task_id is empty")
	}
	if strings.TrimSpace(a.Adjudicator) == "" {
		problems = append(problems, "adjudicator is empty; a resolution nobody owns is not one")
	}
	if len(a.Annotators) < 2 {
		problems = append(problems, "fewer than two independent annotators")
	}
	for _, d := range a.Disagreements {
		if !d.Final.Valid() {
			problems = append(problems, fmt.Sprintf("%s: %q is not a rubric label", d.Path, d.Final))
		}
	}
	if len(a.Disagreements) > 0 && strings.TrimSpace(a.Note) == "" {
		problems = append(problems,
			"disagreements were resolved with no note; a call nobody explained is one "+
				"nobody can revisit")
	}
	return problems
}

// LoadForAnnotator returns one annotator's own labels and nothing else.
//
// The blind is the point. It refuses to return another annotator's file, so
// a second reading cannot become a review of the first — and a workflow that
// showed them would produce two labels that agree for the wrong reason.
func LoadForAnnotator(taskDir, taskID, annotator string) (*Annotation, error) {
	body, err := os.ReadFile(annotatorPath(taskDir, taskID, annotator)) //nolint:gosec // derived from the task set
	if errors.Is(err, os.ErrNotExist) {
		// Fall back to the single-annotator file, but only when it is this
		// annotator's. A dev task labelled once by somebody else must not
		// appear as a starting point.
		single, err := LoadAnnotation(taskDir, taskID)
		if err != nil || single == nil {
			return nil, err
		}
		if !sameAnnotator(single.Annotator, annotator) {
			return nil, nil
		}
		return single, nil
	}
	if err != nil {
		return nil, err
	}
	var a Annotation
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

func sameAnnotator(a, b string) bool { return slug(a) == slug(b) }

// SaveForAnnotator writes one annotator's independent labels.
func SaveForAnnotator(taskDir string, a Annotation) (string, error) {
	if problems := a.Validate(); len(problems) > 0 {
		return "", fmt.Errorf("eval: refusing to save: %s", strings.Join(problems, "; "))
	}
	path := annotatorPath(taskDir, a.TaskID, a.Annotator)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	body, err := yaml.Marshal(a)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# Independent localization labels for %s by %s, rubric %s.\n"+
		"#\n"+
		"# One annotator's own reading. It is not a review of anybody else's: the\n"+
		"# annotate command will not show another annotator's labels before this\n"+
		"# one is written, because two readings that saw each other are one.\n"+
		"#\n"+
		"# Evaluator-only. Nothing in the pipeline may read it.\n",
		a.TaskID, a.Annotator, a.RubricVersion)
	if err := os.WriteFile(path, append([]byte(header), body...), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// LoadIndependentAnnotations returns every annotator's labels for a task.
// Used by the adjudicator and by the agreement report — never by an
// annotator who has not yet submitted.
func LoadIndependentAnnotations(taskDir, taskID string) ([]Annotation, error) {
	dir := filepath.Join(taskDir, AnnotationDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Annotation
	prefix := taskID + "."
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".annotation.yaml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // inside the annotation dir
		if err != nil {
			return nil, err
		}
		var a Annotation
		dec := yaml.NewDecoder(strings.NewReader(string(body)))
		dec.KnownFields(true)
		if err := dec.Decode(&a); err != nil {
			return nil, fmt.Errorf("eval: %s: %w", name, err)
		}
		out = append(out, a)
	}
	// The unsuffixed single-annotator file counts as one reading.
	if single, err := LoadAnnotation(taskDir, taskID); err == nil && single != nil {
		seen := false
		for _, a := range out {
			if sameAnnotator(a.Annotator, single.Annotator) {
				seen = true
			}
		}
		if !seen {
			out = append(out, *single)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Annotator < out[j].Annotator })
	return out, nil
}

// Agreement is how two or more independent readings compared.
type Agreement struct {
	TaskID     string   `json:"task_id,omitempty"`
	Annotators []string `json:"annotators"`
	// Compared is the number of files both annotators labelled. A file only
	// one of them considered is not a disagreement; it is a gap, counted
	// separately, because treating it as one would punish thoroughness.
	Compared     int      `json:"compared"`
	OnlyOneRated int      `json:"rated_by_one_only"`
	Agreed       int      `json:"agreed"`
	Disagreed    int      `json:"disagreed"`
	RawAgreement float64  `json:"raw_agreement"`
	Disputes     []string `json:"disputes,omitempty"`

	// Kappa is Cohen's kappa, and Interpretable says whether it is worth
	// reading.
	//
	// It corrects raw agreement for the agreement two annotators would reach
	// by chance, which matters because these categories are very unevenly
	// used: on a typical task almost everything is NOT_REQUIRED, so two
	// annotators who agreed on nothing else would still agree most of the
	// time. But the same skew makes kappa unstable — one disagreement in a
	// rare category swings it — and on a handful of files it is noise with a
	// Greek letter on it. So it is reported with a flag rather than alone.
	Kappa              float64 `json:"kappa"`
	KappaInterpretable bool    `json:"kappa_interpretable"`
	KappaCaveat        string  `json:"kappa_caveat,omitempty"`

	// Matrix is the joint label count, keyed "A>B". It is carried so pooled
	// kappa over a whole set can be computed from the per-task summaries: a
	// per-task kappa rests on a handful of files and moves by a tenth when
	// one of them flips, and the pooled figure is the one worth quoting.
	Matrix map[string]int `json:"matrix,omitempty"`
}

// minKappaSample is the smallest number of jointly labelled files on which
// this reports kappa as meaningful. It is a floor against nonsense, not a
// claim that thirty is enough.
const minKappaSample = 30

// CompareAnnotations measures agreement between two independent readings.
func CompareAnnotations(a, b Annotation) Agreement {
	ag := Agreement{TaskID: a.TaskID, Annotators: []string{a.Annotator, b.Annotator}}
	marginalA, marginalB := map[Label]int{}, map[Label]int{}

	paths := map[string]bool{}
	for p := range a.Files {
		paths[p] = true
	}
	for p := range b.Files {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	for _, p := range sorted {
		la, okA := a.Files[p]
		lb, okB := b.Files[p]
		if !okA || !okB {
			ag.OnlyOneRated++
			continue
		}
		ag.Compared++
		if ag.Matrix == nil {
			ag.Matrix = map[string]int{}
		}
		ag.Matrix[string(la)+">"+string(lb)]++
		marginalA[la]++
		marginalB[lb]++
		if la == lb {
			ag.Agreed++
			continue
		}
		ag.Disagreed++
		ag.Disputes = append(ag.Disputes, fmt.Sprintf("%s: %s vs %s", p, la, lb))
	}
	if ag.Compared > 0 {
		ag.RawAgreement = float64(ag.Agreed) / float64(ag.Compared)
	}
	ag.Kappa, ag.KappaInterpretable, ag.KappaCaveat = cohenKappa(ag, marginalA, marginalB)
	return ag
}

// cohenKappa computes kappa and says whether to believe it.
func cohenKappa(ag Agreement, marginalA, marginalB map[Label]int) (float64, bool, string) {
	if ag.Compared == 0 {
		return 0, false, "nothing was jointly labelled"
	}
	n := float64(ag.Compared)
	var expected float64
	for _, label := range Labels() {
		expected += (float64(marginalA[label]) / n) * (float64(marginalB[label]) / n)
	}
	if expected >= 1 {
		// Both annotators used one category for everything. Kappa is
		// undefined here, and reporting zero would read as "no agreement"
		// when what happened is perfect agreement on a degenerate set.
		return 0, false, "both annotators used a single category; kappa is undefined and " +
			"raw agreement is the honest figure"
	}
	k := (ag.RawAgreement - expected) / (1 - expected)
	switch {
	case ag.Compared < minKappaSample:
		return k, false, fmt.Sprintf("only %d jointly labelled file(s); below %d one "+
			"disagreement moves kappa more than any real difference in reading",
			ag.Compared, minKappaSample)
	case expected > 0.9:
		return k, false, fmt.Sprintf("one category covers %.0f%% of the expected agreement; "+
			"kappa is unstable when prevalence is this skewed", expected*100)
	}
	return k, true, ""
}

// ReliabilityReport is agreement across a whole set.
type ReliabilityReport struct {
	RubricVersion string      `json:"rubric_version"`
	Tasks         []Agreement `json:"tasks,omitempty"`
	// SingleAnnotated are tasks with only one reading. Fine for dev, not for
	// a published held-out number.
	SingleAnnotated []string `json:"single_annotated,omitempty"`
	// Unadjudicated are tasks with disagreements nobody has resolved.
	Unadjudicated []string `json:"unadjudicated,omitempty"`
	// Closures are per-task, and the three counts below summarise them.
	// Gaps are reported separately from disagreements all the way through:
	// counting a file one annotator did not consider as a conflict would
	// punish thoroughness and drag kappa down for it.
	Closures       []Closure `json:"closures,omitempty"`
	Gaps           int       `json:"gaps"`
	UnresolvedGaps int       `json:"unresolved_gaps"`
	FullyClosed    int       `json:"fully_closed"`
	// OpenTasks names tasks that are not annotation-complete.
	OpenTasks []string `json:"open_tasks,omitempty"`
	// Pooled figures across every jointly labelled file.
	Compared         int     `json:"compared"`
	Agreed           int     `json:"agreed"`
	Disagreed        int     `json:"disagreed"`
	RawAgreement     float64 `json:"raw_agreement"`
	NeedAdjudication int     `json:"need_adjudication"`
}

// Reliability measures agreement across a task set.
func Reliability(taskDir string, tasks []Task) (ReliabilityReport, error) {
	r := ReliabilityReport{RubricVersion: AnnotationRubricVersion}
	for _, t := range tasks {
		annotations, err := LoadIndependentAnnotations(taskDir, t.ID)
		if err != nil {
			return r, err
		}
		switch len(annotations) {
		case 0:
			continue
		case 1:
			r.SingleAnnotated = append(r.SingleAnnotated, t.ID)
			continue
		}
		// Two readings are compared; a third would need a different
		// coefficient, so the report says what it used rather than
		// averaging pairs into something with no name.
		ag := CompareAnnotations(annotations[0], annotations[1])
		r.Tasks = append(r.Tasks, ag)
		r.Compared += ag.Compared
		r.Agreed += ag.Agreed
		r.Disagreed += ag.Disagreed

		closure, err := CloseOut(taskDir, t.ID)
		if err != nil {
			return r, err
		}
		r.Closures = append(r.Closures, closure)
		r.Gaps += closure.Gaps
		r.UnresolvedGaps += len(closure.UnresolvedGaps)
		if closure.Closed() {
			r.FullyClosed++
		} else {
			r.OpenTasks = append(r.OpenTasks, t.ID)
		}

		if ag.Disagreed == 0 && closure.Gaps == 0 {
			continue
		}
		r.NeedAdjudication++
		if len(closure.UnresolvedDisagreements) > 0 || len(closure.UnresolvedGaps) > 0 {
			r.Unadjudicated = append(r.Unadjudicated, t.ID)
		}
	}
	if r.Compared > 0 {
		r.RawAgreement = float64(r.Agreed) / float64(r.Compared)
	}
	sort.Strings(r.SingleAnnotated)
	sort.Strings(r.Unadjudicated)
	sort.Strings(r.OpenTasks)
	return r, nil
}

// PooledKappa reports kappa over every jointly labelled file in the set.
//
// This is the figure worth quoting. A per-task kappa rests on a handful of
// files and moves by a tenth when one of them flips; pooling the label
// matrices gives a sample that can carry the statistic.
//
// Annotator order is consistent within a task because the readings are
// sorted by name, and kappa is symmetric anyway, so pooling across tasks with
// different pairs is sound as long as the report says the pairs differ —
// which it does, per task.
func (r ReliabilityReport) PooledKappa() (float64, bool, string) {
	marginalA, marginalB := map[Label]int{}, map[Label]int{}
	agreed, compared := 0, 0
	for _, t := range r.Tasks {
		for key, n := range t.Matrix {
			a, b, ok := strings.Cut(key, ">")
			if !ok {
				continue
			}
			marginalA[Label(a)] += n
			marginalB[Label(b)] += n
			compared += n
			if a == b {
				agreed += n
			}
		}
	}
	if compared == 0 {
		return 0, false, "no task has two independent readings"
	}
	pooled := Agreement{
		Compared: compared, Agreed: agreed,
		RawAgreement: float64(agreed) / float64(compared),
	}
	return cohenKappa(pooled, marginalA, marginalB)
}

// OpenItem is one thing adjudication has to resolve.
//
// The two kinds are kept apart all the way through. A disagreement is two
// readings of the same file that differ; a gap is a file only one annotator
// considered. Folding gaps into disagreements would simplify the agreement
// arithmetic and make it wrong: it would count thoroughness as conflict, and
// kappa would fall every time somebody looked at one more file than their
// partner.
//
// What they have in common is that neither may quietly vanish. A gap left
// open means the union of considered files has an entry with no agreed label,
// and a held-out ground truth with holes in it is not ground truth.
type OpenItem struct {
	Path string
	// Gap is true when only one annotator considered the file.
	Gap bool
	// Given is each annotator's label, missing the one who did not look.
	Given map[string]Label
}

// OpenItems lists the disagreements and gaps between two readings, sorted.
func OpenItems(a, b Annotation) []OpenItem {
	paths := map[string]bool{}
	for p := range a.Files {
		paths[p] = true
	}
	for p := range b.Files {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var out []OpenItem
	for _, p := range sorted {
		la, okA := a.Files[p]
		lb, okB := b.Files[p]
		switch {
		case okA && okB && la == lb:
			continue
		case okA && okB:
			out = append(out, OpenItem{Path: p, Given: map[string]Label{
				a.Annotator: la, b.Annotator: lb,
			}})
		default:
			given := map[string]Label{}
			if okA {
				given[a.Annotator] = la
			}
			if okB {
				given[b.Annotator] = lb
			}
			out = append(out, OpenItem{Path: p, Gap: true, Given: given})
		}
	}
	return out
}

// LoadAdjudication reads a task's resolved labels, or nil when there are none.
func LoadAdjudication(taskDir, taskID string) (*Adjudication, error) {
	body, err := os.ReadFile(adjudicationPath(taskDir, taskID)) //nolint:gosec // derived from the task set
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Adjudication
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("eval: %s: %w", adjudicationPath(taskDir, taskID), err)
	}
	return &a, nil
}

// SaveAdjudication writes a task's resolved labels.
func SaveAdjudication(taskDir string, a Adjudication) (string, error) {
	if problems := a.Validate(); len(problems) > 0 {
		return "", fmt.Errorf("eval: refusing to save: %s", strings.Join(problems, "; "))
	}
	path := adjudicationPath(taskDir, a.TaskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	body, err := yaml.Marshal(a)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# Adjudicated localization ground truth for %s.\n"+
		"#\n"+
		"# Written by %s after two independent readings, resolving both\n"+
		"# disagreements and gaps. Gold evidence is legitimate at this stage and\n"+
		"# not during an independent reading.\n"+
		"#\n"+
		"# Evaluator-only. Nothing in the pipeline may read it.\n", a.TaskID, a.Adjudicator)
	if err := os.WriteFile(path, append([]byte(header), body...), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Closure is whether one task's ground truth is finished.
//
// The rule for a publication-quality held-out label is that every file in the
// union of what the annotators considered carries either two matching
// independent labels or an adjudicated one. Anything else is a hole, and a
// hole that disappears from the statistics is worse than one that fails a
// check.
type Closure struct {
	TaskID string `json:"task_id"`
	// Annotators is how many independent readings exist.
	Annotators int `json:"annotators"`
	// Union is the number of distinct files any annotator considered.
	Union int `json:"union"`
	// AgreedBoth are files both labelled identically.
	AgreedBoth int `json:"agreed_both"`
	// Disagreements and Gaps are the two kinds of open item, kept apart.
	Disagreements int `json:"disagreements"`
	Gaps          int `json:"gaps"`
	// Resolved counts open items an adjudication settled.
	Resolved int `json:"resolved"`
	// UnresolvedDisagreements and UnresolvedGaps are what is still missing.
	UnresolvedDisagreements []string `json:"unresolved_disagreements,omitempty"`
	UnresolvedGaps          []string `json:"unresolved_gaps,omitempty"`
}

// Closed reports a task whose ground truth is finished.
func (c Closure) Closed() bool {
	return c.Annotators >= 2 &&
		len(c.UnresolvedDisagreements) == 0 && len(c.UnresolvedGaps) == 0
}

// CloseOut reports whether one task's labels are finished.
func CloseOut(taskDir, taskID string) (Closure, error) {
	c := Closure{TaskID: taskID}
	annotations, err := LoadIndependentAnnotations(taskDir, taskID)
	if err != nil {
		return c, err
	}
	c.Annotators = len(annotations)
	if len(annotations) < 2 {
		return c, nil
	}
	a, b := annotations[0], annotations[1]
	adj, err := LoadAdjudication(taskDir, taskID)
	if err != nil {
		return c, err
	}

	union := map[string]bool{}
	for p := range a.Files {
		union[p] = true
	}
	for p := range b.Files {
		union[p] = true
	}
	c.Union = len(union)

	for _, item := range OpenItems(a, b) {
		settled := adj != nil && adj.Files[item.Path] != ""
		if settled {
			c.Resolved++
		}
		if item.Gap {
			c.Gaps++
			if !settled {
				c.UnresolvedGaps = append(c.UnresolvedGaps, item.Path)
			}
			continue
		}
		c.Disagreements++
		if !settled {
			c.UnresolvedDisagreements = append(c.UnresolvedDisagreements, item.Path)
		}
	}
	c.AgreedBoth = c.Union - c.Disagreements - c.Gaps
	sort.Strings(c.UnresolvedGaps)
	sort.Strings(c.UnresolvedDisagreements)
	return c, nil
}
