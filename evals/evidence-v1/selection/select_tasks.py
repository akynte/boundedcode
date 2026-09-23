#!/usr/bin/env python3
"""Deterministic, pre-registered task selection for BoundedCode Standard
Evidence Suite v1 — design instruction §4.

This script is run exactly once, before any BoundedCode execution, and
its output (selection/*.json plus the printed manifest hash) is frozen. It
performs no result-dependent selection: it has no way to know what
BoundedCode would do with any instance, because BoundedCode is never invoked
here.

Selection rule (recorded here so it is auditable, not only in prose):

  1. Load the eligible instance set for each benchmark track from the
     locally cached, unmodified metadata fetched from the official source
     (see fetch_notes.md in this directory for exact URLs and fetch
     commands).
  2. Apply objective eligibility filters chosen before this script ever saw
     a selection outcome (see ELIGIBILITY_NOTES below) — never a filter
     tuned to include or exclude a specific instance.
  3. Sort the eligible set deterministically by instance/task id (a stable,
     content-independent order).
  4. Within each stratum (language for SWE-Bench Pro Verified, repository
     for SWE Atlas), select the instance whose
     sha256(f"{SEED}:{id}") digest is numerically smallest. This is a
     deterministic pseudo-random selection: reproducible from (seed, the
     eligible id set) alone, and does not depend on this script's own
     iteration order or on any Python RNG's internal state.
  5. Write the selected IDs to selection/<track>.json.
  6. Hash the concatenation of all selection files into the frozen
     manifest (see ../manifest.json, written by freeze_manifest.py).

Re-running this script against the same cached metadata and the same SEED
reproduces byte-identical output. It must not be re-run with a different
SEED, a different filter, or updated metadata after task 1 of either arm has
begun — that would be Evidence Suite v2, not v1.
"""
import hashlib
import json
import sys
from pathlib import Path

# Unchanged across the BoundedCode rename on purpose: this seed is what
# reproduces the exact task set recorded in selection.json. A new seed
# would silently select a different suite and invalidate every run made
# against the old one.
SEED = "local-engineer-evidence-v1"
DATA_DIR = Path(__file__).parent / "cache"
OUT_DIR = Path(__file__).parent

# ---------------------------------------------------------------------------
# Eligibility filters — chosen before any selection outcome was known.
# ---------------------------------------------------------------------------
# SWE-Bench Pro Verified: at least one FAIL_TO_PASS test (a resolvable,
# test-verifiable instance), a published container image (dockerhub_tag),
# and a moderately scoped gold patch (1-5 files) — objective §2's "keep
# engineering overhead reasonably small": a 100+ file gold patch is not
# representative of what this suite is trying to measure with 4 tasks and
# would dominate the sample's runtime and review cost.
SWEBENCH_PRO_FILTER = {
    "min_fail_to_pass": 1,
    "requires_dockerhub_tag": True,
    "max_files_changed": 5,
    "min_files_changed": 1,
}


def digest(id_str: str) -> str:
    return hashlib.sha256(f"{SEED}:{id_str}".encode()).hexdigest()


def pick_min_digest(ids):
    return min(ids, key=digest)


