package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/engine/native"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
	"gopkg.in/yaml.v3"
)

// GradingSpec is what CompletePatch grading needs for one SWE-Bench Pro
// Verified task, taken directly from the same oracle-validation data this
// suite already recorded for real (evals/evidence-v1/oracle-validation-results.json)
// — never reconstructed or guessed at grading time.
type GradingSpec struct {
	TestCommand string // an /bin/sh -c script, run inside the pristine grader container
}

// GradingRegistry mirrors oracle-validation-results.json's real,
// already-executed test commands. It is the ONLY grading mechanism this
// package implements — the official evaluator, never a reimplementation of
// its scoring logic (design instruction §13/§31 from earlier audits):
// exactly the command AgentCompass-equivalent oracle validation used, run
// again here against the agent's patch instead of the gold patch.
var GradingRegistry = map[string]GradingSpec{
	"instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-v1151a6325649aaf997cd541ebe533b53fddf1b07": {
		TestCommand: "go test ./scanner/... -run 'Test_redhatBase_parseUpdatablePacksLine|Test_redhatBase_parseUpdatablePacksLines'",
	},
	"instance_element-hq__element-web-ca58617cee8aa91c93553449bfdf9b3465a5119b-vnan": {
		TestCommand: "yarn jest test/LegacyCallHandler-test.ts -t 'should unmute'",
	},
	"instance_internetarchive__openlibrary-1894cb48d6e7fb498295a5d3ed0596f6f603b784-v0f5aece3601a5b4419f7ccec1dbda2071be28ee4": {
		TestCommand: "python3 -m pytest openlibrary/catalog/add_book/tests/test_add_book.py::test_find_match_title_only_promiseitem_against_noisbn_marc",
	},
	"instance_tutao__tutanota-b4934a0f3c34d9d7649e944b183137e8fad3e859-vbc0d9ba8f0071fbe982809910959a6ff8884dbbf": {
		TestCommand: "cd test && node test.js -f",
	},
}

