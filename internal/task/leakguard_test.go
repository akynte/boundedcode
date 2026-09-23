package task_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/attest"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

// recordingProvider counts the requests that actually reached it.
type recordingProvider struct {
	llm.Provider
	sent int
}

func (p *recordingProvider) Name() string { return "recording" }
func (p *recordingProvider) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	p.sent++
	return &llm.ChatResponse{Content: "ok"}, nil
}

// askingEngine sends the task objective to its provider, the way a real
// engine puts the objective in its prompt. It exposes WrapProvider so the
// runner can guard it.
type askingEngine struct {
	provider llm.Provider
}

func (e *askingEngine) Name() string                 { return "asking-test-engine" }
func (e *askingEngine) Health(context.Context) error { return nil }
func (e *askingEngine) Close() error                 { return nil }
func (e *askingEngine) WrapProvider(wrap func(llm.Provider) llm.Provider) {
	e.provider = wrap(e.provider)
}
func (e *askingEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	if _, err := e.provider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: req.Objective}},
	}); err != nil {
		return nil, err
	}
	return &engine.Response{Summary: "asked", ClaimsDone: true}, nil
}

func canarySuite(t *testing.T, canary string) *oracle.Suite {
	t.Helper()
	dir := t.TempDir()
	body := "checks:\n  - id: guarded\n    argv: [\"true\"]\n    canaries: [\"" + canary + "\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "c.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, err := oracle.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return suite
}

// Hidden content that reaches a prompt by any route — here the operator
// pasted part of a hidden test into the task's title — is stopped at the
// provider, before the request leaves, and the task is blocked.
func TestLeakGuardStopsHiddenContentBeforeItReachesAModel(t *testing.T) {
	requireGo(t)
	const canary = "bc-canary-5d0e1f77aa"
	repo := hiddenRepo(t)
	provider := &recordingProvider{}
	r, st := newRunner(t, &askingEngine{provider: provider})
	r.Oracle = canarySuite(t, canary)

	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "make Add pass the check marked " + canary, Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := r.Run(context.Background(), id, repo)
	if !errors.Is(err, task.ErrOracleLeak) {
		t.Fatalf("want ErrOracleLeak, got %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Error("the error must not repeat the canary")
	}
	if provider.sent != 0 {
		t.Errorf("%d request(s) carrying the canary reached the model", provider.sent)
	}
	got, err := task.NewStore(st).Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != task.StateBlocked {
		t.Errorf("state = %s, want blocked", got.State)
	}
}

func TestLeakGuardLetsOrdinaryRequestsThrough(t *testing.T) {
	requireGo(t)
	provider := &recordingProvider{}
	r, st := newRunner(t, &askingEngine{provider: provider})
	r.Oracle = canarySuite(t, "bc-canary-5d0e1f77aa")
	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "an ordinary objective", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), id, hiddenRepo(t)); err != nil {
		t.Fatal(err)
	}
	if provider.sent != 1 {
		t.Errorf("the ordinary request was not delivered (sent=%d)", provider.sent)
	}
}

// varyingEngine writes a different wrong Add on every attempt, so every
// attempt is a new candidate the hidden check rejects.
type varyingEngine struct{ n int }

func (e *varyingEngine) Name() string                 { return "varying-test-engine" }
func (e *varyingEngine) Health(context.Context) error { return nil }
func (e *varyingEngine) Close() error                 { return nil }
func (e *varyingEngine) Step(_ context.Context, req engine.Request) (*engine.Response, error) {
	e.n++
	body := "package a\n\nfunc Add(x, y int) int { return x*y + " + strings.Repeat("0+", e.n) + "0 }\n"
	if err := os.WriteFile(filepath.Join(req.Worktree, "a.go"), []byte(body), 0o644); err != nil {
		return nil, err
	}
	return &engine.Response{Summary: "tried again", ClaimsDone: true}, nil
}

// The model learns which hidden checks failed on each rejected candidate.
// After the budget, the next rejection ends the task instead of being
// reported, so the suite cannot be searched one attempt at a time.
func TestHiddenFeedbackBudgetStopsTheSearch(t *testing.T) {
	requireGo(t)
	eng := &varyingEngine{}
	r, st := newRunner(t, eng)
	r.Oracle = hiddenSuite(t, "")
	r.HiddenFeedbackRounds = 2
	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "make Add add", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 6, MaxWallTime: 10 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), id, hiddenRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted {
		t.Fatal("no attempt was correct")
	}
	if out.Attempts != 3 {
		t.Errorf("attempts = %d, want 3: two reported rejections, then the third ends the task", out.Attempts)
	}
	if !containsSubstr(out.Reasons, "feedback budget of 2") {
		t.Errorf("the reason must name the budget: %v", out.Reasons)
	}
	if out.Task.State != task.StateFailed {
		t.Errorf("state = %s, want failed", out.Task.State)
	}
}

// Every verification run is a signed record in the evidence chain, and the
// task's commit carries the chain head it was judged by.
func TestVerificationIsRecordedInTheSignedChain(t *testing.T) {
	requireGo(t)
	repo := hiddenRepo(t)
	r, st := newRunner(t, &writingEngine{path: "a.go", body: fixedAdd})
	r.Oracle = hiddenSuite(t, "")
	key, err := attest.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.Ledger.SetSigner(key)

	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "make Add add", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Fatalf("reasons: %v", out.Reasons)
	}

	l := ledger.New(st)
	records, err := l.Chain(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want one per verification run", len(records))
	}
	var v task.VerificationRecord
	if err := json.Unmarshal(records[0].Payload, &v); err != nil {
		t.Fatal(err)
	}
	if v.Predicate != task.VerificationPredicate || len(v.PatchSHA256) != 64 || v.Base == "" ||
		v.OracleDigest != r.Oracle.Digest || len(v.HiddenChecks) != 1 {
		t.Errorf("the record does not say what was verified: %+v", v)
	}
	rep, err := l.VerifyChain(context.Background(), map[string]ed25519.PublicKey{key.ID(): key.Public()})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Intact() || len(rep.Issues) != 0 {
		t.Errorf("chain report: %+v", rep)
	}
	if out.EvidenceHead != records[0].Hash {
		t.Errorf("EvidenceHead = %q, want %q", out.EvidenceHead, records[0].Hash)
	}
	msg, err := exec.Command("git", "-C", repo, "log", "-1", "--format=%(trailers:key=Evidence-Head,valueonly)", out.Branch).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(msg)) != out.EvidenceHead {
		t.Errorf("the commit's Evidence-Head trailer is %q, want %q", strings.TrimSpace(string(msg)), out.EvidenceHead)
	}
}
