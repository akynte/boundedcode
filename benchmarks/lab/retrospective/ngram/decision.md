# Decision: retro-ngram

Decided on dev tasks by median_seconds (lower is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| off | baseline | **baseline** | 52 | 98% (92–100%) | – | – | – |
| ngram | candidate | **inconclusive** | 52 | 94% (86–98%) | 22.6 → 23.6; paired: 7 better, 16 worse (p=0.047), **unchanged** | – | 14 points |

## ngram: inconclusive

- no difference beyond chance (7 tasks better, 16 worse, 3 tied of 26; per-task ratio ×1.03; sign test p=0.047 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- median_seconds by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive
- Predictions: fixed [], not fixed [], **worse instead** []; predicted risks that broke []; unforeseen drops [fix-balance-query session-ledger]

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
