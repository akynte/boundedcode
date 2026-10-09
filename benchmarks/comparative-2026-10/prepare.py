#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Prepares the sources of the candidates to screen (or of the frozen task
set): each repository is checked out at its base commit, without history,
into $EVAL/sources (the layout the benchmark harness reads through
BC_BENCH_SOURCES), and its dependencies are installed by the manifest's
recipe inside the sandbox image, with network, into the evaluation's own
caches ($EVAL/gomod, $EVAL/cargo, $EVAL/m2) or the checkout (node_modules,
vendor). Agent runs and acceptance checks later run without network.

usage: prepare.py [--eval DIR] [ID ...]   (default: every candidate to screen)
"""
import argparse
import json
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
IMAGE = "boundedcode-openhands:local"


def checkout(repo, base, dst):
    tmp = dst + ".partial"
    shutil.rmtree(tmp, ignore_errors=True)
    os.makedirs(tmp)
    git = ["git", "-c", "core.hooksPath=/dev/null"]
    subprocess.run(git + ["init", "-q"], cwd=tmp, check=True)
    subprocess.run(git + ["fetch", "-q", "--depth", "1", "https://github.com/" + repo, base], cwd=tmp, check=True)
    subprocess.run(git + ["-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"], cwd=tmp, check=True)
    head = subprocess.run(git + ["rev-parse", "HEAD"], cwd=tmp, check=True, capture_output=True, text=True).stdout.strip()
    if head != base:
        sys.exit("%s: checked out %s, want %s" % (repo, head, base))
    shutil.rmtree(os.path.join(tmp, ".git"))
    return tmp


def install(src, ev, steps, log):
    """Runs the recipe in the sandbox image as this user, with network."""
    for d in ("gomod", "cargo", "m2/repository"):
        os.makedirs(os.path.join(ev, d), exist_ok=True)
    env = {
        # The image disables the Go proxy (runs are offline); preparation
        # is the one step that downloads.
        "HOME": "/tmp/home", "GOMODCACHE": ev + "/gomod", "GOFLAGS": "-mod=mod", "GOTOOLCHAIN": "local",
        "GOPROXY": "https://proxy.golang.org,direct", "COREPACK_ENABLE_DOWNLOAD_PROMPT": "0", "CARGO_NET_OFFLINE": "false",
        "CARGO_HOME": ev + "/cargo", "MAVEN_ARGS": "-Dmaven.repo.local=" + ev + "/m2/repository",
        "COMPOSER_HOME": "/tmp/home/composer", "npm_config_cache": "/tmp/home/npm",
    }
    args = ["docker", "run", "--rm", "--network", "bridge", "--user", "%d:%d" % (os.getuid(), os.getgid()),
            "--tmpfs", "/tmp/home:rw,exec,size=4g,mode=1777", "--ulimit", "nofile=65536:65536", "-v", src + ":" + src, "-v", ev + ":" + ev, "-w", src]
    for k, v in env.items():
        args += ["-e", k + "=" + v]
    args += [IMAGE, "sh", "-c", " && ".join("(" + s + ")" for s in steps)]
    with open(log, "a") as f:
        f.write("$ %s\n" % " && ".join(steps))
        f.flush()
        return subprocess.run(args, stdout=f, stderr=subprocess.STDOUT).returncode


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval", default=os.path.expanduser("~/.cache/bc-comparative"))
    ap.add_argument("ids", nargs="*")
    a = ap.parse_args()
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    c = json.load(open(os.path.join(HERE, "candidates.json")))
    want = set(a.ids)
    todo = []
    for slot in c["slots"].values():
        for cand in slot["candidates"]:
            if (want and cand["instance_id"] in want) or (not want and cand["instance_id"] in slot["to_screen"]):
                todo.append(cand)
    sp = os.path.join(a.eval, "prepare-status.json")
    status = json.load(open(sp)) if os.path.exists(sp) else {}
    for cand in todo:
        repo, base = cand["repo"], cand["base"]
        dst = os.path.join(a.eval, "sources", repo.replace("/", "__") + "@" + base[:12])
        log = os.path.join(a.eval, "prepare-logs", cand["instance_id"] + ".log")
        os.makedirs(os.path.dirname(log), exist_ok=True)
        if os.path.exists(os.path.join(dst, ".bc-source-ready")):
            status[cand["instance_id"]] = "ready (cached)"
            continue
        print("preparing", cand["instance_id"], flush=True)
        tmp = checkout(repo, base, dst)
        rc = install(tmp, a.eval, m["dependencies"][repo], log)
        if rc != 0:
            status[cand["instance_id"]] = "dependency install failed (exit %d), see %s" % (rc, log)
            shutil.rmtree(tmp, ignore_errors=True)
            continue
        with open(os.path.join(tmp, ".bc-source-ready"), "w") as f:
            f.write("git:https://github.com/%s@%s\n" % (repo, base))
        shutil.rmtree(dst, ignore_errors=True)
        os.rename(tmp, dst)
        status[cand["instance_id"]] = "ready"
    with open(sp, "w") as f:
        json.dump(status, f, indent=1)
    for k, v in status.items():
        print(k, v)


if __name__ == "__main__":
    main()
