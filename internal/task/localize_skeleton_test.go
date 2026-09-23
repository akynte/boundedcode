package task

// Repository size must not be a reason a task cannot start.
//
// LOCALIZE used to refuse above four thousand paths and, separately, to build
// an evidence block larger than decide would accept. Both turned "this
// repository is big" into "this task is impossible", and both were found the
// same way: three imported SWE-bench tasks, none of which reached the phase
// that the work being measured lives in. Django (5,225 paths) hit the first;
// SymPy (1,816 paths plus lexical evidence) hit the second.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// syntheticTree builds n paths spread over directories, deterministically.
func syntheticTree(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		switch {
		case i%7 == 0:
			out = append(out, fmt.Sprintf("pkg%02d/mod%03d/file%04d.py", i%13, i%31, i))
		case i%3 == 0:
			out = append(out, fmt.Sprintf("pkg%02d/file%04d.py", i%13, i))
		default:
			out = append(out, fmt.Sprintf("top%04d.py", i))
		}
	}
	sort.Strings(out)
	return out
}

// A repository larger than the old cap must produce evidence, not an error.
func TestLargeRepositoryProducesBoundedStructureRatherThanFailing(t *testing.T) {
	paths := syntheticTree(6000)
	hits := []string{paths[4200], paths[4201]}

	sk := fitSkeleton(paths, hits, map[string]any{}, nil)

	if sk.Summary.Available != 6000 {
		t.Errorf("available = %d, want 6000", sk.Summary.Available)
	}
	if len(sk.Files) == 0 {
		t.Fatal("no structure survived; the phase would proceed blind")
	}
	if sk.Summary.Retained != len(sk.Files) {
		t.Errorf("retained %d disagrees with %d file(s) emitted", sk.Summary.Retained, len(sk.Files))
	}
	if sk.Summary.Retained+sk.Summary.Dropped != sk.Summary.Available {
		t.Errorf("retained %d + dropped %d != available %d",
			sk.Summary.Retained, sk.Summary.Dropped, sk.Summary.Available)
	}
	if sk.Summary.Dropped == 0 {
		t.Error("6,000 paths fit the budget untouched, so this test no longer exercises truncation")
	}
	// The shape of the repository survives the truncation of its contents.
	if len(sk.Summary.Directories) == 0 || sk.Summary.DirectoriesTotal == 0 {
		t.Error("the directory census is empty, so a truncated tree reads as a small one")
	}
	// The one signal about this objective is never what gets cut.
	kept := map[string]bool{}
	for _, f := range sk.Files {
		kept[f] = true
	}
	for _, h := range hits {
		if !kept[h] {
			t.Errorf("lexical hit %q was dropped before unrelated paths", h)
		}
	}
}

// Whatever the repository, the evidence block must fit what decide accepts.
// Failing that check is the bug; this asserts it cannot be reached.
func TestStructureEvidenceAlwaysFitsThePhaseBudget(t *testing.T) {
	for _, n := range []int{50, 1816, 5225, 20000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			paths := syntheticTree(n)
			// Lexical evidence occupies part of the same block, as it does in
			// the real phase, so the skeleton must fit around it.
			other := map[string]any{"lexical_hits": map[string]any{"files": paths[:min(30, n)]}}
			sk := fitSkeleton(paths, paths[:min(8, n)], other, nil)

			evidence := map[string]any{}
			for k, v := range other {
				evidence[k] = v
			}
			evidence["files"] = sk.Files
			evidence["structure"] = sk.Summary
			body, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			if len(body) > phaseEvidenceLimit {
				t.Fatalf("evidence is %d bytes against a %d-byte phase budget; decide would "+
					"refuse it and the task would stop", len(body), phaseEvidenceLimit)
			}
		})
	}
}

// A repository that already fit must be presented exactly as before, or this
// change alters every existing task as well as the large ones.
func TestSmallRepositoryIsUntouched(t *testing.T) {
	paths := syntheticTree(120)
	sk := fitSkeleton(paths, nil, map[string]any{}, nil)
	if sk.Summary.Dropped != 0 {
		t.Errorf("dropped %d path(s) from a 120-path repository", sk.Summary.Dropped)
	}
	if len(sk.Files) != len(paths) {
		t.Fatalf("emitted %d of %d paths", len(sk.Files), len(paths))
	}
	got := append([]string(nil), sk.Files...)
	sort.Strings(got)
	for i := range paths {
		if got[i] != paths[i] {
			t.Fatalf("the path set changed: %q is not %q", got[i], paths[i])
		}
	}
}

// The ranking must not consult anything that varies between runs.
func TestSkeletonRankingIsDeterministic(t *testing.T) {
	paths := syntheticTree(3000)
	hits := []string{paths[900], paths[12]}
	first := rankSkeleton(paths, hits)
	for range 5 {
		if got := rankSkeleton(paths, hits); strings.Join(got, "\n") != strings.Join(first, "\n") {
			t.Fatal("two rankings of one tree differ; the evidence is not reproducible")
		}
	}
	// Hits first, then their siblings, then the rest shallowest-first.
	if first[0] != hits[0] || first[1] != hits[1] {
		t.Errorf("the lexical hits are not at the head: %v", first[:2])
	}
	var lastDepth int
	seenTail := false
	for _, p := range first {
		d := strings.Count(p, "/")
		if !seenTail && d == 0 {
			seenTail, lastDepth = true, d
			continue
		}
		if seenTail {
			if d < lastDepth {
				t.Fatalf("depth went backwards in the tail at %q", p)
			}
			lastDepth = d
		}
	}
}
