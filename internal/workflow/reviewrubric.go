package workflow

// Typed review rubric: a fixed set of judgment questions over each diff hunk,
// alongside the fresh-context prose review (design.md mechanism M7).
//
// critic.Review is the right idea and the wrong sole instrument: it asks a
// chat model for concerns as JSON, and the concerns — which hunks are risky,
// how risky — vary run to run because nothing about a chat completion is
// stable across identical inputs. This rubric asks the same fixed questions
// of every hunk, calibrated and self-consistent, and uses the answers only
// to decide what a human or the review model reads first. It never decides
// anything the way the review model's accept/reject verdict does.

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
)

// ReviewRubricSite names this judgment site in judgment.yaml's `sites` map.
const ReviewRubricSite = "review_rubric"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            ReviewRubricSite,
		Description:     "asks a fixed rubric of every diff hunk and a whole-diff read-depth score, to order what a reviewer reads first",
		Mechanism:       "M7",
		Version:         "1",
		MaxEffect:       judgment.TierOrdering,
		Redaction:       judgment.RedactRepoText,
		Outcome:         "the gate decision on the change these hunks were part of",
		EffectThreshold: 0,
	})
}

// reviewRubricNouls is the fixed set, asked positively and once each — never
// derived as one minus another, per design.md's own risk note for this
// mechanism.
var reviewRubricNouls = map[string]string{
	"exported_signature": "introduces or changes a public or exported signature",
	"error_handling":     "changes error handling: swallows an error, wraps it differently, converts it to a panic, or returns nil where an error was previously returned",
	"concurrency":        "introduces concurrency, shared mutable state, or a lock",
	"hardcoded_config":   "hardcodes a value that reads as configuration or an environment-specific detail",
	"stub":               "leaves a TODO, FIXME, placeholder, or stub in place of a real implementation",
	"duplication":        "duplicates logic that already exists elsewhere in the diff's context",
	"outside_objective":  "changes behaviour the objective's description does not mention",
}

// ReviewRubricQuestionCount is how many fixed Nouls the rubric asks per
// hunk. Exported so a caller normalizing ReviewRubricFinding.ReadWeight into
// a 0..1 probability — for calibration pairing, say — does not have to
// hardcode a count that would silently drift out of sync with the map above.
var ReviewRubricQuestionCount = len(reviewRubricNouls)

// ReviewRubricFinding is one hunk's answers to the fixed rubric.
type ReviewRubricFinding struct {
	Path   string `json:"path"`
	Header string `json:"header"`
	// Nouls maps each rubric key (see reviewRubricNouls) to its probability.
	// A key absent from the map was not answered for this hunk.
	Nouls map[string]float64 `json:"nouls"`
	// ReadWeight is how many of the rubric's Nouls cleared the site's floor,
	// used only to order hunks — the number itself is never shown as a score
	// of anything, because summing unrelated probabilities is exactly the
	// units error design.md warns retrieval's own rerank against.
	ReadWeight int `json:"read_weight"`
}

// ReviewRubricResult is one diff's rubric pass, plus the whole-diff read-depth
// Score.
type ReviewRubricResult struct {
	Total, Judged      int
	Attempted, Applied bool
	Tier               judgment.Tier
	SkipReason         string
	Findings           []ReviewRubricFinding
	// ReadDepth is the whole-diff Score answer: how much of the change a
	// careful reviewer would want to read line by line. ReadDepthLegend
	// names the level ReadDepth is closest to.
	ReadDepth                           float64
	ReadDepthLegend                     string
	ReadDepthAnswered                   bool
	Requests, InputTokens, OutputTokens int
	Latency                             time.Duration
}

// readDepthLevels is the Score's ordered criteria, lowest first, as the API
// requires.
var readDepthLevels = []string{
	"a mechanical rename or formatting change with no logic difference",
	"a local logic change confined to one function or one file",
	"a change to a contract: an exported signature, an API shape, or a schema",
	"a change to control flow that spans multiple files or crosses a concurrency boundary",
}

