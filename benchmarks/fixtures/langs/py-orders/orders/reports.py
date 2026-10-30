"""Reports on past orders."""

from orders.pricing import apply_discount_legacy


def legacy_revenue(orders):
    """Revenue of orders priced with the old discount table."""
    return sum(apply_discount_legacy(o["amount_cents"], o.get("code")) for o in orders)
