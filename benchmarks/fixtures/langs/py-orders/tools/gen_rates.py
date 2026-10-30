"""Write data/rates.json, the shipping rates the tests use."""

import json
import os

out = os.path.join(os.path.dirname(__file__), "..", "data", "rates.json")
os.makedirs(os.path.dirname(out), exist_ok=True)
with open(out, "w") as f:
    json.dump({"bands": [{"max_kg": 1, "cents": 499}, {"max_kg": 5, "cents": 899}, {"max_kg": 20, "cents": 1999}],
               "surcharge_per_kg": 150}, f, indent=2)
print("wrote", os.path.normpath(out))