// RunnerFactory builds the real dependencies a live run needs, once, and
// reuses them across all 16 runs — the generator/provider construction is
// expensive (a health probe, per cmd/bcode/task.go's engineFor) and identical
// for every CONTROL run and every TREATMENT run (only the Jev judge
// differs), so it is built once rather than per run.
type RunnerFactory struct {
	// Provider is the real, configured generator provider — constructed
	// exactly the way cmd/bcode/task.go's engineFor builds one (providers.yaml
	// -> llm.NewRouter -> Router.For(llm.RoleCoding)), never a special
	// Evidence-Suite-only provider. Both arms use this same Provider.
	Provider llm.Provider
	// EngineOptions carries the profile-derived settings (budgets,
	// sampling, thinking) both arms share — design instruction §10/§12:
	// "the exact same generator config must be used by both arms."
	EngineOptions native.Options
	// TreatmentJudge is a judgment.Judge constructed once from the FROZEN
	// evals/evidence-v1/jev-profile.yaml with no Recorder — it exists so
	// callers (and tests) can prove construction succeeds up to the
	// network boundary without a workspace. It is NOT what a treatment
	// run actually uses: ExecuteRun rebuilds a fresh judge per run from
	// JevProfilePath/JevAPIKeyEnv with a Recorder bound to that run's own
	// store, since judgment.Recorder is workspace-scoped the same way
	// Retriever/Graph are (see buildEngine's doc comment).
	TreatmentJudge judgment.Judge
	// JevProfilePath and JevAPIKeyEnv are what ExecuteRun uses to rebuild
	// the real, per-run treatment judge (with a Recorder) — set alongside
	// TreatmentJudge by the same construction call.
	JevProfilePath string
	JevAPIKeyEnv   string

	// AtlasEvalAPIKey is the Atlas evaluator credential's actual value
	// (from ATLAS_EVAL_API_KEY), used only to build the trusted Harbor
	// verifier subprocess's environment (see harbor.go's
	// BuildAtlasVerifierCmd/AtlasEvaluatorEnv) — never passed to
	// task.Runner or any sandbox.Spec. Empty means SWE Atlas grading
	// cannot proceed past the credential check.
	AtlasEvalAPIKey string
	// PythonExe and HarborExe locate the venv this suite's Harbor
	// tooling was installed into — see EnsurePatchedHarborRuntime.
	PythonExe string
	HarborExe string
	// RepoRoot locates evals/evidence-v1/tooling/run_atlas_grader.py —
	// the Go-callable Python driver gradeSWEAtlas shells out to.
	RepoRoot string
	// RunVerifierCmd executes the constructed verifier exec.Cmd.
	// Production leaves it nil, so the real command actually runs
	// (cmd.CombinedOutput()). Tests set it to intercept the exact
	// command BuildAtlasVerifierCmd produced — argv and env — without a
	// live network call, exercising every real construction step up to
	// that one boundary.
	RunVerifierCmd func(*exec.Cmd) ([]byte, error)
	// BoundedCodeCommit is recorded on every result — design instruction
	// §38's environment identity.
	BoundedCodeCommit string
	// RuntimeConfigSHA256 is evals/evidence-v1/runtime-config.yaml's hash,
	// computed once by LoadRuntimeConfig and recorded on every Result —
	// GAP 1 §3/§47's frozen generator identity.
	RuntimeConfigSHA256 string
	// EngineFactory overrides how the generator engine is built. Production
	// callers leave it nil, so buildEngine uses the real construction path
	// (native.New against Provider/EngineOptions) — the same primitives
	// cmd/bcode/task.go's engineFor uses. Tests set this to inject a
	// deterministic fake engine, so the rest of ExecuteRun's real
	// construction (task.NewRunner, sandbox.Runner, judgment.Judge wiring,
	// PersistResult) is exercised without a network call. This is the one
	// seam that differs from a normal task; every other construction step
	// is identical to what cmd/bcode/task.go does for an ordinary task.
	EngineFactory func() (engine.Engine, error)
}

// BuildTreatmentJudge constructs a judgment.Judge from the frozen
// evals/evidence-v1/jev-profile.yaml — design instruction §11/§15: "do not
// call TypeSafe directly from Evidence Runner... the runtime path must
// remain registered Site -> judgment subsystem -> Jev provider." This
// function builds the *subsystem's own* judgment.Client
// (internal/judgment.New), which every registered Site already calls
// through the normal judgment.Consult path — it does not add a second
// TypeSafe client or bypass that path in any way.
func BuildTreatmentJudge(profilePath string, apiKeyEnv string, deps judgment.Deps) (judgment.Judge, error) {
	prof, err := loadJevProfile(profilePath)
	if err != nil {
		return nil, err
	}
	cfg := judgment.Config{
		Enabled:       true,
		Endpoint:      judgment.DefaultEndpoint,
		Model:         prof.JevModel,
		APIKeyEnv:     apiKeyEnv,
		Redact:        judgment.RedactMode(prof.Redact),
		MinConfidence: judgment.DefaultMinConfidence,
		Sites:         map[string]judgment.Tier{},
	}
	for _, s := range prof.Sites {
		cfg.Sites[s.Site] = judgment.Tier(s.Authority)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("evidence: frozen jev-profile.yaml produced an invalid judgment "+
			"config: %w", err)
	}
	return judgment.New(cfg, deps)
}

// jevProfileFile mirrors evals/evidence-v1/jev-profile.yaml's own shape
// (see that file's own comments for why each field exists).
type jevProfileFile struct {
	JevModel string `yaml:"jev_model"`
	Redact   string `yaml:"redact"`
	Sites    []struct {
		Site      string `yaml:"site"`
		Authority string `yaml:"authority"`
	} `yaml:"sites"`
}

