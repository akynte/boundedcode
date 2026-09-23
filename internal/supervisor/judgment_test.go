package supervisor

import (
	"testing"

	"github.com/akynte/boundedcode/internal/store"
)

func openRoot(t *testing.T) *store.Root {
	t.Helper()
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	return root
}

// The shipped state. A workspace with no judgment.yaml must build a judge that
// answers nothing, without complaining about it: judgments are an addition,
// and an installation that never enables them should never hear about them.
func TestNoConfigurationYieldsAJudgeThatAnswersNothing(t *testing.T) {
	j, err := Judge(openRoot(t), nil, nil)
	if err != nil {
		t.Fatalf("an absent judgment.yaml was an error: %v", err)
	}
	if j == nil || j.Available() {
		t.Fatalf("judge = %v; want an unavailable one", j)
	}
}
