import unittest

from score import Model, score


class ScoreTest(unittest.TestCase):
    def test_score(self):
        self.assertEqual(score([1, 0]), 0.5)
        self.assertTrue(Model().predict([1, 1]))


if __name__ == "__main__":
    unittest.main()
