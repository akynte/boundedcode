// Package gitops wraps the git CLI for the operations the control plane
// needs: repository inspection, per-task worktrees, diffs and local commits.
// It never pushes, force-pushes, resets or merges.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// hardening disables every git mechanism that executes repository-controlled
// commands on the host. Worktrees are written by a sandboxed agent, so the
// host must never run hooks, fsmonitor daemons or external diff/pager
// programs on their behalf. (External diff drivers and textconv are disabled
// per command with --no-ext-diff/--no-textconv.)
var hardening = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "core.pager=cat",
	"-c", "protocol.ext.allow=never",
	"-c", "uploadpack.packObjectsHook=",
}

// Run executes git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...), args...)...)
	cmd.Dir = dir
	// Never prompt for credentials; never use the user's pager/editor.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_EDITOR=true", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// RepoInfo describes a repository.
type RepoInfo struct {
	Root          string `json:"root"`
	Origin        string `json:"origin"`
	Branch        string `json:"branch"`
	DefaultBranch string `json:"default_branch"`
	Head          string `json:"head"`
	Dirty         bool   `json:"dirty"`
}

// Inspect reads repository metadata for the repo containing path.
func Inspect(ctx context.Context, path string) (RepoInfo, error) {
	root, err := Run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return RepoInfo{}, fmt.Errorf("%s is not inside a git repository: %w", path, err)
	}
	info := RepoInfo{Root: root}
	info.Origin, _ = Run(ctx, root, "remote", "get-url", "origin")
	info.Branch, _ = Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	info.Head, _ = Run(ctx, root, "rev-parse", "HEAD")
	if ref, err := Run(ctx, root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		info.DefaultBranch = strings.TrimPrefix(ref, "origin/")
	} else {
		info.DefaultBranch = info.Branch
	}
	st, _ := Run(ctx, root, "status", "--porcelain")
	info.Dirty = st != ""
	return info, nil
}

