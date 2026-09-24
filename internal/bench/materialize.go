package bench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// Candidate is the isolated repository a worker edits.  The path is never the
// operator checkout and is never reused by another RunSpec.
type Candidate struct {
	Path             string `json:"path"`
	Repository       string `json:"repository"`
	ConfiguredBase   string `json:"configured_base"`
	BaseCommit       string `json:"base_commit"`
	BaseTree         string `json:"base_tree"`
	FixtureHash      string `json:"fixture_hash,omitempty"`
	StartingManifest string `json:"starting_manifest"`
	StartingDirty    bool   `json:"starting_dirty"`
	SetupManifest    string `json:"setup_manifest,omitempty"`
	OracleOutside    bool   `json:"oracle_outside"`
}

// Materializer creates a fresh candidate from the task's immutable input.
type Materializer struct {
	// GitTimeout bounds repository and git operations. A benchmark must not
	// hang forever before its external worker timeout starts.
	GitTimeout time.Duration
	// SetupEnvironment contains suite-level variables for deterministic setup
	// commands. Task-level variables override these in Materialize, matching
	// the worker environment's precedence.
	SetupEnvironment map[string]string
	// Sandbox confines setup commands to the candidate and its private scratch
	// directory when the CLI supplies one. A nil Sandbox is rejected unless the
	// caller explicitly opts into a trusted, non-benchmark diagnostic.
	Sandbox              sandbox.Runner
	AllowUnconfinedSetup bool
}

func (m Materializer) timeout() time.Duration {
	if m.GitTimeout > 0 {
		return m.GitTimeout
	}
	return 2 * time.Minute
}

// Materialize copies/clone the configured base into dest. Dest must not exist;
// refusing an existing path is what makes a retry visibly different from a
// contaminated resume.
func (m Materializer) Materialize(ctx context.Context, t Task, dest string) (Candidate, error) {
	if dest == "" {
		return Candidate{}, errors.New("bench: candidate destination is empty")
	}
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return Candidate{}, fmt.Errorf("bench: candidate destination is a symlink: %s", dest)
		}
		return Candidate{}, fmt.Errorf("bench: candidate destination already exists: %s", dest)
	} else if !os.IsNotExist(err) {
		return Candidate{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return Candidate{}, err
	}
	if err := os.Mkdir(dest, 0o750); err != nil {
		return Candidate{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dest)
		}
	}()

	source := t.ResolvedRepository()
	base := strings.TrimSpace(t.BaseCommit)
	var resolved string
	var err error
	if t.Fixture != "" {
		if err := copyTree(source, dest); err != nil {
			return Candidate{}, fmt.Errorf("bench: copy fixture: %w", err)
		}
		resolved, err = initFixtureRepo(ctx, dest, m.timeout())
		if err != nil {
			return Candidate{}, err
		}
	} else {
		resolved, err = cloneAt(ctx, source, dest, base, m.timeout())
		if err != nil {
			return Candidate{}, err
		}
	}
	if err := checkConfiguredBase(t, source, dest, base, resolved); err != nil {
		return Candidate{}, err
	}
	fixtureHash := ""
	if t.Fixture != "" {
		var fixtureErr error
		fixtureHash, fixtureErr = hashDirectory(dest, fixtureContentSkip)
		if fixtureErr != nil {
			return Candidate{}, fmt.Errorf("bench: hash materialized fixture: %w", fixtureErr)
		}
		if t.FixtureHash != "" && !strings.EqualFold(fixtureHash, t.FixtureHash) {
			return Candidate{}, fmt.Errorf("bench: fixture content changed: manifest %s, materialized %s", t.FixtureHash, fixtureHash)
		}
	}
	if err := rejectOracleInHistory(ctx, dest, t); err != nil {
		return Candidate{}, err
	}
	before, err := gitOutput(ctx, m.timeout(), dest, "rev-parse", "HEAD")
	if err != nil {
		return Candidate{}, fmt.Errorf("bench: resolve materialized base: %w", err)
	}
	tree, err := gitOutput(ctx, m.timeout(), dest, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return Candidate{}, err
	}
	manifest, err := ContentManifest(dest)
	if err != nil {
		return Candidate{}, err
	}
	dirty, err := gitDirty(ctx, m.timeout(), dest)
	if err != nil {
		return Candidate{}, err
	}
	if dirty {
		return Candidate{}, fmt.Errorf("bench: materialized base %s is dirty before the worker starts", before)
	}
	c := Candidate{
		Path: dest, Repository: source, ConfiguredBase: base, BaseCommit: before,
		BaseTree: tree, FixtureHash: fixtureHash, StartingManifest: manifest, OracleOutside: true,
	}
	if !t.Setup.Empty() {
		if m.Sandbox == nil && !m.AllowUnconfinedSetup {
			return Candidate{}, errors.New("bench: setup requires a process sandbox; refusing direct execution")
		}
		if err := validateEnvironment("setup", mergeStrings(m.SetupEnvironment, t.Environment)); err != nil {
			return Candidate{}, err
		}
		setupEnv := mergeStrings(m.SetupEnvironment, t.Environment)
		if err := runSetup(ctx, t.Setup, dest, setupEnv, t.NetworkPolicy, m.Sandbox, m.timeout()); err != nil {
			return Candidate{}, fmt.Errorf("bench: setup: %w", err)
		}
		c.SetupManifest, err = ContentManifest(dest)
		if err != nil {
			return Candidate{}, err
		}
	}
	// A setup command is allowed to create deterministic generated files, but
	// it may not smuggle evaluator material into the worker view.
	if err := AssertNoOracle(t, dest); err != nil {
		return Candidate{}, err
	}
	if err := rejectOracleInHistory(ctx, dest, t); err != nil {
		return Candidate{}, err
	}
	if !t.Setup.Empty() {
		// Both arms must start from the same post-setup state. Production
		// task.Runner creates a worktree from HEAD and deliberately does not
		// sync uncommitted files, so an uncommitted setup would silently give
		// BOUNDED a different starting candidate from RAW.
		setupCommit, setupTree, committed, commitErr := commitSetupBaseline(ctx, dest, m.timeout())
		if commitErr != nil {
			return Candidate{}, fmt.Errorf("bench: commit setup baseline: %w", commitErr)
		}
		if committed {
			c.BaseCommit, c.BaseTree = setupCommit, setupTree
			// StartingManifest is the state a worker is allowed to edit. Keep
			// SetupManifest as the explicit record of the pre-commit content.
			c.StartingManifest = c.SetupManifest
		}
	}
	cleanup = false
	return c, nil
}

