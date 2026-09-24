package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
	"time"
)

func testTaskAndSuite(t *testing.T) (Task, Suite) {
	t.Helper()
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	oracle := filepath.Join(root, "oracle")
	writeTestFile(t, filepath.Join(fixture, "go.mod"), "module example.com/bench\n\ngo 1.26\n")
	writeTestFile(t, filepath.Join(fixture, "calculator.go"), "package bench\n\nfunc Add(a, b int) int { return a + b + 1 }\n")
	hidden := "package bench\n\nimport \"testing\"\n\nfunc TestHidden(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"wrong\") } }\n"
	writeTestFile(t, filepath.Join(oracle, "hidden_test.go"), hidden)
	task := Task{SchemaVersion: TaskSchemaVersion, ID: "test-001", Title: "fix add",
		Description: "Make Add return the sum.", Repository: "fixture", Fixture: "fixture", BaseCommit: "fixture",
		Evaluator: Evaluator{Oracle: "oracle", Files: []string{"hidden_test.go"}, Command: Command{Argv: []string{"go", "test", "./..."}}, TimeoutSeconds: 20, MustNotChange: []string{"hidden_test.go"}},
		Limits:    Limits{WallClockSeconds: 5, MaxGenerationRequests: 2, MaxVerificationAttempts: 1}, NetworkPolicy: "none", MutableScope: []string{"calculator.go"}, Verification: "standard"}
	task.path = filepath.Join(root, "task.yaml")
	task.oracleRoot = oracle
	task.hiddenFiles = map[string][]byte{"hidden_test.go": []byte(hidden)}
	task.hiddenHash = hiddenDigest(task.hiddenFiles)
	suite := Suite{SchemaVersion: SuiteSchemaVersion, ID: "test-suite", Version: "1", Tasks: []Task{task},
		Model: ModelConfig{Provider: "test-provider", Model: "test-model", Runtime: "test-runtime", ContextTokens: 4096}}
	suite.path = filepath.Join(root, "suite.yaml")
	suite.hash = suiteHash(suite)
	return task, suite
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
}

func patchCalculator(claim bool) ScriptedWorker {
	return ScriptedWorker{ClaimSuccess: claim, Apply: func(_ context.Context, req WorkerRequest) error {
		body, err := os.ReadFile(filepath.Join(req.Workspace, "calculator.go"))
		if err != nil {
			return err
		}
		body = []byte(strings.Replace(string(body), "a + b + 1", "a + b", 1))
		return os.WriteFile(filepath.Join(req.Workspace, "calculator.go"), body, 0o640)
	}}
}

type fakeBounded struct {
	worker Worker
	seen   *[]string
	mu     *sync.Mutex
}

func (f fakeBounded) Execute(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if f.seen != nil {
		f.mu.Lock()
		*f.seen = append(*f.seen, req.RunID+":"+req.Workspace)
		f.mu.Unlock()
	}
	return f.worker.Run(ctx, req)
}

func testRunner(t *testing.T, suite Suite, raw, bounded Worker) *Runner {
	t.Helper()
	return &Runner{Suite: suite, OutputRoot: t.TempDir(),
		Raw: RawAdapter{Worker: raw}, Bounded: BoundedAdapter{Factory: func(context.Context, WorkerRequest) (BoundedExecutor, error) {
			return fakeBounded{worker: bounded}, nil
		}}, Evaluator: IndependentEvaluator{Runner: &recordingSandbox{}}, Environment: EnvironmentSnapshot{OS: "test", Architecture: "test", CPUCount: 1}}
}

func runOne(t *testing.T, runner *Runner, spec RunSpec) Result {
	t.Helper()
	result, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run %s: %v", spec.RunID, err)
	}
	return result
}

