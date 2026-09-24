package bench

import (
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
)

// Candidate is the isolated repository a worker edits.  The path is never the
// operator checkout and is never reused by another RunSpec.
type Candidate struct {
	Path             string `json:"path"`
	Repository       string `json:"repository"`
	ConfiguredBase   string `json:"configured_base"`
	BaseCommit       string `json:"base_commit"`
	BaseTree         string `json:"base_tree"`
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
	if _, err := os.Stat(dest); err == nil {
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
		BaseTree: tree, StartingManifest: manifest, OracleOutside: true,
	}
	if !t.Setup.Empty() {
		if err := runSetup(ctx, t.Setup, dest, t.Environment, m.timeout()); err != nil {
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
	cleanup = false
	return c, nil
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
	if err := runGit(ctx, timeout, dir, "add", "-A"); err != nil {
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
		if !strings.EqualFold(want, got) {
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

func runSetup(ctx context.Context, command Command, dir string, env map[string]string, timeout time.Duration) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := command.Argv
	if command.Shell {
		argv = append([]string{"/bin/sh", "-c"}, argv...)
	}
	cmd := exec.CommandContext(c, argv[0], argv[1:]...) //nolint:gosec // argv comes from a frozen task definition
	cmd.Dir = dir
	cmd.Env = safeCommandEnv(env)
	out, err := cmd.CombinedOutput()
	if c.Err() != nil {
		return fmt.Errorf("setup timed out: %w", c.Err())
	}
	if err != nil {
		return fmt.Errorf("setup failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func safeCommandEnv(extra map[string]string) []string {
	env := []string{
		"HOME=" + os.TempDir(), "TMPDIR=" + os.TempDir(), "PATH=" + os.Getenv("PATH"),
		"LANG=C", "LC_ALL=C", "NO_COLOR=1", "TERM=dumb",
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !validEnvName(k) {
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

func gitOutput(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, "git", args...) //nolint:gosec // fixed git arguments
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDirty(ctx context.Context, timeout time.Duration, dir string) (bool, error) {
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
	cmd := exec.CommandContext(c, "git", args...) //nolint:gosec // fixed git arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ContentManifest is a path-and-bytes identity independent of timestamps,
// inode numbers, and git index state. It is the benchmark's candidate hash.
func ContentManifest(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bench: candidate contains symlink %s", rel)
		}
		if d.Type().IsRegular() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // path derived from candidate root
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%d:%s=%d:", len(rel), rel, len(body))
		h.Write(body)
	}
	return "candidate-" + hex.EncodeToString(h.Sum(nil))[:32], nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
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
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "oracle" || strings.HasPrefix(d.Name(), ".bench-oracle") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(p) //nolint:gosec // path is walked beneath src
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		mode := d.Type().Perm()
		if mode == 0 {
			mode = 0o640
		}
		return os.WriteFile(target, body, mode) //nolint:gosec // scratch candidate
	})
}

func rejectOracleInHistory(ctx context.Context, dir string, t Task) error {
	names := append([]string(nil), t.Evaluator.Files...)
	for name := range t.hiddenFiles {
		names = append(names, name)
	}
	names = sortStrings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		// A file in the base tree would be visible even if the evaluator did
		// not copy it later. --all catches an accidental historical copy.
		out, err := gitOutput(ctx, 30*time.Second, dir, "log", "--all", "--format=", "--name-only", "--", name)
		if err == nil && strings.TrimSpace(out) != "" {
			return fmt.Errorf("bench: evaluator file %s is present in candidate git history", name)
		}
	}
	return nil
}

// AssertNoOracle checks the actual worker view, not merely the task object.
func AssertNoOracle(t Task, root string) error {
	for name := range t.hiddenFiles {
		full := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("bench: evaluator file %s is visible in worker workspace", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	// A directory named oracle is not automatically a leak (a project may have
	// one), but a benchmark oracle path is forbidden and easy to detect.
	if t.OracleRoot() != "" {
		if inside, err := pathWithin(root, t.OracleRoot()); err != nil {
			return err
		} else if inside {
			return errors.New("bench: evaluator oracle is inside worker workspace")
		}
	}
	return nil
}

// GitDiff returns the candidate patch against its immutable base, including
// untracked files. It stages only the disposable candidate, never the source.
func GitDiff(ctx context.Context, dir, base string) (string, error) {
	if err := runGit(ctx, 30*time.Second, dir, "add", "-A"); err != nil {
		return "", err
	}
	return gitOutput(ctx, 30*time.Second, dir, "diff", "--cached", "--binary", base)
}

func GitChangedFiles(ctx context.Context, dir, base string) ([]string, error) {
	if err := runGit(ctx, 30*time.Second, dir, "add", "-A"); err != nil {
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
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
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
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(p) //nolint:gosec // path beneath candidate
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), body, 0o640) //nolint:gosec // evaluator scratch
	})
}

func copyFile(dst string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o640) //nolint:gosec // evaluator scratch
}

func ioCopy(dst io.Writer, src io.Reader) (int64, error) { return io.Copy(dst, src) }