func commitSetupBaseline(ctx context.Context, dir string, timeout time.Duration) (commit, tree string, changed bool, err error) {
	if err = validateCandidateGitDir(dir); err != nil {
		return "", "", false, err
	}
	if err = runGit(ctx, timeout, dir, "add", "-A", "-f"); err != nil {
		return "", "", false, err
	}
	status, err := gitOutput(ctx, timeout, dir, "status", "--porcelain")
	if err != nil {
		return "", "", false, err
	}
	if strings.TrimSpace(status) == "" {
		base, baseErr := gitOutput(ctx, timeout, dir, "rev-parse", "HEAD")
		if baseErr != nil {
			return "", "", false, baseErr
		}
		tree, treeErr := gitOutput(ctx, timeout, dir, "rev-parse", "HEAD^{tree}")
		return base, tree, false, treeErr
	}
	if err := runGitEnv(ctx, timeout, dir, []string{
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	}, "-c", "user.name=boundedcode-benchmark", "-c", "user.email=benchmark@localhost", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "benchmark setup baseline"); err != nil {
		return "", "", false, err
	}
	commit, err = gitOutput(ctx, timeout, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", false, err
	}
	tree, err = gitOutput(ctx, timeout, dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", "", false, err
	}
	return commit, tree, true, nil
}

func initFixtureRepo(ctx context.Context, dir string, timeout time.Duration) (string, error) {
	if err := runGit(ctx, timeout, dir, "init", "-q"); err != nil {
		return "", fmt.Errorf("bench: initialize fixture repository: %w", err)
	}
	for _, args := range [][]string{
		{"config", "user.name", "boundedcode-benchmark"},
		{"config", "user.email", "benchmark@localhost"},
		{"config", "commit.gpgsign", "false"},
	} {
		if err := runGit(ctx, timeout, dir, args...); err != nil {
			return "", err
		}
	}
	if err := runGit(ctx, timeout, dir, "add", "-A", "-f"); err != nil {
		return "", err
	}
	env := []string{
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	}
	if err := runGitEnv(ctx, timeout, dir, env, "commit", "-q", "-m", "benchmark immutable base"); err != nil {
		return "", err
	}
	return gitOutput(ctx, timeout, dir, "rev-parse", "HEAD")
}