func TestSetupBecomesTheCommonCommittedBaseline(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	task.Setup = Command{Argv: []string{"sh", "-c", "printf generated > generated.txt"}}
	candidate, err := (Materializer{AllowUnconfinedSetup: true}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "candidate"))
	if err != nil {
		t.Fatal(err)
	}
	if candidate.SetupManifest == "" || candidate.StartingManifest != candidate.SetupManifest {
		t.Fatalf("setup state was not made the common baseline: %+v", candidate)
	}
	if err := VerifyBaseRevision(context.Background(), candidate.Path, candidate.BaseCommit, candidate.BaseTree, true); err != nil {
		t.Fatalf("committed setup baseline is not the raw base: %v", err)
	}
	if _, err := os.Stat(filepath.Join(candidate.Path, "generated.txt")); err != nil {
		t.Fatalf("setup output is missing: %v", err)
	}
	if dirty, err := gitDirty(context.Background(), 10*time.Second, candidate.Path); err != nil || dirty {
		t.Fatalf("setup baseline is not clean: dirty=%t err=%v", dirty, err)
	}
}

func TestSetupReceivesSuiteAndTaskEnvironment(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	task.Setup = Command{Argv: []string{"sh", "-c", "printf '%s:%s' \"$SUITE_VALUE\" \"$TASK_VALUE\" > generated.txt"}}
	task.Environment = map[string]string{"TASK_VALUE": "task"}
	candidate, err := (Materializer{SetupEnvironment: map[string]string{"SUITE_VALUE": "suite"}, AllowUnconfinedSetup: true}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "candidate"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(candidate.Path, "generated.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "suite:task" {
		t.Fatalf("setup environment = %q, want suite:task", body)
	}
}

func TestAuxiliaryOracleCollisionDoesNotExposeOracleBytes(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	task.oracleFiles = map[string][]byte{"helper.txt": []byte("evaluator-only")}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "helper.txt"), "ordinary source")
	if err := AssertNoOracle(task, root); err != nil {
		t.Fatalf("different source bytes at an oracle path were treated as a leak: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "helper.txt"), "evaluator-only")
	if err := AssertNoOracle(task, root); err == nil {
		t.Fatal("oracle bytes copied into the worker workspace were accepted")
	}
}

func TestMaterializerRejectsFixtureGitSymlink(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(task.ResolvedRepository(), ".git")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := (Materializer{}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "candidate")); err == nil {
		t.Fatal("materializer accepted a fixture .git symlink")
	}
}

func TestFrozenFixtureDigestRejectsChangedSourceBytes(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	task.FixtureHash = strings.Repeat("0", 64)
	if _, err := (Materializer{}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "candidate")); err == nil {
		t.Fatal("materializer accepted fixture bytes that differed from the frozen digest")
	}
}

func TestMaterializerPinsGitBaseAndRejectsADifferentRevision(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), "package main\n")
	if err := runGit(context.Background(), 10*time.Second, source, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	_ = runGit(context.Background(), 10*time.Second, source, "config", "user.name", "test")
	_ = runGit(context.Background(), 10*time.Second, source, "config", "user.email", "test@example")
	_ = runGit(context.Background(), 10*time.Second, source, "config", "commit.gpgsign", "false")
	_ = runGit(context.Background(), 10*time.Second, source, "add", "-A")
	if err := runGitEnv(context.Background(), 10*time.Second, source, []string{"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}, "commit", "-q", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	base, err := gitOutput(context.Background(), 10*time.Second, source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := testTaskAndSuite(t)
	task.Fixture = ""
	task.Repository = source
	task.BaseCommit = base
	task.path = filepath.Join(source, "task.yaml")
	candidate, err := (Materializer{}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "candidate"))
	if err != nil {
		t.Fatal(err)
	}
	if candidate.BaseCommit != base || candidate.StartingDirty {
		t.Fatalf("materialized base was not pinned: %+v", candidate)
	}
	task.BaseCommit = "0000000000000000000000000000000000000000"
	if _, err := (Materializer{}).Materialize(context.Background(), task, filepath.Join(t.TempDir(), "wrong")); err == nil {
		t.Fatal("a different configured base revision was accepted")
	}
}

func TestRunnerRejectsOutputInsideFixture(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	runner := testRunner(t, suite, patchCalculator(true), patchCalculator(true))
	runner.OutputRoot = filepath.Join(suite.Tasks[0].ResolvedRepository(), "results")
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	if _, err := runner.Run(context.Background(), schedule.Entries[0]); err == nil {
		t.Fatal("runner wrote benchmark artifacts inside the fixture")
	}
}

func TestSameBaseStateAndIndependentEvaluation(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	runner := testRunner(t, suite, patchCalculator(true), patchCalculator(true))
	schedule, err := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 9, nil)
	if err != nil {
		t.Fatal(err)
	}
	results, err := runner.RunSchedule(context.Background(), schedule, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].Repository.StartingCandidate != results[1].Repository.StartingCandidate || results[0].Repository.BaseCommit != results[1].Repository.BaseCommit {
		t.Fatalf("modes did not start at the same identity: %+v / %+v", results[0].Repository, results[1].Repository)
	}
	for _, result := range results {
		if result.Evaluator.Status != EvaluatorPass || !result.Derived.IndependentVerifiedSuccess {
			t.Fatalf("%s was not independently verified: %+v", result.Mode, result)
		}
	}
}

