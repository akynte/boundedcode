package retrieval

// Memory-note relevance: a judgment site over which durable notes enter the
// packet (design.md mechanism M8, second sub-site).
//
// Notes are operator-accepted text, not repository source or generated
// output, so this runs at redact: strict — the default — unlike the other
// two M8 sub-sites. addNotes today fills the memory budget newest-first with
// no relevance filter at all: a stale or off-topic note can crowd out one
// that actually bears on the current objective, and the only recovery is an
// operator manually pruning the note file.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/memory"
)

// NoteRelevanceSite names this judgment site in judgment.yaml's `sites` map.
const NoteRelevanceSite = "note_relevance"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            NoteRelevanceSite,
		Description:     "judges whether a durable memory note bears on the current objective before it competes for the packet's memory budget",
		Mechanism:       "M8",
		Version:         "1",
		MaxEffect:       judgment.TierOrdering,
		Redaction:       judgment.RedactStrict,
		Outcome:         "",
		EffectThreshold: 0,
	})
}

// NoteRelevance is one note's judged bearing on the objective.
type NoteRelevance struct {
	ID       string  `json:"id"`
	Relevant float64 `json:"relevant"`
}

// NoteRelevanceResult is one pass's outcome.
type NoteRelevanceResult struct {
	Total, Judged                       int
	Attempted, Applied                  bool
	Tier                                judgment.Tier
	SkipReason                          string
	Scores                              []NoteRelevance
	Requests, InputTokens, OutputTokens int
}

// NoteRelevanceTuning bounds one pass.
type NoteRelevanceTuning struct {
	Floor     float64
	BatchSize int
	Budget    time.Duration
}

const (
	DefaultNoteRelevanceFloor     = 0.35
	DefaultNoteRelevanceBatchSize = 15
	DefaultNoteRelevanceBudget    = 5 * time.Second
)

func (t NoteRelevanceTuning) withDefaults() NoteRelevanceTuning {
	if t.Floor <= 0 {
		t.Floor = DefaultNoteRelevanceFloor
	}
	if t.BatchSize <= 0 {
		t.BatchSize = DefaultNoteRelevanceBatchSize
	}
	if t.Budget <= 0 {
		t.Budget = DefaultNoteRelevanceBudget
	}
	return t
}

func noteRelevanceProposition(id string) string {
	return "Considering the note with id `" + id + "`, against `objective`: does this note " +
		"bear on carrying out the objective — would an engineer following it read this note " +
		"first?"
}

// CheckNoteRelevance judges every note against objective.
//
// It never returns an error: a check that failed is a check that did not
// happen, matching every other judgment call site in this codebase.
func CheckNoteRelevance(ctx context.Context, j judgment.Judge, objective string, notes []memory.Note,
	tuning NoteRelevanceTuning, logf func(string, ...any)) NoteRelevanceResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := NoteRelevanceResult{Total: len(notes), Tier: judgment.SiteTier(j, NoteRelevanceSite)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = SkipNoJudge
		return res
	case strings.TrimSpace(objective) == "":
		res.SkipReason = SkipNoObjective
		return res
	case len(notes) == 0:
		res.SkipReason = SkipNoCandidates
		return res
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()

	for start := 0; start < len(notes); start += tn.BatchSize {
		end := min(start+tn.BatchSize, len(notes))
		scores, judged, note := checkNoteBatch(ctx, j, objective, notes[start:end], logf)
		res.Judged += judged
		res.Scores = append(res.Scores, scores...)
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
	}
	res.Applied = res.Judged > 0
	return res
}

func checkNoteBatch(ctx context.Context, j judgment.Judge, objective string, batch []memory.Note,
	logf func(string, ...any)) ([]NoteRelevance, int, judgment.Note) {

	st := judgment.NewState(judgment.RedactStrict)
	if err := st.Objective(objective); err != nil {
		logf("note relevance: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()}
	}
	ids := make([]string, 0, len(batch))
	qs := make(map[string]judgment.Question, len(batch))
	for n, note := range batch {
		id := "note" + strconv.Itoa(n)
		// TrustedFact, not ClaimText: a memory note is operator-accepted
		// text (see internal/memory's own package comment), not a model's
		// unreviewed claim about its own work.
		if err := st.TrustedFact(id, note.Text); err != nil {
			logf("note relevance: %v", err)
			continue
		}
		ids = append(ids, id)
		qs[id] = judgment.Noul(noteRelevanceProposition(id))
	}
	if len(ids) == 0 {
		return nil, 0, judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible notes in batch"}
	}
	answers, note := judgment.AskAll(ctx, j, st, qs)
	var scores []NoteRelevance
	judged := 0
	for n, id := range ids {
		a := answers[id]
		if !a.Answered {
			continue
		}
		judged++
		scores = append(scores, NoteRelevance{ID: batch[n].ID, Relevant: a.Noul})
	}
	return scores, judged, note
}