func loadJevProfile(path string) (jevProfileFile, error) {
	var f jevProfileFile
	body, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := yamlUnmarshal(body, &f); err != nil {
		return f, fmt.Errorf("evidence: parsing jev-profile.yaml: %w", err)
	}
	if f.JevModel == "" {
		return f, fmt.Errorf("evidence: jev-profile.yaml has no jev_model")
	}
	return f, nil
}

// Result is the durable, structured record for one completed run — design
// instruction §4 (GAP 4)'s minimum field set, populated from real
// task.Outcome and grading data, never invented. Fields this package
// cannot obtain reliably (generator token/cost accounting is not yet
// exposed by internal/task.Outcome in a per-run breakout — see "Known
// limitations" in docs/evidence/evidence-v1.md) are left at their zero
// value / omitted rather than guessed, per design instruction §16's "if a
// field cannot be obtained reliably, store null/N/A. Do not guess."
type Result struct {
	TaskID    string    `json:"task_id"`
	Benchmark Benchmark `json:"benchmark"`
	Arm       Arm       `json:"arm"`
	RunID     string    `json:"run_id"`
	SuiteHash string    `json:"suite_hash"`

	// The two tags below keep their original spelling on purpose. A JSON
	// tag here is a storage format, not branding: evidence records written
	// by earlier runs are retained and still read, and renaming the key
	// would make those fields silently decode as empty.
	BoundedCodeStatus       string `json:"local_engineer_status"` // task.State
	OfficialBenchmarkStatus Status `json:"official_benchmark_status"`

	Attempts    int    `json:"attempts"`
	TokensUsed  int    `json:"tokens_used"`
	WallTimeMS  int64  `json:"wall_time_ms"`
	PatchSHA256 string `json:"patch_sha256,omitempty"`
	PatchBytes  int    `json:"patch_bytes"`

	EvaluatorOutputSHA256 string `json:"evaluator_output_sha256,omitempty"`
	EvaluatorExitCode     int    `json:"evaluator_exit_code"`

	WorkspaceFingerprint string `json:"workspace_fingerprint"`
	ImageDigest          string `json:"image_digest"`
	BoundedCodeCommit    string `json:"local_engineer_commit"`

	GeneratorProvider string `json:"generator_provider,omitempty"`
	GeneratorModel    string `json:"generator_model,omitempty"`
	// RuntimeConfigSHA256 is evals/evidence-v1/runtime-config.yaml's hash
	// at the moment this run's engine was built — design instruction GAP
	// 1 §3/§47: proves CONTROL and treatment (and any two runs of this
	// suite) used the identical, frozen generator identity.
	RuntimeConfigSHA256 string `json:"runtime_config_sha256,omitempty"`

	// EvaluatorCost is the official Atlas verifier's own token/cost usage,
	// when Harbor exposes it — kept separate from generator/Jev cost
	// (design instruction §42/§43): the evaluator is part of the
	// benchmark, never counted as a BoundedCode call.
	EvaluatorCost *float64 `json:"evaluator_cost,omitempty"`

	// TelemetryStatus records whether judgment/telemetry export completed
	// for this run — design instruction §18: a telemetry export failure
	// must never erase or block a real benchmark result. "complete" for
	// CONTROL (nothing to export) and for a treatment run whose export
	// succeeded; "incomplete" if export was attempted and failed.
	TelemetryStatus string `json:"telemetry_status,omitempty"`

	Detail string `json:"detail,omitempty"`
}

