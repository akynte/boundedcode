package ledger

// Report turns paired predictions into the reliability diagnostic
// internal/eval's own Calibrate already computes for retrieval relevance —
// mean squared error against the base rate (Brier skill), and a reliability
// table by probability band. This is a smaller, general form of the same
// math for a boolean outcome rather than a three-way relevance label,
// because a judgment site's calibration is a property of *that site*, not
// only of retrieval, and this package (where predictions are recorded)
// should not have to import internal/eval (where benchmark controls live)
// to report on them.
//
// Two partitions are not optional here, and both exist because pooling would
// produce a number that describes nothing:
//
//   - Shadow against operational. At the logged tier a prediction changes
//     nothing, so whatever happened next is an observation of a world the
//     prediction did not touch. Once a site is promoted its own effect can
//     move the outcome it is later scored against: a hunk tainted because it
//     was judged a weakening draws the attention that gets it rejected, and
//     the site is then scored as having predicted a rejection it helped
//     cause. Scoring those rows beside shadow rows is self-confirming, so
//     Calibrate reports them apart and the promotion decision reads Shadow.
//   - Version. A model version or a materially reworded question is a
//     different measurement. CalibrateGrouped partitions on both rather
//     than averaging them into a figure describing neither.

import "sort"

// ReliabilityBin is one probability band of the calibration table.
type ReliabilityBin struct {
	Low, High float64 `json:"-"`
	Count     int     `json:"count"`
	// MeanPredicted is what the site said; Observed is what the outcomes
	// show. The gap between them is the calibration error in that band.
	MeanPredicted float64 `json:"mean_predicted"`
	Observed      float64 `json:"observed"`
}

// Population is one scored subset of a site's paired rows.
type Population struct {
	// Name is "shadow" or "operational".
	Name string `json:"name"`
	// N is how many paired rows this population is built from.
	N int `json:"n"`
	// Positives is how many paired outcomes were true.
	Positives int     `json:"positives"`
	BaseRate  float64 `json:"base_rate"`
	// Brier is the mean squared error of the predicted probability against
	// the observed outcome — a proper score, unlike accuracy: it cannot be
	// gamed by a model that hedges, and it penalises confident mistakes
	// hardest. BrierBaseline is the Brier score of always predicting the
	// base rate; Skill is 1 - Brier/BrierBaseline, so positive means the
	// site beats guessing the base rate and negative means it does not.
	Brier         float64          `json:"brier"`
	BrierBaseline float64          `json:"brier_baseline"`
	Skill         float64          `json:"skill"`
	Bins          []ReliabilityBin `json:"bins,omitempty"`
	// Interpretable is false below MinCalibrationSample, and false for a
	// degenerate population; a Brier score or a reliability bin computed
	// from a handful of rows is not a finding, and neither is a skill figure
	// against a baseline that is already perfect.
	Interpretable bool `json:"interpretable"`
	// Degenerate says every outcome in this population was the same, which
	// is why Skill is not reported: it is undefined, not zero.
	Degenerate bool `json:"degenerate,omitempty"`
}

// Report is one site's calibration, from whatever paired rows exist so far.
type Report struct {
	Site string `json:"site"`
	// Pending is how many predictions for this site are still unresolved and
	// therefore in no population below.
	Pending int `json:"pending"`
	// Shadow holds rows whose prediction changed nothing — the evidence a
	// promotion decision should rest on. Operational holds rows whose own
	// effect could have moved the outcome they are scored against.
	Shadow      Population `json:"shadow"`
	Operational Population `json:"operational"`
	// Versions lists the (model, site version) combinations present, so a
	// reader can see when a report is pooling across a model change even
	// though the populations above do not partition on it. A report with
	// more than one entry here should be read through CalibrateGrouped.
	Versions []VersionCount `json:"versions,omitempty"`
}

// VersionCount is how many paired rows one (model, site version) produced.
type VersionCount struct {
	Model       string `json:"model"`
	SiteVersion string `json:"site_version"`
	N           int    `json:"n"`
}

// MinCalibrationSample is the smallest paired sample this reports a Skill
// figure on as meaningful. Below it, N and BaseRate are still reported —
// they are facts — but Skill and the bins are not, because a figure from
// four rows is not a calibration measurement.
const MinCalibrationSample = 20

// DefaultCalibrationBins is ten bands of width 0.1, matching eval.Calibrate's
// default so a report reads the same way wherever it comes from.
const DefaultCalibrationBins = 10

