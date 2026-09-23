# Standard Evidence Suite v1

See [`docs/evidence/evidence-v1.md`](../../docs/evidence/evidence-v1.md) for
the full report: headline, selected tasks, Jev ablation design, reproduction
commands, and limitations.

```
evals/evidence-v1/
  manifest.json          frozen manifest (task IDs, benchmark revisions, hashes)
  jev-profile.yaml        frozen EXPERIMENTAL_JEV_ASSISTED profile
  freeze_manifest.py       writes manifest.json from selection.json + jev-profile.yaml
  selection/
    select_tasks.py        deterministic, pre-registered task selection
    selection.json          the 8 selected task IDs + selection rule
    fetch_notes.md           exact commands used to build cache/ from official sources
    cache/                   metadata extracted from the official datasets (no solution text)
  judgments/
    m4-dataset.jsonl        verification_integrity component-eval dataset (34 cases)
  raw/
    control/                Arm A (Jev OFF) run artifacts — empty until run
    jev-assisted/            Arm B (Jev Assisted) run artifacts — empty until run
  reports/                  generated reports — empty until run
```

Live execution has not occurred — see the report's "Environment" and
"Limitations" sections for the exact blockers and the commands to finish.
