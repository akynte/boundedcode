# Retrieval benchmark (2026-10)

Retrieval measured apart from patch correctness: for real issues, which of
the files their fix changed does each retrieval approach put in front of the
model, at what precision, context size and latency. The results and the
decisions taken from them are in
[docs/benchmarks/retrieval-2026-10.md](../../docs/benchmarks/retrieval-2026-10.md).

| File | What it is |
|---|---|
| [`manifest.json`](manifest.json) | Datasets (SWE-bench Multilingual and SWE-bench Lite, both MIT) pinned by revision and sha256, the seed, the split rule, the language of each repository |
| [`prepare.py`](prepare.py) | Downloads and checks the datasets, selects instances and the development/held-out split by the seeded rule, clones the repositories and checks out each base commit once |
| [`agent_tasks.py`](agent_tasks.py) | Writes the held-out agent-level locate tasks |
| [`agent-tasks/`](agent-tasks/) | Those 16 tasks (`bcode bench direct --tasks benchmarks/retrieval/agent-tasks`): read-only questions built from issue texts, judged by whether the answer names a file the fix changed |

## Reproduce

```bash
cd benchmarks/retrieval
uv run --with pyarrow python3 prepare.py        # ~/.cache/bc-retrieval: datasets, tasks.json, clones (network, once)
cd ../..
bcode bench retrieval --split dev     --arms basic,adaptive,basic+tests,adaptive+tests --out dev.json
bcode bench retrieval --split heldout --arms basic,adaptive,basic+tests,adaptive+tests --out heldout.json
# Ablations: adaptive-<strategy> drops one strategy, adaptive+<strategy> always runs it.
python3 benchmarks/retrieval/agent_tasks.py     # only to regenerate agent-tasks/
bcode bench direct --tasks benchmarks/retrieval/agent-tasks --retrieval basic    --out basic.json
bcode bench direct --tasks benchmarks/retrieval/agent-tasks --retrieval adaptive --out adaptive.json
```

No model is involved in `bench retrieval`. Retrieval times depend on the
machine and on the page cache; everything else is deterministic for a given
checkout.