// ExecuteRun drives one planned run through the real, unmodified
// BoundedCode pipeline end to end — design instruction §8/§17 (GAP 3): load
// -> Prepare -> construct real task.Runner (real engine, real judge for
// treatment) -> Runner.Run -> extract patch -> grade in a fresh pristine
// container -> Result.
//
// This is real, compiling, and structurally correct — built against the
// exact same construction primitives cmd/bcode/task.go uses for an ordinary
// task (llm.NewRouter, native.New, judgment.New) — but it has not been
// exercised with a real credential (forbidden for this task) or a live
// model call. Its wiring is verified with a fake generator elsewhere in
// this package's test suite; the network round trip itself is the one
// thing that only a real, credentialed run can confirm.
func ExecuteRun(ctx context.Context, runRoot string, rf *RunnerFactory, p RunPlan, runID string) (res Result, patch, evaluatorRaw []byte, judgments []JudgmentTelemetry, err error) {
	res = Result{TaskID: p.TaskID, Benchmark: p.Benchmark, Arm: p.Arm, RunID: runID,
		SuiteHash: p.SuiteHash, ImageDigest: p.ImageRef, BoundedCodeCommit: rf.BoundedCodeCommit,
		GeneratorProvider: rf.Provider.Name(), RuntimeConfigSHA256: rf.RuntimeConfigSHA256,
		OfficialBenchmarkStatus: StatusInfraErr, TelemetryStatus: "complete"}

	prepared, perr := Prepare(ctx, runRoot, p)
	if perr != nil {
		res.Detail = "prepare failed: " + perr.Error()
		res.OfficialBenchmarkStatus = StatusInvalid
		return res, nil, nil, nil, perr
	}
	res.WorkspaceFingerprint = prepared.SourceFingerprint

	storeRoot := filepath.Join(runRoot, "run-stores", runID)
	root, serr := store.OpenRoot(storeRoot)
	if serr != nil {
		res.Detail = "opening store: " + serr.Error()
		return res, nil, nil, nil, serr
	}
	// Best-effort: the run's results are already written by the time this
	// runs, and a close failure cannot change what was recorded.
	//
	// contextcheck wants the run's context passed down into Close. It must
	// not be: store.Close deliberately checkpoints under its own bounded
	// detached context, because a cancelled run still has to flush its
	// databases. Handing it a dead context would skip the checkpoint of
	// exactly the run whose evidence matters most — the same reasoning as
	// ledger.recording and cleanupContext in this package.
	defer func() { _ = root.CloseAll() }() //nolint:contextcheck // store.Close checkpoints under its own bounded detached context by design
	st, werr := root.OpenWorkspace(ctx, workspace.DeriveID("/evidence-suite/"+p.TaskID, "", runID))
	if werr != nil {
		res.Detail = "opening workspace: " + werr.Error()
		return res, nil, nil, nil, werr
	}

	eng, eerr := rf.buildEngine(st)
	if eerr != nil {
		res.Detail = "constructing generator engine: " + eerr.Error()
		return res, nil, nil, nil, eerr
	}

	sb := sandbox.Runner(&container.Runner{})
	r, rerr := task.NewRunner(st, eng, sb, "evidence-suite-v1")
	if rerr != nil {
		res.Detail = "constructing runner: " + rerr.Error()
		return res, nil, nil, nil, rerr
	}
	r.SandboxSpec = sandbox.Spec{Image: prepared.ImageRef, ImageDir: prepared.ImageDir, Network: sandbox.NetworkNone}
	if p.Arm == ArmExperimentalJevFull {
		// Rebuild the treatment judge per run, with a Recorder bound to
		// this run's own store — judgment.Recorder is workspace-scoped
		// the same way Retriever/Graph are (see buildEngine), so
		// rf.TreatmentJudge (built once, with no Recorder, only to prove
		// construction succeeds — see its doc comment) is never what an
		// actual run uses.
		judge, jerr := BuildTreatmentJudge(rf.JevProfilePath, rf.JevAPIKeyEnv,
			judgment.Deps{Recorder: newEvidenceJournal(st)})
		if jerr != nil {
			res.Detail = "constructing the per-run treatment judge: " + jerr.Error()
			return res, nil, nil, nil, jerr
		}
		r.Judge = judge
	}

	taskID := task.NewID("evidence-" + runID)
	if cerr := task.NewStore(st).Create(ctx, task.Task{
		ID: taskID, Title: "Evidence Suite v1: " + p.TaskID, Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 3, MaxWallTime: 60 * time.Minute},
	}); cerr != nil {
		res.Detail = "creating task: " + cerr.Error()
		return res, nil, nil, nil, cerr
	}

	started := time.Now()
	outcome, runErr := r.Run(ctx, taskID, prepared.WorkspacePath)
	res.WallTimeMS = time.Since(started).Milliseconds()
	if runErr != nil {
		res.Detail = "Runner.Run: " + runErr.Error()
		return res, nil, nil, nil, runErr
	}
	res.BoundedCodeStatus = string(outcome.Task.State)
	res.Attempts = outcome.Attempts
	res.TokensUsed = outcome.TokensUsed

	// GAP 3 §13/§18: export whatever the real judgment ledger recorded
	// for this run, after Runner.Run reaches a terminal state — never
	// before (nothing to export yet) and never by calling Jev again. A
	// CONTROL run never attaches a Judge, so it always has zero rows —
	// design instruction §17. A failed export must not erase the real
	// benchmark result: it degrades TelemetryStatus and continues.
	if p.Arm == ArmExperimentalJevFull {
		rows, jterr := ExtractJudgmentTelemetry(ctx, st, taskID)
		if jterr != nil {
			res.TelemetryStatus = "incomplete"
		} else {
			judgments = rows
			res.TelemetryStatus = "complete"
		}
	}

	patch = []byte(outcome.Diff)
	if len(patch) > 0 && patch[len(patch)-1] != '\n' {
		patch = append(patch, '\n')
	}
	res.PatchBytes = len(patch)
	if len(patch) > 0 {
		res.PatchSHA256 = sha256Hex(patch)
	}

	switch p.Benchmark {
	case BenchmarkSWEBenchProVerified:
		grading, ok := GradingRegistry[p.TaskID]
		if !ok {
			res.OfficialBenchmarkStatus = StatusInfraErr
			res.Detail = "no grading spec registered for this task"
			return res, patch, nil, judgments, nil
		}
		if len(patch) == 0 {
			res.OfficialBenchmarkStatus = StatusFailed
			res.Detail = "the run produced no patch"
			return res, patch, nil, judgments, nil
		}
		gradeStatus, evalOut, exitCode, gerr := gradeSWEBenchPro(ctx, p.ImageRef, patch, grading)
		if gerr != nil {
			res.OfficialBenchmarkStatus = StatusInfraErr
			res.Detail = "grading: " + gerr.Error()
			return res, patch, evalOut, judgments, nil
		}
		res.OfficialBenchmarkStatus = gradeStatus
		res.EvaluatorExitCode = exitCode
		evaluatorRaw = evalOut
		if len(evalOut) > 0 {
			res.EvaluatorOutputSHA256 = sha256Hex(evalOut)
		}
		return res, patch, evaluatorRaw, judgments, nil

	case BenchmarkSWEAtlasTestWriting, BenchmarkSWEAtlasRefactoring:
		evalOut, gstatus, gerr := gradeSWEAtlas(ctx, runRoot, rf, p, patch)
		res.OfficialBenchmarkStatus = gstatus
		if gerr != nil {
			res.Detail = gerr.Error()
		}
		if len(evalOut) > 0 {
			res.EvaluatorOutputSHA256 = sha256Hex(evalOut)
		}
		return res, patch, evalOut, judgments, nil

	default:
		res.OfficialBenchmarkStatus = StatusInvalid
		res.Detail = fmt.Sprintf("unknown benchmark %q", p.Benchmark)
		return res, patch, nil, judgments, nil
	}
}

