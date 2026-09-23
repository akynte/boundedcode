package eval

import (
	"fmt"
	"math"
	"sort"
)

// Whether a Jev relevance score has useful probability semantics on this
// domain.
//
// Production compares the score against an absolute floor: a graph candidate
// below RelevanceFloor releases its reserved budget slot. That only makes
// sense if 0.35 means something — if candidates scoring 0.35 are relevant
// about a third of the time. A score that ranks well and is badly calibrated
// would make the ranking useful and every threshold arbitrary, and nothing in
// the integration would show the difference.
//
// The labels give the comparison. REQUIRED against NOT_REQUIRED is the
// clearest binary subset there is: the two categories mean "an engineer had
// to read this" and "they did not", which is exactly the proposition the
// question asks. USEFUL is neither, and it is reported separately rather than
// pushed into one side — forcing it into REQUIRED would inflate the positive
// rate and make the model look underconfident; into NOT_REQUIRED, the
// reverse. Either way the calibration figure would be an artefact of the
// coercion.
//
// These are diagnostics. Task success remains the engineering outcome; a
// well-calibrated reranker that does not help is still not worth running.

// CalibrationPoint is one scored, labelled candidate.
type CalibrationPoint struct {
	TaskID string  `json:"task_id"`
	Path   string  `json:"path"`
	Score  float64 `json:"score"`
	Label  Label   `json:"label"`
	// Origin is how retrieval found it, so the distribution can be split by
	// origin — which is the assumption the whole cross-origin ordering rests
	// on and has never been checked.
	Origin string `json:"origin,omitempty"`
}

// ReliabilityBin is one band of the calibration table.
type ReliabilityBin struct {
	Low   float64 `json:"low"`
	High  float64 `json:"high"`
	Count int     `json:"count"`
	// MeanScore is what the model said; Observed is what the labels show.
	// The gap between them is the calibration error in that band.
	MeanScore float64 `json:"mean_score"`
	Observed  float64 `json:"observed_rate"`
}

// Calibration is the diagnostic for one population.
type Calibration struct {
	// Population names what was measured: "all", or an origin.
	Population string `json:"population"`
	// Positives are REQUIRED, Negatives NOT_REQUIRED. Excluded counts USEFUL
	// candidates, which are reported and not scored.
	Positives int `json:"positives"`
	Negatives int `json:"negatives"`
	Excluded  int `json:"excluded_useful"`

	// Brier is the mean squared error of the probability against the
	// outcome. It is the headline because it is proper: it cannot be gamed
	// by a model that hedges, and it penalises confident mistakes hardest.
	Brier float64 `json:"brier"`
	// BaseRate is the share of REQUIRED in this population, and
	// BrierBaseline is the Brier score of always predicting it. A Brier
	// score reported without it is unreadable: 0.15 is excellent when the
	// base rate is 0.5 and useless when it is 0.05.
	BaseRate      float64 `json:"base_rate"`
	BrierBaseline float64 `json:"brier_baseline"`
	// Skill is 1 - Brier/BrierBaseline: positive means better than always
	// guessing the base rate, negative means worse.
	Skill float64 `json:"skill"`

	Bins []ReliabilityBin `json:"bins,omitempty"`

	// MeanRequired and MeanNotRequired are the score distributions by label.
	// A model that separates the two classes is useful for ranking even if
	// the absolute numbers are miscalibrated, and these say which situation
	// this is.
	MeanRequired    float64 `json:"mean_score_required"`
	MeanNotRequired float64 `json:"mean_score_not_required"`
	MeanUseful      float64 `json:"mean_score_useful,omitempty"`
	Separation      float64 `json:"separation"`

	// ECE is the expected calibration error, and Interpretable says whether
	// to read it. It is bin-count dependent and unstable on small samples,
	// so it is reported with a flag rather than quoted alone.
	ECE              float64 `json:"ece"`
	ECEInterpretable bool    `json:"ece_interpretable"`
	Caveat           string  `json:"caveat,omitempty"`
}

// minCalibrationSample is the smallest population this reports ECE on as
// meaningful. A floor against nonsense, not a claim that fifty is enough.
const minCalibrationSample = 50

// DefaultCalibrationBins is ten bands of width 0.1.
const DefaultCalibrationBins = 10

