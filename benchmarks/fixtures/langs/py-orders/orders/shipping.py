"""Shipping costs."""

import json
import os

RATES = os.path.join(os.path.dirname(__file__), "..", "data", "rates.json")


def _rates():
    with open(RATES) as f:
        return json.load(f)


def shipping_cost(weight_kg):
    """Shipping in cents for a parcel of weight_kg: the price of the first
    band (in rates.json, bands sorted by max_kg) whose max_kg is at least
    the weight; parcels over every band pay the last band's price plus
    surcharge_per_kg for each started kilogram above it."""
    r = _rates()
    for band in r["bands"]:
        if weight_kg < band["max_kg"]:
            return band["cents"]
    last = r["bands"][-1]
    extra = weight_kg - last["max_kg"]
    return last["cents"] + int(extra) * r["surcharge_per_kg"]
