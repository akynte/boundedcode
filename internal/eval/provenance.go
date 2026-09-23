package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/version"
)

// Everything a benchmark run must record about itself to be reproducible, and
// the identity derived from it.
//
// The rule this enforces is narrow and unforgiving: two runs that differ in
// anything that could move a number must not be able to look like the same
// experiment. The failure it prevents is the quiet one — a threshold nudged, a
// prompt reworded, a model alias that moved underneath — where two artifacts
// carry the same label and different measurements, and whoever compares them
// later has no way to notice.
//
// Nothing here holds a secret or a line of anybody's source. Prompts are
// recorded as digests, the dataset as a digest of its task definitions, and
// the model identifiers are the names the operator configured.

// Provenance is the frozen description of one benchmark run.
type Provenance struct {
	// ExperimentID is the digest over everything below that could change a
	// result. See ExperimentDigest for exactly what goes in and what does not.
	ExperimentID string `json:"experiment_id"`
	// RunID identifies this execution rather than the configuration. Two
	// runs of one experiment on two machines share an ExperimentID and
	// differ here. See RunFingerprint.
	RunID string `json:"run_id,omitempty"`
	// StartedAt is when the execution began.
	StartedAt time.Time `json:"started_at,omitzero"`
	// AllowUnverifiedModel lets a benchmark proceed when the service names
	// no model. It is a development escape: the run happens, and it is not
	// publishable. Recorded so a reader of the artifact can see it was used.
	AllowUnverifiedModel bool `json:"allow_unverified_model,omitempty"`

	// --- code revision ---

	// Commit is the BoundedCode revision the harness was built from, and
	// Dirty says the working tree had uncommitted changes when it ran.
	//
	// A dirty tree does not invalidate a run, but it makes the commit a lie
	// about what executed, so it is part of the identity and the report says
	// so in its own line rather than in a footnote.
	Commit  string `json:"commit"`
	Dirty   bool   `json:"working_tree_dirty"`
	Version string `json:"version"`

	// --- what was run ---

	Set    string   `json:"set,omitempty"`
	Repeat int      `json:"repeat,omitempty"`
	Arms   []string `json:"arms,omitempty"`

	// --- models ---

	// JudgmentModelRequested is the id from judgment.yaml;
	// JudgmentModelServed is what the service reported running, when it says.
	// A difference between them means the artifact describes a model the run
	// did not use.
	JudgmentModelRequested string `json:"judgment_model_requested,omitempty"`
	JudgmentModelServed    string `json:"judgment_model_served,omitempty"`
	JudgmentModelPinned    bool   `json:"judgment_model_pinned,omitempty"`
	// ReasoningModel is the local model that did the engineering, and
	// EmbeddingModel the one behind the local semantic control arm. Both are
	// the exact identifiers, not the role names.
	ReasoningModel string `json:"reasoning_model,omitempty"`
	EmbeddingModel string `json:"embedding_model,omitempty"`

	// --- inference parameters ---

	Inference InferenceParams `json:"inference,omitzero"`
	// Seed is the harness seed where one applies. Local generation is not
	// deterministic across runs even at a fixed seed, which is why repeats
	// exist; the seed is recorded so the parts that are reproducible are.
	Seed int64 `json:"seed,omitempty"`

	// --- tuning ---

	Tuning         retrieval.RerankTuning `json:"tuning,omitzero"`
	LocalizeTuning task.LocalizeTuning    `json:"localize_tuning,omitzero"`

	// --- prompts and schemas ---

	// RubricDigest is over the exact relevance rubric text sent to the judge.
	// Rewording a prompt changes what was measured as surely as changing a
	// threshold does, and a prompt is too long to put in an artifact.
	RubricDigest string `json:"rubric_digest"`
	// JudgmentSchemaDigest is over the wire contract: the primitive kinds and
	// the shape of their criteria.
	JudgmentSchemaDigest string `json:"judgment_schema_digest"`
	// AnnotationRubricVersion is the ground-truth rubric the localization
	// labels were written against. A rubric change must not silently
	// reinterpret old labels, so the version travels with the numbers.
	AnnotationRubricVersion string `json:"annotation_rubric_version,omitempty"`

	// --- dataset ---

	// DatasetDigest is over every task definition in the set, and
	// MembershipDigest over the dev/held-out assignment alone. They are
	// separate because they fail differently: a task edited mid-experiment
	// and a task moved between sets are different mistakes, and a single
	// digest would only say "something changed".
	DatasetDigest    string `json:"dataset_digest"`
	MembershipDigest string `json:"membership_digest"`
	// AnnotationDigest is over the localization labels in force. Empty when
	// no task carries any.
	AnnotationDigest string `json:"annotation_digest,omitempty"`
	TaskCount        int    `json:"task_count"`
	ScorableCount    int    `json:"scorable_localization_tasks"`

	// --- cache and execution ---

	// Redact is the judgment redaction mode, which decides what evidence the
	// judged arm sees and therefore which comparisons are model-level.
	Redact string `json:"redact,omitempty"`
	// Cache is the state of every cache that could move a measured number.
	// It is part of the identity when it can: see CachePolicy.Identity.
	Cache CachePolicy `json:"cache"`
	// ArmOrders is the sequence arms ran in, per cell. Recorded so a reader
	// can check the counterbalancing rather than trust it, and so an order
	// effect stays a checkable explanation rather than an unfalsifiable one.
	ArmOrders []ArmOrder `json:"arm_orders,omitempty"`
	// SplitProtocol is the rule that assigned dev and held-out.
	SplitProtocol string `json:"split_protocol,omitempty"`

	// --- runtime ---

	Runtime RuntimeIdentity `json:"runtime,omitzero"`
}

