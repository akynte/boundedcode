import unittest

from orders.pricing import apply_discount


class PricingTest(unittest.TestCase):
    def test_discount(self):
        self.assertEqual(apply_discount(1000, "WELCOME10"), 900)

    def test_unknown_code(self):
        self.assertEqual(apply_discount(1000, "NOPE"), 1000)
