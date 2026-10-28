package retrieve

import (
	"path"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
)

// Demotions by intent: for a fix or a change, a test file is evidence but
// rarely the place to change, and prose rarely the place to look.
const (
	testFactor = 0.6
	docsFactor = 0.5
	minScore   = 0.05
	// exampleFactor: examples and benchmarks use the project's code; the
	// change is rarely there.
	exampleFactor = 0.7
	// relativeCut drops items scoring below this share of the best.
	relativeCut = 0.3
	// maxRescued: places kept below the cut for names the prose mentions.
	maxRescued = 3
)

// rank combines each file's reasons into a score and orders the files.
// Reasons combine as independent evidence (1 - Π(1 - w)), each anchor
// counted once per file with its strongest weight, so agreement between
// strategies raises a file and repetition of one does not.
func (c *collector) rank(sig Signals, opt Options) []Item {
	var items []Item
	for _, f := range sortedKeys(c.files) {
		a := c.files[f]
		best := map[string]Reason{}
		for _, w := range a.why {
			k := string(w.Strategy) + "\x00" + w.Anchor
			if b, ok := best[k]; !ok || w.Weight > b.Weight {
				best[k] = w
			}
		}
		miss := 1.0
		var why []Reason
		for _, k := range sortedKeys(best) {
			w := best[k]
			miss *= 1 - min(w.Weight, 0.99)
			why = append(why, w)
		}
		sort.SliceStable(why, func(i, j int) bool { return why[i].Weight > why[j].Weight })
		score := 1 - miss
		test := isTestPath(f) || isTestData(f)
		if test && sig.Intent != IntentTest {
			score *= testFactor
		}
		if isExample(f) {
			score *= exampleFactor
		}
		if l := c.r.langOf[f]; l != nil && l.Kind == lang.KindMarkup && sig.Intent != IntentExplain && sig.Intent != IntentConfig {
			score *= docsFactor
		}
		if score < minScore {
			continue
		}
		items = append(items, Item{File: f, Line: a.line, Symbol: a.symbol, Score: score, Why: why, Test: test})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].File < items[j].File
	})
	// Far weaker than the best is noise next to it, except the best place
	// of each name the request's prose mentions: a request that names two
	// things (a Go field and a TypeScript function) needs both.
	cut := len(items)
	for i, it := range items {
		if it.Score < relativeCut*items[0].Score {
			cut = i
			break
		}
	}
	covered := map[string]bool{}
	for _, it := range items[:min(cut, opt.Limit)] {
		for _, w := range it.Why {
			if w.Named != "" {
				covered[w.Named] = true
			}
		}
	}
	var rescued []Item
	for _, it := range items[cut:] {
		if len(rescued) == maxRescued || it.Test {
			continue
		}
		for _, w := range it.Why {
			if w.Named != "" && !covered[w.Named] {
				covered[w.Named] = true
				rescued = append(rescued, it)
				break
			}
		}
	}
	items = items[:min(cut, max(opt.Limit-len(rescued), 0))]
	return append(items, rescued...)
}

// isTestData: fixtures and test data, which look like the project's code
// but are not it.
func isTestData(rel string) bool {
	for _, d := range strings.Split(path.Dir(rel), "/") {
		switch d {
		case "testdata", "fixtures", "__fixtures__", "test-fixtures", "__snapshots__", "__mocks__":
			return true
		}
	}
	return false
}

// isExample: example, demo and benchmark code.
func isExample(rel string) bool {
	for _, d := range strings.Split(path.Dir(rel), "/") {
		switch d {
		case "examples", "example", "samples", "demo", "demos", "benches", "benchmarks":
			return true
		}
	}
	return false
}
