package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// The dev/held-out sampling protocol, and the audit that checks it held.
//
// The bias this prevents is the one nobody admits to and everybody is
// vulnerable to: choosing the held-out set after seeing how the model did on
// it. A split decided by a documented rule, before any model has been run,
// cannot be adjusted afterwards without the adjustment being visible — the
// membership digest changes and the experiment identity with it.
//
// The second failure is subtler. Two tasks from the same subsystem, or two
// variations of one bug, are not independent: tuning on one is partly tuning
// on the other, and a held-out number that contains its dev twin is not
// held out. This code surfaces those pairs for a person; it never deletes
// one, because "these are the same problem" is a judgment about the work.

// SplitProtocolVersion identifies the rule below. It travels in the report,
// so a split made under a different rule cannot be quietly compared with one
// made under this.
const SplitProtocolVersion = "1.0"

// SplitProtocol is the documented rule, printable so an operator and a reader
// see the same words.
const SplitProtocol = `Dev / held-out assignment protocol ` + SplitProtocolVersion + `

When:
    Before any model is run against the set. A split adjusted after seeing
    results is not a split; it is a choice of result.

How:
    Assignment is deterministic from the task id and a seed recorded with the
    set: hash(seed, task id), lowest fraction to held-out. Nobody picks.

    The seed is chosen once and written down. Rerolling it after seeing
    numbers is the same mistake as hand-picking, and the recorded seed is
    what makes that visible.

Balance:
    Held-out should resemble dev on the facets that plausibly matter —
    category, subsystem, language. The assignment is checked for balance
    afterwards and reported; a badly skewed split is fixed by adding tasks,
    not by moving them.

Families:
    Tasks a person judges to be variations of one problem share a family and
    are assigned together, to the same side. A family split across dev and
    held-out leaks: tuning on one member tunes on the other.

    ` + "`bcode eval audit`" + ` surfaces likely families the metadata does not
    declare — same subsystem, similar objective, overlapping expected files.
    It proposes; it never reassigns.

Synthetic tasks:
    Dev only. A held-out number quoted from a task written for the benchmark
    describes the benchmark's author.

Chronological separation:
    Recommended when many tasks come from one evolving repository, and
    optional otherwise.

    A random split of an evolving codebase leaks in a way the family rule
    does not catch: a dev task from March and a held-out task from June may
    touch the same subsystem after it was refactored, and tuning against the
    earlier one is partly tuning against the later. Worse, the held-out set
    then measures a system tuned on the future of its own repository, which
    is not the situation anybody deploys into.

    Where it applies, assign by date — everything before a cutoff to dev,
    everything after to held-out — rather than by hash, and record the cutoff
    with the set. That also aligns the split with training-data leakage: a
    held-out set drawn entirely from after a model's cutoff is the only kind
    whose numbers are not partly a memory test.

    The cost is that the two sides stop being exchangeable: later work may be
    harder, or in a different part of the codebase, and a difference between
    the sets is then confounded with time. Balance is reported either way, and
    a chronological split needs it read more carefully rather than less.

Freezing:
    Once held-out evaluation begins, membership and labels are frozen. The
    membership digest in every report is what makes a later change visible.`

// AssignSet is the deterministic assignment rule.
//
// It is a pure function of the task id and the seed, so the split can be
// recomputed by anybody and cannot be nudged. heldoutFraction is the share
// intended for held-out.
func AssignSet(seed int64, taskID string, heldoutFraction float64) Set {
	if heldoutFraction <= 0 {
		return SetDev
	}
	if heldoutFraction >= 1 {
		return SetHeldout
	}
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(seed)) //nolint:gosec // a seed, not a size
	h.Write(buf[:])
	fmt.Fprintf(h, "%d:%s", len(taskID), taskID)
	sum := h.Sum(nil)
	// The top 32 bits as a fraction of the range: stable across platforms
	// and independent of how many tasks exist, so adding a task does not
	// reassign the others.
	v := float64(binary.BigEndian.Uint32(sum[:4])) / float64(1<<32)
	if v < heldoutFraction {
		return SetHeldout
	}
	return SetDev
}

