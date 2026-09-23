package judgeval

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

// The three-arm comparison — design instruction §5.
//
// An arm's absence is reported, never invented: a site with no meaningful
// deterministic classifier reports ArmResult.Available=false with a named
// reason rather than this package fabricating a keyword heuristic just so
// three bars appear on a chart. See ArmDeterministicBaseline and
// ArmLocalControl below for M4's actual state, which is the flagship
// worked example this file wires end to end; the other ten registered
// sites do not yet have a campaign adapter (see AdapterRegistered) and
// this file does not pretend otherwise.

// Arm names one leg of the comparison.
type Arm string

const (
	ArmDeterministicBaseline Arm = "deterministic_baseline"
	ArmLocalControl          Arm = "local_control"
	ArmJev                   Arm = "jev"
)

// ArmResult is one arm's outcome against one dataset split.
type ArmResult struct {
	Arm       Arm  `json:"arm"`
	Available bool `json:"available"`
	// UnavailableReason is set whenever Available is false, naming the
	// exact blocker rather than leaving a reader to guess — design
	// instruction §5: "mark it unavailable with the exact blocker."
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	N         int      `json:"n"`
	Predicted []string `json:"-"`
	Actual    []string `json:"-"`
	// Undecided counts cases the judge did not answer — a transport
	// failure, a refusal, an answer below the floor. They are excluded
	// from Classification rather than scored: a case nobody answered is
	// not evidence that the site predicts the negative label, and
	// counting it as one would make an outage look like a result.
	Undecided        int      `json:"undecided"`
	UndecidedClasses []string `json:"undecided_classes,omitempty"`

	Classification *ClassificationReport `json:"classification,omitempty"`

	Requests     int           `json:"requests"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
	LatencyTotal time.Duration `json:"latency_total_ns"`
}

// integrityState is the JSON shape GenerateMutationCases writes into
// Case.State for the verification_integrity site.
type integrityState struct {
	Objective string `json:"objective"`
	Path      string `json:"path"`
	Body      string `json:"body"`
}

type integrityGroundTruth struct {
	Label string `json:"label"`
	Rule  string `json:"rule"`
}

// RunM4Campaign runs every configured arm over cases (a dataset's Dev() or
// Heldout() slice) for verification_integrity, the M4 site. It is the one
// site this package wires an actual arm adapter for; see the package
// comment. j is the judge for the Jev arm — pass judgment.Off() or a
// judgment.Fake for a network-free run (design instruction §20's
// requirement that normal `go test ./...` never reaches the network is
// satisfied by every test in this package passing judgment.Fake or nil,
// never a live Client).
func RunM4Campaign(ctx context.Context, j judgment.Judge, cases []Case, tuning workflow.IntegrityTuning) (map[Arm]ArmResult, error) {
	results := map[Arm]ArmResult{
		ArmDeterministicBaseline: {
			Arm: ArmDeterministicBaseline, Available: false,
			UnavailableReason: "no deterministic classifier for test-weakening exists in this " +
				"repository; nothing here approximates 'does this hunk weaken what it checks' " +
				"without semantic judgment (confirmed by inspection, not assumed — see " +
				"judgment-validation.md)",
		},
		ArmLocalControl: {
			Arm: ArmLocalControl, Available: false,
			UnavailableReason: "no local instruct or classifier model is wired for this " +
				"proposition in the current architecture; internal/retrieval/local_rerank.go " +
				"provides an embedding-cosine control, but only for retrieval relevance " +
				"ranking, not for a weakens/legitimate judgment over a diff hunk. Wiring one " +
				"would be a new inference path, which this campaign task is scoped not to add",
		},
	}

	jev, err := runM4JevArm(ctx, j, cases, tuning)
	if err != nil {
		return nil, err
	}
	results[ArmJev] = jev
	return results, nil
}

func runM4JevArm(ctx context.Context, j judgment.Judge, cases []Case, tuning workflow.IntegrityTuning) (ArmResult, error) {
	res := ArmResult{Arm: ArmJev, N: len(cases)}
	if j == nil || !j.Available() {
		res.Available = false
		res.UnavailableReason = "no judge available (nil or Available()==false); pass " +
			"judgment.Off() explicitly to record that as the reason rather than a silent skip"
		return res, nil
	}
	res.Available = true

	for _, c := range cases {
		if c.Site != workflow.IntegritySite {
			return res, fmt.Errorf("case %s is for site %q, RunM4Campaign only evaluates %q",
				c.CaseID, c.Site, workflow.IntegritySite)
		}
		var st integrityState
		if err := json.Unmarshal(c.State, &st); err != nil {
			return res, fmt.Errorf("case %s: decoding state: %w", c.CaseID, err)
		}
		var gt integrityGroundTruth
		if err := json.Unmarshal(c.GroundTruth, &gt); err != nil {
			return res, fmt.Errorf("case %s: decoding ground_truth: %w", c.CaseID, err)
		}

		hunks := workflow.SplitDiff(st.Body)
		for i := range hunks {
			hunks[i].IsTest = true // dataset cases are all test-hunk mutations by construction
		}

		started := time.Now()
		out, checkErr := workflow.CheckTestIntegrity(ctx, j, st.Objective, hunks, tuning, nil)
		res.LatencyTotal += time.Since(started)
		res.Requests += out.Requests
		res.InputTokens += out.InputTokens
		res.OutputTokens += out.OutputTokens

		// A case the judge did not answer is not a prediction. Scoring it as
		// "not_weakening" — which is what reading Findings alone does — would
		// credit or blame the site for an answer it never gave.
		if checkErr != nil {
			res.Undecided++
			if class, ok := judgment.FailureOf(checkErr); ok && !slices.Contains(res.UndecidedClasses, string(class)) {
				// The distinct classes, not one entry per case: a reader
				// wants to know an outage happened, not to count it N times.
				res.UndecidedClasses = append(res.UndecidedClasses, string(class))
			}
			continue
		}

		predicted := "not_weakening"
		for _, f := range out.Findings {
			if f.Path == st.Path {
				predicted = string(MutationLabelWeakening)
				break
			}
		}
		res.Predicted = append(res.Predicted, predicted)
		res.Actual = append(res.Actual, gt.Label)
	}

	report := Classify(res.Predicted, res.Actual)
	res.Classification = &report
	return res, nil
}

// AdapterRegistered reports whether this package has a campaign arm adapter
// wired for site — currently true only for verification_integrity. A false
// here means `bcode judgment eval <site>` can load and validate the site's
// dataset and report dataset statistics, but cannot run the Jev arm against
// it: the honest state for the ten sites this audit did not have time to
// wire an adapter for, rather than a generic adapter silently producing
// numbers no one designed a proposition for.
func AdapterRegistered(site string) bool {
	return site == workflow.IntegritySite
}

// FallbackTrial simulates each way a judge can fail to answer — design
// instruction §19 — and confirms the harness's ordinary behaviour survives:
// the deterministic verification path this campaign wraps must complete
// exactly as it would with no judge configured at all. It does not use a
// live provider: a "provider timeout" is simulated by a context that is
// already expired, and "malformed answer" / "unavailable" are simulated
// with judgment.Fake and judgment.Off, which is what unanswered Noul
// answers already model correctly (see judgment.Answer's doc comment).
type FallbackTrial struct {
	Name  string
	Judge judgment.Judge
	Ctx   func() context.Context
}

// RunFallbackTrials runs every trial and reports which ones the campaign
// completed without error, and — critically — whether IntegrityResult ever
// reported Applied without Attempted, or any Finding when the judge had
// nothing to answer with. Either would be the R2 fallback guarantee
// breaking under instrumentation, which no benchmark harness may cause.
func RunFallbackTrials(trials []FallbackTrial, objective string, hunks []workflow.Hunk, tuning workflow.IntegrityTuning) []FallbackTrialResult {
	out := make([]FallbackTrialResult, 0, len(trials))
	for _, tr := range trials {
		ctx := context.Background()
		if tr.Ctx != nil {
			ctx = tr.Ctx()
		}
		// The error is deliberately not consulted: this trial's whole point
		// is that every way of not getting an answer leaves the harness
		// behaving as it would with no judge at all, which is a statement
		// about res, not about the error.
		res, _ := workflow.CheckTestIntegrity(ctx, tr.Judge, objective, hunks, tuning, nil)
		out = append(out, FallbackTrialResult{
			Name:                  tr.Name,
			Completed:             true,
			AttemptedWithoutJudge: res.Attempted && (tr.Judge == nil || !tr.Judge.Available()),
			FindingsWithNoJudge:   len(res.Findings) > 0 && (tr.Judge == nil || !tr.Judge.Available()),
			Result:                res,
		})
	}
	return out
}

// FallbackTrialResult is one trial's outcome.
type FallbackTrialResult struct {
	Name      string
	Completed bool
	// AttemptedWithoutJudge and FindingsWithNoJudge must both always be
	// false; either being true is a broken invariant, not a metric.
	AttemptedWithoutJudge bool
	FindingsWithNoJudge   bool
	Result                workflow.IntegrityResult
}
