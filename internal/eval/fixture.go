package eval

import (
	"context"
	"fmt"
	"os"

	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// IndexFixture indexes one task's fixture into a throwaway workspace and
// returns its retrieval candidates.
//
// It indexes the fixture rather than the operator's own repository because a
// diagnostic must score the candidates a run would actually have: retrieving
// from a different tree would measure something the task never saw.
//
// It lives here rather than in cmd/bcode so the §2.3 exemption covering
// ephemeral evaluation workspaces stays on one package — internal/eval
// already creates and removes exactly these directories for every run.
func IndexFixture(ctx context.Context, root *store.Root, t Task,
	analyzers []index.Analyzer) ([]retrieval.Slice, func(), error) {

	if root == nil {
		return nil, nil, fmt.Errorf("eval: no store root to index the fixture into")
	}
	id := workspace.DeriveID(t.FixturePath(), "", "diagnostic-"+t.ID)
	st, err := root.OpenWorkspace(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	dir := st.Dir()
	// Cleanup must not inherit a cancelled context: Store.Close builds its
	// own bounded one so a WAL checkpoint is never skipped because the
	// caller went away.
	release := func() { //nolint:contextcheck // cleanup, as in indexCopy
		_ = st.Close()
		_ = os.RemoveAll(dir)
	}

	ix := index.New(st, index.Options{Analyzers: analyzers})
	repo := workspace.Repository{
		ID: workspace.DeriveRepositoryID(id, ".", ""), Name: t.ID,
		Path: ".", DefaultBranch: "main",
	}
	if err := ix.RegisterRepository(ctx, repo); err != nil {
		release()
		return nil, nil, err
	}
	stats, err := ix.Repository(ctx, repo.ID, t.FixturePath())
	if err != nil {
		release()
		return nil, nil, err
	}
	if stats.Files == 0 {
		release()
		return nil, nil, fmt.Errorf("eval: the fixture for %s indexed to nothing", t.ID)
	}

	pkt, err := retrieval.New(st).Build(ctx, retrieval.Request{
		Query: t.Objective, TokenBudget: 20000,
	})
	if err != nil {
		release()
		return nil, nil, err
	}
	return pkt.Slices, release, nil
}