// Calibrate computes a site's calibration report from its paired rows,
// partitioned into shadow and operational populations.
func Calibrate(site string, pairs []Pair, pending int, bins int) Report {
	if bins <= 0 {
		bins = DefaultCalibrationBins
	}
	r := Report{Site: site, Pending: pending}

	var shadow, operational []Pair
	counts := map[VersionCount]int{}
	for _, p := range pairs {
		if p.Intervened {
			operational = append(operational, p)
		} else {
			shadow = append(shadow, p)
		}
		counts[VersionCount{Model: p.Model, SiteVersion: p.SiteVersion}]++
	}
	r.Shadow = scorePopulation("shadow", shadow, bins)
	r.Operational = scorePopulation("operational", operational, bins)

	for key, n := range counts {
		key.N = n
		r.Versions = append(r.Versions, key)
	}
	sort.Slice(r.Versions, func(i, j int) bool {
		if r.Versions[i].Model != r.Versions[j].Model {
			return r.Versions[i].Model < r.Versions[j].Model
		}
		return r.Versions[i].SiteVersion < r.Versions[j].SiteVersion
	})
	return r
}

// CalibrateGrouped reports one Report per (model, site version) present in
// pairs, so a reader never has to wonder whether a figure spans a model
// change. Pending is attributed to the whole site rather than split, because
// an unresolved prediction has no outcome to attribute.
func CalibrateGrouped(site string, pairs []Pair, pending int, bins int) []Report {
	groups := map[VersionCount][]Pair{}
	for _, p := range pairs {
		key := VersionCount{Model: p.Model, SiteVersion: p.SiteVersion}
		groups[key] = append(groups[key], p)
	}
	out := make([]Report, 0, len(groups))
	for key, rows := range groups {
		rep := Calibrate(site, rows, 0, bins)
		rep.Versions = []VersionCount{{Model: key.Model, SiteVersion: key.SiteVersion, N: len(rows)}}
		out = append(out, rep)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Versions[0], out[j].Versions[0]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.SiteVersion < b.SiteVersion
	})
	if len(out) > 0 {
		out[0].Pending = pending
	}
	return out
}

func scorePopulation(name string, pairs []Pair, bins int) Population {
	p := Population{Name: name, N: len(pairs)}
	if len(pairs) == 0 {
		return p
	}
	for _, row := range pairs {
		if row.Outcome {
			p.Positives++
		}
	}
	p.BaseRate = float64(p.Positives) / float64(len(pairs))
	// A population whose outcomes are all true or all false has no skill
	// figure to report: the baseline of always predicting the base rate is
	// already perfect, so the ratio Skill is computed from is undefined.
	// Reporting 0 there would read as "no better than the baseline" when
	// what is true is "this cannot be scored yet" — so it is marked
	// uninterpretable and the reader sees the count and the base rate, which
	// are the facts that exist.
	degenerate := p.Positives == 0 || p.Positives == len(pairs)
	p.Interpretable = len(pairs) >= MinCalibrationSample && !degenerate
	p.Degenerate = degenerate

	var sq, baselineSQ float64
	for _, row := range pairs {
		observed := 0.0
		if row.Outcome {
			observed = 1.0
		}
		d := row.Predicted - observed
		sq += d * d
		bd := p.BaseRate - observed
		baselineSQ += bd * bd
	}
	p.Brier = sq / float64(len(pairs))
	p.BrierBaseline = baselineSQ / float64(len(pairs))
	if p.BrierBaseline > 0 {
		p.Skill = 1 - p.Brier/p.BrierBaseline
	}
	if p.Interpretable {
		p.Bins = reliabilityBins(pairs, bins)
	}
	return p
}

func reliabilityBins(pairs []Pair, bins int) []ReliabilityBin {
	width := 1.0 / float64(bins)
	out := make([]ReliabilityBin, bins)
	for i := range out {
		out[i] = ReliabilityBin{Low: float64(i) * width, High: float64(i+1) * width}
	}
	sums := make([]float64, bins)
	positives := make([]int, bins)
	for _, p := range pairs {
		idx := int(p.Predicted / width)
		if idx >= bins {
			idx = bins - 1
		}
		if idx < 0 {
			idx = 0
		}
		out[idx].Count++
		sums[idx] += p.Predicted
		if p.Outcome {
			positives[idx]++
		}
	}
	for i := range out {
		if out[i].Count == 0 {
			continue
		}
		out[i].MeanPredicted = sums[i] / float64(out[i].Count)
		out[i].Observed = float64(positives[i]) / float64(out[i].Count)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Low < out[j].Low })
	return out
}
