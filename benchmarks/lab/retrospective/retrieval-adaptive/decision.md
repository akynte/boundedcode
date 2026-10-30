# Decision: retro-retrieval-adaptive

Decided on dev tasks by success_rate (higher is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| basic | baseline | **baseline** | 46 | 89% (79–95%) | – | – | – |
| adaptive | candidate | **inconclusive** | 46 | 96% (88–99%) | 0.891 → 0.957; paired: 3 better, 1 worse (p=0.313), **unchanged** | – | 22 points |

## adaptive: inconclusive

- no difference beyond chance (3 tasks better, 1 worse, 19 tied of 23; mean pass-rate change +0.065; sign test p=0.313 (needs ≤0.05 and an effect of at least 0.03)); the paired test needs at least 5 tasks all better and none worse
- success_rate by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive
- Predictions: fixed [], not fixed [], **worse instead** []; predicted risks that broke []; unforeseen drops [session-x-tools]

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
