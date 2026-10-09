# Comparative evaluation (2026-10): BoundedCode vs. a plain OpenHands agent

| File | What it is |
|---|---|
| [`protocol.md`](protocol.md) | The frozen protocol: questions, systems, tasks, fairness rules, measurements, analysis plan |
| [`manifest.json`](manifest.json) | Dataset pin, slots, seed, exclusions, dependency recipes, budgets, command adaptations |
| [`candidates.json`](candidates.json) | Every candidate per slot, ranked, with exclusion reasons (from `select.py`) |
| [`screening.json`](screening.json) | Gate 1 per screened candidate |
| [`frozen-tasks.json`](frozen-tasks.json) | The task set: ids, base commits, sha256 of each task spec and reference patch |
| [`config.yaml`](config.yaml) | BoundedCode's configuration for the runs |
| [`deviations.md`](deviations.md) | Every change after the freeze |
| `results/` | Machine-readable results (the home directory is written as `~` in stored records; nothing else is changed): `runs/` (harness reports, logs, memory samples), `replay/`, `run-log.jsonl`, `results.json`, `summary.json`, `failure-analysis.json` |
| [`report.md`](report.md) | The comparative report |

## Reproduce

Requirements:
- Linux with Docker (Docker Desktop works) and the sandbox image
  (`bcode setup --only sandbox`);
- Go, Python 3 and `uv`;
- the reference model (Qwen3.6-35B-A3B UD-Q4_K_M) behind llama.cpp
  v0.5.0 on `127.0.0.1:8765`, started with the arguments in
  `results/environment.json`;
- network only for steps 1 and 2.

The evaluation's state lives in `~/.cache/bc-comparative` (`--eval`
changes it):
- sources;
- dependency caches;
- task specs with hidden tests;
- reference patches;
- run work directories.

Agents never see that directory except their own worktree.

```bash
cd benchmarks/comparative-2026-10
make -C ../.. build                         # the binary under test: bin/boundedcode
uv run --with pyarrow python select.py      # 1. rank candidates, write task specs (downloads the pinned dataset)
python3 prepare.py                          # 2. sources at base commits + dependencies (network)
python3 screen_freeze.py screen             # 3. gate 1, offline
python3 screen_freeze.py freeze             #    first two valid per slot -> frozen-tasks.json
python3 run.py                              # 4. both systems, frozen order (hours)
python3 gate_replay.py                      # 5. BoundedCode's gate on the baseline's patches
python3 analyze.py                          # 6. results.json, summary.json, report tables
```

`run.py` and `gate_replay.py` are resumable: they skip results that exist.

The model samples at temperature 0.6, so individual outcomes will differ
between reproductions. The task set, order, configuration and acceptance
commands will not.
