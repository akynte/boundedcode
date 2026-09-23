"""A cleanup guard for Evidence Suite v1's benchmark tooling.

Why this exists: an earlier preparation session ran `rm -rf` on a path that
included the repository's pre-existing `evals/` directory instead of only
its own scratch subfolder — a near-miss caught before commit, but only by
accident (a `git status` run for an unrelated reason, several turns later).
Every destructive filesystem operation this suite's tooling performs must
go through `safe_remove_all` from here on, so a mistake in *which* path was
typed cannot become a mistake in *what got deleted*.

The approved root is `RUN_ROOT`
(`.boundedcode-runs/evidence-v1/` at the repository root, created on
first use). Nothing this suite's tooling deletes may live outside it.
"""
from __future__ import annotations

import os
import shutil
import sys
from pathlib import Path

# The repository root, resolved once, canonically, at import time — not
# re-derived from a relative path each call, which is exactly the kind of
# thing a symlink or a changed cwd could quietly bend.
REPO_ROOT = Path(__file__).resolve().parents[3]
assert (REPO_ROOT / "go.mod").exists(), (
    f"safe_cleanup.py's REPO_ROOT resolved to {REPO_ROOT}, which has no go.mod; "
    "the parents[3] offset from this file's location is wrong for the current layout"
)

def _run_root() -> Path:
    """The scratch root this checkout actually uses.

    The directory was named .local-engineer-runs before the project was
    renamed. A checkout that still has one keeps using it, matching
    evidenceRunRoot() on the Go side. The two must agree: this is the guard
    that decides what may be deleted, and a guard pointing at a directory
    nothing writes to would leave the directory that is in use unguarded.

    Keyed on the legacy evidence directory rather than on whether the new
    parent exists, because an empty .boundedcode-runs/ appears as soon as
    any test touches the root.
    """
    legacy = REPO_ROOT / ".local-engineer-runs" / "evidence-v1"
    if legacy.is_dir():
        return legacy
    return REPO_ROOT / ".boundedcode-runs" / "evidence-v1"


RUN_ROOT = _run_root()


class UnsafeCleanupError(Exception):
    """Raised when a cleanup target is not strictly beneath the allowed root."""


def _resolve_strict(path: os.PathLike | str) -> Path:
    """Resolve to a canonical absolute path, following symlinks, without
    requiring the path to exist (a target about to be deleted may not
    exist yet, or may be a dangling symlink — both must still resolve to
    somewhere checkable rather than raising)."""
    p = Path(path)
    if not p.is_absolute():
        p = Path(os.getcwd()) / p
    # os.path.realpath resolves symlinks AND normalizes '..'/'.' components,
    # which is the property a string-prefix check cannot give: a target like
    # "<run_root>/x/../../../evals" has the run root as a string prefix but
    # resolves outside it, and a symlink inside the run root that points
    # outside it resolves outside it too. strict=False so this also works
    # for a target that does not exist yet.
    return Path(os.path.realpath(p, strict=False))


def is_within_allowed_root(target: os.PathLike | str, allowed_root: os.PathLike | str) -> bool:
    """True only if target's canonical path is allowed_root itself or
    strictly beneath it. Never true for allowed_root's own parent, for an
    empty path, or for a path that merely starts with the same string."""
    if target is None:
        return False
    target_str = str(target)
    if target_str.strip() == "":
        return False

    resolved_target = _resolve_strict(target)
    resolved_root = _resolve_strict(allowed_root)

    if resolved_root == resolved_root.anchor or str(resolved_root) in ("/", ""):
        # An allowed_root that resolves to the filesystem root is a caller
        # bug, not a permission to delete everything under it.
        return False

    try:
        resolved_target.relative_to(resolved_root)
    except ValueError:
        return False
    return True


def safe_remove_all(target: os.PathLike | str, allowed_root: os.PathLike | str = RUN_ROOT) -> None:
    """Recursively remove `target`, refusing unless it resolves strictly
    beneath `allowed_root`.

    Deliberately refuses to delete `allowed_root` itself, too: a cleanup
    call for the run root's *contents* should name a subdirectory, and a
    caller that passed the root by mistake (e.g. an unset/empty subpath
    that collapsed to the root) gets the same refusal a path traversal
    would, rather than silently wiping every task's data at once.
    """
    if not is_within_allowed_root(target, allowed_root):
        raise UnsafeCleanupError(
            f"refusing to remove {target!r}: it does not resolve to a location "
            f"strictly beneath the approved benchmark scratch root "
            f"({allowed_root!r}). This is the guard described in "
            f"docs/evidence/evidence-v1.md's Cleanup safety section — if you "
            f"actually need to remove something outside the run root, do it "
            f"by hand, deliberately, never through this function."
        )
    resolved_target = _resolve_strict(target)
    resolved_root = _resolve_strict(allowed_root)
    if resolved_target == resolved_root:
        raise UnsafeCleanupError(
            f"refusing to remove the run root itself ({allowed_root!r}); "
            "name a subdirectory of it instead."
        )
    if not resolved_target.exists():
        return  # already gone; not an error — a cleanup step may run twice.
    shutil.rmtree(resolved_target)


def ensure_run_root() -> Path:
    """Create RUN_ROOT if absent and return it. Every benchmark scratch
    workspace, cloned repository, evaluator state directory, extracted
    dataset cache, and log this suite's tooling produces must live under a
    subdirectory of this, per design instruction §1/§29."""
    RUN_ROOT.mkdir(parents=True, exist_ok=True)
    return RUN_ROOT


def task_workspace(task_id: str, kind: str) -> Path:
    """A fresh, namespaced scratch directory for one task under one kind
    of use ("control", "jev-assisted", "oracle", "baseline"), created if
    absent. Two arms of the same task never share a directory — see
    docs/evidence/evidence-v1.md's Arm isolation section — because they
    are different subdirectories by construction, not by a convention a
    caller could forget."""
    if not task_id or "/" in task_id or ".." in task_id:
        raise UnsafeCleanupError(f"unsafe task_id for a workspace path: {task_id!r}")
    d = ensure_run_root() / kind / task_id
    d.mkdir(parents=True, exist_ok=True)
    return d


if __name__ == "__main__":  # pragma: no cover - manual smoke use only
    print("RUN_ROOT:", RUN_ROOT)
    print("exists:", RUN_ROOT.exists())
    sys.exit(0)