// CommonDir returns the absolute git common directory (shared by worktrees).
func CommonDir(ctx context.Context, path string) (string, error) {
	d, err := Run(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Clean(d), nil
}

// TaskBranch is the branch name for a task.
func TaskBranch(taskID string) string { return "agent/" + taskID }

// EnsureWorktree creates (or reuses) a worktree at path on branch, starting
// from base. It is idempotent so a crashed task can be resumed.
func EnsureWorktree(ctx context.Context, repo, path, branch, base string) error {
	if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
		// The worktree exists and is agent-writable: verify it before any
		// host git command runs inside it.
		common, err := CommonDir(ctx, repo)
		if err != nil {
			return err
		}
		if err := CheckTaskWorktree(path, common, branch); err != nil {
			return err
		}
		cur, err := Run(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return err
		}
		if cur != branch {
			return fmt.Errorf("worktree %s is on %s, expected %s", path, cur, branch)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = Run(ctx, repo, "worktree", "add", path, branch)
		return err
	}
	_, err := Run(ctx, repo, "worktree", "add", "-b", branch, path, base)
	return err
}

// RemoveWorktree removes a task worktree. The branch is kept: work is never
// discarded implicitly.
func RemoveWorktree(ctx context.Context, repo, path string) error {
	_, err := Run(ctx, repo, "worktree", "remove", "--force", path)
	return err
}

// ChangedFiles lists files changed relative to base, including uncommitted
// and untracked files.
func ChangedFiles(ctx context.Context, worktree, base string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for l := range strings.SplitSeq(s, "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	d, err := Run(ctx, worktree, "diff", "--name-only", base)
	if err != nil {
		return nil, err
	}
	add(d)
	u, err := Run(ctx, worktree, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	add(u)
	return out, nil
}

// Diff returns the patch of the working tree relative to base, including
// untracked files (via intent-to-add on a temporary index).
func Diff(ctx context.Context, worktree, base string, stat bool) (string, error) {
	untracked, err := Run(ctx, worktree, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv"}
	if stat {
		args = append(args, "--stat")
	}
	args = append(args, base)
	d, err := Run(ctx, worktree, args...)
	if err != nil {
		return "", err
	}
	if untracked == "" {
		return d, nil
	}
	var b strings.Builder
	b.WriteString(d)
	for f := range strings.SplitSeq(untracked, "\n") {
		if f == "" {
			continue
		}
		if stat {
			fmt.Fprintf(&b, "\n %s (new, untracked)", f)
			continue
		}
		nd, err := runAllowExit1(ctx, worktree, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-index", "/dev/null", f)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + nd)
	}
	return b.String(), nil
}

func runAllowExit1(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...), args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return string(out), nil
	}
	return string(out), err
}

// CommitAll commits every change in the worktree on its current branch. It
// refuses to commit on a non-agent branch.
func CommitAll(ctx context.Context, worktree, message string) (string, error) {
	br, err := Run(ctx, worktree, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(br, "agent/") {
		return "", fmt.Errorf("refusing to commit on non-agent branch %q", br)
	}
	if _, err := Run(ctx, worktree, "add", "-A"); err != nil {
		return "", err
	}
	if st, _ := Run(ctx, worktree, "status", "--porcelain"); st == "" {
		return Run(ctx, worktree, "rev-parse", "HEAD")
	}
	if _, err := Run(ctx, worktree, "-c", "user.name=boundedcode-agent", "-c", "user.email=agent@boundedcode.invalid", "-c", "commit.gpgSign=false",
		"commit", "-q", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return Run(ctx, worktree, "rev-parse", "HEAD")
}

// AdminDir returns the worktree's private git directory
// (<common>/worktrees/<name>), which holds its HEAD and index.
func AdminDir(ctx context.Context, worktree string) (string, error) {
	d, err := Run(ctx, worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return filepath.Clean(d), nil
}

// CheckWorktree verifies that worktree's .git pointer still refers to an
// admin directory inside the repository's common dir, and that the admin
// directory (writable by the sandboxed agent, because it holds HEAD and the
// index) still points back at that common dir and carries no per-worktree
// config. Either redirect would let the agent supply a git config (filter
// drivers, gpg.program, ...) that host git would execute; we refuse to
// touch such a worktree.
func CheckWorktree(worktree, commonDir string) error {
	if fi, err := os.Lstat(filepath.Join(worktree, ".git")); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("worktree %s: .git is not a regular file (tampered?)", worktree)
	}
	b, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		return fmt.Errorf("worktree %s: .git pointer: %w", worktree, err)
	}
	ptr, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return fmt.Errorf("worktree %s: .git is not a gitdir pointer", worktree)
	}
	ptr = filepath.Clean(ptr)
	want := filepath.Join(filepath.Clean(commonDir), "worktrees") + string(filepath.Separator)
	if !strings.HasPrefix(ptr, want) || strings.Contains(strings.TrimPrefix(ptr, want), string(filepath.Separator)) {
		return fmt.Errorf("worktree %s: .git points to %s, outside %s (tampered?)", worktree, ptr, want)
	}
	if fi, err := os.Lstat(ptr); err != nil || !fi.IsDir() {
		return fmt.Errorf("worktree %s: admin dir %s is missing or not a directory (tampered?)", worktree, ptr)
	}
	cd, err := os.ReadFile(filepath.Join(ptr, "commondir"))
	if err != nil {
		return fmt.Errorf("worktree %s: admin commondir: %w", worktree, err)
	}
	target := strings.TrimSpace(string(cd))
	if !filepath.IsAbs(target) {
		target = filepath.Join(ptr, target)
	}
	if !sameDir(target, commonDir) {
		return fmt.Errorf("worktree %s: admin commondir points to %s, not %s (tampered?)", worktree, target, commonDir)
	}
	if _, err := os.Lstat(filepath.Join(ptr, "config.worktree")); err == nil {
		return fmt.Errorf("worktree %s: per-worktree config %s is not allowed", worktree, filepath.Join(ptr, "config.worktree"))
	}
	return nil
}

// CheckTaskWorktree is CheckWorktree plus a check that HEAD is still the
// task's own branch (the agent can rewrite HEAD in the admin dir).
func CheckTaskWorktree(worktree, commonDir, branch string) error {
	if err := CheckWorktree(worktree, commonDir); err != nil {
		return err
	}
	b, _ := os.ReadFile(filepath.Join(worktree, ".git"))
	admin := filepath.Clean(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir: "))
	head, err := os.ReadFile(filepath.Join(admin, "HEAD"))
	if err != nil {
		return fmt.Errorf("worktree %s: HEAD: %w", worktree, err)
	}
	if got, want := strings.TrimSpace(string(head)), "ref: refs/heads/"+branch; got != want {
		return fmt.Errorf("worktree %s: HEAD is %q, expected %q (tampered?)", worktree, got, want)
	}
	return nil
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

var (
	hunkHeaderRE = regexp.MustCompile(`^@@ [^@]* @@ ?(.*)$`)
	// declRE matches declarations in Go, TypeScript/JavaScript, Python, Java
	// and SQL well enough to name what a hunk touches.
	declRE = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?(?:` +
		`func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)` +
		`|type\s+([A-Za-z_]\w*)` +
		`|(?:abstract\s+)?(?:class|interface|enum)\s+([A-Za-z_]\w*)` +
		`|function\*?\s+([A-Za-z_$][\w$]*)` +
		`|(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*(?:async\s*)?(?:\(|function)` +
		`|def\s+([A-Za-z_]\w*)` +
		`|(?i:create\s+(?:table|index|view)(?:\s+if\s+not\s+exists)?)\s+([A-Za-z_][\w.]*)` +
		`)`)
	methodRE = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|async\s+|readonly\s+)*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*(?::[^{]*)?\{\s*$`)
)

// ChangedSymbols names the declarations a worktree's change touches,
// relative to base: the enclosing declaration of every hunk (from git's
// function context) and declarations added or removed in it. Qualified as
// "path:Name". Heuristic and language-agnostic; used for the ledger and for
// context packs, never for correctness decisions.
func ChangedSymbols(ctx context.Context, worktree, base string) ([]string, error) {
	// Zero context lines: the hunk header then names the declaration that
	// encloses the change, not one in the surrounding context.
	patch, err := Run(ctx, worktree, "diff", "-U0", "--no-color", "--no-ext-diff", "--no-textconv", base)
	if err != nil {
		return nil, err
	}
	untracked, err := Run(ctx, worktree, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for f := range strings.SplitSeq(untracked, "\n") {
		if f == "" {
			continue
		}
		nd, err := runAllowExit1(ctx, worktree, "diff", "-U0", "--no-color", "--no-ext-diff", "--no-textconv", "--no-index", "/dev/null", f)
		if err != nil {
			return nil, err
		}
		patch += "\n" + nd
	}
	seen := map[string]bool{}
	var out []string
	file := ""
	add := func(name string) {
		if name == "" || file == "" {
			return
		}
		k := file + ":" + name
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	// A hunk names the declarations it adds or removes; only a hunk without
	// any (a change inside a body) is attributed to its enclosing declaration
	// from the header (which, for an insertion, is the preceding one).
	header, hunkDecl := "", false
	flush := func() {
		if !hunkDecl {
			add(declName(header))
		}
		header, hunkDecl = "", false
	}
	for l := range strings.SplitSeq(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "diff "):
			flush()
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if file == "/dev/null" {
				file = ""
			}
		case strings.HasPrefix(l, "--- "):
			if f := strings.TrimPrefix(strings.TrimPrefix(l, "--- "), "a/"); f != "/dev/null" {
				file = f // a deleted file keeps its old name
			}
		case strings.HasPrefix(l, "@@"):
			flush()
			if m := hunkHeaderRE.FindStringSubmatch(l); m != nil {
				header = m[1]
			}
		case strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-"):
			if n := declName(l[1:]); n != "" {
				hunkDecl = true
				add(n)
			}
		}
	}
	flush()
	return out, nil
}

func declName(line string) string {
	if m := declRE.FindStringSubmatch(line); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	if m := methodRE.FindStringSubmatch(line); m != nil {
		switch m[1] {
		case "if", "for", "while", "switch", "catch", "return", "function":
			return ""
		}
		return m[1]
	}
	return ""
}

// DeleteTaskBranch deletes an agent/* branch. It is only called on explicit
// user request (task cleanup --delete-branch) and refuses any other branch.
func DeleteTaskBranch(ctx context.Context, repo, branch string) error {
	if !strings.HasPrefix(branch, "agent/") {
		return fmt.Errorf("refusing to delete non-agent branch %q", branch)
	}
	_, err := Run(ctx, repo, "branch", "-D", branch)
	return err
}

// PruneWorktrees drops administrative entries of removed worktrees.
func PruneWorktrees(ctx context.Context, repo string) error {
	_, err := Run(ctx, repo, "worktree", "prune")
	return err
}
