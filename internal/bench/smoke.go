package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/worktree"
)

// SmokeEngine is a deterministic engine used only by the explicitly
// non-official smoke suite and harness self-tests. It is still passed through
// production task.Runner; it is not a model, and no official suite may select
// it.
type SmokeEngine struct {
	Path       string
	From       string
	To         string
	Tokens     int
	LastTaskID string
}

func NewSmokeEngine() *SmokeEngine {
	return &SmokeEngine{Path: "calculator.go", From: "return a + b + 1", To: "return a + b", Tokens: 1}
}

func (e *SmokeEngine) Name() string                 { return "benchmark-smoke-engine" }
func (e *SmokeEngine) Health(context.Context) error { return nil }
func (e *SmokeEngine) Close() error                 { return nil }
func (e *SmokeEngine) Edits() bool                  { return true }

func (e *SmokeEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	if err := ctx.Err(); err != nil {
		return &engine.Response{Summary: "smoke engine stopped: " + err.Error()}, err
	}
	path := req.Worktree
	if e.Path != "" {
		path = filepath.Join(req.Worktree, filepath.FromSlash(e.Path))
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is the task worktree supplied by production Runner
	if err != nil {
		return nil, err
	}
	updated := strings.Replace(string(body), e.From, e.To, 1)
	if updated == string(body) && e.From != e.To {
		return nil, fmt.Errorf("smoke engine: expected text %q was not found in %s", e.From, e.Path)
	}
	if err := worktree.WriteWithin(filepath.Dir(path), filepath.Base(path), []byte(updated)); err != nil {
		return nil, err
	}
	e.LastTaskID = req.TaskID
	_, _ = ledger.ContentManifestContext(ctx, req.Worktree) // keep the production candidate path check explicit
	return &engine.Response{Summary: "non-official deterministic smoke edit", ClaimsDone: true,
		Edited: true, TokensUsed: e.Tokens}, nil
}

// NewSmokeRawWorker returns the equally deterministic RAW fixture worker. The
// two arms are intentionally separate adapters so the smoke run exercises the
// scheduler, isolation, evaluator, result, and report paths without pretending
// to measure a model.
func NewSmokeRawWorker() Worker {
	return ScriptedWorker{ClaimSuccess: true, Apply: func(_ context.Context, req WorkerRequest) error {
		path := filepath.Join(req.Workspace, "calculator.go")
		body, err := os.ReadFile(path) //nolint:gosec // candidate workspace
		if err != nil {
			return err
		}
		updated := strings.Replace(string(body), "return a + b + 1", "return a + b", 1)
		return os.WriteFile(path, []byte(updated), 0o640) //nolint:gosec // smoke candidate
	}}
}
