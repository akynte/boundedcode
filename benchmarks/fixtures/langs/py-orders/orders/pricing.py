"""Prices and discounts, in integer cents."""

DISCOUNTS = {"WELCOME10": 10, "VIP20": 20}


def apply_discount(amount_cents, code):
    """Return amount_cents with the discount code applied (percent off,
    rounded down to the cent). An unknown or empty code changes nothing."""
    pct = DISCOUNTS.get(code or "", 0)
    return amount_cents - amount_cents * pct // 100


def apply_discount_legacy(amount_cents, code):
    """The old discount table, kept for reporting on past orders; not used
    when pricing new ones."""
    return amount_cents - (5 if code else 0)