// InferenceParams are the generation settings that were in force.
type InferenceParams struct {
	Temperature   float64 `json:"temperature,omitempty"`
	MaxTokens     int     `json:"max_tokens,omitempty"`
	ContextTokens int     `json:"context_tokens,omitempty"`
	MaxSteps      int     `json:"max_steps,omitempty"`
	MaxTools      int     `json:"max_tools,omitempty"`
	Thinking      string  `json:"thinking,omitempty"`
	Profile       string  `json:"profile,omitempty"`
}

// RuntimeIdentity is enough about the machine to interpret a latency, and no
// more. It deliberately holds nothing that identifies the operator.
type RuntimeIdentity struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	CPUs   int    `json:"cpus"`
	GoVer  string `json:"go_version"`
	VRAMMB int    `json:"vram_mb,omitempty"`
	RAMMB  int    `json:"ram_mb,omitempty"`
}

// CaptureRevision reads the code revision from git.
//
// A missing git, or a directory that is not a checkout, yields "unknown" and
// dirty: the honest reading of "I cannot tell what code this was" is not
// "clean".
func CaptureRevision(ctx context.Context, dir string) (commit string, dirty bool) {
	// A short deadline of its own: reading a revision must not hang a
	// benchmark on a repository whose git is wedged, and the caller's
	// context may have hours left on it.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sha, err := gitOutput(ctx, dir, "rev-parse", "HEAD")
	if err != nil || sha == "" {
		return "unknown", true
	}
	status, err := gitOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		return sha, true
	}
	return sha, strings.TrimSpace(status) != ""
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // fixed arguments
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DatasetDigests derives the two dataset identities.
//
// The task digest covers everything that defines the problem — objective,
// fixture, scope, acceptance, budget, verification level — and the expected
// labels, because a change to ground truth changes what a recall number means.
// It excludes the notes field, which is prose for a human reader and moves
// without changing any measurement.
func DatasetDigests(tasks []Task) (dataset, membership, annotation string, scorable int) {
	sorted := append([]Task(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	data := sha256.New()
	member := sha256.New()
	labels := sha256.New()
	var anyLabel bool

	for _, t := range sorted {
		shape, _ := json.Marshal(struct {
			ID           string     `json:"id"`
			Objective    string     `json:"objective"`
			Fixture      string     `json:"fixture"`
			Category     Category   `json:"category"`
			Scope        []string   `json:"scope"`
			Verification string     `json:"verification"`
			LeakRisk     LeakRisk   `json:"leak_risk"`
			Budget       Budget     `json:"budget"`
			Acceptance   Acceptance `json:"acceptance"`
		}{t.ID, t.Objective, t.Fixture, t.Category, t.Scope, t.Verification,
			t.LeakRisk, t.Budget, t.Acceptance})
		fmt.Fprintf(data, "%s\x00%s\x00", t.ID, shape)
		fmt.Fprintf(member, "%s\x00%s\x00", t.ID, t.Membership())

		if len(t.Expected.Files) > 0 || len(t.Expected.Symbols) > 0 {
			anyLabel = true
			scorable++
			expected, _ := json.Marshal(t.Expected)
			fmt.Fprintf(labels, "%s\x00%s\x00", t.ID, expected)
		}
	}
	dataset = hex.EncodeToString(data.Sum(nil))
	membership = hex.EncodeToString(member.Sum(nil))
	if anyLabel {
		annotation = hex.EncodeToString(labels.Sum(nil))
	}
	return dataset, membership, annotation, scorable
}

// PromptDigests derive the identity of what was asked and in what shape.
func PromptDigests() (rubric, schema string) {
	r := sha256.Sum256([]byte(retrieval.RelevanceRubric()))
	// The wire contract, as a value rather than a description of one: the
	// primitive kinds and the criteria shape each carries. A change here —
	// noul gaining criteria, say — changes what the service was asked even
	// when the rubric text is identical.
	s := sha256.Sum256([]byte("noul:none|noul:true-false|choice:option-map|score:ordered-levels|v1"))
	return hex.EncodeToString(r[:]), hex.EncodeToString(s[:])
}

// ExperimentDigest is the identity of a configuration.
//
// What goes in is everything that could move a number: the code revision and
// whether it was modified, the set and its membership, the dataset and its
// labels, every model identifier, the tuning, the prompts and the wire shape.
//
// What stays out is everything that cannot: the wall-clock time, the machine,
// the served model id, and the results themselves. Two people running the same
// configuration on different hardware are running the same experiment and
// should see the same id; the hardware is recorded beside it, not inside it.
//
// The served model is deliberately excluded even though a mismatch matters. It
// is an observation about a run, not a choice about the experiment, and
// folding it in would give the same configuration two identities depending on
// what the service happened to do.
func ExperimentDigest(p Provenance) string {
	h := sha256.New()
	arms := append([]string(nil), p.Arms...)
	sort.Strings(arms)
	tuning, _ := json.Marshal(p.Tuning)
	localize, _ := json.Marshal(p.LocalizeTuning)
	inference, _ := json.Marshal(p.Inference)

	for _, part := range []string{
		"experiment-v1",
		p.Commit, fmt.Sprint(p.Dirty),
		p.Set, fmt.Sprint(p.Repeat), strings.Join(arms, ","),
		p.JudgmentModelRequested, p.ReasoningModel, p.EmbeddingModel,
		string(tuning), string(localize), string(inference), fmt.Sprint(p.Seed),
		p.RubricDigest, p.JudgmentSchemaDigest, p.AnnotationRubricVersion,
		p.DatasetDigest, p.MembershipDigest, p.AnnotationDigest,
		// The cache policy belongs in the identity because it changes what
		// the cost and latency figures mean. Two runs differing only in a
		// warm judgment cache measured different things.
		p.Cache.Identity(), p.SplitProtocol,
	} {
		// Length-prefixed: a separator is not injective, and two different
		// configurations concatenating to the same string is exactly the
		// collision this function exists to prevent.
		fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return "exp-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// CaptureRuntime records the machine, for interpreting latency.
func CaptureRuntime(vramMB, ramMB int) RuntimeIdentity {
	return RuntimeIdentity{
		OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(),
		GoVer: runtime.Version(), VRAMMB: vramMB, RAMMB: ramMB,
	}
}

// Finalise fills the derived fields and returns the completed provenance.
func (p Provenance) Finalise() Provenance {
	if p.Version == "" {
		p.Version = version.Current().Version
	}
	if p.RubricDigest == "" || p.JudgmentSchemaDigest == "" {
		p.RubricDigest, p.JudgmentSchemaDigest = PromptDigests()
	}
	if p.Cache == (CachePolicy{}) {
		p.Cache = DefaultCachePolicy()
	}
	if p.SplitProtocol == "" {
		p.SplitProtocol = SplitProtocolVersion
	}
	p.ExperimentID = ExperimentDigest(p)
	return p
}

// ModelIdentityStatus is what can be established about which external model
// actually served a run.
//
// A pinned request identifier and a verified served identifier are two
// different pieces of evidence, and conflating them is the mistake this type
// exists to prevent. "We asked for jev-1.13.0 and got HTTP 200" establishes
// that the service accepted the identifier. It does not establish that
// jev-1.13.0 produced the answers: the vendor documents the response's model
// field as "the model that performed the evaluation" but documents nothing
// about how an alias resolves or whether a pinned request is honoured, so
// there is no contract to infer the second fact from the first.
type ModelIdentityStatus string

const (
	// ModelIdentityNotApplicable: no external judge was involved.
	ModelIdentityNotApplicable ModelIdentityStatus = "not_applicable"
	// ModelIdentityVerified: the service named the model it ran and it is
	// the one that was requested.
	ModelIdentityVerified ModelIdentityStatus = "verified"
	// ModelIdentityMismatch: the service named a different model.
	ModelIdentityMismatch ModelIdentityStatus = "mismatch"
	// ModelIdentityUnpinned: an alias was requested, so there is no
	// identifier to verify against.
	ModelIdentityUnpinned ModelIdentityStatus = "unpinned"
	// ModelIdentityUnverifiable: a pinned identifier was requested and the
	// service named no model, so which one served the request is unknown.
	ModelIdentityUnverifiable ModelIdentityStatus = "MODEL_IDENTITY_UNVERIFIABLE"
)

// ModelIdentity reports what is established about the serving model.
func (p Provenance) ModelIdentity() (ModelIdentityStatus, string) {
	switch {
	case p.JudgmentModelRequested == "":
		return ModelIdentityNotApplicable, "no external judge was used"
	case !p.JudgmentModelPinned:
		return ModelIdentityUnpinned, fmt.Sprintf("%q is an alias, so there is no "+
			"identifier for the service to be checked against",
			p.JudgmentModelRequested)
	case p.JudgmentModelServed == "":
		return ModelIdentityUnverifiable, fmt.Sprintf("%s was requested and accepted, but "+
			"the service named no model in its response, so which one produced these "+
			"answers is unknown. Acceptance of an identifier is not evidence that the "+
			"identifier served the request, and the vendor documents no guarantee that "+
			"it is.", p.JudgmentModelRequested)
	case p.JudgmentModelServed != p.JudgmentModelRequested:
		return ModelIdentityMismatch, fmt.Sprintf("the service ran %s, not the requested %s",
			p.JudgmentModelServed, p.JudgmentModelRequested)
	}
	return ModelIdentityVerified, p.JudgmentModelServed
}

// ValidForBenchmark reports whether a run's observed properties permit its
// numbers to be published as a measurement of the model it names.
//
// This is the publication bar, and it is stricter than production on exactly
// one axis: the serving model must be observable. A benchmark that claims to
// evaluate a pinned external model has to be able to say which model it
// evaluated, and "the identifier was accepted" does not say that.
//
// Production stays permissive and should. A task whose judge answered from a
// different snapshot got a slightly different ordering, which is advisory
// anyway; there is nothing to invalidate. An experiment cannot absorb the
// same uncertainty, because the artifact would name a model on no evidence.
//
// AllowUnverifiedModel is the development escape. It keeps every other
// benchmark check and permits an unverifiable serving model, at the cost of
// a run that Publishable refuses and the report marks as such.
func (p Provenance) ValidForBenchmark() (bool, string) {
	status, detail := p.ModelIdentity()
	switch status {
	case ModelIdentityNotApplicable, ModelIdentityVerified:
		return true, ""
	case ModelIdentityUnverifiable:
		if p.AllowUnverifiedModel {
			return true, ""
		}
		return false, string(ModelIdentityUnverifiable) + ": " + detail
	default:
		// A mismatch and an alias are both refused outright. Neither is
		// something a development flag should be able to wave through: one
		// means the artifact would name the wrong model, the other that it
		// could name no model at all.
		return false, detail
	}
}

// Publishable reports whether this run is fit to publish, and why not.
//
// A dirty tree and an unestablished serving model are the two hard refusals.
// Everything else a benchmark needs — ground truth, a held-out set, a
// reachable service — is checked by the preflight before anything runs; this
// is the last look at what actually did.
func (p Provenance) Publishable() (bool, string) {
	switch {
	case p.Dirty:
		return false, "the working tree had uncommitted changes, so the recorded commit " +
			"does not describe the code that ran"
	case p.Commit == "unknown":
		return false, "the code revision could not be determined"
	}
	status, detail := p.ModelIdentity()
	switch status {
	case ModelIdentityNotApplicable, ModelIdentityVerified:
		return true, ""
	case ModelIdentityUnverifiable:
		// Publishability is not negotiable by a flag. AllowUnverifiedModel
		// lets a development run proceed; it does not make the result
		// quotable, and this is where that distinction is enforced.
		return false, string(ModelIdentityUnverifiable) + ": " + detail
	default:
		return false, detail
	}
}

// RunFingerprint is the identity of one execution, as distinct from the
// experiment it belongs to.
//
// ExperimentDigest answers "which configuration is this" and is deliberately
// stable across machines: two people running the same configuration are
// running the same experiment and their numbers should be comparable. That
// makes it the wrong thing to trace a specific set of numbers back to. A run
// happened on a machine, at a commit that may have been modified, against a
// model the service chose, with caches in some state and arms in some order —
// and all of that is needed to explain a figure that looks odd.
//
// So there are two identities. The experiment says what was chosen; the run
// says what actually occurred.
func (p Provenance) RunFingerprint(startedAt time.Time) string {
	h := sha256.New()
	orders, _ := json.Marshal(p.ArmOrders)
	runtime, _ := json.Marshal(p.Runtime)
	for _, part := range []string{
		"run-v1",
		p.ExperimentID,
		p.Commit, fmt.Sprint(p.Dirty),
		p.JudgmentModelRequested, p.JudgmentModelServed,
		p.ReasoningModel, p.EmbeddingModel,
		p.Cache.Identity(), string(orders), string(runtime),
		startedAt.UTC().Format(time.RFC3339Nano),
	} {
		fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return "run-" + hex.EncodeToString(h.Sum(nil))[:16]
}
