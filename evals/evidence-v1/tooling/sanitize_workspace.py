"""Sanitized Agent Workspace + Pristine Official Grading.

Why this exists: preflight found that all 4 selected SWE-Bench Pro Verified
container images ship their full, unrestricted upstream git history — every
future commit, including whichever one carries the gold patch, reachable
with `git log --all` and no network call. This module is the fix: it turns
a container's base-state working tree into a fresh, single-commit git
repository with no reachable or recoverable trace of that history, so an
agent workspace built from it cannot use `git log`/`git show`/`git cat-file`/
`git fsck` to reach the future, no matter how thoroughly it looks — while
official grading still happens against the *original*, pristine image and
its official evaluator, which the agent never has shell access to.

Two functions matter:

  - sanitize_git_repo(src, dest, ...) is the pure, Docker-free operation:
    copy a working tree (excluding .git), `git init` fresh, one commit.
    This is what the regression tests exercise directly against a
    constructed fixture repo — see test_sanitize_workspace.py.
  - verify_sanitized(dest, known_future_shas) runs every probe design
    instruction §6/§7/§26 lists and returns a structured, all-must-pass
    result. It is used both by the tests and by the real per-task
    materialization script below.

Everything this module writes lives under
safe_cleanup.RUN_ROOT/agent-workspaces/, via safe_cleanup.task_workspace,
so a sanitized workspace can never be created — or torn down — outside the
guarded scratch root.
"""
from __future__ import annotations

import hashlib
import os
import shutil
import stat
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable

import safe_cleanup

# A fixed, deterministic commit identity and timestamp: two sanitizations of
# the same working tree must produce the same commit hash, or a fingerprint
# comparison (design instruction §15) would fail for a reason that has
# nothing to do with the source tree actually differing.
GIT_AUTHOR_NAME = "Evidence Suite Sanitizer"
GIT_AUTHOR_EMAIL = "evidence-suite@localhost"
GIT_COMMIT_MESSAGE = "benchmark base snapshot"
GIT_COMMIT_EPOCH = "1700000000 +0000"  # arbitrary, fixed; not a real event time


class LeakageError(Exception):
    """Raised when a sanitized workspace fails an anti-leakage probe."""


def _run(args: list[str], cwd: Path, env: dict | None = None, check: bool = True) -> subprocess.CompletedProcess:
    full_env = dict(os.environ)
    if env:
        full_env.update(env)
    return subprocess.run(args, cwd=str(cwd), env=full_env, capture_output=True, text=True, check=check)


def sanitize_git_repo(src: Path, dest: Path, commit_message: str = GIT_COMMIT_MESSAGE) -> str:
    """Copy src's working tree (excluding any .git) into dest, then turn
    dest into a fresh single-commit repository. dest must not already
    exist. Returns the new HEAD commit hash.

    File contents, executable bits, and symlinks are preserved
    (shutil.copytree(..., symlinks=True) does not follow a symlink's
    target, and copystat preserves the mode bits copytree already copies
    by default) — design instruction §4's "preserve executable bits,
    symlinks" and §17's "symlink/executable preservation" test both check
    this directly.
    """
    src = Path(src)
    dest = Path(dest)
    if dest.exists():
        raise FileExistsError(f"sanitize target already exists: {dest}")
    if not safe_cleanup.is_within_allowed_root(dest, safe_cleanup.RUN_ROOT):
        raise safe_cleanup.UnsafeCleanupError(
            f"refusing to sanitize into {dest}: not beneath the guarded run root")

    dest.parent.mkdir(parents=True, exist_ok=True)

    def ignore_git(directory, names):
        return {n for n in names if n == ".git"}

    shutil.copytree(src, dest, symlinks=True, ignore=ignore_git)

    # Defense in depth: copytree's ignore already excludes .git at every
    # level, but a nested repository (a vendored copy, a submodule checked
    # out with its own .git) could still carry one a shallow ignore list
    # missed if copytree's traversal order ever changed. Sweep for any
    # remaining .git after the copy and remove it the same way §4 asks
    # ("do not preserve ... .git/modules ... worktree metadata").
    for git_dir in dest.rglob(".git"):
        safe_cleanup.safe_remove_all(git_dir, safe_cleanup.RUN_ROOT)

    env = {
        "GIT_AUTHOR_NAME": GIT_AUTHOR_NAME, "GIT_AUTHOR_EMAIL": GIT_AUTHOR_EMAIL,
        "GIT_COMMITTER_NAME": GIT_AUTHOR_NAME, "GIT_COMMITTER_EMAIL": GIT_AUTHOR_EMAIL,
        "GIT_AUTHOR_DATE": GIT_COMMIT_EPOCH, "GIT_COMMITTER_DATE": GIT_COMMIT_EPOCH,
        # No hooks: a sanitized tree's own (copied) .git/hooks is already
        # gone, but a global hooksPath must not run either.
        "GIT_CONFIG_NOSYSTEM": "1",
    }
    _run(["git", "init", "--quiet"], dest, env)
    _run(["git", "config", "core.hooksPath", os.devnull], dest, env)
    _run(["git", "config", "user.name", GIT_AUTHOR_NAME], dest, env)
    _run(["git", "config", "user.email", GIT_AUTHOR_EMAIL], dest, env)
    _run(["git", "add", "-A"], dest, env)
    _run(["git", "commit", "--quiet", "--no-verify", "-m", commit_message], dest, env)
    head = _run(["git", "rev-parse", "HEAD"], dest, env).stdout.strip()
    return head