func TestFreshWorkspaceAndModeIsolation(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	var calls int
	var mu sync.Mutex
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(_ context.Context, req WorkerRequest) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if _, err := os.Stat(filepath.Join(req.Workspace, "from-another-run")); err == nil {
			return errors.New("previous run marker leaked")
		}
		if first {
			return os.WriteFile(filepath.Join(req.Workspace, "from-another-run"), []byte("x"), 0o640)
		}
		body, _ := os.ReadFile(filepath.Join(req.Workspace, "calculator.go"))
		return os.WriteFile(filepath.Join(req.Workspace, "calculator.go"), []byte(strings.Replace(string(body), "a + b + 1", "a + b", 1)), 0o640)
	}}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 3, nil)
	results, err := runner.RunSchedule(context.Background(), schedule, nil)
	if err != nil || len(results) != 2 {
		t.Fatalf("schedule: %v results=%d", err, len(results))
	}
	// A second repetition gets another candidate, not the first candidate.
	schedule, _ = BuildSchedule(suite, []Mode{Raw, Bounded}, 2, 3, nil)
	if _, err := runner.RunSchedule(context.Background(), schedule, map[string]bool{schedule.Entries[0].RunID: true, schedule.Entries[1].RunID: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOracleIsAbsentFromWorkerViewAndRequest(t *testing.T) {
	task, suite := testTaskAndSuite(t)
	var seen string
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(_ context.Context, req WorkerRequest) error {
		seen = string(mustJSON(req))
		if _, err := os.Stat(filepath.Join(req.Workspace, "oracle", "hidden_test.go")); err == nil {
			return errors.New("oracle directory leaked")
		}
		return nil
	}}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	runOne(t, runner, schedule.Entries[0])
	if requestHasForbiddenKey(json.RawMessage(seen)) || strings.Contains(seen, task.hiddenHash) {
		t.Fatalf("worker request carried oracle metadata: %s", seen)
	}
}

func TestWorkerSandboxSpecExcludesAbsoluteOraclePath(t *testing.T) {
	task, suite := testTaskAndSuite(t)
	probe := &recordingSandbox{}
	worker, err := NewCommandWorker([]string{"sh", "-c", "sed -i 's/a + b + 1/a + b/' calculator.go"},
		WithCommandSandbox(probe), WithCommandEnv(map[string]string{"ORACLE_PATH": task.OracleRoot()}), WithCommandName("oracle-probe"))
	if err != nil {
		t.Skipf("no process sandbox available: %v", err)
	}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 1, nil)
	results, err := runner.RunSchedule(context.Background(), schedule, nil)
	if err != nil || len(results) != 2 {
		t.Fatalf("sandbox probe schedule: %v results=%d", err, len(results))
	}
	for _, result := range results {
		if result.Status != RunCompleted {
			t.Fatalf("worker could see or failed to probe the oracle path: %+v", result.System)
		}
	}
	if probe.spec.Dir == "" || len(probe.spec.ReadOnly) == 0 || len(probe.spec.ReadWrite) == 0 {
		t.Fatalf("worker did not receive a bounded sandbox spec: %+v", probe.spec)
	}
	for _, path := range append(append([]string{}, probe.spec.ReadOnly...), probe.spec.ReadWrite...) {
		if path == task.OracleRoot() {
			t.Fatal("worker sandbox grants the evaluator oracle directory")
		}
	}
}

