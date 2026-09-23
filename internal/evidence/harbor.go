package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsurePatchedHarborRuntime works around a real, reproducible defect in
// the pinned harbor==0.18.0 package: its Docker environment templates
// (docker-compose-build.yaml and docker-compose-prebuilt.yaml) set
// `command: ["sh", "-c", "sleep infinity"]` with no `entrypoint:`
// override. Every selected SWE Atlas task image inherits
// `ENTRYPOINT ["/bin/bash"]` (no CMD) from its ghcr.io/scaleapi/swe-atlas
// base, so Docker execs `/bin/bash sh -c "sleep infinity"`: since "-c"
// isn't bash's first argument, bash never treats it as its own -c flag —
// it treats the string "sh" as a script filename, resolves it via PATH to
// /bin/sh (a real ELF binary, busybox), refuses to read a binary file as
// a script, and the container exits 126. Confirmed by direct reproduction
// independent of harbor entirely:
//
//	docker run --rm --entrypoint /bin/bash <image> sh -c "echo hi"
//	  -> "/bin/sh: /bin/sh: cannot execute binary file", exit 126
//
// and confirmed fixed by adding an explicit `entrypoint: ["/bin/sh"]` to
// harbor's compose templates so its own `-c "sleep infinity"` command
// finally reaches /bin/sh directly instead of being swallowed as a bash
// script name. 3 of the 4 selected Atlas task images hit this (any base
// with a raw `ENTRYPOINT ["/bin/bash"]`); the 4th (wp-calypso RF) inherits
// a `docker-entrypoint.sh` wrapper that already execs "$@" correctly and
// was never affected.
//
// This cannot be fixed by editing the installed harbor package in place
// (that would mutate a third-party dependency outside this repository).
// Instead, this function makes a private, gitignored shadow copy of the
// installed harbor package under runRoot (the existing guarded evidence
// scratch root), patches only the two compose template files in that
// copy, and returns a PYTHONPATH prefix that makes `import harbor` resolve
// to the patched copy instead of the real installation — every other file
// in the copy is byte-identical to the pinned installed package, so
// nothing about harbor's own evaluator logic is altered, weakened, or
// reimplemented. It is idempotent: a second call reuses the existing copy
// if it is already patched.
func EnsurePatchedHarborRuntime(ctx context.Context, pythonExe, runRoot string) (pythonPath string, err error) {
	src, err := harborPackageDir(ctx, pythonExe)
	if err != nil {
		return "", fmt.Errorf("evidence: locating the installed harbor package: %w", err)
	}

	// PYTHONPATH must point at a directory that itself CONTAINS a
	// subdirectory named exactly "harbor" for `import harbor` to resolve
	// to this copy — so the shadow lives at <shadowRoot>/harbor, and the
	// returned PYTHONPATH prefix is <shadowRoot>, never the package
	// directory itself.
	shadowRoot := filepath.Join(runRoot, "harbor-runtime-patched")
	dst := filepath.Join(shadowRoot, "harbor")
	marker := filepath.Join(dst, ".bc-entrypoint-patch-applied")
	if _, err := os.Stat(marker); err == nil {
		return shadowRoot, nil
	}

	if err := os.RemoveAll(shadowRoot); err != nil {
		return "", err
	}
	if err := os.MkdirAll(shadowRoot, 0o755); err != nil {
		return "", err
	}
	cp := exec.CommandContext(ctx, "cp", "-r", src, dst)
	if out, err := cp.CombinedOutput(); err != nil {
		return "", fmt.Errorf("copying harbor package for patching: %w\n%s", err, out)
	}

	dockerDir := filepath.Join(dst, "environments", "docker")
	for _, f := range []string{"docker-compose-build.yaml", "docker-compose-prebuilt.yaml"} {
		// Resolved and checked against the run root before anything is
		// written. The file name is a fixed literal, but the tree it sits in
		// was copied out of the installed harbor package, and a symlink in
		// that copy would otherwise redirect this write outside the run
		// root entirely.
		path, cerr := containedPath(runRoot, filepath.Join(dockerDir, f))
		if cerr != nil {
			return "", cerr
		}
		if err := patchComposeEntrypoint(path); err != nil {
			return "", fmt.Errorf("patching %s: %w", f, err)
		}
	}

	if err := os.WriteFile(marker, []byte("entrypoint fix for exit-126 on ENTRYPOINT[\"/bin/bash\"] "+
		"base images; see internal/evidence/harbor.go\n"), 0o644); err != nil {
		return "", err
	}
	return shadowRoot, nil
}