def select_swebench_pro(n=4):
    all_instances = json.loads((DATA_DIR / "swebenchpro_all.json").read_text())
    f = SWEBENCH_PRO_FILTER
    eligible = [
        row for row in all_instances
        if row["fail_to_pass_count"] >= f["min_fail_to_pass"]
        and row["dockerhub_tag"]
        and f["min_files_changed"] <= row["files_changed"] <= f["max_files_changed"]
    ]
    eligible.sort(key=lambda r: r["instance_id"])  # deterministic base order

    by_lang = {}
    for row in eligible:
        by_lang.setdefault(row["repo_language"], []).append(row)

    # Stratify across languages — design instruction §5: prefer diversity
    # across Go/Python/JS/TS where the dataset supports it, never forcing a
    # distribution the eligible set cannot support.
    langs_present = sorted(by_lang)  # deterministic order (alphabetical)
    selected = []
    used_repos = set()
    lang_cycle = langs_present[:]
    li = 0
    while len(selected) < n and any(by_lang.values()):
        if not lang_cycle:
            lang_cycle = [l for l in langs_present if by_lang.get(l)]
            if not lang_cycle:
                break
        lang = lang_cycle[li % len(lang_cycle)]
        li += 1
        bucket = by_lang.get(lang, [])
        # Prefer a repo not already selected, for within-benchmark diversity.
        candidates = [r for r in bucket if r["repo"] not in used_repos] or bucket
        if not candidates:
            del by_lang[lang]
            lang_cycle = [l for l in lang_cycle if l != lang]
            continue
        chosen_id = pick_min_digest([r["instance_id"] for r in candidates])
        chosen = next(r for r in candidates if r["instance_id"] == chosen_id)
        selected.append(chosen)
        used_repos.add(chosen["repo"])
        by_lang[lang] = [r for r in bucket if r["instance_id"] != chosen_id]
        if not by_lang[lang]:
            del by_lang[lang]
            lang_cycle = [l for l in lang_cycle if l != lang]

    return selected[:n], {
        "filter": f,
        "eligible_count": len(eligible),
        "languages_present_in_eligible_set": {l: len(v) for l, v in
            {k: [r for r in eligible if r["repo_language"] == k] for k in langs_present}.items()},
        "stratification": "round-robin across repo_language, alphabetical language "
                           "order, min-sha256-digest pick within each language bucket, "
                           "preferring an unused repo",
    }


ATLAS_FILTER_NOTES = (
    "Every task under data/tw/ and data/rf/ in scaleapi/SWE-Atlas that has a "
    "complete harbor task directory (task.toml, environment/, tests/, "
    "instruction.md all present) is eligible. No instance was excluded on "
    "difficulty or repository grounds beyond the 'different repository "
    "preferred' stratification rule in design instruction §6."
)


def select_atlas(track, n=2):
    rows = json.loads((DATA_DIR / f"atlas_{track}.json").read_text())
    rows = [r for r in rows if r["complete"]]
    rows.sort(key=lambda r: r["task_id"])

    by_repo = {}
    for r in rows:
        by_repo.setdefault(r["repo_key"], []).append(r)
    repo_order = sorted(by_repo)  # deterministic

    selected = []
    ri = 0
    used = set()
    while len(selected) < n and by_repo:
        repo = repo_order[ri % len(repo_order)]
        ri += 1
        if repo not in by_repo:
            continue
        bucket = by_repo[repo]
        chosen_id = pick_min_digest([r["task_id"] for r in bucket])
        chosen = next(r for r in bucket if r["task_id"] == chosen_id)
        selected.append(chosen)
        used.add(repo)
        by_repo[repo] = [r for r in bucket if r["task_id"] != chosen_id]
        if not by_repo[repo]:
            del by_repo[repo]
            repo_order = [x for x in repo_order if x != repo]
        if repo_order and ri % len(repo_order) == 0 and len(used) >= len(repo_order):
            pass  # allow repeats once every repo has contributed, if n exceeds repo count

    return selected[:n], {
        "eligible_count": len(rows),
        "distinct_repos_in_eligible_set": len(by_repo) + len(used),
        "notes": ATLAS_FILTER_NOTES,
        "stratification": "round-robin across repo_key, alphabetical order, "
                           "min-sha256-digest pick within each repo bucket",
    }


def main():
    if not DATA_DIR.exists():
        print(f"error: {DATA_DIR} does not exist. Run fetch_metadata.py first, or see "
              f"fetch_notes.md for how this cache was produced.", file=sys.stderr)
        sys.exit(1)

    swebench_selected, swebench_rule = select_swebench_pro(4)
    tw_selected, tw_rule = select_atlas("tw", 2)
    rf_selected, rf_rule = select_atlas("rf", 2)

    out = {
        "seed": SEED,
        "swebench_pro_verified": {"rule": swebench_rule, "selected": swebench_selected},
        "swe_atlas_test_writing": {"rule": tw_rule, "selected": tw_selected},
        "swe_atlas_refactoring": {"rule": rf_rule, "selected": rf_selected},
    }
    (OUT_DIR / "selection.json").write_text(json.dumps(out, indent=2, sort_keys=True) + "\n")

    print("Selected SWE-Bench Pro Verified:", [r["instance_id"] for r in swebench_selected])
    print("Selected SWE Atlas Test Writing:", [r["task_id"] for r in tw_selected])
    print("Selected SWE Atlas Refactoring:", [r["task_id"] for r in rf_selected])


if __name__ == "__main__":
    main()
