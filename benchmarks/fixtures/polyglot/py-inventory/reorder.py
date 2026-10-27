"""Purchase planning from stock levels."""


def reorder_plan(inventory, targets):
    """Units to order per SKU to bring its stock up to its target.

    SKUs already at or above their target are left out.
    """
    plan = {}
    for sku, target in targets.items():
        have = inventory.quantity(sku)
        if have < target:
            plan[sku] = target - have
    return plan
