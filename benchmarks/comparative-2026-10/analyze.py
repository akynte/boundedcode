#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Analysis (protocol.md section 6) of the recorded runs. It reads only
results/: runs/, replay/, failure-analysis.json (the analyst's categories)
and the runs' state databases, and writes results/results.json (per task)
and results/summary.json (paired counts, verification agreement,
resources), then prints the report tables in Markdown.

usage: analyze.py
"""
import json
import math
import os
import sqlite3

HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(HERE, "results")


def load(p):
    try:
        return json.load(open(p))
    except (OSError, ValueError):
        return None


def peaks(path):
    """Peak MiB per column of a sampler log, and the minimum available."""
    out = {}
    try:
        lines = open(path).read().split("\n")
    except OSError:
        return out
    cols = lines[0].split()[1:]
    for line in lines[1:]:
        f = line.split()
        if len(f) != len(cols) + 1:
            continue
        for c, v in zip(cols, f[1:]):
            v = int(v)
            if c == "mem_avail":
                out[c + "_min"] = min(out.get(c + "_min", v), v)
            else:
                out[c] = max(out.get(c, 0), v)
    return out


def model_calls(state_dir, task_id):
    db = os.path.join(os.path.expanduser(state_dir or ""), "data", "state.db")
    if not os.path.exists(db):
        return None
    try:
        con = sqlite3.connect("file:%s?mode=ro" % db, uri=True)
        n, p, c = con.execute("SELECT count(*), coalesce(sum(prompt_tokens),0), coalesce(sum(completion_tokens),0) FROM model_calls WHERE task_id = ?", (task_id,)).fetchone()
        con.close()
        return {"calls": n, "prompt_tokens": p, "completion_tokens": c}
    except sqlite3.Error:
        return None


def run(sysname, tid, runs="runs"):
    rep = load(os.path.join(RES, runs, "%s-%s.json" % (sysname, tid)))
    if not rep or not rep.get("results"):
        return None
    r = rep["results"][0]
    out = {
        "hidden_pass": r["success"], "status": r.get("task_status", ""), "attempts": r.get("attempts", 0),
        "wall_seconds": round(r.get("wall_seconds", 0)), "processed_tokens": r.get("local_tokens", 0),
        "generated_tokens": r.get("generated_tokens", 0), "cached_prompt_tokens": r.get("cached_prompt_tokens", 0),
        "error": r.get("error", ""), "failed_checks": [c[:300] for c in r.get("failed_checks") or []],
        "patch_bytes": os.path.getsize(os.path.expanduser(r["agent_patch"])) if r.get("agent_patch") and os.path.exists(os.path.expanduser(r["agent_patch"])) else 0,
        "memory_peak_mib": peaks(os.path.join(RES, runs, "%s-%s.mem.log" % (sysname, tid))),
        "model": model_calls(r.get("state_dir"), r.get("task_id")),
    }
    if sysname == "B":
        out["verification"] = "task_verified" if r.get("self_verified") else ("tests_green" if r.get("tests_green") else "not_green")
    return out


def replay(tid, replays="replay"):
    r = load(os.path.join(RES, replays, "R-%s.json" % tid))
    if not r:
        return None
    if "results" not in r:
        return {"replayed": False, "why": r.get("why", "")}
    x = r["results"][0]
    return {"replayed": True, "hidden_pass": x["success"],
            "verification": "task_verified" if x.get("self_verified") else ("tests_green" if x.get("tests_green") else "not_green")}


def mcnemar(b, c):
    n = b + c
    if n == 0:
        return 1.0
    k = min(b, c)
    return min(1.0, 2 * sum(math.comb(n, i) for i in range(k + 1)) / 2 ** n)


ARMS = [
    # (name, baseline runs, replays of its patches): the corrected arm is the
    # primary comparison (deviations.md D3); the original arm is kept.
    ("corrected", "runs-baseline-corrected", "replay-baseline-corrected"),
    ("original", "runs", "replay"),
]


def analyse(arm, oruns, replays, frozen, cats):
    rows = []
    for t in frozen["tasks"]:
        b, o, rp = run("B", t["id"]), run("O", t["id"], oruns), replay(t["id"], replays)
        for k, v in (("B", b), ("O", o)):
            if v and not v["hidden_pass"]:
                v["failure"] = cats.get("%s-%s" % (k if k == "B" else "O-" + arm, t["id"]))
        rows.append({"id": t["id"], "slot": t["slot"], "stratum": t["stratum"], "B": b, "O": o, "replay": rp})
    done = [r for r in rows if r["B"] and r["O"]]
    pair = {"both": 0, "B_only": 0, "O_only": 0, "neither": 0}
    for r in done:
        bp, op = r["B"]["hidden_pass"], r["O"]["hidden_pass"]
        pair["both" if bp and op else "B_only" if bp else "O_only" if op else "neither"] += 1
    strata = {}
    for r in done:
        st = strata.setdefault(r["stratum"], {"tasks": 0, "B_pass": 0, "O_pass": 0})
        st["tasks"] += 1
        st["B_pass"] += r["B"]["hidden_pass"]
        st["O_pass"] += r["O"]["hidden_pass"]
    # Verification agreement over every gated patch: B's runs and the
    # replays of O's patches.
    gated = [("B", r["id"], r["B"]["verification"], r["B"]["hidden_pass"]) for r in rows if r["B"]]
    gated += [("O-replay", r["id"], r["replay"]["verification"], r["replay"]["hidden_pass"]) for r in rows if r["replay"] and r["replay"].get("replayed")]

    def agree(pred, src=None):
        sel = [g for g in gated if pred(g[2]) and (src is None or g[0] == src)]
        return {"selected": len(sel), "hidden_pass": sum(g[3] for g in sel)}
    verif = {
        "patches_gated": len(gated),
        "task_verified": agree(lambda v: v == "task_verified"),
        "checks_pass": agree(lambda v: v in ("task_verified", "tests_green")),
        "tests_green_only": agree(lambda v: v == "tests_green"),
        "not_green": agree(lambda v: v == "not_green"),
        "baseline_patches_not_green": agree(lambda v: v == "not_green", "O-replay"),
        "false_passes_task_verified": [g[:2] for g in gated if g[2] == "task_verified" and not g[3]],
        "missed_passes": [g[:2] for g in gated if g[2] != "task_verified" and g[3]],
        "replay_reproduced_baseline": [r["id"] for r in rows if r["replay"] and r["replay"].get("replayed") and r["O"] and r["replay"]["hidden_pass"] == r["O"]["hidden_pass"]],
        "replay_mismatch": [r["id"] for r in rows if r["replay"] and r["replay"].get("replayed") and r["O"] and r["replay"]["hidden_pass"] != r["O"]["hidden_pass"]],
    }

    def tot(k, f):
        return sum((r[k][f] or 0) for r in done)
    summary = {
        "arm": arm, "baseline_runs": oruns, "tasks_frozen": len(rows), "tasks_with_both_runs": len(done), "paired": pair,
        "mcnemar_exact_p_two_sided": round(mcnemar(pair["B_only"], pair["O_only"]), 4), "strata": strata,
        "verification": verif,
        "totals": {sn: {"hidden_pass": tot(sn, "hidden_pass"), "wall_seconds": tot(sn, "wall_seconds"),
                        "processed_tokens": tot(sn, "processed_tokens"), "generated_tokens": tot(sn, "generated_tokens")} for sn in ("B", "O")},
        "api_cost_usd": 0, "api_cost_note": "local model; no API cost. Energy was not measured.",
    }
    return rows, summary


def main():
    frozen = load(os.path.join(HERE, "frozen-tasks.json"))
    cats = load(os.path.join(RES, "failure-analysis.json")) or {}
    out_rows, out_sum = {}, {}
    for arm, oruns, replays in ARMS:
        rows, summary = analyse(arm, oruns, replays, frozen, cats)
        out_rows[arm], out_sum[arm] = rows, summary
        print("## Baseline arm: %s" % arm)
        print()
        print("| Task | Stratum | BoundedCode: hidden | B: verification | B: attempts | B: time | B: tokens | Baseline: hidden | O: time | O: tokens | Gate replay of O's patch |")
        print("|---|---|---|---|---|---|---|---|---|---|---|")
        yn = {True: "**pass**", False: "fail", None: "–"}
        for r in rows:
            b, o, rp = r["B"] or {}, r["O"] or {}, r["replay"] or {}
            rps = ("%s (hidden %s)" % (rp["verification"], "pass" if rp["hidden_pass"] else "fail")) if rp.get("replayed") else (rp.get("why") or "–")
            print("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |" % (
                r["id"], r["stratum"], yn[b.get("hidden_pass")], b.get("verification", "–"), b.get("attempts", "–"),
                fmt_t(b.get("wall_seconds")), fmt_k(b.get("processed_tokens")), yn[o.get("hidden_pass")],
                fmt_t(o.get("wall_seconds")), fmt_k(o.get("processed_tokens")), rps))
        print()
        print(json.dumps(summary, indent=1))
        print()
    json.dump(out_rows, open(os.path.join(RES, "results.json"), "w"), indent=1)
    json.dump(out_sum, open(os.path.join(RES, "summary.json"), "w"), indent=1)


def fmt_t(s):
    return "–" if s is None else "%dm%02ds" % (s // 60, s % 60)


def fmt_k(n):
    return "–" if n is None else "%.0fk" % (n / 1000)


if __name__ == "__main__":
    main()