func cloneAt(ctx context.Context, source, dest, base string, timeout time.Duration) (string, error) {
	if base == "fixture" {
		return "", errors.New("bench: base_commit=fixture is only valid with fixture")
	}
	// Clone rather than worktree-add: the source may be another repository and
	// a benchmark run must not add registrations/branches to it.
	if err := runGit(ctx, timeout, filepath.Dir(dest), "clone", "--no-hardlinks", "--quiet", source, dest); err != nil {
		return "", fmt.Errorf("bench: clone repository %s: %w", source, err)
	}
	if err := runGit(ctx, timeout, dest, "checkout", "--detach", "--quiet", base); err != nil {
		return "", fmt.Errorf("bench: checkout base %s: %w", base, err)
	}
	return gitOutput(ctx, timeout, dest, "rev-parse", "HEAD")
}

func checkConfiguredBase(t Task, source, dest, configured, resolved string) error {
	switch {
	case configured == "fixture":
		if t.Fixture == "" {
			return errors.New("bench: base_commit=fixture requires a fixture")
		}
		return nil
	case strings.HasPrefix(configured, "content-sha256:"):
		want := strings.TrimPrefix(configured, "content-sha256:")
		got, err := ContentManifest(dest)
		if err != nil {
			return err
		}
		got = strings.TrimPrefix(got, "candidate-")
		matches := strings.EqualFold(want, got)
		// Accept the old 128-bit fixture identity on input while emitting the
		// full SHA-256 identity everywhere new. This keeps old task files
		// readable without weakening new manifests.
		if len(want) == 32 && len(got) >= 32 {
			matches = strings.EqualFold(want, got[:32])
		}
		if !matches {
			return fmt.Errorf("bench: fixture content hash %s does not match configured %s", got, want)
		}
		return nil
	case configured == "":
		return errors.New("bench: no base commit configured")
	default:
		// A normal git object id is exact. Do not accept a prefix or a dirty
		// tree as a substitute for the immutable revision named by the task.
		if !isHexObjectID(configured) {
			return fmt.Errorf("bench: base_commit %q is not a git object id, fixture, or content-sha256:<hash>", configured)
		}
		if !strings.EqualFold(configured, resolved) {
			return fmt.Errorf("bench: checked out %s but task configured base %s", resolved, configured)
		}
		return nil
	}
}

func isHexObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func runSetup(ctx context.Context, command Command, dir string, env map[string]string, networkPolicy string, runner sandbox.Runner, timeout time.Duration) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := command.Argv
	if command.Shell {
		argv = append([]string{"/bin/sh", "-c"}, argv...)
	}
	tmp := filepath.Join(dir, ".bench-setup-tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var cmd *exec.Cmd
	if runner != nil {
		network, err := evaluatorNetwork(networkPolicy)
		if err != nil {
			return fmt.Errorf("setup: %w", err)
		}
		spec := sandbox.Spec{
			Network: network, ReadOnly: benchmarkReadOnlyPaths(),
			ReadWrite: uniqueSorted([]string{dir, tmp}), Dir: dir, TmpDir: tmp,
			Env: safeCommandEnvAt(env, tmp),
		}
		if err := spec.Validate(); err != nil {
			return err
		}
		cmd, err = runner.Command(c, spec, argv...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.CommandContext(c, argv[0], argv[1:]...) //nolint:gosec // argv comes from a frozen task definition
		cmd.Dir = dir
		cmd.Env = safeCommandEnvAt(env, tmp)
	}
	output := newCappedBuffer(1 << 20)
	cmd.Stdout, cmd.Stderr = output, output
	runErr := runProcess(c, cmd)
	if c.Err() != nil {
		return fmt.Errorf("setup timed out: %w", c.Err())
	}
	if output.Overflowed() {
		return errors.New("setup output exceeded the configured limit")
	}
	if runErr != nil {
		return fmt.Errorf("setup failed: %w: %s", runErr, strings.TrimSpace(RedactText(output.String())))
	}
	return nil
}

// safeCommandEnv is the compatibility wrapper for callers that use the host
// temporary directory as their isolated HOME.
//
//nolint:unused
func safeCommandEnv(extra map[string]string) []string {
	return safeCommandEnvAt(extra, os.TempDir())
}

func safeCommandEnvAt(extra map[string]string, home string) []string {
	env := []string{
		"HOME=" + home, "TMPDIR=" + home, "PATH=" + os.Getenv("PATH"),
		"LANG=C", "LC_ALL=C", "NO_COLOR=1", "TERM=dumb",
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !validEnvName(k) || reservedEnvName(k) {
			continue
		}
		env = append(env, k+"="+extra[k])
	}
	return env
}

func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func benchmarkGitEnv(extra []string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"), "LANG=C", "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_ATTR_NOSYSTEM=1",
	}
	return append(env, extra...)
}