// buildEngine constructs the real generator engine for one run — never a
// fallback engine.Verify{} on failure, since a construction failure here
// must surface as an INFRA_ERROR, not silently run a no-op "verified" task
// that would masquerade as a real attempt (design instruction §25, and the
// bug this session found and fixed: an earlier draft of this function did
// exactly that fallback).
//
// Retriever and Graph are workspace-scoped (native.New bakes them into the
// engine at construction, not per Step), so — unlike Provider and the
// tuning knobs in EngineOptions, which are identical for every run and
// built once — they are constructed fresh from st for each run, the same
// way cmd/bcode/task.go's retrieverFor/graphFor do for an ordinary task.
func (rf *RunnerFactory) buildEngine(st *store.Store) (engine.Engine, error) {
	if rf.EngineFactory != nil {
		return rf.EngineFactory()
	}
	opts := rf.EngineOptions
	opts.Provider = rf.Provider
	opts.Retriever = retrieval.New(st)
	opts.Graph = graph.New(st)
	return native.New(opts)
}

// gradeSWEBenchPro applies patch to a fresh pristine container of image and
// runs grading.TestCommand — the same official-evaluator-equivalent
// mechanism this suite's earlier oracle validation already proved correct
// against the real gold patch (evals/evidence-v1/oracle-validation-results.json).
// A non-zero exit is FAILED, not INFRA_ERROR: the test command itself
// running and reporting failure is exactly what a wrong patch looks like.
func gradeSWEBenchPro(ctx context.Context, image string, patch []byte, grading GradingSpec) (Status, []byte, int, error) {
	cid, err := startEntrypointOverriddenContainer(ctx, image)
	if err != nil {
		return StatusInfraErr, nil, -1, err
	}
	defer func() {
		stopCtx, cancel := cleanupContext(ctx)
		defer cancel()
		_ = exec.CommandContext(stopCtx, "docker", "stop", "-t", "2", cid).Run()
	}()

	tmp, err := os.MkdirTemp("", "evidence-grade-")
	if err != nil {
		return StatusInfraErr, nil, -1, err
	}
	defer os.RemoveAll(tmp)
	patchFile := filepath.Join(tmp, "agent.patch")
	if err := os.WriteFile(patchFile, patch, 0o600); err != nil {
		return StatusInfraErr, nil, -1, err
	}
	if err := exec.CommandContext(ctx, "docker", "cp", patchFile, cid+":/tmp/agent.patch").Run(); err != nil {
		return StatusInfraErr, nil, -1, fmt.Errorf("docker cp: %w", err)
	}

	check := exec.CommandContext(ctx, "docker", "exec", cid, "bash", "-c",
		"cd /app && git apply --check /tmp/agent.patch")
	if out, err := check.CombinedOutput(); err != nil {
		return StatusFailed, out, -1, nil // a patch that does not apply is a failed attempt, not infra
	}

	apply := exec.CommandContext(ctx, "docker", "exec", cid, "bash", "-c",
		"cd /app && git apply /tmp/agent.patch")
	if out, err := apply.CombinedOutput(); err != nil {
		return StatusInfraErr, out, -1, fmt.Errorf("git apply (passed --check but failed to apply): %w", err)
	}

	test := exec.CommandContext(ctx, "docker", "exec", cid, "bash", "-c", "cd /app && "+grading.TestCommand)
	out, err := test.CombinedOutput()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return StatusInfraErr, out, -1, err
		}
	}
	if exitCode == 0 {
		return StatusSolved, out, exitCode, nil
	}
	return StatusFailed, out, exitCode, nil
}

// yamlUnmarshal is a tiny indirection so this file's one YAML dependency is
// explicit; it defers to the same gopkg.in/yaml.v3 the rest of
// internal/judgment already uses.
func yamlUnmarshal(body []byte, v any) error {
	return yaml.Unmarshal(body, v)
}

var _ = strings.TrimSpace // keep strings imported for future trimming needs in this file
var _ = json.Marshal      // keep json imported; Result is (de)serialized by callers in this package
