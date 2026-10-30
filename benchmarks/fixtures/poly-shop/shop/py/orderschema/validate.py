"""Validate orders against schema/order.schema.json (the subset of JSON
Schema the shop uses: required properties and property types)."""

import json
import os

SCHEMA = os.path.join(os.path.dirname(__file__), "..", "..", "schema", "order.schema.json")
TYPES = {"string": str, "integer": int, "array": list, "object": dict, "boolean": bool}


def load_schema():
    with open(SCHEMA) as f:
        return json.load(f)


def errors(order, schema=None):
    """The ways order breaks the schema, as messages; empty when valid."""
    schema = schema or load_schema()
    out = []
    for name in schema.get("required", []):
        if name not in order:
            out.append(f"missing {name}")
    for name, prop in schema.get("properties", {}).items():
        if name not in order:
            continue
        want = TYPES.get(prop.get("type"))
        v = order[name]
        if want is not None and (not isinstance(v, want) or (want is int and isinstance(v, bool))):
            out.append(f"{name} is not {prop['type']}")
    return out