// SplitAudit is what the audit found.
type SplitAudit struct {
	ProtocolVersion string `json:"protocol_version"`
	DevCount        int    `json:"dev_count"`
	HeldoutCount    int    `json:"heldout_count"`
	// Balance compares the two sides on each facet. A facet where held-out
	// looks nothing like dev is a reason to add tasks, not to move them.
	Balance []FacetBalance `json:"balance,omitempty"`
	// CrossSetPairs are task pairs that look related and sit on opposite
	// sides of the split. Surfaced for review, never acted on.
	CrossSetPairs []RelatedPair `json:"cross_set_pairs,omitempty"`
	// SplitFamilies are declared families with members on both sides. Unlike
	// the pairs above, these are not a guess: somebody said they were the
	// same problem.
	SplitFamilies []string `json:"split_families,omitempty"`
	// SyntheticHeldout names synthetic tasks that landed in held-out.
	SyntheticHeldout []string `json:"synthetic_in_heldout,omitempty"`
	// Undeclared counts tasks missing the metadata the protocol balances on.
	Undeclared []string `json:"missing_traits,omitempty"`
}

// FacetBalance compares one facet across the split.
type FacetBalance struct {
	Facet   string         `json:"facet"`
	Dev     map[string]int `json:"dev"`
	Heldout map[string]int `json:"heldout"`
	// Skewed marks a value present on one side and absent on the other,
	// which is where a held-out number stops describing the same population.
	Skewed []string `json:"skewed,omitempty"`
}

// RelatedPair is two tasks that may be the same problem.
type RelatedPair struct {
	// A is the dev task, B the held-out one.
	A      string   `json:"a"`
	B      string   `json:"b"`
	Score  float64  `json:"similarity"`
	Shared []string `json:"shared"`
}

// AuditSplit reports what a person should look at before freezing a split.
func AuditSplit(tasks []Task) SplitAudit {
	a := SplitAudit{ProtocolVersion: SplitProtocolVersion}
	byFamily := map[string]map[Set]int{}

	for _, t := range tasks {
		switch t.Membership() {
		case SetHeldout:
			a.HeldoutCount++
			if t.Origin.Synthetic {
				a.SyntheticHeldout = append(a.SyntheticHeldout, t.ID)
			}
		default:
			a.DevCount++
		}
		if t.Traits.Empty() {
			a.Undeclared = append(a.Undeclared, t.ID)
		}
		if f := t.Traits.Family; f != "" {
			if byFamily[f] == nil {
				byFamily[f] = map[Set]int{}
			}
			byFamily[f][t.Membership()]++
		}
	}

	for family, sides := range byFamily {
		if sides[SetDev] > 0 && sides[SetHeldout] > 0 {
			a.SplitFamilies = append(a.SplitFamilies, fmt.Sprintf(
				"%s (dev=%d, heldout=%d)", family, sides[SetDev], sides[SetHeldout]))
		}
	}
	sort.Strings(a.SplitFamilies)
	sort.Strings(a.SyntheticHeldout)
	sort.Strings(a.Undeclared)

	a.Balance = []FacetBalance{
		facetBalance("category", tasks, func(t Task) string { return string(t.Category) }),
		facetBalance("subsystem", tasks, func(t Task) string { return t.Traits.Subsystem }),
		facetBalance("language", tasks, func(t Task) string { return t.Traits.Language }),
	}
	a.CrossSetPairs = crossSetPairs(tasks)
	return a
}

