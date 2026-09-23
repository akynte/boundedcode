"""Regression tests for the sanitization implementation — design
instruction §17. No Docker and no network needed: every test builds its
own small fixture git repository with a base/future/solution commit, a
tag, a remote-tracking branch, and a reflog, sanitizes it, and probes the
result the way an adversarial reviewer would.

Run with: python3 -m unittest test_sanitize_workspace -v
"""
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

import safe_cleanup
import sanitize_workspace as sw


def git(args, cwd, env=None):
    full_env = dict(os.environ)
    full_env.setdefault("GIT_AUTHOR_NAME", "fixture")
    full_env.setdefault("GIT_AUTHOR_EMAIL", "fixture@localhost")
    full_env.setdefault("GIT_COMMITTER_NAME", "fixture")
    full_env.setdefault("GIT_COMMITTER_EMAIL", "fixture@localhost")
    if env:
        full_env.update(env)
    r = subprocess.run(["git", *args], cwd=str(cwd), env=full_env,
                        capture_output=True, text=True, check=True)
    return r.stdout.strip()


class FullHistoryFixture:
    """base commit -> future commit -> solution commit, plus a tag, a
    remote-tracking branch pointing at the solution, and reflog entries —
    the exact shape design instruction §17's "Full-history fixture" asks
    for."""

    def __init__(self, tmp: Path):
        self.dir = tmp / "upstream"
        self.dir.mkdir()
        git(["init", "--quiet"], self.dir)
        git(["config", "user.name", "fixture"], self.dir)
        git(["config", "user.email", "fixture@localhost"], self.dir)

        (self.dir / "app.py").write_text("print('base')\n")
        (self.dir / "README.md").write_text("base readme\n")
        script = self.dir / "run.sh"
        script.write_text("#!/bin/sh\necho base\n")
        script.chmod(script.stat().st_mode | stat.S_IEXEC)
        (self.dir / "link.txt").symlink_to("app.py")
        git(["add", "-A"], self.dir)
        git(["commit", "--quiet", "-m", "base commit"], self.dir)
        self.base_commit = git(["rev-parse", "HEAD"], self.dir)

        (self.dir / "app.py").write_text("print('future, unrelated change')\n")
        git(["add", "-A"], self.dir)
        git(["commit", "--quiet", "-m", "future commit"], self.dir)
        self.future_commit = git(["rev-parse", "HEAD"], self.dir)

        (self.dir / "app.py").write_text("print('the actual fix')\n")
        git(["add", "-A"], self.dir)
        git(["commit", "--quiet", "-m", "solution commit — fixes the bug"], self.dir)
        self.solution_commit = git(["rev-parse", "HEAD"], self.dir)

        git(["tag", "v1.0.0"], self.dir)
        git(["update-ref", "refs/remotes/origin/master", "HEAD"], self.dir)
        git(["remote", "add", "origin", "https://example.invalid/upstream.git"], self.dir)

        # Reset the working checkout back to the base commit (detached),
        # the way a benchmark image's base-state container would be:
        # HEAD is at base_commit, but the future/solution objects, the tag,
        # and the remote-tracking ref are all still present in the same
        # repository.
        git(["checkout", "--quiet", self.base_commit], self.dir)
        git(["reflog", "expire", "--expire=now", "--all"], self.dir, env={})  # realistic noise
        # Re-touch reflog with a benign entry so the fixture has *some*
        # reflog content to prove sanitization drops it too.
        git(["checkout", "--quiet", "-b", "work"], self.dir)