func TestFalseSuccessAndFalseFailure(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	// An unchanged candidate with a successful self-report is a false success.
	runner := testRunner(t, suite, ScriptedWorker{ClaimSuccess: true}, patchCalculator(true))
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	falseSuccess := runOne(t, runner, schedule.Entries[0])
	if !falseSuccess.Derived.FalseSuccess || falseSuccess.Derived.IndependentVerifiedSuccess || falseSuccess.Status != RunTaskFailed {
		t.Fatalf("false success classification: %+v", falseSuccess.Derived)
	}
	// A correct candidate whose worker says it failed is a false failure.
	runner = testRunner(t, suite, patchCalculator(false), patchCalculator(false))
	falseFailure := runOne(t, runner, schedule.Entries[0])
	if !falseFailure.Derived.FalseFailure || !falseFailure.Derived.IndependentVerifiedSuccess {
		t.Fatalf("false failure classification: %+v", falseFailure.Derived)
	}
}

func TestCommandExitIsTaskOutcomeNotInfrastructureFault(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	worker, err := NewCommandWorker([]string{"sh", "-c", "exit 7"}, WithCommandSandbox(&recordingSandbox{}), WithCommandName("exit-test"))
	if err != nil {
		t.Fatal(err)
	}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	result := runOne(t, runner, schedule.Entries[0])
	if result.Status != RunTaskFailed || result.InfrastructureError != "" {
		t.Fatalf("ordinary command exit was classified as infrastructure: %+v", result)
	}
}

func TestTimeoutStillEvaluatesCurrentCandidate(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	suite.Tasks[0].Limits.WallClockSeconds = 1
	suite.hash = suiteHash(suite)
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(ctx context.Context, req WorkerRequest) error {
		if err := patchCalculator(true).Apply(ctx, req); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	result := runOne(t, runner, schedule.Entries[0])
	if result.Status != RunTimeout || result.Execution.TerminationReason != "timeout" {
		t.Fatalf("timeout was not preserved: %+v", result)
	}
	if result.Evaluator.Status != EvaluatorPass || !result.Derived.IndependentVerifiedSuccess {
		t.Fatalf("timed-out candidate was not independently evaluated: %+v", result.Evaluator)
	}
}

func TestInfrastructureErrorIsNotTaskFailure(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(context.Context, WorkerRequest) error { return errors.New("provider socket closed") }}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	result := runOne(t, runner, schedule.Entries[0])
	if result.Status != RunInfrastructure || result.Evaluator.Status == EvaluatorPass && result.Derived.IndependentVerifiedSuccess {
		t.Fatalf("infrastructure error was collapsed: %+v", result)
	}
}

func TestReportScheduleCoverageMarksPartialPairIncomplete(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	runner := testRunner(t, suite, patchCalculator(true), patchCalculator(true))
	schedule, err := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := runOne(t, runner, schedule.Entries[0])
	report := AddScheduleCoverage(BuildReport([]Result{raw}), schedule)
	if report.IncompletePairs == 0 || report.PerformanceEvidence {
		t.Fatalf("partial schedule was treated as complete: %+v", report)
	}
}