@dataclass
class LeakageProbeResult:
    reachable_commits: int = 0
    branches: list[str] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    remotes: list[str] = field(default_factory=list)
    reflog_entries: int = 0
    future_sha_reachable: list[str] = field(default_factory=list)  # any that ARE reachable = leak
    unreachable_future_objects: list[str] = field(default_factory=list)  # any found = leak
    has_object_alternates: bool = False
    passed: bool = False
    failure_reasons: list[str] = field(default_factory=list)


def verify_sanitized(dest: Path, known_future_shas: Iterable[str] = ()) -> LeakageProbeResult:
    """Run every anti-leakage probe design instructions §6/§7/§26 list
    against a sanitized workspace and return a structured, all-must-pass
    result. known_future_shas are commit hashes captured from the
    *original* image before sanitization (see materialize_task.py) —
    verifying they are absent is the strongest test, because it is a
    positive check against a concrete artifact rather than an absence-only
    check that could pass by coincidence.
    """
    dest = Path(dest)
    r = LeakageProbeResult()

    def git(*args, check=False):
        return _run(["git", *args], dest, check=check)

    log_all = git("log", "--all", "--oneline")
    r.reachable_commits = len([l for l in log_all.stdout.splitlines() if l.strip()])

    branches = git("branch", "-a")
    r.branches = [b.strip().lstrip("* ") for b in branches.stdout.splitlines() if b.strip()]

    tags = git("tag")
    r.tags = [t for t in tags.stdout.splitlines() if t.strip()]

    remotes = git("remote", "-v")
    r.remotes = [l for l in remotes.stdout.splitlines() if l.strip()]

    # `git reflog --all` legitimately logs the one sanitization commit
    # twice — once under HEAD's own reflog, once under the current
    # branch's — so a raw line count is not the leak signal. The signal is
    # whether any reflog *entry names a commit hash other than the current
    # HEAD*: that would mean some other commit (an upstream one) passed
    # through a ref this repository once pointed at.
    head = git("rev-parse", "HEAD").stdout.strip()
    reflog = git("reflog", "--all")
    reflog_lines = [l for l in reflog.stdout.splitlines() if l.strip()]
    other_commits = {l.split()[0] for l in reflog_lines if not l.split()[0].startswith(head[:7])}
    r.reflog_entries = len(other_commits)

    for sha in known_future_shas:
        cat = git("cat-file", "-e", sha)
        if cat.returncode == 0:
            r.future_sha_reachable.append(sha)
        show = git("show", sha)
        if show.returncode == 0:
            if sha not in r.future_sha_reachable:
                r.future_sha_reachable.append(sha)

    # `git fsck --unreachable` lists loose objects still physically present
    # in .git/objects even though no ref points to them. A freshly
    # `git init`'d repository with exactly one commit and one `add` should
    # report none at all; any unreachable commit it does report is
    # suspicious content that has no business being there.
    fsck = git("fsck", "--full", "--unreachable", "--no-reflogs")
    unreachable_shas = set()
    for line in (fsck.stdout + fsck.stderr).splitlines():
        line = line.strip()
        if line.startswith("unreachable commit "):
            unreachable_shas.add(line.split()[-1])
    r.unreachable_future_objects = sorted(unreachable_shas)

    alt_file = dest / ".git" / "objects" / "info" / "alternates"
    r.has_object_alternates = alt_file.exists() and alt_file.read_text().strip() != ""

    reasons = []
    if r.reachable_commits != 1:
        reasons.append(f"expected exactly 1 reachable commit, found {r.reachable_commits}")
    if len(r.branches) > 1:
        reasons.append(f"expected at most 1 branch, found {r.branches}")
    if r.tags:
        reasons.append(f"expected no tags, found {r.tags}")
    if r.remotes:
        reasons.append(f"expected no remotes, found {r.remotes}")
    if r.reflog_entries > 0:
        reasons.append(f"reflog references {r.reflog_entries} commit(s) other than the "
                        f"sanitization commit itself — reflog leakage")
    if r.future_sha_reachable:
        reasons.append(f"future commit(s) reachable: {r.future_sha_reachable}")
    if r.unreachable_future_objects:
        reasons.append(f"unreachable future object(s) still present: {r.unreachable_future_objects}")
    if r.has_object_alternates:
        reasons.append("Objects/info/alternates points somewhere — an escape hatch to another "
                        "object database")

    r.passed = not reasons
    r.failure_reasons = reasons
    return r


def fingerprint_tree(root: Path) -> str:
    """A deterministic hash of a source tree's content, excluding .git: for
    every file (sorted, relative path), hash (path, mode, content-or-link-
    target). Two independently sanitized copies of the same base state
    must produce the same fingerprint — design instruction §15's
    control/treatment equality check."""
    root = Path(root)
    h = hashlib.sha256()
    for path in sorted(p for p in root.rglob("*") if ".git" not in p.parts):
        rel = path.relative_to(root).as_posix()
        if path.is_symlink():
            h.update(f"L {rel} -> {os.readlink(path)}\n".encode())
        elif path.is_dir():
            h.update(f"D {rel}\n".encode())
        elif path.is_file():
            mode = stat.S_IMODE(path.lstat().st_mode)
            exe = "x" if mode & stat.S_IXUSR else "-"
            h.update(f"F {rel} {exe} ".encode())
            h.update(hashlib.sha256(path.read_bytes()).digest())
            h.update(b"\n")
    return h.hexdigest()


def extract_patch(dest: Path) -> str:
    """The agent's changes as a single unified diff against the sanitized
    workspace's one baseline commit — design instruction §10. Suitable for
    application to the corresponding pristine official base state, since
    the sanitized tree's file contents at commit time are byte-identical
    to that base state (see fingerprint_tree / test_working_tree_fidelity)."""
    dest = Path(dest)
    out = _run(["git", "diff", "--binary", "HEAD"], dest)
    return out.stdout
