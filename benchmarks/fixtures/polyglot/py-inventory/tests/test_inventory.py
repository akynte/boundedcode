import unittest

from inventory import Inventory
from reorder import reorder_plan


class InventoryTest(unittest.TestCase):
    def test_add_and_quantity(self):
        inv = Inventory()
        inv.add("apple", 3)
        inv.add("apple", 2)
        self.assertEqual(inv.quantity("apple"), 5)
        self.assertEqual(inv.quantity("pear"), 0)

    def test_low_stock_sorted(self):
        inv = Inventory()
        inv.add("b", 1)
        inv.add("a", 2)
        inv.add("c", 9)
        self.assertEqual(inv.low_stock(5), ["a", "b"])

    def test_reorder_plan(self):
        inv = Inventory()
        inv.add("a", 2)
        inv.add("b", 10)
        self.assertEqual(reorder_plan(inv, {"a": 5, "b": 5, "c": 1}), {"a": 3, "c": 1})


if __name__ == "__main__":
    unittest.main()
