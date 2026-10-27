"""Stock levels for a small warehouse."""


class InsufficientStock(Exception):
    """Raised when a removal would take a SKU's stock below zero."""


class Inventory:
    def __init__(self):
        self._items = {}

    def add(self, sku, qty):
        if qty <= 0:
            raise ValueError("quantity must be positive")
        self._items[sku] = self._items.get(sku, 0) + qty

    def remove(self, sku, qty):
        if qty <= 0:
            raise ValueError("quantity must be positive")
        have = self._items.get(sku, 0)
        if have < qty:
            raise InsufficientStock(f"{sku}: have {have}, need {qty}")
        self._items[sku] = have - qty

    def quantity(self, sku):
        return self._items.get(sku, 0)

    def total_value(self, prices):
        """Value of the stock in cents; SKUs without a price are skipped."""
        return sum(qty * prices[sku] for sku, qty in self._items.items() if sku in prices)

    def low_stock(self, threshold):
        """SKUs with fewer than threshold units, in alphabetical order."""
        return sorted(sku for sku, qty in self._items.items() if qty < threshold)