// harborPackageDir asks the given Python interpreter (the venv's own,
// matching whatever installed harbor==0.18.0) where its harbor package
// actually lives, rather than hardcoding a version-specific site-packages
// path.
func harborPackageDir(ctx context.Context, pythonExe string) (string, error) {
	cmd := exec.CommandContext(ctx, pythonExe, "-c", "import harbor, os; print(os.path.dirname(harbor.__file__))")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, out.String())
	}
	dir := trimTrailingNewline(out.String())
	if dir == "" {
		return "", fmt.Errorf("empty harbor package path")
	}
	return dir, nil
}

func trimTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func patchComposeEntrypoint(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const needle = `command: [ "sh", "-c", "sleep infinity" ]`
	const replacement = "entrypoint: [ \"/bin/sh\" ]\n    command: [ \"-c\", \"sleep infinity\" ]"
	s := string(body)
	if bytes.Contains(body, []byte("entrypoint:")) {
		return nil // already patched or already declares its own entrypoint
	}
	if !bytes.Contains(body, []byte(needle)) {
		return fmt.Errorf("expected content %q not found in %s — harbor's template may have "+
			"changed; re-diagnose before assuming this patch still applies", needle, path)
	}
	patched := replaceOnce(s, needle, replacement)
	// G703: path is the return of containedPath, which resolves every symlink
	// in the chain and refuses anything landing outside the run root. The
	// containment is enforced above rather than asserted here; gosec's taint
	// analysis cannot see through the validator, which is what this marks.
	return os.WriteFile(path, []byte(patched), 0o644) //nolint:gosec // G703: containedPath already resolved and bounded this path
}

func replaceOnce(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// StartAtlasEnvironment shells out to `harbor task start-env` for one
// SWE Atlas task directory using the patched runtime, proving the
// environment itself starts — GAP B's "environment starts" requirement.
// It does not run the official verifier (see ExecuteRun's SWE Atlas
// branch for why that step needs its own, separate credential this
// session cannot use).
func StartAtlasEnvironment(ctx context.Context, harborExe, pythonPath, taskDir string) (output []byte, err error) {
	cmd := exec.CommandContext(ctx, harborExe, "task", "start-env", "--path", taskDir, "--non-interactive")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+pythonPath)
	out, err := cmd.CombinedOutput()
	return out, err
}

// AtlasEvaluatorEnv builds the environment for the trusted Harbor grader
// subprocess when it actually invokes the official SWE Atlas verifier —
// design instruction GAP 2 §7/§8. Every selected task's own task.toml
// declares `[verifier.env] EVAL_API_KEY = "${OPENAI_API_KEY}"`; this
// function is the minimal trusted translation from the operator-facing
// ATLAS_EVAL_API_KEY into the names that declaration expects, so an
// operator never has to reuse — or risk confusing with — the generator's
// own credential.
//
// It is a pure function, deliberately not a wholesale os.Environ()
// passthrough: only PATH, HOME, PYTHONPATH (needed for the entrypoint
// patch), and the two verifier-facing credential names are set. This
// environment is used ONLY for the trusted grader subprocess Harbor runs
// — it is never passed to internal/sandbox/container.Runner, which builds
// the untrusted agent's sandboxed command environment from Spec.Env alone
// and never reads host env at all (see secret_isolation_test.go).
// lastJSONLine returns the last non-empty line of out — run_atlas_grader.py
// prints exactly one JSON object as its final line of stdout; anything
// before it is library progress output this function skips over.
func lastJSONLine(out []byte) []byte {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "{") {
			return []byte(l)
		}
	}
	return out
}

func AtlasEvaluatorEnv(pythonPath, atlasEvalAPIKey string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"PYTHONPATH=" + pythonPath,
		"OPENAI_API_KEY=" + atlasEvalAPIKey,
		"EVAL_API_KEY=" + atlasEvalAPIKey,
	}
}

// AtlasVerifierScript is the official rubric verifier every selected SWE
// Atlas task ships at this exact path — confirmed by direct inspection of
// a real task directory (data/tw/task-6902ef3ab97fe23e2ad2727a/tests/
// evaluate_tests.py), whose own docstring reads "Requires env vars:
// EVAL_API_KEY, EVAL_BASE_URL, EVAL_MODEL" and whose code reads
// `os.environ.get("EVAL_API_KEY") or os.environ.get("OPENAI_API_KEY")` —
// exactly the two names AtlasEvaluatorEnv sets. It is Harbor/task content,
// never reimplemented or approximated here.
const AtlasVerifierScript = "tests/evaluate_tests.py"

