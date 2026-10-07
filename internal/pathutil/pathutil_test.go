package pathutil

import (
	"path/filepath"
	"testing"
)

func TestWithinAndEqual(t *testing.T) {
	defer func(f bool) { FoldsCase = f }(FoldsCase)
	j := filepath.Join
	for _, fold := range []bool{false, true} {
		FoldsCase = fold
		if !Within(j("/a", "b", "c"), j("/a", "b")) || Within(j("/a", "bc"), j("/a", "b")) || !Within("/a", "/a") {
			t.Errorf("fold=%v: Within", fold)
		}
		if got := Within(j("/A", "B", "c"), j("/a", "b")); got != fold {
			t.Errorf("fold=%v: case-folded Within = %v", fold, got)
		}
		if got := Equal("/Users/Me", "/users/me"); got != fold {
			t.Errorf("fold=%v: Equal = %v", fold, got)
		}
	}
}
