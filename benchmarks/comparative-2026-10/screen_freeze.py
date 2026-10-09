#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Gate 1 and the freeze (protocol.md section 3).

screen: for each candidate to screen whose sources are prepared, runs
  `bench tasks --screen-gold` (the hidden acceptance must fail on the base
  and pass with the reference patch, offline in the sandbox) and records
  the result in screening.json.
freeze: picks, per slot and in rank order, the first `tasks_per_slot`
  candidates that passed gate 1; writes frozen-tasks.json (ids, base
  commits, sha256 of each task spec and reference patch) and copies the
  task specs to $EVAL/tasks/frozen for the runs.

usage: screen_freeze.py screen|freeze [--eval DIR] [--binary PATH]
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
from run import env_for  # noqa: E402


def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest()


def screen(a, c):
    path = os.path.join(HERE, "screening.json")
    results = json.load(open(path)) if os.path.exists(path) else {}
    prep = json.load(open(os.path.join(a.eval, "prepare-status.json")))
    env = env_for(a.eval)
    tasks = os.path.join(a.eval, "tasks", "tasks")
    gold = os.path.join(a.eval, "tasks", "gold")
    for slot in c["slots"].values():
        for tid in slot["to_screen"]:
            if tid in results:
                continue
            if not prep.get(tid, "").startswith("ready"):
                if a.final:
                    results[tid] = {"valid": False, "stage": "prepare", "detail": prep.get(tid, "not prepared")}
                    json.dump(results, open(path, "w"), indent=1)
                continue
            out = os.path.join(a.eval, "screening", tid + ".json")
            os.makedirs(os.path.dirname(out), exist_ok=True)
            print(time.strftime("%H:%M:%S"), "screen", tid, flush=True)
            start = time.time()
            p = subprocess.run([a.binary, "bench", "tasks", "--tasks", tasks, "--only", tid, "--screen-gold", gold, "--out", out],
                               env=env, capture_output=True, text=True)
            try:
                r = json.load(open(out))[0]
                results[tid] = {"valid": r["valid"], "stage": "gate1", "base_fails": r["base_fails"], "gold_passes": r["gold_passes"],
                                "error": r.get("error", ""), "seconds": round(time.time() - start),
                                "base_tail": (r.get("base_output") or "")[-300:], "gold_tail": (r.get("gold_output") or "")[-300:]}
            except (OSError, ValueError, IndexError, KeyError):
                results[tid] = {"valid": False, "stage": "gate1", "error": "screening produced no result: " + p.stderr[-400:]}
            json.dump(json.loads(json.dumps(results).replace(os.path.expanduser("~"), "~")), open(path, "w"), indent=1)
            print("  ", tid, "valid" if results[tid]["valid"] else "INVALID", results[tid].get("error", "")[:120], flush=True)


def freeze(a, c, m):
    res = json.load(open(os.path.join(HERE, "screening.json")))
    frozen = {"seed": m["seed"], "dataset_revision": m["dataset"]["revision"], "tasks": [], "slots": {}}
    dst = os.path.join(a.eval, "tasks", "frozen")
    shutil.rmtree(dst, ignore_errors=True)
    os.makedirs(dst)
    strata = {s["name"]: s["stratum"] for s in m["slots"]}
    for name, slot in c["slots"].items():
        picked = [t for t in slot["to_screen"] if res.get(t, {}).get("valid")][: m["tasks_per_slot"]]
        frozen["slots"][name] = {"screened": slot["to_screen"], "valid": [t for t in slot["to_screen"] if res.get(t, {}).get("valid")], "picked": picked}
        for tid in picked:
            spec = os.path.join(a.eval, "tasks", "tasks", tid + ".yaml")
            shutil.copy(spec, dst)
            t = json.load(open(spec))
            cand = next(x for x in slot["candidates"] if x["instance_id"] == tid)
            frozen["tasks"].append({"id": tid, "slot": name, "stratum": strata[name], "repo": cand["repo"], "base": cand["base"],
                                    "acceptance": t["checks"][0]["run"][2], "task_spec_sha256": sha(spec),
                                    "reference_patch_sha256": sha(os.path.join(a.eval, "tasks", "gold", tid + ".patch"))})
    json.dump(frozen, open(os.path.join(HERE, "frozen-tasks.json"), "w"), indent=1)
    for t in frozen["tasks"]:
        print(t["slot"], t["id"])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("step", choices=["screen", "freeze"])
    ap.add_argument("--eval", default=os.path.expanduser("~/.cache/bc-comparative"))
    ap.add_argument("--binary", default=os.path.join(ROOT, "bin", "boundedcode"))
    ap.add_argument("--final", action="store_true", help="screen: record candidates whose preparation failed as invalid")
    a = ap.parse_args()
    c = json.load(open(os.path.join(HERE, "candidates.json")))
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    screen(a, c) if a.step == "screen" else freeze(a, c, m)


if __name__ == "__main__":
    main()