func TestResumeSkipsCompletedRunIDs(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	var mu sync.Mutex
	calls := 0
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(ctx context.Context, req WorkerRequest) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return patchCalculator(true).Apply(ctx, req)
	}}
	runner := testRunner(t, suite, worker, worker)
	schedule, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 1, nil)
	if _, err := runner.RunSchedule(context.Background(), schedule, nil); err != nil {
		t.Fatal(err)
	}
	first := calls
	if _, err := runner.RunSchedule(context.Background(), schedule, nil); err != nil {
		t.Fatal(err)
	}
	if calls != first {
		t.Fatalf("resume reran completed work: calls %d -> %d", first, calls)
	}
}

func TestExplicitRerunSelectionExecutesTheSelectedCell(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	var mu sync.Mutex
	calls := 0
	worker := ScriptedWorker{ClaimSuccess: true, Apply: func(ctx context.Context, req WorkerRequest) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return patchCalculator(true).Apply(ctx, req)
	}}
	runner := testRunner(t, suite, worker, worker)
	schedule, err := BuildSchedule(suite, []Mode{Raw, Bounded}, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunSchedule(context.Background(), schedule, nil); err != nil {
		t.Fatal(err)
	}
	first := calls
	selected := schedule.Entries[0].RunID
	if _, err := runner.RunSchedule(context.Background(), schedule, map[string]bool{selected: true}); err != nil {
		t.Fatal(err)
	}
	if calls != first+1 {
		t.Fatalf("explicit rerun did not execute exactly one cell: calls %d -> %d", first, calls)
	}
}

func TestScheduleSeedReproducibilityAndVariation(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	for i := 1; i <= 6; i++ {
		task := suite.Tasks[0]
		task.ID = fmt.Sprintf("task-%d", i)
		task.path = suite.Tasks[0].path
		suite.Tasks = append(suite.Tasks, task)
	}
	suite.hash = suiteHash(suite)
	a, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 2, 11, nil)
	b, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 2, 11, nil)
	if fmt.Sprint(a.Entries) != fmt.Sprint(b.Entries) {
		t.Fatal("same seed did not reproduce schedule")
	}
	c, _ := BuildSchedule(suite, []Mode{Raw, Bounded}, 2, 12, nil)
	if fmt.Sprint(a.Entries) == fmt.Sprint(c.Entries) {
		t.Fatal("different seeds unexpectedly produced identical order")
	}
}

func TestConfigurationMismatchAndRedaction(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	spec, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	r := NewResult(suite, suite.Tasks[0], spec.Entries[0], EnvironmentSnapshot{OS: "test"})
	other := r
	other.Model.ContextTokens = intPtr(2048)
	if CompareResultPair(r, other).OK {
		t.Fatal("context mismatch was accepted")
	}
	r.Configuration.Environment = map[string]string{"API_KEY": "super-secret", "SAFE": "yes"}
	path := filepath.Join(t.TempDir(), "result.json")
	if err := WriteResult(path, r); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "super-secret") {
		t.Fatal("result persisted a credential")
	}
	var round Result
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatal(err)
	}
	if round.Configuration.Environment["API_KEY"] == "super-secret" {
		t.Fatal("redaction did not round-trip")
	}
}

