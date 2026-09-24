package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Observed failure: "session: recording workspace identity: rename
// .../workspace.json.tmp .../workspace.json: no such file or directory".
//
// session.Open calls RecordWorkspace on every MCP tool call, and an agent
// dispatching several tool calls concurrently against the same workspace is
// the ordinary case, not an edge case (one user request literally said "in
// parallel inspect the current state of the changed files and the key
// modules"). RecordWorkspace used to write a single fixed temp file name
// (path+".tmp") shared by every caller: two concurrent writers both wrote it,
// the first to reach os.Rename moved the file out from under the second, and
// the second's own os.Rename then failed with ENOENT — not because anything
// was wrong, but because its temp file no longer existed under that name.
//
// This drives many concurrent writers at the same workspace and requires
// every one to succeed; run with -race to also catch a data race on the file
// itself, not just the logical rename failure.
func TestRecordWorkspaceIsSafeUnderConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.CloseAll() })

	ws, err := workspace.Init(dir, workspace.InitOptions{Name: "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := root.OpenWorkspace(ctx, ws.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	const writers = 32
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			errs <- st.RecordWorkspace(ws)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent RecordWorkspace call failed: %v", err)
		}
	}
}
