# How `cache/` was built

Every file under `cache/` is derived, unmodified in content, from the
official published sources below. No instance text was edited, paraphrased,
or hand-picked before selection ran; these are metadata extractions only
(exact commands given so this is independently repeatable).

## SWE-Bench Pro Verified

Source: https://huggingface.co/datasets/opencompass/SWEBench-Pro-Verified
(publisher: OpenCompass / East China Normal University, Shanghai AI
Laboratory, Fudan University; arXiv:2609.08149; Apache-2.0 dataset license).

```bash
curl -sL "https://huggingface.co/api/datasets/opencompass/SWEBench-Pro-Verified/parquet/default/test/0.parquet" \
  -o swebenchpro.parquet
python3 - <<'PY'
import pandas as pd
df = pd.read_parquet("swebenchpro.parquet")
df["files_changed"] = df["patch"].apply(lambda p: p.count("diff --git "))
cols = ["repo","instance_id","repo_language","fail_to_pass_count",
        "pass_to_pass_count","dockerhub_tag","files_changed","issue_categories"]
df[cols].to_json("cache/swebenchpro_all.json", orient="records", indent=2)
PY
```

Confirmed at fetch time: 731 rows, 18 columns, languages
{go: 280, python: 266, js: 165, ts: 20}, matching the dataset card's stated
size. The full `patch`/`test_patch`/`problem_statement` text is **not**
vendored into this repository — only the 8 metadata columns above, which is
what the deterministic selection filter needs. The full instance content is
fetched at run time, directly from the same HuggingFace dataset, by whatever
harness executes the selected instance (AgentCompass /
princeton-nlp-style SWE-bench harness), keyed by `instance_id`.

## SWE Atlas — Test Writing and Refactoring

Source: https://github.com/scaleapi/SWE-Atlas (publisher: Scale AI;
arXiv:2605.08366; Apache-2.0 repository license). Task format: a
[harbor](https://github.com/laude-institute/harbor) dataset — each task is a
directory under `data/tw/<task-id>/` or `data/rf/<task-id>/` containing
`task.toml`, `environment/` (a Dockerfile), `tests/`, `instruction.md`, and
`solution/`.

```bash
# Task Writing: data/tw/dataset.toml lists every task name + content digest.
curl -sL "https://raw.githubusercontent.com/scaleapi/SWE-Atlas/main/data/tw/dataset.toml" \
  -o tw-dataset.toml   # 90 tasks

# Refactoring: no dataset.toml; list the directory instead (70 tasks, single page).
curl -sL "https://api.github.com/repos/scaleapi/SWE-Atlas/contents/data/rf?per_page=100" \
  -o rf-listing.json

# For each task id, fetch task.toml (repository/base_commit for TW; difficulty/
# tags for RF) and, for RF, environment/Dockerfile (its ghcr.io image tag
# encodes <owner>_<repo>, since RF's task.toml carries no repository field).
```

Confirmed at fetch time: 90 TW tasks (11 distinct repositories), 70 RF tasks
(10 distinct repositories: JavaScript, TypeScript, Python, Go, C, C++
represented via the `tags` field) — matching the paper's stated 90/70 counts
exactly. Every task directory fetched had the complete harbor layout
(`task.toml`, `environment/`, `tests/`, `instruction.md`); this was spot
checked on `data/tw/task-6902ef3ab97fe23e2ad271ee` directly and assumed for
the rest (`complete: true` in the cache), since every `task.toml` and every
`environment/Dockerfile` parsed successfully for all 90+70 tasks — a task
missing `tests/` or `instruction.md` would not itself have blocked those two
fetches, so this is a reasonable but not exhaustive completeness check; see
"Known limitations" in `../../docs/evidence/evidence-v1.md`.

## Fetch timestamp

All fetches for Evidence Suite v1's selection were performed 2026-09-21
(same UTC day the manifest was frozen — see `../manifest.json`). A dataset
that adds or corrects instances after this date does not change this
suite's frozen selection; a later suite that wanted to reselect would be
Evidence Suite v2.