func readDepthLegend(score float64) string {
	idx := int(score + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(readDepthLevels) {
		idx = len(readDepthLevels) - 1
	}
	return readDepthLevels[idx]
}

// ReviewRubricTuning bounds one pass.
type ReviewRubricTuning struct {
	MaxHunks        int
	BatchSize       int
	Budget          time.Duration
	ReadWeightFloor float64
}

const (
	DefaultReviewRubricMaxHunks        = 40
	DefaultReviewRubricBatchSize       = 6
	DefaultReviewRubricBudget          = 10 * time.Second
	DefaultReviewRubricReadWeightFloor = 0.6
)

func (t ReviewRubricTuning) withDefaults() ReviewRubricTuning {
	if t.MaxHunks <= 0 {
		t.MaxHunks = DefaultReviewRubricMaxHunks
	}
	if t.BatchSize <= 0 {
		t.BatchSize = DefaultReviewRubricBatchSize
	}
	if t.Budget <= 0 {
		t.Budget = DefaultReviewRubricBudget
	}
	if t.ReadWeightFloor <= 0 {
		t.ReadWeightFloor = DefaultReviewRubricReadWeightFloor
	}
	return t
}

const reviewRubricCollection = "hunks"

func rubricProposition(id, key, phrase string) string {
	return "Considering the diff hunk with id `" + id + "` in `" + reviewRubricCollection + "`: " +
		"does it " + phrase + "?"
}

func readDepthProposition() string {
	return "Considering every hunk in `" + reviewRubricCollection + "` as one change: how much " +
		"of it would a careful reviewer want to read line by line, rather than skim?"
}

// CheckReviewRubric asks the fixed rubric of every hunk plus the whole-diff
// read-depth Score.
//
// It never returns an error: a check that failed is a check that did not
// happen, matching every other judgment call site in this codebase.
func CheckReviewRubric(ctx context.Context, j judgment.Judge, objective string, hunks []Hunk,
	tuning ReviewRubricTuning, logf func(string, ...any)) ReviewRubricResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := ReviewRubricResult{Total: len(hunks), Tier: judgment.SiteTier(j, ReviewRubricSite)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res
	case strings.TrimSpace(objective) == "":
		res.SkipReason = "no_objective"
		return res
	case len(hunks) == 0:
		res.SkipReason = "no_hunks"
		return res
	case !judgment.RedactModeOf(j).AtLeast(judgment.RedactRepoText):
		res.SkipReason = "redact_strict"
		return res
	}
	candidates := hunks
	if len(candidates) > tn.MaxHunks {
		candidates = candidates[:tn.MaxHunks]
		res.SkipReason = "too_many_hunks"
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()
	started := time.Now()

	if depth, legend, note := checkReadDepth(ctx, j, hunks, logf); depth != nil {
		res.ReadDepth = *depth
		res.ReadDepthLegend = legend
		res.ReadDepthAnswered = true
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
	}

	for start := 0; start < len(candidates); start += tn.BatchSize {
		end := min(start+tn.BatchSize, len(candidates))
		findings, judged, note := checkRubricBatch(ctx, j, candidates[start:end], tn, logf)
		res.Judged += judged
		res.Findings = append(res.Findings, findings...)
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
	}
	res.Latency = time.Since(started)
	res.Applied = res.Judged > 0 || res.ReadDepthAnswered
	sort.SliceStable(res.Findings, func(i, j int) bool { return res.Findings[i].ReadWeight > res.Findings[j].ReadWeight })
	return res
}

func checkReadDepth(ctx context.Context, j judgment.Judge, hunks []Hunk, logf func(string, ...any)) (*float64, string, judgment.Note) {
	st := judgment.NewState(judgment.RedactRepoText)
	items := make([]*judgment.RepoItem, 0, len(hunks))
	for n, h := range hunks {
		item, err := st.NewRepoItem("d"+strconv.Itoa(n), h.Path)
		if err != nil {
			continue
		}
		item.Meta("hunk_header", h.Header)
		if err := item.Text("diff", h.Body, st); err != nil {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, "", judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible hunks"}
	}
	if err := st.AddItems(reviewRubricCollection, items); err != nil {
		logf("review rubric: %v", err)
		return nil, "", judgment.Note{Source: judgment.SourceError, Detail: err.Error()}
	}
	answers, note := judgment.AskAll(ctx, j, st, map[string]judgment.Question{
		"depth": judgment.Score(readDepthProposition(), readDepthLevels),
	})
	if a := answers["depth"]; a.Answered {
		v := a.Score
		return &v, readDepthLegend(v), note
	}
	return nil, "", note
}

func checkRubricBatch(ctx context.Context, j judgment.Judge, batch []Hunk, tn ReviewRubricTuning,
	logf func(string, ...any)) ([]ReviewRubricFinding, int, judgment.Note) {

	st := judgment.NewState(judgment.RedactRepoText)
	items := make([]*judgment.RepoItem, 0, len(batch))
	ids := make([]string, 0, len(batch))
	used := make([]Hunk, 0, len(batch))
	qs := make(map[string]judgment.Question, len(batch)*len(reviewRubricNouls))
	for n, h := range batch {
		id := "r" + strconv.Itoa(n)
		item, err := st.NewRepoItem(id, h.Path)
		if err != nil {
			logf("review rubric: %v", err)
			continue
		}
		item.Meta("hunk_header", h.Header)
		if err := item.Text("diff", h.Body, st); err != nil {
			logf("review rubric: %v", err)
			continue
		}
		items = append(items, item)
		ids = append(ids, id)
		used = append(used, h)
		for key, phrase := range reviewRubricNouls {
			qs[key+"_"+id] = judgment.Noul(rubricProposition(id, key, phrase))
		}
	}
	if len(items) == 0 {
		return nil, 0, judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible hunks in batch"}
	}
	if err := st.AddItems(reviewRubricCollection, items); err != nil {
		logf("review rubric: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()}
	}

	answers, note := judgment.AskAll(ctx, j, st, qs)
	var findings []ReviewRubricFinding
	judged := 0
	for n, id := range ids {
		nouls := map[string]float64{}
		weight := 0
		answered := false
		for key := range reviewRubricNouls {
			a := answers[key+"_"+id]
			if !a.Answered {
				continue
			}
			answered = true
			nouls[key] = a.Noul
			if a.Noul >= tn.ReadWeightFloor {
				weight++
			}
		}
		if !answered {
			continue
		}
		judged++
		findings = append(findings, ReviewRubricFinding{
			Path: used[n].Path, Header: used[n].Header, Nouls: nouls, ReadWeight: weight,
		})
	}
	return findings, judged, note
}
