package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Where a benchmark actually is, as a state rather than an impression.
//
// The three stages exist because "ready" is not one question. A harness can
// be complete while the dataset is empty; a dataset can be labelled while the
// configuration is still moving. Collapsing those into a single readiness
// flag is how a run happens before the thing it depends on.

// Stage is a readiness state.
type Stage string

const (
	// StageDatasetConstruction: the machinery works and there is nothing to
	// measure yet.
	StageDatasetConstruction Stage = "READY_FOR_DATASET_CONSTRUCTION"
	// StageDevTuning: enough real dev tasks carry ground truth, and the
	// judge answers, so parameters can be fitted.
	StageDevTuning Stage = "READY_FOR_DEV_TUNING"
	// StageHeldoutEvaluation: tuning is frozen and the held-out set meets
	// the bar. The last stage, and the only one whose numbers are quotable.
	StageHeldoutEvaluation Stage = "READY_FOR_HELDOUT_EVALUATION"
)

// ReadinessInput is what the assessment is made from.
type ReadinessInput struct {
	Tasks       []Task
	Coverage    AnnotationStatus
	Admission   AdmissionSummary
	Reliability ReliabilityReport
	// SmokeOK says an authenticated minimal call to the judgment service
	// succeeded.
	SmokeOK bool
	// TuningFrozen says the operator has declared the dev-fitted parameters
	// final. It is a claim they make, recorded in the set's own metadata,
	// not something this code can infer: a configuration that has not
	// changed recently is not the same as one somebody froze.
	TuningFrozen bool
	// HeldoutRequired is the qualification bar.
	HeldoutRequired int
	// MinDevForTuning is how many labelled dev tasks make tuning meaningful.
	MinDevForTuning int
}

// DefaultMinDevForTuning is the floor for fitting anything on dev.
//
// Ten is not a sample that settles a threshold; it is the point below which
// fitting one is obviously self-deception. The real bar is whether the dev
// comparison separates arms at all, which only a run can answer.
const DefaultMinDevForTuning = 10

// Readiness is the assessed stage and why.
type Readiness struct {
	Stage Stage `json:"stage"`
	// Blockers are what stands between this stage and the next.
	Blockers []string `json:"blockers,omitempty"`
	// Next names the stage those blockers lead to.
	Next Stage `json:"next,omitempty"`
}

// Assess reports the stage a benchmark has reached.
func Assess(in ReadinessInput) Readiness {
	if in.HeldoutRequired <= 0 {
		in.HeldoutRequired = DefaultThresholds().HeldoutRealTasks
	}
	if in.MinDevForTuning <= 0 {
		in.MinDevForTuning = DefaultMinDevForTuning
	}

	var devLabelled, heldoutTotal, heldoutLabelled int
	for _, t := range in.Tasks {
		if t.Membership() != SetHeldout {
			continue
		}
		heldoutTotal++
	}
	devLabelled = in.Coverage.BySet[SetDev]
	heldoutLabelled = in.Coverage.BySet[SetHeldout]

	// Dev tuning first: nothing later is reachable without it.
	var devBlockers []string
	if !in.SmokeOK {
		devBlockers = append(devBlockers,
			"the authenticated judgment smoke call has not succeeded; `bcode judgment smoke`")
	}
	if devLabelled < in.MinDevForTuning {
		devBlockers = append(devBlockers, fmt.Sprintf(
			"%d dev task(s) carry localization ground truth, and %d are needed to fit "+
				"anything meaningfully; `bcode eval annotate <task>`",
			devLabelled, in.MinDevForTuning))
	}
	if in.Admission.Ineligible > 0 {
		devBlockers = append(devBlockers, fmt.Sprintf(
			"%d task(s) fail the admission protocol; `bcode eval admit`", in.Admission.Ineligible))
	}
	if len(devBlockers) > 0 {
		return Readiness{Stage: StageDatasetConstruction, Next: StageDevTuning,
			Blockers: devBlockers}
	}

	// Then held-out.
	var heldBlockers []string
	if !in.TuningFrozen {
		heldBlockers = append(heldBlockers,
			"the tuned configuration has not been declared frozen; a held-out run under a "+
				"configuration still being adjusted measures the adjustment")
	}
	if heldoutTotal < in.HeldoutRequired {
		heldBlockers = append(heldBlockers, fmt.Sprintf(
			"%d held-out task(s); qualification requires %d", heldoutTotal, in.HeldoutRequired))
	}
	if heldoutLabelled < heldoutTotal {
		heldBlockers = append(heldBlockers, fmt.Sprintf(
			"%d of %d held-out task(s) carry ground truth", heldoutLabelled, heldoutTotal))
	}
	if n := len(in.Reliability.Unadjudicated); n > 0 {
		heldBlockers = append(heldBlockers, fmt.Sprintf(
			"%d task(s) have unresolved annotator disagreements: %s",
			n, strings.Join(in.Reliability.Unadjudicated, ", ")))
	}
	if heldoutTotal > 0 {
		var single []string
		for _, id := range in.Reliability.SingleAnnotated {
			for _, t := range in.Tasks {
				if t.ID == id && t.Membership() == SetHeldout {
					single = append(single, id)
				}
			}
		}
		sort.Strings(single)
		if len(single) > 0 {
			heldBlockers = append(heldBlockers, fmt.Sprintf(
				"%d held-out task(s) have only one annotator; publication-quality labels "+
					"are read twice and adjudicated: %s", len(single), strings.Join(single, ", ")))
		}
	}
	if len(heldBlockers) > 0 {
		return Readiness{Stage: StageDevTuning, Next: StageHeldoutEvaluation,
			Blockers: heldBlockers}
	}
	return Readiness{Stage: StageHeldoutEvaluation}
}

// Format renders the assessment.
func (r Readiness) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", r.Stage)
	if len(r.Blockers) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "\nBefore %s:\n", r.Next)
	for _, blocker := range r.Blockers {
		fmt.Fprintf(&b, "  - %s\n", blocker)
	}
	return b.String()
}