func TestFrozenManifestRejectsTaskChanges(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	manifest, err := Freeze(context.Background(), suite, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFrozen(suite, manifest); err != nil {
		t.Fatal(err)
	}
	changed := suite
	changed.Tasks = append([]Task(nil), suite.Tasks...)
	changed.Tasks[0].Description += " changed"
	changed.Tasks[0].hiddenFiles = map[string][]byte{"hidden_test.go": []byte("changed")}
	changed.Tasks[0].hiddenHash = hiddenDigest(changed.Tasks[0].hiddenFiles)
	changed.hash = suiteHash(changed)
	if err := VerifyFrozen(changed, manifest); err == nil {
		t.Fatal("changed task silently joined a frozen manifest")
	}
}

func TestStoreLeaseRestoresSharedRootActiveState(t *testing.T) {
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, restore, err := StoreFactoryWithRestore(context.Background(), root, "lease-one")
	if err != nil {
		t.Fatal(err)
	}
	if root.Active() != st.ID() {
		t.Fatalf("active workspace = %s, want %s", root.Active(), st.ID())
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if root.Active() != "" {
		t.Fatalf("active workspace after lease = %s, want empty", root.Active())
	}
}

func TestPhysicalExecutionGetsAFreshProductionStore(t *testing.T) {
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	first, err := StoreFactoryForRequest(context.Background(), root, WorkerRequest{RunID: "run-stable", ExecutionID: "attempt-one"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := StoreFactoryForRequest(context.Background(), root, WorkerRequest{RunID: "run-stable", ExecutionID: "attempt-two"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.ID() == second.ID() {
		t.Fatalf("a physical rerun reused the production store %s", first.ID())
	}
	if root.Active() != second.ID() {
		t.Fatalf("root active workspace = %s, want %s", root.Active(), second.ID())
	}
}

func TestIndependentBoundedFactoriesDoNotShareTaskState(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	var mu sync.Mutex
	var seen []string
	base := patchCalculator(true)
	runner := &Runner{Suite: suite, OutputRoot: t.TempDir(), Environment: EnvironmentSnapshot{OS: "test"},
		Evaluator: IndependentEvaluator{Runner: &recordingSandbox{}},
		Raw:       RawAdapter{Worker: base}, Bounded: BoundedAdapter{Factory: func(_ context.Context, req WorkerRequest) (BoundedExecutor, error) {
			return fakeBounded{worker: base, seen: &seen, mu: &mu}, nil
		}}}
	schedule, _ := BuildSchedule(suite, []Mode{Bounded}, 2, 1, nil)
	if _, err := runner.RunSchedule(context.Background(), schedule, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("bounded repetitions shared an executor/workspace: %v", seen)
	}
}

func TestEvaluatorRequiresSandboxByDefault(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	result := (IndependentEvaluator{}).Evaluate(context.Background(), task, t.TempDir(), t.TempDir())
	if result.Status != EvaluatorError || !strings.Contains(result.Error, "requires a process sandbox") {
		t.Fatalf("unconfined evaluator was not rejected: %+v", result)
	}
}

func TestEvaluatorReportsErrorForMissingCommand(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	bad := suite.Tasks[0]
	bad.Evaluator.Command = Command{}
	result := (IndependentEvaluator{AllowUnconfined: true}).Evaluate(context.Background(), bad, t.TempDir(), t.TempDir())
	if result.Status != EvaluatorError || result.Independent {
		t.Fatalf("missing evaluator was not an independent ERROR: %+v", result)
	}
}

func TestTimeoutOfEvaluatorIsDistinct(t *testing.T) {
	task, _ := testTaskAndSuite(t)
	task.Evaluator.Command = Command{Argv: []string{"sh", "-c", "sleep 2"}}
	task.Evaluator.TimeoutSeconds = 1
	candidate := t.TempDir()
	writeTestFile(t, filepath.Join(candidate, "go.mod"), "module x\n\ngo 1.26\n")
	result := (IndependentEvaluator{AllowUnconfined: true}).Evaluate(context.Background(), task, candidate, t.TempDir())
	if result.Status != EvaluatorTimeout {
		t.Fatalf("status=%s, want TIMEOUT", result.Status)
	}
}

func TestResultJSONCarriesExplicitUnavailableMeasurements(t *testing.T) {
	_, suite := testTaskAndSuite(t)
	spec, _ := BuildSchedule(suite, []Mode{Raw}, 1, 1, nil)
	r := NewResult(suite, suite.Tasks[0], spec.Entries[0], EnvironmentSnapshot{OS: "test"})
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"input_tokens", "output_tokens", "tool_calls", "generation_requests"} {
		if !strings.Contains(string(body), `"`+key+`":null`) {
			t.Errorf("missing explicit null for %s", key)
		}
	}
	_ = time.Second // keep time imported for the timeout test's readability
}