func gitOutput(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args = append([]string{"-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(c, "git", args...) //nolint:gosec // fixed git arguments
	cmd.Dir = dir
	cmd.Env = benchmarkGitEnv(nil)
	output := newCappedBuffer(1 << 20)
	cmd.Stdout, cmd.Stderr = output, output
	err := runProcess(c, cmd)
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return strings.TrimSpace(output.String()), nil
}

func gitDirty(ctx context.Context, timeout time.Duration, dir string) (bool, error) {
	if err := validateCandidateGitDir(dir); err != nil {
		return false, err
	}
	out, err := gitOutput(ctx, timeout, dir, "status", "--porcelain")
	return out != "", err
}

func runGit(ctx context.Context, timeout time.Duration, dir string, args ...string) error {
	_, err := gitOutput(ctx, timeout, dir, args...)
	return err
}

func runGitEnv(ctx context.Context, timeout time.Duration, dir string, env []string, args ...string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args = append([]string{"-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(c, "git", args...) //nolint:gosec // fixed git arguments
	cmd.Dir = dir
	cmd.Env = benchmarkGitEnv(env)
	output := newCappedBuffer(1 << 20)
	cmd.Stdout, cmd.Stderr = output, output
	err := runProcess(c, cmd)
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return nil
}

// ContentManifest is a path-and-bytes identity independent of timestamps,
// inode numbers, and git index state. It is the benchmark's candidate hash.
func ContentManifest(root string) (string, error) {
	return ContentManifestContext(context.Background(), root)
}

func ContentManifestContext(ctx context.Context, root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// Never follow a repository-control path supplied by a worker. A
		// symlink here could make every later Git/content inspection address
		// files outside the disposable candidate.
		if rel == ".git" {
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("bench: candidate contains symlink %s", p)
			}
			if !d.IsDir() {
				return fmt.Errorf("bench: candidate .git is not a directory")
			}
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", p)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("bench: candidate contains non-regular file %s", rel)
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // path derived from candidate root
		if err != nil {
			return "", err
		}
		info, infoErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if infoErr != nil {
			return "", infoErr
		}
		fmt.Fprintf(h, "%d:%s=%d:%o:", len(rel), rel, len(body), info.Mode().Perm())
		h.Write(body)
	}
	return "candidate-" + hex.EncodeToString(h.Sum(nil)), nil
}

func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bench: fixture source %s is a symlink", src)
	}
	if !info.IsDir() {
		return fmt.Errorf("bench: copy source %s is not a directory", src)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("bench: fixture contains symlink %s", rel)
			}
			if !d.IsDir() {
				return fmt.Errorf("bench: fixture .git is not a directory")
			}
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: fixture contains symlink %s", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("bench: fixture contains non-regular file %s", rel)
		}
		body, err := os.ReadFile(p) //nolint:gosec // path is walked beneath src
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		entryInfo, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		mode := entryInfo.Mode().Perm()
		if mode == 0 {
			mode = 0o640
		}
		return os.WriteFile(target, body, mode) //nolint:gosec // scratch candidate
	})
}

