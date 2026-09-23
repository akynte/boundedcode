#!/usr/bin/env python3
"""Freeze evals/evidence-v1/manifest.json — design instruction §27.

Run once, after selection/select_tasks.py has produced selection.json and
before any BoundedCode execution against a selected task. Writes
manifest.json and prints:

    Evidence Suite v1 manifest frozen:
    <sha256>

No task may begin before that line is printed. The hash covers every input
this suite's result depends on and does not already carry its own hash
(selection.json and the jev-profile do; this manifest's own hash is the
single number a reader checks first).
"""
import hashlib
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).parent.parent.parent  # repo root
SUITE_DIR = Path(__file__).parent


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True).stdout.strip()


def main():
    selection_path = SUITE_DIR / "selection" / "selection.json"
    profile_path = SUITE_DIR / "jev-profile.yaml"
    if not selection_path.exists():
        print(f"error: {selection_path} does not exist; run selection/select_tasks.py first", file=sys.stderr)
        sys.exit(1)
    if not profile_path.exists():
        print(f"error: {profile_path} does not exist", file=sys.stderr)
        sys.exit(1)

    selection = json.loads(selection_path.read_text())
    rev = git("rev-parse", "HEAD")
    dirty = bool(git("status", "--porcelain"))

    manifest = {
        "suite_version": "evidence-v1",
        "schema_version": 1,
        # Key names unchanged across the rename: manifest.json is frozen and
        # hash-pinned, and these are the keys the frozen copy holds.
        "local_engineer_commit": rev,
        "local_engineer_dirty": dirty,
        "task_ids": {
            "swe_bench_pro_verified": [r["instance_id"] for r in selection["swebench_pro_verified"]["selected"]],
            "swe_atlas_test_writing": [r["task_id"] for r in selection["swe_atlas_test_writing"]["selected"]],
            "swe_atlas_refactoring": [r["task_id"] for r in selection["swe_atlas_refactoring"]["selected"]],
        },
        "benchmark_revisions": {
            "swe_bench_pro_verified": {
                "dataset": "opencompass/SWEBench-Pro-Verified",
                "source": "https://huggingface.co/datasets/opencompass/SWEBench-Pro-Verified",
                "paper": "arXiv:2609.08149",
                "license": "apache-2.0",
                "evaluator": "AgentCompass (https://github.com/open-compass/AgentCompass), "
                             "FAIL_TO_PASS/PASS_TO_PASS test-based resolution",
            },
            "swe_atlas": {
                "repo": "https://github.com/scaleapi/SWE-Atlas",
                "paper": "arXiv:2605.08366",
                "license": "apache-2.0",
                "evaluator": "harbor (https://github.com/laude-institute/harbor, pinned v0.18.0) + "
                             "Modal sandboxes; rubric grading via Claude Opus 4.5 as judge model "
                             "plus programmatic checks",
            },
        },
        "selection_algorithm": "evals/evidence-v1/selection/select_tasks.py",
        "selection_seed": selection["seed"],
        "selection_sha256": sha256_file(selection_path),
        "jev_profile_sha256": sha256_file(profile_path),
        "generator_model": "UNSET — no generator credentials were available when this manifest "
                            "was frozen; see docs/evidence/evidence-v1.md 'Environment'. Must be "
                            "filled in and this manifest re-frozen (as a new suite_version, if "
                            "already begun) before Arm A/B execution.",
        "generator_settings": {},
        "budget": {
            "attempts_per_task": "Local Engineer default (see le.yaml); not overridden for this suite",
            "runs_per_task_per_arm": 1,
        },
        "timeout": "per-task, per benchmark's own task.toml / instance defaults "
                   "(SWE Atlas: agent.timeout_sec in each task.toml; SWE-Bench Pro "
                   "Verified: AgentCompass's default)",
        "network_policy": {
            "swe_bench_pro_verified": "AgentCompass anti-leakage: code-hosting domains "
                                       "(GitHub, GitLab, Gitee, Bitbucket, mirrors) blocked "
                                       "during the agent phase; dependency-fetch services allowed.",
            "swe_atlas": "harbor allowlist network_mode; only package registries and language "
                         "toolchains reachable during the agent phase (see each task's "
                         "agent.allowed_hosts in task.toml).",
        },
        "container_identifiers": "SWE-Bench Pro Verified: dockerhub_tag per selected instance, "
                                  "recorded in selection/cache/swebenchpro_all.json. SWE Atlas: "
                                  "ghcr.io image per task, resolved from each task's "
                                  "environment/Dockerfile FROM line at build time by harbor.",
    }

    out_path = SUITE_DIR / "manifest.json"
    body = json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    out_path.write_text(body)
    digest = hashlib.sha256(body.encode()).hexdigest()

    print("Evidence Suite v1 manifest frozen:")
    print(digest)


if __name__ == "__main__":
    main()
