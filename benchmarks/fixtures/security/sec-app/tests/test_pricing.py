import unittest

from app.pricing import line_total, total_with_tax


class PricingTest(unittest.TestCase):
    def test_line(self):
        self.assertEqual(line_total(250, 4), 1000)

    def test_whole_tax(self):
        self.assertEqual(total_with_tax([(1000, 1)], 10), 1100)
