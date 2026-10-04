#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Per-task metrics for a `bench tasks` (or `--baseline`) report.

Reads the suite JSON and, for each result, the task's state database and the
agent runtime's persisted event log, and prints JSON with: model calls and
tokens, prompt-cache hit rate, verification runs, Serena and code-graph
calls, frontier escalations, context-pack source tokens, and the tokens of
tool observations (everything the agent read through its tools, including
source files; an upper bound on repository content that entered context).

Usage: scripts/bench-validation-metrics.py REPORT.json [SCREEN.json]
"""

import glob
import json
import os
import sqlite3
import sys


def est(s):
    # Same estimator as contextplan.EstimateTokens.
    return (len(s.encode()) * 10 + 31) // 32


def observations(state_dir, task_id):
    pats = [os.path.join(state_dir, "data", "tasks", task_id, "runtime", "*", "events", "*.json"),
            os.path.join(state_dir, "runtime", "*", "events", "*.json")]
    total, by_tool, actions = 0, {}, 0
    for pat in pats:
        for f in glob.glob(pat):
            try:
                e = json.load(open(f))
            except (OSError, ValueError):
                continue
            if e.get("kind") == "ActionEvent":
                actions += 1
            if e.get("kind") != "ObservationEvent":
                continue
            text = "".join(c.get("text", "") for c in (e.get("observation", {}).get("content") or []) if isinstance(c, dict))
            n = est(text)
            total += n
            t = e.get("tool_name", "?")
            by_tool[t] = by_tool.get(t, 0) + n
    return total, by_tool, actions


def main():
    rep = json.load(open(sys.argv[1]))
    screen = {}
    if len(sys.argv) > 2:
        screen = {r["id"]: r for r in json.load(open(sys.argv[2]))}
    out = []
    for r in rep["results"]:
        sd, tid = r.get("state_dir", ""), r.get("task_id", "")
        m = {"id": r["id"], "category": r["category"], "accepted": r["success"], "task_status": r["task_status"],
             "self_verified": r["self_verified"], "wall_seconds": round(r["wall_seconds"]), "attempts": r["attempts"],
             "condensations": r["condensations"], "resumes": r["resumes"], "interrupted": r.get("interrupted", False),
             "serena_calls": r["intel"]["serena_calls"], "graph_calls": r["intel"]["graph_calls"],
             "context_packs": r["intel"]["context_packs"], "pack_code_tokens": r["intel"]["code_tokens"],
             "pack_tokens": r["intel"]["pack_tokens"], "error": r.get("error", ""), "failed_checks": r.get("failed_checks", [])}
        if r["id"] in screen:
            m["repo_source_tokens"] = screen[r["id"]]["source_tokens"]
        db = os.path.join(sd, "data", "state.db")
        if sd and os.path.exists(db):
            c = sqlite3.connect(db)
            calls, p, comp, cached = c.execute(
                "SELECT count(*), coalesce(sum(prompt_tokens),0), coalesce(sum(completion_tokens),0), coalesce(sum(cached_tokens),0) FROM model_calls WHERE task_id=?", (tid,)).fetchone()
            m.update({"local_model_calls": calls, "local_prompt_tokens": p, "local_output_tokens": comp,
                      "prompt_cache_hit_rate": round(cached / p, 3) if p else None})
            m["verification_runs"] = c.execute("SELECT count(*) FROM verification_runs WHERE task_id=?", (tid,)).fetchone()[0]
            esc = c.execute("SELECT trigger, reason, status, packet_tokens, outcome FROM escalations WHERE task_id=? ORDER BY id", (tid,)).fetchall()
            m["frontier_calls"] = sum(1 for e in esc if e[2] in ("sent", "answered", "failed"))
            m["escalations"] = [{"trigger": e[0], "reason": e[1], "status": e[2], "packet_tokens": e[3], "outcome": e[4]} for e in esc]
        obs, by_tool, actions = observations(sd, tid)
        m.update({"tool_observation_tokens": obs, "tool_observation_tokens_by_tool": by_tool, "agent_tool_calls": actions})
        out.append(m)
    json.dump(out, sys.stdout, indent=1)
    print()


if __name__ == "__main__":
    main()