// Calibrate measures probability semantics over labelled, scored candidates.
func Calibrate(population string, points []CalibrationPoint, bins int) Calibration {
	if bins <= 0 {
		bins = DefaultCalibrationBins
	}
	c := Calibration{Population: population}

	var scored []CalibrationPoint
	var requiredSum, notRequiredSum, usefulSum float64
	for _, p := range points {
		switch p.Label {
		case LabelRequired:
			c.Positives++
			requiredSum += p.Score
			scored = append(scored, p)
		case LabelNotRequired:
			c.Negatives++
			notRequiredSum += p.Score
			scored = append(scored, p)
		case LabelUseful:
			c.Excluded++
			usefulSum += p.Score
		}
	}
	if c.Positives > 0 {
		c.MeanRequired = requiredSum / float64(c.Positives)
	}
	if c.Negatives > 0 {
		c.MeanNotRequired = notRequiredSum / float64(c.Negatives)
	}
	if c.Excluded > 0 {
		c.MeanUseful = usefulSum / float64(c.Excluded)
	}
	c.Separation = c.MeanRequired - c.MeanNotRequired

	n := len(scored)
	if n == 0 {
		c.Caveat = "no candidate carries both a score and a REQUIRED/NOT_REQUIRED label"
		return c
	}
	c.BaseRate = float64(c.Positives) / float64(n)

	var brier float64
	for _, p := range scored {
		outcome := 0.0
		if p.Label == LabelRequired {
			outcome = 1
		}
		d := p.Score - outcome
		brier += d * d
	}
	c.Brier = brier / float64(n)
	// Always predicting the base rate is the reference a Brier score is read
	// against.
	c.BrierBaseline = c.BaseRate * (1 - c.BaseRate)
	if c.BrierBaseline > 0 {
		c.Skill = 1 - c.Brier/c.BrierBaseline
	}

	c.Bins = reliabilityBins(scored, bins)
	var ece float64
	for _, b := range c.Bins {
		ece += (float64(b.Count) / float64(n)) * math.Abs(b.MeanScore-b.Observed)
	}
	c.ECE = ece

	switch {
	case n < minCalibrationSample:
		c.Caveat = fmt.Sprintf("%d labelled candidate(s); below %d the bins hold a handful "+
			"each and ECE is noise", n, minCalibrationSample)
	case c.Positives == 0 || c.Negatives == 0:
		c.Caveat = "one class is absent; calibration is undefined and the Brier score " +
			"only measures how close the model stayed to a constant"
	case c.BaseRate < 0.05 || c.BaseRate > 0.95:
		c.Caveat = fmt.Sprintf("the base rate is %.0f%%; with prevalence this skewed a "+
			"model that always answers the majority scores well and means nothing",
			c.BaseRate*100)
	default:
		c.ECEInterpretable = true
	}
	return c
}

func reliabilityBins(points []CalibrationPoint, bins int) []ReliabilityBin {
	out := make([]ReliabilityBin, bins)
	width := 1.0 / float64(bins)
	for i := range out {
		out[i].Low = float64(i) * width
		out[i].High = float64(i+1) * width
	}
	for _, p := range points {
		idx := int(p.Score / width)
		if idx >= bins {
			idx = bins - 1
		}
		if idx < 0 {
			idx = 0
		}
		out[idx].Count++
		out[idx].MeanScore += p.Score
		if p.Label == LabelRequired {
			out[idx].Observed++
		}
	}
	for i := range out {
		if out[i].Count == 0 {
			continue
		}
		out[i].MeanScore /= float64(out[i].Count)
		out[i].Observed /= float64(out[i].Count)
	}
	return out
}

// CalibrationByOrigin splits the diagnostic by how retrieval found each
// candidate.
//
// This is the assumption the whole cross-origin ordering rests on. The
// argument for judged reranking is that one question produces one comparable
// signal; if lexical anchors and graph-expanded nodes turn out to be scored
// on visibly different scales, that argument is wrong in the specific way
// that matters, and the deterministic within-origin sort was right.
func CalibrationByOrigin(points []CalibrationPoint, bins int) []Calibration {
	byOrigin := map[string][]CalibrationPoint{}
	var origins []string
	for _, p := range points {
		origin := p.Origin
		if origin == "" {
			origin = "(unrecorded)"
		}
		if _, seen := byOrigin[origin]; !seen {
			origins = append(origins, origin)
		}
		byOrigin[origin] = append(byOrigin[origin], p)
	}
	sort.Strings(origins)

	out := []Calibration{Calibrate("all", points, bins)}
	for _, origin := range origins {
		out = append(out, Calibrate(origin, byOrigin[origin], bins))
	}
	return out
}

// Format renders a calibration table for a terminal.
func (c Calibration) Format() string {
	var b []byte
	appendf := func(format string, args ...any) { b = append(b, fmt.Sprintf(format, args...)...) }

	appendf("%s: %d REQUIRED, %d NOT_REQUIRED, %d USEFUL excluded\n",
		c.Population, c.Positives, c.Negatives, c.Excluded)
	if c.Caveat != "" {
		appendf("  %s\n", c.Caveat)
	}
	appendf("  base rate %.2f  Brier %.4f (baseline %.4f, skill %+.3f)\n",
		c.BaseRate, c.Brier, c.BrierBaseline, c.Skill)
	appendf("  mean score: REQUIRED %.3f, NOT_REQUIRED %.3f, separation %+.3f\n",
		c.MeanRequired, c.MeanNotRequired, c.Separation)
	if c.ECEInterpretable {
		appendf("  ECE %.4f\n", c.ECE)
	} else {
		appendf("  ECE %.4f (not interpretable here)\n", c.ECE)
	}
	appendf("  %-14s %6s %10s %10s\n", "band", "n", "predicted", "observed")
	for _, bin := range c.Bins {
		if bin.Count == 0 {
			continue
		}
		appendf("  %.1f–%.1f        %6d %10.3f %10.3f\n",
			bin.Low, bin.High, bin.Count, bin.MeanScore, bin.Observed)
	}
	return string(b)
}