func rejectOracleInHistory(ctx context.Context, dir string, t Task) error {
	if err := validateEvaluatorMaterial(t); err != nil {
		return err
	}
	if err := validateCandidateGitDir(dir); err != nil {
		return err
	}
	declared := make(map[string]bool, len(t.hiddenFiles))
	for name := range t.hiddenFiles {
		declared[name] = true
	}
	names := make([]string, 0, len(declared)+len(t.oracleFiles))
	for name := range declared {
		names = append(names, name)
	}
	for name := range t.oracleFiles {
		if !declared[name] {
			names = append(names, name)
		}
	}
	for _, canary := range t.Evaluator.Canaries {
		if canary == "" {
			continue
		}
		out, err := gitOutput(ctx, 30*time.Second, dir, "log", "--all", "-S"+canary, "--format=%H", "--")
		if err != nil {
			return fmt.Errorf("bench: inspect evaluator canary history: %w", err)
		}
		if strings.TrimSpace(out) != "" {
			return errors.New("bench: evaluator canary is present in candidate git history")
		}
	}
	for _, name := range sortStrings(names) {
		if name == "" {
			continue
		}
		// A file in the base tree would be visible even if the evaluator did
		// not copy it later. --all catches an accidental historical copy.
		out, err := gitOutput(ctx, 30*time.Second, dir, "log", "--all", "--format=", "--name-only", "--", name)
		if err != nil {
			return fmt.Errorf("bench: inspect evaluator file history %s: %w", name, err)
		}
		if strings.TrimSpace(out) == "" {
			continue
		}
		if declared[name] {
			return fmt.Errorf("bench: evaluator file %s is present in candidate git history", name)
		}
		// Auxiliary oracle files (for example an evaluator-only go.mod) may
		// share a path with a legitimate source file. Inspect historical blobs,
		// not only the current checkout: an older commit can still expose the
		// oracle to a RAW worker through git show.
		if body, ok := t.oracleFiles[name]; ok {
			commits, commitErr := gitOutput(ctx, 30*time.Second, dir, "log", "--all", "--format=%H", "--", name)
			if commitErr != nil {
				return fmt.Errorf("bench: inspect evaluator history %s: %w", name, commitErr)
			}
			for _, commit := range strings.Fields(commits) {
				blob, blobErr := gitOutput(ctx, 30*time.Second, dir, "show", commit+":"+name)
				if blobErr != nil {
					return fmt.Errorf("bench: read evaluator history %s: %w", name, blobErr)
				}
				if bytes.Equal([]byte(blob), body) {
					return fmt.Errorf("bench: evaluator file %s is present in candidate git history", name)
				}
			}
			continue
		}
		return fmt.Errorf("bench: evaluator file %s is present in candidate git history", name)
	}
	return nil
}

// AssertNoOracle checks the actual worker view, not merely the task object.
func leakedCanary(t Task, text string) string {
	for _, canary := range t.Evaluator.Canaries {
		if canary != "" && strings.Contains(text, canary) {
			return canary
		}
	}
	return ""
}

