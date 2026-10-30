"""Choosing between promotion codes."""

from orders.pricing import apply_discount as discounted


def best_offer(amount_cents, codes):
    """The code that gives the lowest price, or None when none helps."""
    best, best_price = None, amount_cents
    for c in codes:
        p = discounted(amount_cents, c)
        if p < best_price:
            best, best_price = c, p
    return best


def describe(code):
    """A label for a code (no pricing)."""
    return f"code {code}"