class TestSanitizeGitRepo(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.fixture = FullHistoryFixture(self.root)
        self.dest = safe_cleanup.RUN_ROOT / "test-sanitize" / self._testMethodName
        if self.dest.exists():
            safe_cleanup.safe_remove_all(self.dest, safe_cleanup.RUN_ROOT)

    def tearDown(self):
        self.tmp.cleanup()
        if self.dest.exists():
            safe_cleanup.safe_remove_all(self.dest, safe_cleanup.RUN_ROOT)

    def sanitize(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        return sw.verify_sanitized(self.dest, known_future_shas=[
            self.fixture.future_commit, self.fixture.solution_commit,
        ])

    def test_agent_repo_sees_only_the_synthetic_snapshot_commit(self):
        result = self.sanitize()
        self.assertEqual(result.reachable_commits, 1)
        self.assertTrue(result.passed, result.failure_reasons)

    def test_future_sha_rejection(self):
        result = self.sanitize()
        self.assertEqual(result.future_sha_reachable, [])
        for sha in (self.fixture.future_commit, self.fixture.solution_commit):
            cat = subprocess.run(["git", "cat-file", "-e", sha], cwd=str(self.dest),
                                  capture_output=True)
            self.assertNotEqual(cat.returncode, 0, f"{sha} must not be readable in the sanitized repo")
            show = subprocess.run(["git", "show", sha], cwd=str(self.dest), capture_output=True)
            self.assertNotEqual(show.returncode, 0, f"git show {sha} must fail in the sanitized repo")

    def test_reflog_does_not_survive(self):
        result = self.sanitize()
        # reflog_entries counts commits referenced in the reflog OTHER than
        # the sanitization's own HEAD commit; the sanitization's own commit
        # legitimately appears twice (HEAD's reflog + the branch's), which
        # is not leakage. Anything else would be.
        self.assertEqual(result.reflog_entries, 0)

    def test_no_original_tags(self):
        result = self.sanitize()
        self.assertEqual(result.tags, [])

    def test_no_remotes(self):
        result = self.sanitize()
        self.assertEqual(result.remotes, [])

    def test_no_unreachable_future_objects_via_fsck(self):
        result = self.sanitize()
        self.assertEqual(result.unreachable_future_objects, [])

    def test_no_object_alternates_escape_hatch(self):
        result = self.sanitize()
        self.assertFalse(result.has_object_alternates)

    def test_working_tree_fidelity(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        self.assertEqual((self.dest / "app.py").read_text(), "print('base')\n")
        self.assertEqual((self.dest / "README.md").read_text(), "base readme\n")

    def test_symlink_and_executable_bit_preserved(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        link = self.dest / "link.txt"
        self.assertTrue(link.is_symlink())
        self.assertEqual(os.readlink(link), "app.py")
        script = self.dest / "run.sh"
        mode = script.stat().st_mode
        self.assertTrue(mode & stat.S_IXUSR, "executable bit must survive sanitization")

    def test_no_git_directory_nested_or_otherwise_survives_the_copy(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        git_dirs = list(self.dest.rglob(".git"))
        # Exactly one — the freshly created one at the root.
        self.assertEqual(len(git_dirs), 1)
        self.assertEqual(git_dirs[0], self.dest / ".git")

    def test_patch_portability(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        (self.dest / "app.py").write_text("print('agent change')\n")
        patch = sw.extract_patch(self.dest)
        self.assertIn("agent change", patch)

        # Apply the patch to an independent, pristine copy of the same base
        # state — simulating the official grading environment, which the
        # agent workspace never touches.
        pristine = safe_cleanup.RUN_ROOT / "test-sanitize" / (self._testMethodName + "-pristine")
        if pristine.exists():
            safe_cleanup.safe_remove_all(pristine, safe_cleanup.RUN_ROOT)
        try:
            import shutil
            shutil.copytree(self.fixture.dir, pristine, symlinks=True,
                             ignore=lambda d, n: {x for x in n if x == ".git"})
            # A real official grading environment is its own fresh
            # container with no enclosing repository above it, so
            # `git apply` there resolves paths against that container's
            # filesystem root unambiguously. This test runs on the host,
            # where "pristine" sits nested inside the boundedcode repo
            # itself — without its own .git, `git apply` would walk
            # upward and resolve against *that* enclosing repository's
            # toplevel instead of `pristine`. Giving pristine its own git
            # root (as design instruction §3.B explicitly permits: "the
            # grading workspace may internally contain whatever the
            # official evaluator needs, including original git metadata")
            # is what actually makes this test exercise the same
            # unambiguous path resolution a real grading container has.
            git(["init", "--quiet"], pristine)
            git(["add", "-A"], pristine)
            git(["commit", "--quiet", "-m", "pristine base"], pristine)
            patch_file = pristine.parent / "test.patch"
            patch_file.write_text(patch)
            apply = subprocess.run(["git", "apply", str(patch_file)],
                                    capture_output=True, text=True, cwd=str(pristine))
            self.assertEqual(apply.returncode, 0, apply.stderr)
            self.assertEqual((pristine / "app.py").read_text(), "print('agent change')\n")
            patch_file.unlink()
        finally:
            if pristine.exists():
                safe_cleanup.safe_remove_all(pristine, safe_cleanup.RUN_ROOT)

    def test_fingerprint_is_stable_across_independent_sanitizations(self):
        dest_a = safe_cleanup.RUN_ROOT / "test-sanitize" / (self._testMethodName + "-a")
        dest_b = safe_cleanup.RUN_ROOT / "test-sanitize" / (self._testMethodName + "-b")
        for d in (dest_a, dest_b):
            if d.exists():
                safe_cleanup.safe_remove_all(d, safe_cleanup.RUN_ROOT)
        try:
            sw.sanitize_git_repo(self.fixture.dir, dest_a)
            sw.sanitize_git_repo(self.fixture.dir, dest_b)
            self.assertEqual(sw.fingerprint_tree(dest_a), sw.fingerprint_tree(dest_b),
                              "two independent sanitizations of the same base state must "
                              "fingerprint identically — this is the control/treatment "
                              "equality check design instruction §15 requires")
        finally:
            for d in (dest_a, dest_b):
                if d.exists():
                    safe_cleanup.safe_remove_all(d, safe_cleanup.RUN_ROOT)

    def test_refuses_to_sanitize_outside_the_run_root(self):
        outside = Path(tempfile.mkdtemp()) / "not-under-run-root"
        try:
            with self.assertRaises(safe_cleanup.UnsafeCleanupError):
                sw.sanitize_git_repo(self.fixture.dir, outside)
        finally:
            if outside.parent.exists():
                import shutil
                shutil.rmtree(outside.parent, ignore_errors=True)

    def test_refuses_to_overwrite_an_existing_destination(self):
        sw.sanitize_git_repo(self.fixture.dir, self.dest)
        with self.assertRaises(FileExistsError):
            sw.sanitize_git_repo(self.fixture.dir, self.dest)


if __name__ == "__main__":
    unittest.main()
