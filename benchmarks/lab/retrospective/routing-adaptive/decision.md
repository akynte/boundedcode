# Decision: retro-routing-adaptive

Decided on dev tasks by success_rate (higher is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| off | baseline | **baseline** | 52 | 98% (92–100%) | – | – | – |
| adaptive | candidate | **inconclusive** | 52 | 92% (84–96%) | 0.981 → 0.923; paired: 0 better, 2 worse (p=0.250), **unchanged** | – | 14 points |

## adaptive: inconclusive

- no difference beyond chance (0 tasks better, 2 worse, 24 tied of 26; mean pass-rate change -0.058; sign test p=0.250 (needs ≤0.05 and an effect of at least 0.03)); the paired test needs at least 5 tasks all better and none worse
- success_rate by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive
- Predictions: fixed [], not fixed [], **worse instead** []; predicted risks that broke []; unforeseen drops [fix-balance-query multi-feature-discount]

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
