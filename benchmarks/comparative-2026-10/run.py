#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Runs the frozen comparative matrix (protocol.md): for every task in
frozen-tasks.json, BoundedCode (B) and the baseline (O), in the frozen
order, strictly one at a time, with a memory sampler alongside.

Resumable: a (system, task) pair whose result file exists is skipped.
Every run writes, under results/runs/:
  <sys>-<id>.json          the harness's report (machine-readable)
  <sys>-<id>.stderr.log    the harness's progress output
  <sys>-<id>.mem.log       memory samples (sampler.py)
and appends one line to results/run-log.jsonl.

usage: run.py [--eval DIR] [--only ID ...] [--binary PATH]
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


def env_for(ev):
    e = dict(os.environ)
    e.update({
        "BOUNDEDCODE_HOME": os.path.join(ev, "home"),
        "BC_BENCH_SOURCES": os.path.join(ev, "sources"),
        "GOMODCACHE": os.path.join(ev, "gomod"),
        "CARGO_HOME": os.path.join(ev, "cargo"),
        "BC_MAVEN_REPOSITORY": os.path.join(ev, "m2", "repository"),
        "BOUNDEDCODE_SECRETS": "file",
        "TZ": "UTC",
        "GOTOOLCHAIN": "local",
    })
    return e


def sh(*args):
    try:
        return subprocess.run(args, capture_output=True, text=True, timeout=60).stdout.strip()
    except (OSError, subprocess.TimeoutExpired):
        return ""


def environment(binary):
    """What the runs used, recorded once (results/environment.json)."""
    pid = sh("pgrep", "-x", "llama-server").split("\n")[0]
    args = open("/proc/%s/cmdline" % pid).read().split("\0") if pid else []
    return {
        "recorded_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "boundedcode_version": sh(binary, "version"),
        "boundedcode_sha256": hashlib.sha256(open(binary, "rb").read()).hexdigest(),
        "git_commit": sh("git", "-C", HERE, "rev-parse", "HEAD"),
        "git_dirty": bool(sh("git", "-C", HERE, "status", "--porcelain", "--untracked-files=no")),
        "sandbox_image": sh("docker", "image", "inspect", "-f", "{{.Id}}", "boundedcode-openhands:local"),
        "llama_server_args": [a for a in args if a],
        "llama_build_info": open(os.path.expanduser("~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/BUILD_INFO")).read() if os.path.exists(os.path.expanduser("~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/BUILD_INFO")) else "",
        "host": {"kernel": sh("uname", "-sr"), "cpu": sh("sh", "-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"),
                 "mem_total_kib": sh("sh", "-c", "grep MemTotal /proc/meminfo | awk '{print $2}'"),
                 "gpu": sh("nvidia-smi", "--query-gpu=name,driver_version,memory.total", "--format=csv,noheader"),
                 "docker": sh("docker", "version", "--format", "{{.Server.Version}} {{.Server.Os}}/{{.Server.Arch}}")},
        "tools": {"go": sh("go", "version"), "codebase_memory_mcp": sh("codebase-memory-mcp", "--version"), "gitleaks": sh("gitleaks", "version")},
    }


def scrub(*paths):
    """Replaces the home directory with ~ in stored records (no other change)."""
    home = os.path.expanduser("~")
    for p in paths:
        if os.path.exists(p):
            s = open(p, errors="surrogateescape").read()
            if home in s:
                open(p, "w", errors="surrogateescape").write(s.replace(home, "~"))


def order(seed, tid):
    """True when B runs first for this task (frozen by the seed)."""
    return int(hashlib.sha256((seed + "order:" + tid).encode()).hexdigest(), 16) % 2 == 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval", default=os.path.expanduser("~/.cache/bc-comparative"))
    ap.add_argument("--binary", default=os.path.join(HERE, "..", "..", "bin", "boundedcode"))
    ap.add_argument("--only", nargs="*")
    ap.add_argument("--systems", default="B,O", help="which systems to run (deviation D3 re-runs O alone)")
    ap.add_argument("--outdir", default="runs", help="directory under results/ for the run records")
    ap.add_argument("--pilot", help="run this unpicked candidate as the declared pilot (results/pilot/, excluded from analysis)")
    a = ap.parse_args()
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    frozen = json.load(open(os.path.join(HERE, "frozen-tasks.json")))
    if a.pilot:
        frozen = {"tasks": [{"id": a.pilot}]}
    ev = a.eval
    home_cfg = os.path.join(ev, "home", "config")
    os.makedirs(home_cfg, exist_ok=True)
    shutil.copy(os.path.join(HERE, "config.yaml"), os.path.join(home_cfg, "config.yaml"))
    out = os.path.join(HERE, "results", "pilot" if a.pilot else a.outdir)
    os.makedirs(out, exist_ok=True)
    envp = os.path.join(out, "environment.json") if a.outdir != "runs" else os.path.join(HERE, "results", "environment.json")
    if not os.path.exists(envp):
        e = environment(a.binary)
        if e["git_dirty"]:
            sys.exit("the checkout has uncommitted changes to tracked files: run from the frozen commit")
        json.dump(e, open(envp, "w"), indent=1)
        scrub(envp)
    taskdir = os.path.join(ev, "tasks", "tasks" if a.pilot else "frozen")
    env = env_for(ev)
    for t in frozen["tasks"]:
        tid = t["id"]
        if a.only and tid not in a.only:
            continue
        systems = [x for x in (["B", "O"] if order(m["seed"], tid) else ["O", "B"]) if x in a.systems.split(",")]
        for sysname in systems:
            stem = os.path.join(out, "%s-%s" % (sysname, tid))
            if os.path.exists(stem + ".json"):
                continue
            cmd = [a.binary, "bench", "tasks", "--tasks", taskdir, "--only", tid, "--out", stem + ".json", "--serena", "off"]
            if sysname == "O":
                cmd.append("--baseline")
            print(time.strftime("%H:%M:%S"), "run", sysname, tid, flush=True)
            sampler = subprocess.Popen([sys.executable, os.path.join(HERE, "sampler.py"), stem + ".mem.log"])
            start = time.time()
            with open(stem + ".stderr.log", "w") as f:
                # The harness enforces the 60-minute task limit; this outer
                # limit only catches a hung harness.
                try:
                    rc = subprocess.run(cmd, env=env, stdout=f, stderr=subprocess.STDOUT, timeout=95 * 60).returncode
                except subprocess.TimeoutExpired:
                    rc = "outer-timeout"
            wall = time.time() - start
            sampler.terminate()
            sampler.wait()
            scrub(stem + ".json", stem + ".stderr.log")
            rec = {"system": sysname, "task": tid, "exit": rc, "wall_seconds": round(wall, 1),
                   "started_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(start)),
                   "command": [c.replace(os.path.expanduser("~"), "~") for c in cmd[1:]]}
            log = "pilot-log.jsonl" if a.pilot else ("run-log.jsonl" if a.outdir == "runs" else a.outdir + "-log.jsonl")
            with open(os.path.join(HERE, "results", log), "a") as f:
                f.write(json.dumps(rec) + "\n")
            print(time.strftime("%H:%M:%S"), "done", sysname, tid, "exit", rc, "%.0fs" % wall, flush=True)


if __name__ == "__main__":
    main()