// EnsureAtlasTaskDirectory sparse-clones (or reuses a cached copy of) the
// official scaleapi/SWE-Atlas task directory for taskID —
// task.toml/environment/tests/solution — which prepareSWEAtlas
// deliberately does not fetch (that function only pulls the app source
// for the agent's own disposable workspace via `docker cp`). Grading
// needs the real Harbor task directory to start the grading environment
// through Harbor's own API. Cached under runRoot so a resumed campaign
// never re-clones a task it already has.
func EnsureAtlasTaskDirectory(ctx context.Context, runRoot string, benchmark Benchmark, taskID string) (string, error) {
	track := "tw"
	if benchmark == BenchmarkSWEAtlasRefactoring {
		track = "rf"
	}
	dst := filepath.Join(runRoot, "atlas-task-dirs", taskID)
	if _, err := os.Stat(filepath.Join(dst, "task.toml")); err == nil {
		return dst, nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "atlas-clone-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	clone := exec.CommandContext(ctx, "git", "clone", "--filter=blob:none", "--sparse", "--depth", "1",
		"https://github.com/scaleapi/SWE-Atlas.git", tmp)
	if out, err := clone.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cloning scaleapi/SWE-Atlas: %w\n%s", err, out)
	}
	sparse := exec.CommandContext(ctx, "git", "-C", tmp, "sparse-checkout", "set", "data/"+track+"/"+taskID)
	if out, err := sparse.CombinedOutput(); err != nil {
		return "", fmt.Errorf("sparse-checkout: %w\n%s", err, out)
	}
	src := filepath.Join(tmp, "data", track, taskID)
	if _, err := os.Stat(filepath.Join(src, "task.toml")); err != nil {
		return "", fmt.Errorf("cloned task directory missing task.toml: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("moving the cloned task directory into place: %w", err)
	}
	return dst, nil
}

// AtlasGraderScript is the Go-callable Python driver
// (evals/evidence-v1/tooling/run_atlas_grader.py) that owns one Atlas
// environment's full lifecycle — start, upload the official tests/
// directory, apply the agent's patch, run the official verifier, stop —
// using Harbor's own public Environment API (start/upload_file/
// upload_dir/exec/stop) directly, in one process, rather than the
// `harbor task start-env` CLI command. That CLI command always tears its
// environment down in a `finally: await environment.stop()` block when
// the process exits (confirmed by reading harbor/cli/tasks.py), whether
// --interactive or --non-interactive, so a second, separate process could
// never discover and exec into what it started — the real reason
// FindAtlasEnvironmentContainer/BuildAtlasVerifierCmd (an earlier attempt
// in this session, assuming a container would stay running for a second
// process) never worked end to end. Reusing Harbor's own methods for the
// whole lifecycle is the smallest correct fix, not a reimplementation of
// what they do.
const AtlasGraderScript = "evals/evidence-v1/tooling/run_atlas_grader.py"

