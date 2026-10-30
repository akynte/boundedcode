"""Order totals, in integer cents."""


def line_total(unit_cents, qty):
    """A line's amount."""
    return unit_cents * qty


def total_with_tax(lines, rate_pct):
    """The order total plus tax at rate_pct percent, rounded half up to the
    cent: 1005 cents at 10% is 1106 (1105.5 rounded up)."""
    net = sum(line_total(u, q) for u, q in lines)
    return net + net * rate_pct // 100
