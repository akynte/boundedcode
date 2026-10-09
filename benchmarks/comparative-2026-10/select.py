#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Deterministic candidate selection for the comparative evaluation.

Reads manifest.json (slots, seed, dataset revision, exclusions) and the
pinned SWE-bench Multilingual parquet, and writes candidates.json: for each
slot, every eligible instance ranked by sha256(seed + instance_id). Nothing
here looks at task content beyond the eligibility rules in the manifest, so
the ranking cannot be tuned to either system.

Then, for the first `screen_per_slot` candidates of each slot, writes task
specs (problem statement as the request, the dataset's test patch as hidden
acceptance, its test command) and gold patches into OUT, outside the
repository, for screening (gate 1) and the runs.

usage: uv run --with pyarrow select.py [--out DIR]
"""
import argparse
import hashlib
import importlib.util
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))

# Reuse the task conversion of the earlier validations (test command, URLs).
spec = importlib.util.spec_from_file_location("swe", os.path.join(ROOT, "scripts", "bench-swe-tasks.py"))
swe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(swe)


def previously_used():
    """Every SWE-style instance id named anywhere in earlier benchmark work."""
    pat = re.compile(r"\b([a-z0-9][a-z0-9.-]*__[a-z0-9][a-z0-9._-]*-\d+)\b")
    found = set()
    for base in ("benchmarks/reports", "docs/benchmarks"):
        for d, _, files in os.walk(os.path.join(ROOT, base)):
            for f in files:
                if f.endswith((".json", ".md", ".jsonl", ".yaml", ".txt")):
                    try:
                        found |= set(pat.findall(open(os.path.join(d, f), errors="replace").read()))
                    except OSError:
                        pass
    return found


def adapt(cmd, m):
    """Applies the manifest's command adaptations (tools the sandbox image
    lacks, replaced by the equivalent binary it has)."""
    for c in m.get("command_adaptations", []):
        cmd = re.sub(c["from"], c["to"], cmd)
    return cmd


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.expanduser("~/.cache/bc-comparative/tasks"))
    a = ap.parse_args()
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    ds = m["dataset"]
    path = swe.fetch(swe.ML_URL.format(rev=ds["revision"]), os.path.join(a.out, "..", "datasets", "ml-" + ds["revision"][:12] + ".parquet"))
    digest = swe.sha256(path)
    if digest != ds["file_sha256"]:
        sys.exit("dataset file sha256 %s != pinned %s" % (digest, ds["file_sha256"]))
    import pyarrow.parquet as pq

    rows = {r["instance_id"]: r for r in pq.read_table(path).to_pylist()}
    used = previously_used() | set(m["exclude_instances"])
    manifest_files = re.compile(m["gold_must_not_touch"])
    out = {"seed": m["seed"], "dataset_file_sha256": digest, "previously_used_excluded": sorted(used), "slots": {}}
    os.makedirs(os.path.join(a.out, "tasks"), exist_ok=True)
    os.makedirs(os.path.join(a.out, "gold"), mode=0o700, exist_ok=True)
    for slot in m["slots"]:
        ranked = []
        for iid, r in rows.items():
            if r["repo"] not in slot["repos"]:
                continue
            why = ""
            if iid in used:
                why = "used in earlier benchmark work"
            elif any(manifest_files.search(f) for f in re.findall(r"^diff --git a/(\S+)", r["patch"], re.M)):
                why = "gold patch changes a dependency manifest"
            else:
                try:
                    swe.test_command({"eval_script": r["eval_script"]})
                except Exception:  # noqa: BLE001 - no recognisable test command
                    why = "no test command in the eval script"
            rank = hashlib.sha256((m["seed"] + iid).encode()).hexdigest()
            ranked.append({"instance_id": iid, "repo": r["repo"], "base": r["base_commit"], "rank": rank, "excluded": why})
        ranked.sort(key=lambda c: c["rank"])
        eligible = [c for c in ranked if not c["excluded"]]
        screen = eligible[: m["screen_per_slot"]]
        out["slots"][slot["name"]] = {"repos": slot["repos"], "candidates": ranked, "to_screen": [c["instance_id"] for c in screen]}
        for c in screen:
            r = rows[c["instance_id"]]
            name = r["repo"].split("/")[1]
            inst = {"eval_script": r["eval_script"]}
            task = {
                "id": c["instance_id"], "category": slot["name"],
                "source": "SWE-bench/SWE-bench_Multilingual@" + ds["revision"][:12] + ":" + c["instance_id"],
                "sources": {name: "git:https://github.com/%s@%s" % (r["repo"], r["base_commit"])},
                "repos": [name],
                "request": r["problem_statement"],
                "hidden": [{"repo": name, "patch": r["test_patch"]}],
                "checks": [{"repo": name, "run": ["sh", "-c", adapt(swe.test_command(inst), m)]}],
                "timeout": m["budgets"]["task_timeout"],
            }
            with open(os.path.join(a.out, "tasks", c["instance_id"] + ".yaml"), "w", encoding="utf-8") as f:
                json.dump(task, f, indent=1, ensure_ascii=False)
            with open(os.path.join(a.out, "gold", c["instance_id"] + ".patch"), "w") as f:
                f.write(r["patch"])
            print(slot["name"], c["instance_id"], r["base_commit"][:12], "|", task["checks"][0]["run"][2][:110])
    with open(os.path.join(HERE, "candidates.json"), "w") as f:
        json.dump(out, f, indent=1)


if __name__ == "__main__":
    main()