func facetBalance(name string, tasks []Task, value func(Task) string) FacetBalance {
	b := FacetBalance{Facet: name, Dev: map[string]int{}, Heldout: map[string]int{}}
	for _, t := range tasks {
		v := value(t)
		if v == "" {
			v = "(undeclared)"
		}
		if t.Membership() == SetHeldout {
			b.Heldout[v]++
			continue
		}
		b.Dev[v]++
	}
	for v := range b.Heldout {
		if b.Dev[v] == 0 {
			b.Skewed = append(b.Skewed, v+" only in heldout")
		}
	}
	for v := range b.Dev {
		if len(b.Heldout) > 0 && b.Heldout[v] == 0 {
			b.Skewed = append(b.Skewed, v+" only in dev")
		}
	}
	sort.Strings(b.Skewed)
	return b
}

// crossSetPairs finds tasks on opposite sides that look like the same
// problem.
//
// The signal is deliberately crude and deliberately generous: shared
// subsystem, overlapping objective vocabulary, overlapping expected files.
// A false positive costs a person ten seconds; a missed pair costs the
// held-out set its independence.
func crossSetPairs(tasks []Task) []RelatedPair {
	var dev, held []Task
	for _, t := range tasks {
		if t.Membership() == SetHeldout {
			held = append(held, t)
			continue
		}
		dev = append(dev, t)
	}

	var out []RelatedPair
	for _, d := range dev {
		for _, h := range held {
			score, shared := relatedness(d, h)
			if score < relatednessFloor {
				continue
			}
			out = append(out, RelatedPair{A: d.ID, B: h.ID, Score: score, Shared: shared})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].A+out[i].B < out[j].A+out[j].B
	})
	return out
}

// relatednessFloor is low on purpose: this list is read by a person, and the
// cost of an extra row is nothing next to the cost of a pair nobody saw.
const relatednessFloor = 0.25

func relatedness(a, b Task) (float64, []string) {
	var score float64
	var shared []string

	if a.Traits.Subsystem != "" && a.Traits.Subsystem == b.Traits.Subsystem {
		score += 0.4
		shared = append(shared, "subsystem "+a.Traits.Subsystem)
	}
	if a.Category == b.Category {
		score += 0.1
		shared = append(shared, "category "+string(a.Category))
	}
	if a.Origin.Repository != "" && a.Origin.Repository == b.Origin.Repository {
		score += 0.1
		shared = append(shared, "repository "+a.Origin.Repository)
	}
	// Overlapping ground truth is the strongest signal there is: two tasks
	// whose fixes touch the same files are two views of one area.
	if overlap := stringOverlap(a.Expected.Files, b.Expected.Files); overlap > 0 {
		score += 0.4 * overlap
		shared = append(shared, fmt.Sprintf("%.0f%% of expected files", overlap*100))
	}
	if overlap := vocabularyOverlap(a.Objective, b.Objective); overlap > 0.3 {
		score += 0.3 * overlap
		shared = append(shared, fmt.Sprintf("%.0f%% objective vocabulary", overlap*100))
	}
	if score > 1 {
		score = 1
	}
	return score, shared
}

func stringOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	var hits int
	for _, s := range b {
		if set[s] {
			hits++
		}
	}
	smaller := min(len(a), len(b))
	return float64(hits) / float64(smaller)
}

// vocabularyOverlap is Jaccard over content words. Stop words are dropped
// because every objective in a set shares "the" and "a", and a measure that
// counted those would rate every pair as related.
func vocabularyOverlap(a, b string) float64 {
	wa, wb := contentWords(a), contentWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	var shared int
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	union := len(wa) + len(wb) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"to": true, "of": true, "in": true, "on": true, "for": true, "with": true,
	"that": true, "this": true, "it": true, "we": true, "should": true, "when": true,
	"not": true, "no": true, "so": true, "as": true, "by": true, "at": true,
	"make": true, "need": true, "needs": true, "our": true, "can": true, "has": true,
}

func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(word) < 3 || stopWords[word] {
			continue
		}
		out[word] = true
	}
	return out
}
