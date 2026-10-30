# Decision: retro-p8-orchestration

Decided on dev tasks by median_seconds (lower is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| before | baseline | **baseline** | 52 | 92% (84–96%) | – | – | – |
| after | candidate | **inconclusive** | 52 | 96% (89–99%) | 23.5 → 22.6; paired: 11 better, 11 worse (p=0.584), **unchanged** | – | 17 points |

## after: inconclusive

- no difference beyond chance (11 tasks better, 11 worse, 4 tied of 26; per-task ratio ×0.95; sign test p=0.584 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- median_seconds by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
