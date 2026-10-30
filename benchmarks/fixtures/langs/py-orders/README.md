# orders

Order pricing for the shop.

Tests read shipping rates from `data/rates.json`, which is generated (it is
not in the repository):

    python3 tools/gen_rates.py
    python3 -m unittest discover -s tests -v
