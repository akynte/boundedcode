"""Regression tests for the cleanup guard — design instruction §26.

Run with: python3 -m unittest evals.evidence-v1.tooling.test_safe_cleanup -v
or simply: python3 evals/evidence-v1/tooling/test_safe_cleanup.py
"""
import os
import tempfile
import unittest
from pathlib import Path

import safe_cleanup as sc


class TestIsWithinAllowedRoot(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "run-root"
        self.root.mkdir()

    def tearDown(self):
        self.tmp.cleanup()

    def test_permits_a_real_subdirectory(self):
        target = self.root / "task-123" / "tmp"
        target.mkdir(parents=True)
        self.assertTrue(sc.is_within_allowed_root(target, self.root))

    def test_permits_a_not_yet_created_subdirectory(self):
        target = self.root / "task-999" / "not-made-yet"
        self.assertTrue(sc.is_within_allowed_root(target, self.root))

    def test_refuses_the_root_itself_is_still_within(self):
        # is_within_allowed_root allows the root itself (equality); the
        # stronger "never delete the root" rule lives in safe_remove_all,
        # tested separately below.
        self.assertTrue(sc.is_within_allowed_root(self.root, self.root))

    def test_refuses_repository_root(self):
        self.assertFalse(sc.is_within_allowed_root(sc.REPO_ROOT, self.root))

    def test_refuses_evals_directory(self):
        self.assertFalse(sc.is_within_allowed_root(sc.REPO_ROOT / "evals", self.root))

    def test_refuses_parent_of_run_root(self):
        self.assertFalse(sc.is_within_allowed_root(self.root.parent, self.root))

    def test_refuses_filesystem_root(self):
        self.assertFalse(sc.is_within_allowed_root("/", self.root))

    def test_refuses_home_directory(self):
        self.assertFalse(sc.is_within_allowed_root(os.path.expanduser("~"), self.root))

    def test_refuses_empty_path(self):
        self.assertFalse(sc.is_within_allowed_root("", self.root))
        self.assertFalse(sc.is_within_allowed_root("   ", self.root))

    def test_refuses_none(self):
        self.assertFalse(sc.is_within_allowed_root(None, self.root))

    def test_refuses_relative_traversal_escaping_the_root(self):
        escaping = self.root / "task-1" / ".." / ".." / "outside"
        self.assertFalse(sc.is_within_allowed_root(escaping, self.root))

    def test_refuses_dot_when_cwd_is_outside_the_root(self):
        old = os.getcwd()
        try:
            os.chdir(str(sc.REPO_ROOT))
            self.assertFalse(sc.is_within_allowed_root(".", self.root))
        finally:
            os.chdir(old)

    def test_permits_dot_when_cwd_is_inside_the_root(self):
        sub = self.root / "task-2"
        sub.mkdir()
        old = os.getcwd()
        try:
            os.chdir(str(sub))
            self.assertTrue(sc.is_within_allowed_root(".", self.root))
        finally:
            os.chdir(old)

    def test_refuses_a_symlink_that_escapes_the_root(self):
        outside = Path(self.tmp.name) / "outside-target"
        outside.mkdir()
        link = self.root / "escape-link"
        try:
            link.symlink_to(outside, target_is_directory=True)
        except (OSError, NotImplementedError):
            self.skipTest("symlinks not supported in this environment")
        # The string path is inside the root; its resolved target is not.
        self.assertTrue(str(link).startswith(str(self.root)))  # the naive check would pass
        self.assertFalse(sc.is_within_allowed_root(link, self.root))  # the real check refuses

    def test_permits_a_symlink_that_stays_inside_the_root(self):
        real = self.root / "real-subdir"
        real.mkdir()
        link = self.root / "inside-link"
        try:
            link.symlink_to(real, target_is_directory=True)
        except (OSError, NotImplementedError):
            self.skipTest("symlinks not supported in this environment")
        self.assertTrue(sc.is_within_allowed_root(link, self.root))

    def test_string_prefix_alone_would_be_fooled_by_a_sibling_name(self):
        # e.g. allowed_root=".../run-root" must not accidentally permit
        # ".../run-root-evil" merely because the string starts the same way.
        sibling = Path(str(self.root) + "-evil") / "x"
        self.assertFalse(sc.is_within_allowed_root(sibling, self.root))


class TestSafeRemoveAll(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "run-root"
        self.root.mkdir()

    def tearDown(self):
        self.tmp.cleanup()

    def test_removes_a_real_subdirectory(self):
        target = self.root / "task-1"
        (target / "nested").mkdir(parents=True)
        (target / "nested" / "file.txt").write_text("x")
        sc.safe_remove_all(target, self.root)
        self.assertFalse(target.exists())

    def test_is_a_noop_for_an_already_absent_target(self):
        target = self.root / "does-not-exist"
        sc.safe_remove_all(target, self.root)  # must not raise

    def test_refuses_to_remove_the_run_root_itself(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(self.root, self.root)
        self.assertTrue(self.root.exists())

    def test_refuses_repository_root(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(sc.REPO_ROOT, self.root)

    def test_refuses_evals_directory(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(sc.REPO_ROOT / "evals", self.root)
        # And the actual directory must still be there afterward.
        self.assertTrue((sc.REPO_ROOT / "evals" / "tasks").exists())

    def test_refuses_parent_of_run_root(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(self.root.parent, self.root)

    def test_refuses_filesystem_root(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all("/", self.root)

    def test_refuses_empty_path(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all("", self.root)

    def test_refuses_relative_traversal(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(self.root / ".." / "..", self.root)

    def test_refuses_a_symlink_escaping_the_root_and_does_not_touch_the_target(self):
        outside = Path(self.tmp.name) / "precious"
        outside.mkdir()
        (outside / "keepme.txt").write_text("do not delete")
        link = self.root / "escape-link"
        try:
            link.symlink_to(outside, target_is_directory=True)
        except (OSError, NotImplementedError):
            self.skipTest("symlinks not supported in this environment")
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.safe_remove_all(link, self.root)
        self.assertTrue((outside / "keepme.txt").exists())


class TestTaskWorkspace(unittest.TestCase):
    def test_rejects_a_task_id_with_a_path_separator(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.task_workspace("../escape", "control")

    def test_rejects_a_task_id_with_dotdot(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.task_workspace("task-1/../../etc", "control")

    def test_rejects_an_empty_task_id(self):
        with self.assertRaises(sc.UnsafeCleanupError):
            sc.task_workspace("", "control")

    def test_builds_a_workspace_under_the_real_run_root(self):
        d = sc.task_workspace("preflight-self-test-task", "control")
        try:
            self.assertTrue(sc.is_within_allowed_root(d, sc.RUN_ROOT))
            self.assertTrue(str(d).startswith(str(sc.RUN_ROOT)))
        finally:
            sc.safe_remove_all(d, sc.RUN_ROOT)


if __name__ == "__main__":
    unittest.main()
