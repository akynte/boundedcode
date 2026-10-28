#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Prepares the retrieval benchmark (benchmarks/retrieval/manifest.json).

Downloads the pinned datasets, checks their sha256, selects instances and
the development/held-out split by the manifest's seeded rule (nothing here
looks at an instance's content), clones each repository once (blobs
fetched lazily) and checks out every selected base commit once so later
runs need no network. Writes OUT/tasks.json:

  {"id", "repo", "language", "split", "base", "request", "patch",
   "failing_tests"}

The fix's patch is kept so the benchmark can derive the gold files and
symbols; it is never shown to a retrieval approach.

usage: uv run --with pyarrow python3 prepare.py [--out DIR] [--no-clone]
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))


def sha256_file(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def rank(seed, key):
    return hashlib.sha256((seed + key).encode()).hexdigest()


def fetch(ds, out):
    url = "https://huggingface.co/datasets/%s/resolve/%s/%s" % (ds["hf"], ds["revision"], ds["file"])
    dst = os.path.join(out, "datasets", ds["hf"].replace("/", "__") + "-" + ds["revision"][:12] + ".parquet")
    if not os.path.exists(dst):
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        urllib.request.urlretrieve(url, dst + ".part")
        os.rename(dst + ".part", dst)
    got = sha256_file(dst)
    if got != ds["file_sha256"]:
        sys.exit("%s: sha256 %s != pinned %s" % (dst, got, ds["file_sha256"]))
    return dst


def git(*args, cwd=None):
    subprocess.run(["git", "-c", "core.hooksPath=/dev/null", *args], cwd=cwd, check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.expanduser("~/.cache/bc-retrieval"))
    ap.add_argument("--no-clone", action="store_true")
    a = ap.parse_args()
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    import pyarrow.parquet as pq

    rows = []
    for ds in m["datasets"].values():
        rows += pq.read_table(fetch(ds, a.out)).to_pylist()
    langs = m["languages"]
    by_repo = {}
    for r in rows:
        if r["repo"] in langs:
            by_repo.setdefault(r["repo"], []).append(r)
    # The split: per language, by seeded repository rank.
    split = {}
    per_lang = {}
    for repo in by_repo:
        per_lang.setdefault(langs[repo], []).append(repo)
    for lang, repos in per_lang.items():
        repos.sort(key=lambda x: rank(m["seed"], x))
        n = round(m["dev_share"] * len(repos))
        if len(repos) >= 3:
            n = max(n, 1)
        for i, repo in enumerate(repos):
            split[repo] = "dev" if i < n else "heldout"
    tasks = []
    for repo in sorted(by_repo):
        inst = sorted(by_repo[repo], key=lambda r: rank(m["seed"], r["instance_id"]))[: m["per_repo"]]
        for r in inst:
            ftp = r["FAIL_TO_PASS"]
            if isinstance(ftp, str):
                ftp = json.loads(ftp)
            tasks.append({"id": r["instance_id"], "repo": repo, "language": langs[repo], "split": split[repo],
                          "base": r["base_commit"], "request": r["problem_statement"], "patch": r["patch"],
                          "failing_tests": list(ftp)})
    os.makedirs(a.out, exist_ok=True)
    with open(os.path.join(a.out, "tasks.json"), "w") as f:
        json.dump(tasks, f, indent=1)
    print("%d tasks, %d repositories: %d dev, %d held out" % (len(tasks), len(by_repo),
          sum(t["split"] == "dev" for t in tasks), sum(t["split"] == "heldout" for t in tasks)))
    if a.no_clone:
        return
    for repo in sorted(by_repo):
        d = os.path.join(a.out, "repos", repo.replace("/", "__"))
        if not os.path.isdir(d):
            print("clone", repo, flush=True)
            git("clone", "-q", "--filter=blob:none", "--no-checkout", "https://github.com/%s" % repo, d)
        for t in tasks:
            if t["repo"] == repo:
                git("checkout", "-q", "--detach", "-f", t["base"], cwd=d)


if __name__ == "__main__":
    main()
