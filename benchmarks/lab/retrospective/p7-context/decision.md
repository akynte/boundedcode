# Decision: retro-p7-context

Decided on dev tasks by success_rate (higher is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| before | baseline | **baseline** | 52 | 90% (82–95%) | – | – | – |
| after | candidate | **inconclusive** | 52 | 96% (89–99%) | 0.904 → 0.962; paired: 3 better, 0 worse (p=0.125), **unchanged** | – | 19 points |

## after: inconclusive

- no difference beyond chance (3 tasks better, 0 worse, 23 tied of 26; mean pass-rate change +0.058; sign test p=0.125 (needs ≤0.05 and an effect of at least 0.03)); the paired test needs at least 5 tasks all better and none worse
- success_rate by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
