package retrieval

import (
	"fmt"
	"reflect"
	"testing"
)

// TestSpreadByFileDoesNotLetOneFileFillTheResults pins the selection rule of
// the reverse hop. Before it, a widely used declaration returned its first
// consumers in node-id order, which on a real repository meant two test files
// took every slot and the other eleven files that used the symbol were never
// named.
func TestSpreadByFileDoesNotLetOneFileFillTheResults(t *testing.T) {
	var in []Slice
	for i := 0; i < 10; i++ {
		in = append(in, Slice{Path: "tests/big_test.go", Symbol: fmt.Sprintf("t%d", i)})
	}
	in = append(in,
		Slice{Path: "app/handler.go", Symbol: "Handle"},
		Slice{Path: "app/worker.go", Symbol: "Work"},
	)

	got := spreadByFile(in, 4)
	if len(got) != 4 {
		t.Fatalf("got %d slices, want 4", len(got))
	}
	files := map[string]int{}
	for _, s := range got {
		files[s.Path]++
	}
	if len(files) != 3 {
		t.Fatalf("selection kept %d distinct files, want all 3: %v", len(files), files)
	}
	if files["tests/big_test.go"] > 2 {
		t.Errorf("one file took %d of 4 slots: %v", files["tests/big_test.go"], files)
	}
	// Round-robin visits files in first-seen order, so the result is stable.
	if got[0].Path != "tests/big_test.go" || got[1].Path != "app/handler.go" || got[2].Path != "app/worker.go" {
		t.Errorf("selection is not the documented round-robin order: %v", paths(got))
	}

	// Under the limit nothing is reordered: the hop is advisory and must not
	// shuffle a result that already fits.
	small := in[:3]
	if kept := spreadByFile(small, 8); !reflect.DeepEqual(kept, small) {
		t.Errorf("a set that fits was reordered: %v", paths(kept))
	}
}

func paths(ss []Slice) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Path+"#"+s.Symbol)
	}
	return out
}
