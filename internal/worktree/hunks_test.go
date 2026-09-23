package worktree_test

import (
	"reflect"
	"testing"

	"github.com/akynte/boundedcode/internal/worktree"
)

const samplePatch = `diff --git a/a.go b/a.go
index 1111111..2222222 100644
--- a/a.go
+++ b/a.go
@@ -3,2 +3,3 @@ package a
-func Add(x, y int) int { return x - y }
+func Add(x, y int) int {
+	return x + y
+}
@@ -10,0 +12,2 @@ func Other() {}
+func New() {}
+
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,4 +0,0 @@
-package a
diff --git a/fresh.go b/fresh.go
new file mode 100644
--- /dev/null
+++ b/fresh.go
@@ -0,0 +1,3 @@
+package a
`

func TestRangesReadsBothSides(t *testing.T) {
	before, after := worktree.Ranges(samplePatch)
	wantBefore := map[string][]worktree.LineRange{
		"a.go":    {{Start: 3, End: 4}, {Start: 10, End: 10}},
		"gone.go": {{Start: 1, End: 4}},
	}
	wantAfter := map[string][]worktree.LineRange{
		"a.go":     {{Start: 3, End: 5}, {Start: 12, End: 13}},
		"fresh.go": {{Start: 1, End: 3}},
	}
	if !reflect.DeepEqual(before, wantBefore) {
		t.Errorf("before = %v, want %v", before, wantBefore)
	}
	if !reflect.DeepEqual(after, wantAfter) {
		t.Errorf("after = %v, want %v", after, wantAfter)
	}
}

func TestLineRangeOverlaps(t *testing.T) {
	r := worktree.LineRange{Start: 10, End: 12}
	for _, c := range []struct {
		start, end int
		want       bool
	}{{1, 9, false}, {1, 10, true}, {11, 11, true}, {12, 20, true}, {13, 20, false}} {
		if got := r.Overlaps(c.start, c.end); got != c.want {
			t.Errorf("Overlaps(%d, %d) = %v", c.start, c.end, got)
		}
	}
}
