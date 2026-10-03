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
	"strings"
)

// Run executes git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
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
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
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
	args := []string{"diff", "--no-color", "--no-ext-diff"}
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
		nd, err := runAllowExit1(ctx, worktree, "diff", "--no-color", "--no-index", "/dev/null", f)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + nd)
	}
	return b.String(), nil
}

func runAllowExit1(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
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
	if _, err := Run(ctx, worktree, "-c", "user.name=boundedcode-agent", "-c", "user.email=agent@boundedcode.invalid",
		"commit", "-q", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return Run(ctx, worktree, "rev-parse", "HEAD")
}