func AssertNoOracle(t Task, root string) error {
	if err := validateEvaluatorMaterial(t); err != nil {
		return err
	}
	declared := make(map[string]bool, len(t.hiddenFiles))
	for name := range t.hiddenFiles {
		declared[name] = true
	}
	// Every file in the oracle snapshot is evaluator-only, not just the files
	// explicitly listed under evaluator.files. The latter controls which files
	// are overlaid into the evaluator workspace; it must not accidentally make
	// an unlisted oracle helper visible to the worker. An auxiliary oracle path
	// may coexist with a different legitimate source file; identical bytes are
	// treated as evaluator material.
	for name := range t.hiddenFiles {
		full := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("bench: evaluator file %s is visible in worker workspace", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for name, body := range t.oracleFiles {
		if declared[name] {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(name))
		current, err := os.ReadFile(full) //nolint:gosec // name came from the oracle snapshot
		if err == nil {
			if bytes.Equal(current, body) {
				return fmt.Errorf("bench: evaluator file %s is visible in worker workspace", name)
			}
			// A source file with the same path but different bytes is not the
			// evaluator material; the evaluator copy will overwrite it only in
			// the independent workspace.
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	// A directory named oracle is not automatically a leak (a project may have
	// one), but a benchmark oracle path is forbidden and easy to detect.
	if t.OracleRoot() != "" {
		if inside, err := pathWithin(comparablePath(root), comparablePath(t.OracleRoot())); err != nil {
			return err
		} else if inside {
			return errors.New("bench: evaluator oracle is inside worker workspace")
		}
	}
	if leaked, err := candidateContainsMaterial(root, t.hiddenFiles); err != nil {
		return err
	} else if leaked {
		return errors.New("bench: evaluator material is visible in worker workspace under another path")
	}
	if len(t.Evaluator.Canaries) > 0 {
		found, err := candidateContainsCanary(root, t.Evaluator.Canaries)
		if err != nil {
			return err
		}
		if found {
			return errors.New("bench: hidden evaluator canary is visible in worker workspace")
		}
	}
	return nil
}

func candidateContainsMaterial(root string, material map[string][]byte) (bool, error) {
	if len(material) == 0 {
		return false, nil
	}
	found := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", path)
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if found || !d.Type().IsRegular() {
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // path is walked beneath candidate
		if readErr != nil {
			return readErr
		}
		for _, hidden := range material {
			if len(hidden) > 0 && bytes.Contains(body, hidden) {
				found = true
				break
			}
		}
		return nil
	})
	return found, err
}

func candidateContainsCanary(root string, canaries []string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", path)
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if found || !d.Type().IsRegular() {
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // path is walked beneath candidate
		if readErr != nil {
			return readErr
		}
		for _, canary := range canaries {
			if canary != "" && strings.Contains(string(body), canary) {
				found = true
				break
			}
		}
		return nil
	})
	return found, err
}

// GitDiff returns the candidate patch against its immutable base, including
// untracked files. It stages only the disposable candidate, never the source.
func GitDiff(ctx context.Context, dir, base string) (string, error) {
	if err := validateCandidateGitDir(dir); err != nil {
		return "", err
	}
	if err := runGit(ctx, 30*time.Second, dir, "add", "-A", "-f"); err != nil {
		return "", err
	}
	return gitOutput(ctx, 30*time.Second, dir, "diff", "--cached", "--binary", base)
}

// GitInspectionFingerprint captures the repository metadata that can change
// the meaning of harness-side git inspection (configuration, attributes, and
// hooks). A raw worker is not allowed to rewrite that trust boundary; ordinary
// index churn is deliberately not included because the harness stages the
// candidate again after the worker exits.
func validateCandidateGitDir(dir string) error {
	root := filepath.Join(dir, ".git")
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bench: candidate .git is a symlink")
	}
	if !info.IsDir() {
		return fmt.Errorf("bench: candidate .git is not a directory")
	}
	return nil
}

func GitInspectionFingerprint(dir string) (string, error) {
	if err := validateCandidateGitDir(dir); err != nil {
		return "", err
	}
	root := filepath.Join(dir, ".git")
	h := sha256.New()
	for _, rel := range []string{"config", "config.worktree", "attributes", filepath.Join("info", "attributes"), "hooks"} {
		path := filepath.Join(root, rel)
		entry, statErr := os.Lstat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return "", statErr
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("bench: git metadata contains symlink %s", path)
		}
		if entry.IsDir() {
			if err := filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
					return fmt.Errorf("bench: git metadata contains non-regular file %s", p)
				}
				body, readErr := os.ReadFile(p) //nolint:gosec // path is beneath .git metadata
				if readErr != nil {
					return readErr
				}
				name, _ := filepath.Rel(root, p)
				fmt.Fprintf(h, "%d:%s=%d:", len(name), filepath.ToSlash(name), len(body))
				h.Write(body)
				return nil
			}); err != nil {
				return "", err
			}
			continue
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // path is beneath .git metadata
		if readErr != nil {
			return "", readErr
		}
		fmt.Fprintf(h, "%d:%s=%d:", len(rel), filepath.ToSlash(rel), len(body))
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func VerifyBaseCommit(ctx context.Context, dir, expected string) error {
	return VerifyBaseRevision(ctx, dir, expected, "", true)
}