// atlasGraderOutcome mirrors the one JSON object run_atlas_grader.py
// prints to stdout.
type atlasGraderOutcome struct {
	OK         bool   `json:"ok"`
	ReturnCode int    `json:"return_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Error      string `json:"error"`
	// Reward is the official verifier's own pass/fail signal, read back
	// from /logs/verifier/reward.txt (evaluate_tests.py's own
	// write_reward() writes it there) — confirmed by a real, live,
	// credential-free run in this session (reward 0 for a mutation-
	// testing task where the LLM rubric step was skipped for lack of a
	// credential). The verifier's own process exit code (OK/ReturnCode)
	// only reports whether the script ran without crashing, never the
	// grading verdict — nil means the driver could not read a reward
	// back at all, an infrastructure condition distinct from a real 0.
	Reward *float64 `json:"reward"`
}

// BuildAtlasGraderCmd constructs the exact subprocess ExecuteRun's SWE
// Atlas grading branch runs — the real call site. The Atlas evaluator
// credential reaches the trusted verifier subprocess ONLY via this
// command's own environment (AtlasEvaluatorEnv) — a completely separate
// process from anything internal/sandbox/container.Runner constructs for
// the untrusted agent, which never reads host env or this function's
// output; the two paths share no construction code.
func BuildAtlasGraderCmd(ctx context.Context, pythonExe, pythonPath, scriptPath, taskDir, patchFile, workdir, atlasEvalAPIKey string, timeoutSec int) *exec.Cmd {
	cmd := exec.CommandContext(ctx, pythonExe, scriptPath, taskDir, patchFile, workdir,
		AtlasVerifierScript, fmt.Sprint(timeoutSec))
	cmd.Env = AtlasEvaluatorEnv(pythonPath, atlasEvalAPIKey)
	return cmd
}

// gradeSWEAtlas is the real call site ExecuteRun's SWE Atlas branch
// invokes — design instruction: ExecuteRun -> Atlas grading -> official
// Harbor/verifier command -> AtlasEvaluatorEnv -> cmd.Env -> verifier.
// Real end to end: fetches the task directory, patches the harbor
// runtime's entrypoint bug, writes the agent's patch to a scratch file,
// and runs the grading driver script — proven for real in this session
// (a live, credential-free run through EnsureAtlasTaskDirectory,
// EnsurePatchedHarborRuntime, environment start/upload/patch-apply, and
// the official verifier launching, with the LLM step itself skipping
// cleanly for lack of a credential — see run_atlas_grader.py).
func gradeSWEAtlas(ctx context.Context, runRoot string, rf *RunnerFactory, p RunPlan, patch []byte) (evaluatorOutput []byte, status Status, err error) {
	if rf.AtlasEvalAPIKey == "" {
		return nil, StatusInfraErr, fmt.Errorf("missing Atlas evaluator credential")
	}
	taskDir, derr := EnsureAtlasTaskDirectory(ctx, runRoot, p.Benchmark, p.TaskID)
	if derr != nil {
		return nil, StatusInfraErr, fmt.Errorf("fetching the Harbor task directory: %w", derr)
	}
	pythonPath, herr := EnsurePatchedHarborRuntime(ctx, rf.PythonExe, runRoot)
	if herr != nil {
		return nil, StatusInfraErr, fmt.Errorf("preparing the patched harbor runtime: %w", herr)
	}

	patchFile := filepath.Join(runRoot, "atlas-grade-scratch", p.TaskID+".agent.patch")
	if err := os.MkdirAll(filepath.Dir(patchFile), 0o755); err != nil {
		return nil, StatusInfraErr, err
	}
	if err := os.WriteFile(patchFile, patch, 0o600); err != nil {
		return nil, StatusInfraErr, err
	}
	defer os.Remove(patchFile)

	scriptPath := filepath.Join(rf.RepoRoot, AtlasGraderScript)
	cmd := BuildAtlasGraderCmd(ctx, rf.PythonExe, pythonPath, scriptPath, taskDir, patchFile,
		p.ImageDir, rf.AtlasEvalAPIKey, 900)

	run := rf.RunVerifierCmd
	if run == nil {
		run = func(c *exec.Cmd) ([]byte, error) { return c.CombinedOutput() }
	}
	out, runErr := run(cmd)
	if runErr != nil {
		return out, StatusInfraErr, fmt.Errorf("running the Atlas grading driver: %w", runErr)
	}

	var outcome atlasGraderOutcome
	if jerr := json.Unmarshal(lastJSONLine(out), &outcome); jerr != nil {
		return out, StatusInfraErr, fmt.Errorf("parsing the Atlas grading driver's output: %w", jerr)
	}
	if outcome.Error != "" {
		return out, StatusInfraErr, fmt.Errorf("%s", outcome.Error)
	}
	if !outcome.OK {
		// The verifier script itself crashed or errored (return_code !=
		// 0) — an infrastructure condition, not a graded FAILED: a
		// verdict was never reached at all. Confirmed by a real,
		// credential-free live run in this session: a clean run reports
		// return_code 0 with reward.txt written, even when reward is 0.
		return out, StatusInfraErr, fmt.Errorf("the official Atlas verifier script exited non-zero "+
			"(return_code=%d) before writing a reward — see stdout/stderr in the persisted "+
			"evaluator output", outcome.ReturnCode)
	}
	if outcome.Reward == nil {
		return out, StatusInfraErr, fmt.Errorf("the official Atlas verifier ran successfully but " +
			"this driver could not read back its reward.txt artifact")
	}
	// Official pass/fail signal — /logs/verifier/reward.txt, not the
	// script's own exit code (see atlasGraderOutcome's doc comment for
	// the real, live-run evidence this distinction is based on). A
	// reward of 1.0 is a clean pass; anything less is not treated as
	// solved, since this suite's frozen tasks are binary rubric/mutation
	// checks, not partial-credit scoring.
	if *outcome.Reward >= 1.0 {
		return out, StatusSolved, nil
	}
	return out, StatusFailed, nil
}
