package eval

import (
	"path"
	"sort"
	"strings"
)

// Scoring what retrieval found, separately from whether the task was solved.
//
// §8.3 makes context-retrieval misses a primary metric, and Task.Expected has
// recorded the files a real fixing commit touched since the task format was
// written — with nothing reading it. This is the consumer. Everything here is
// computed after the run, from the packet contents and the ground truth, and
// neither the score nor the expectation is ever shown to the system.
//
// Two stages are scored separately, because they fail differently and a single
// number cannot tell them apart:
//
//   - Generator recall asks whether the candidate set contained the file at
//     all, before any reranking. A miss here is retrieval's.
//   - Reranker recall and precision ask whether the file survived into the
//     packet the model was given. A miss here, with a generator hit, is the
//     ranking's.
//
// A benchmark that reports only the second cannot distinguish "the reranker
// buried it" from "nothing ever proposed it", and would credit or blame the
// reranker for either.

// LocalizationStatus says whether a run could be scored at all.
type LocalizationStatus string

const (
	// LocalizationScored: the task records ground truth and the run was
	// measured against it.
	LocalizationScored LocalizationStatus = "scored"
	// LocalizationUnscorable: the task records no localization ground truth.
	//
	// It is a status rather than an absence because the two read very
	// differently in a report. A missing score invites the reader to assume
	// zero; a run marked unscorable says the measurement was never possible,
	// and names the annotation that would make it so.
	LocalizationUnscorable LocalizationStatus = "unscorable_for_localization"
)

// LocalizationScore is how well retrieval found the files the work needed.
type LocalizationScore struct {
	Status LocalizationStatus `json:"status"`

	// Expected is how many files an annotator marked REQUIRED, and Useful how
	// many they marked USEFUL. RubricVersion says which rubric they were
	// written under, so a later rubric change cannot silently reinterpret a
	// published number.
	Expected      int    `json:"expected,omitempty"`
	Useful        int    `json:"useful,omitempty"`
	RubricVersion string `json:"rubric_version,omitempty"`
	// UsefulCarried is how many USEFUL files the packet held. They are
	// excluded from precision's denominator rather than counted against it.
	UsefulCarried        int `json:"useful_carried,omitempty"`
	PrecisionDenominator int `json:"precision_denominator,omitempty"`

	// GeneratorRecall is the share of expected files the candidate set held
	// before reranking, over GeneratedN candidates. It bounds everything
	// downstream: a file no generator proposed cannot be ranked into a packet.
	GeneratorRecall float64  `json:"generator_recall"`
	GeneratedN      int      `json:"generated_n"`
	GeneratorMissed []string `json:"generator_missed,omitempty"`

	// RerankerRecall and RerankerPrecision are over the packet the model
	// actually received, at K = RetrievedK slices' worth of distinct files.
	RerankerRecall    float64  `json:"reranker_recall"`
	RerankerPrecision float64  `json:"reranker_precision"`
	RetrievedK        int      `json:"retrieved_k"`
	RerankerMissed    []string `json:"reranker_missed,omitempty"`

	// LostInRanking names expected files the generator proposed and the
	// packet did not carry. This is the reranker's own failure, isolated: the
	// candidate existed and the ordering did not keep it.
	LostInRanking []string `json:"lost_in_ranking,omitempty"`

	// Recall and Precision are the packet-level figures, kept under their
	// original names so existing readers of the artifact do not break.
	Recall    float64  `json:"recall"`
	Precision float64  `json:"precision"`
	Found     int      `json:"found"`
	Retrieved int      `json:"retrieved"`
	Missed    []string `json:"missed,omitempty"`
}

// ScoreLocalization compares what the candidate set and the packet carried
// against the ground truth.
//
// It always returns a score. A task with no ground truth returns one marked
// unscorable rather than nil, so a report can count how much of a set could
// not be measured instead of silently averaging over the remainder.
//
// Paths are compared after normalisation, because the expectation is written
// by hand in a task file and the packet's paths come out of an index:
// "./x.go" and "x.go" are the same file, and a score that said otherwise
// would be measuring the task author's typing.
func ScoreLocalization(expected Expected, generated, retrieved []string) *LocalizationScore {
	if len(expected.Files) == 0 {
		return &LocalizationScore{Status: LocalizationUnscorable}
	}
	gen := normalisedSet(generated)
	got := normalisedSet(retrieved)
	useful := normalisedSet(expected.Useful)

	score := &LocalizationScore{
		Status:        LocalizationScored,
		Expected:      len(expected.Files),
		Useful:        len(expected.Useful),
		RubricVersion: expected.RubricVersion,
		GeneratedN:    len(gen),
		RetrievedK:    len(got),
		Retrieved:     len(got),
	}
	var inGenerator int
	for _, want := range expected.Files {
		key := normalisePath(want)
		proposed := gen[key]
		carried := got[key]
		if proposed {
			inGenerator++
		} else {
			score.GeneratorMissed = append(score.GeneratorMissed, want)
		}
		if carried {
			score.Found++
			continue
		}
		score.Missed = append(score.Missed, want)
		score.RerankerMissed = append(score.RerankerMissed, want)
		if proposed {
			score.LostInRanking = append(score.LostInRanking, want)
		}
	}
	sort.Strings(score.Missed)
	sort.Strings(score.RerankerMissed)
	sort.Strings(score.GeneratorMissed)
	sort.Strings(score.LostInRanking)

	score.GeneratorRecall = float64(inGenerator) / float64(score.Expected)
	score.Recall = float64(score.Found) / float64(score.Expected)
	score.RerankerRecall = score.Recall

	// Precision counts a USEFUL file as neither right nor wrong: it is
	// excluded from the denominator rather than counted against the packet.
	// A reranker that surfaced genuinely helpful context has not made an
	// error, and scoring it as one would push the packet towards carrying
	// only the files the fix happened to change — which is the metric this
	// rubric exists to avoid optimising for.
	judged := 0
	for path := range got {
		if useful[path] {
			score.UsefulCarried++
			continue
		}
		judged++
	}
	score.PrecisionDenominator = judged
	if judged > 0 {
		score.Precision = float64(score.Found) / float64(judged)
	}
	score.RerankerPrecision = score.Precision
	return score
}

// Scored reports whether this score contributes to an aggregate.
func (s *LocalizationScore) Scored() bool {
	return s != nil && s.Status == LocalizationScored
}

func normalisedSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[normalisePath(p)] = true
	}
	return out
}

func normalisePath(p string) string {
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	return path.Clean(p)
}
