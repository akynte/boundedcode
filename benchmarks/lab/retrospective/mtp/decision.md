# Decision: retro-mtp

Decided on dev tasks by median_seconds (lower is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| mtp-off | baseline | **baseline** | 52 | 92% (84–96%) | – | – | – |
| mtp | candidate | **reject** | 52 | 94% (86–98%) | 22.4 → 20.4; paired: 23 better, 3 worse (p=0.000), **improved** | – | 17 points |

## mtp: reject

- costs more than the baseline allows: tokens_per_success 5357 against 4332
- median_seconds by language, paired over its dev tasks: go improved, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive
- Predictions: fixed [], not fixed [], **worse instead** []; predicted risks that broke []; unforeseen drops [diagnose-failing-test session-ledger]
- Gate regressions: tokens_per_success (overall): 5.36e+03 against 4.33e+03 (×1.24; allowed ±15%, the larger of the tolerance and the baseline trials' spread)

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