// VerifyBaseRevision proves that the candidate still contains the exact
// materialized base object. RAW must still be checked out at that object;
// BOUNDED may have advanced HEAD on its task branch, but that branch must
// descend from the same base and the base tree must be unchanged.
func VerifyBaseRevision(ctx context.Context, dir, expectedCommit, expectedTree string, raw bool) error {
	if err := validateCandidateGitDir(dir); err != nil {
		return err
	}
	if !isHexObjectID(expectedCommit) {
		return fmt.Errorf("bench: invalid materialized base revision %q", expectedCommit)
	}
	resolved, err := gitOutput(ctx, 30*time.Second, dir, "rev-parse", expectedCommit+"^{commit}")
	if err != nil || !strings.EqualFold(resolved, expectedCommit) {
		if err == nil {
			err = fmt.Errorf("resolved %s", resolved)
		}
		return fmt.Errorf("bench: worker removed or changed the immutable base revision: %w", err)
	}
	if expectedTree != "" {
		tree, treeErr := gitOutput(ctx, 30*time.Second, dir, "rev-parse", expectedCommit+"^{tree}")
		if treeErr != nil || !strings.EqualFold(tree, expectedTree) {
			if treeErr == nil {
				treeErr = fmt.Errorf("resolved tree %s, expected %s", tree, expectedTree)
			}
			return fmt.Errorf("bench: immutable base tree changed: %w", treeErr)
		}
	}
	head, err := gitOutput(ctx, 30*time.Second, dir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("bench: worker left no inspectable base revision: %w", err)
	}
	if raw && !strings.EqualFold(head, expectedCommit) {
		return fmt.Errorf("bench: worker changed the immutable base revision: got %s, expected %s", head, expectedCommit)
	}
	if !raw {
		if err := runGit(ctx, 30*time.Second, dir, "merge-base", "--is-ancestor", expectedCommit, head); err != nil {
			return fmt.Errorf("bench: bounded candidate is not descended from immutable base %s: %w", expectedCommit, err)
		}
	}
	return nil
}

func GitChangedFiles(ctx context.Context, dir, base string) ([]string, error) {
	if err := validateCandidateGitDir(dir); err != nil {
		return nil, err
	}
	if err := runGit(ctx, 30*time.Second, dir, "add", "-A", "-f"); err != nil {
		return nil, err
	}
	out, err := gitOutput(ctx, 30*time.Second, dir, "diff", "--cached", "--name-only", base)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	sort.Strings(files)
	return files, nil
}

// CopyDirectory copies a candidate for the independent evaluator without ever
// writing into the worker's directory.
func CopyDirectory(src, dst string) error {
	return CopyDirectoryContext(context.Background(), src, dst)
}

func CopyDirectoryContext(ctx context.Context, src, dst string) error {
	srcInfo, srcErr := os.Lstat(src)
	if srcErr != nil {
		return srcErr
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bench: copy source %s is a symlink", src)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("bench: copy source %s is not a directory", src)
	}
	if info, err := os.Lstat(dst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: copy destination is a symlink: %s", dst)
		}
		if !info.IsDir() {
			return fmt.Errorf("bench: copy destination %s is not a directory", dst)
		}
		entries, readErr := os.ReadDir(dst)
		if readErr != nil {
			return readErr
		}
		if len(entries) != 0 {
			return fmt.Errorf("bench: copy destination %s is not empty", dst)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("bench: candidate contains symlink %s", p)
			}
			if !d.IsDir() {
				return fmt.Errorf("bench: candidate .git is not a directory")
			}
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", p)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("bench: candidate contains non-regular file %s", rel)
		}
		body, err := os.ReadFile(p) //nolint:gosec // path beneath candidate
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o750); err != nil {
			return err
		}
		entryInfo, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		mode := entryInfo.Mode().Perm()
		if mode == 0 {
			mode = 0o640
		}
		return os.WriteFile(filepath.Join(dst, rel), body, mode) //nolint:gosec // evaluator scratch
	})
}

func copyFile(dst string, body []byte) error {
	return copyFileMode(dst, body, 0o640)
}

func copyFileMode(dst string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o640
	}
	return os.WriteFile(dst, body, mode) //nolint:gosec // evaluator scratch
}

// ioCopy is retained for small evaluator helpers that need a named copy.
//
//nolint:unused
func ioCopy(dst io.Writer, src io.Reader) (int64, error) { return io.Copy(dst, src) }
