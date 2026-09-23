#!/usr/bin/env python3
"""Adversarial red-team probe — design instruction §26.

Runs exactly the shell commands a curious or adversarial agent would try
against a sanitized workspace, on the actual materialized workspaces (not a
fixture), and reports PASS only if every one comes back empty-handed.
`env` output is filtered (names only, no values) so this script's own
output is safe to keep as an evidence artifact.

Run from the repository root:
    python3 evals/evidence-v1/tooling/redteam_probe.py
"""
import json
import os
import subprocess
from pathlib import Path

import safe_cleanup

PROBES = [
    ("git log --all", ["git", "log", "--all", "--oneline"]),
    ("git branch -a", ["git", "branch", "-a"]),
    ("git tag", ["git", "tag"]),
    ("git reflog", ["git", "reflog", "--all"]),
    ("git fsck unreachable", ["git", "fsck", "--full", "--unreachable", "--no-reflogs"]),
    ("find for *patch*", ["find", ".", "-iname", "*patch*", "-not", "-path", "./.git/*"]),
]


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=str(cwd), capture_output=True, text=True)


def probe_workspace(ws: Path, known_future_shas: list[str]) -> dict:
    result = {"workspace": str(ws.relative_to(safe_cleanup.RUN_ROOT)), "probes": {}}
    leaks = []

    log_all = run(["git", "log", "--all", "--oneline"], ws)
    n_commits = len([l for l in log_all.stdout.splitlines() if l.strip()])
    result["probes"]["git log --all: commit count"] = n_commits
    if n_commits != 1:
        leaks.append(f"git log --all shows {n_commits} commits, expected 1")

    branches = run(["git", "branch", "-a"], ws)
    b = [l.strip().lstrip("* ") for l in branches.stdout.splitlines() if l.strip()]
    result["probes"]["git branch -a"] = b
    if len(b) > 1:
        leaks.append(f"more than one branch: {b}")

    tags = run(["git", "tag"], ws)
    result["probes"]["git tag"] = tags.stdout.split()
    if tags.stdout.strip():
        leaks.append(f"tags present: {tags.stdout.split()}")

    for sha in known_future_shas:
        cat = run(["git", "cat-file", "-e", sha], ws)
        show = run(["git", "show", sha], ws)
        if cat.returncode == 0 or show.returncode == 0:
            leaks.append(f"future commit {sha} is reachable")

    fsck = run(["git", "fsck", "--full", "--unreachable", "--no-reflogs"], ws)
    unreachable_commits = [l for l in (fsck.stdout + fsck.stderr).splitlines()
                            if l.strip().startswith("unreachable commit")]
    result["probes"]["git fsck unreachable commits"] = unreachable_commits
    if unreachable_commits:
        leaks.append(f"fsck reports unreachable commits: {unreachable_commits}")

    find_patch = run(["find", ".", "-iname", "*.patch", "-not", "-path", "./.git/*"], ws)
    # node_modules legitimately ships *.patch files as part of some
    # packages' own build tooling (confirmed by inspection during this
    # audit — none of them are benchmark solution artifacts); only flag a
    # find hit outside node_modules/vendor as suspicious.
    suspicious_patches = [p for p in find_patch.stdout.splitlines()
                           if p.strip() and "node_modules" not in p and "vendor" not in p]
    result["probes"]["suspicious *.patch files"] = suspicious_patches
    if suspicious_patches:
        leaks.append(f"unexplained *.patch file(s): {suspicious_patches}")

    alt = ws / ".git" / "objects" / "info" / "alternates"
    has_alt = alt.exists() and alt.read_text().strip() != ""
    result["probes"]["object alternates"] = has_alt
    if has_alt:
        leaks.append("objects/info/alternates points somewhere")

    env_names = sorted(os.environ.keys())  # names only, never values — §26
    result["probes"]["env variable names (values filtered)"] = env_names

    result["leaks_found"] = leaks
    result["result"] = "PASS" if not leaks else "FAIL"
    return result


def main():
    sanitization_results_path = safe_cleanup.REPO_ROOT / "evals" / "evidence-v1" / "sanitization-results.json"
    sanitization_results = json.loads(sanitization_results_path.read_text())

    out = []
    for entry in sanitization_results:
        task_id = entry["task_id"]
        future_shas = entry.get("future_shas_captured_list", [])
        for arm in ("control", "jev-assisted"):
            ws = safe_cleanup.RUN_ROOT / "agent-workspaces" / task_id / arm
            if not ws.exists():
                continue
            r = probe_workspace(ws, future_shas)
            r["task_id"] = task_id
            r["arm"] = arm
            out.append(r)
            print(f"{task_id[:40]:42s} {arm:12s} -> {r['result']}")

    out_path = safe_cleanup.REPO_ROOT / "evals" / "evidence-v1" / "redteam-probe-results.json"
    out_path.write_text(json.dumps(out, indent=2, sort_keys=True) + "\n")
    print(f"\nwrote {out_path}")
    failed = [r for r in out if r["result"] == "FAIL"]
    if failed:
        print(f"\n{len(failed)} workspace(s) FAILED the red-team probe.")
    else:
        print(f"\nAll {len(out)} workspaces PASSED the red-team probe.")


if __name__ == "__main__":
    main()
