import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from orderschema.validate import errors  # noqa: E402


class ValidateTest(unittest.TestCase):
    def test_valid(self):
        self.assertEqual(errors({"id": "o1", "items": [], "total_cents": 100}), [])

    def test_missing_and_wrong_type(self):
        self.assertEqual(errors({"id": "o1", "items": [], "total_cents": "100"}), ["total_cents is not integer"])
        self.assertIn("missing id", errors({"items": [], "total_cents": 1}))
