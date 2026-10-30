import unittest

from orders.shipping import shipping_cost


class ShippingTest(unittest.TestCase):
    def test_light_parcel(self):
        self.assertEqual(shipping_cost(0.5), 499)

    def test_band_limit_is_inclusive(self):
        # A parcel of exactly 1 kg is in the first band.
        self.assertEqual(shipping_cost(1), 499)
