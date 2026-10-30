# Decision: retro-reasoning-auto

Decided on dev tasks by median_seconds (lower is better). Languages of those runs: go, java, python, rust, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (dev) | Primary (held-out) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| drop | baseline | **baseline** | 46 | 91% (82–96%) | – | – | – |
| auto | candidate | **inconclusive** | 46 | 91% (82–96%) | 22.2 → 20.3; paired: 12 better, 10 worse (p=0.416), **unchanged** | – | 20 points |

## auto: inconclusive

- no difference beyond chance (12 tasks better, 10 worse, 1 tied of 23; per-task ratio ×0.97; sign test p=0.416 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- median_seconds by language, paired over its dev tasks: go unchanged, java inconclusive, python inconclusive, rust inconclusive, typescript inconclusive

Note: decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal
