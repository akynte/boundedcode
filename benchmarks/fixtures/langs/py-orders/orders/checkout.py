"""Checkout totals."""

from orders.pricing import apply_discount
from orders.shipping import shipping_cost


def checkout_total(items, code=None, weight_kg=0.0):
    """The amount to charge: the items' prices with the discount, plus
    shipping for weight_kg."""
    subtotal = sum(i["price_cents"] * i["qty"] for i in items)
    return apply_discount(subtotal, code) + shipping_cost(weight_kg)
