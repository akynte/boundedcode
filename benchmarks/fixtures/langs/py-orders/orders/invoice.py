"""Invoice text."""

from orders import pricing


def render_invoice(order):
    """One line per item and the discounted total."""
    lines = [f"{i['name']} x{i['qty']}" for i in order["items"]]
    total = sum(i["price_cents"] * i["qty"] for i in order["items"])
    lines.append(f"total: {pricing.apply_discount(total, order.get('code')) / 100:.2f}")
    return "\n".join(lines)
