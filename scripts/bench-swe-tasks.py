#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Generate BoundedCode benchmark tasks from public SWE-bench-style datasets.

Reads a manifest (instance ids, dataset revisions), downloads the datasets at
those exact revisions, and writes for each instance:

  <out>/tasks/<id>.yaml   task spec for `boundedcode bench tasks`
                          (problem statement as the request; the dataset's
                          test patch as hidden acceptance; its test command)
  <out>/gold/<id>.patch   reference solution, used only by --screen-gold

Dataset content (issue text, patches) is written to the cache, never to this
repository. With --prepare, each repository is checked out at its base commit
into ~/.cache/boundedcode/bench-sources (the layout the harness expects) and
its dependencies are installed there, because task sandboxes are offline.

Run with: uv run --with pyarrow scripts/bench-swe-tasks.py MANIFEST [--prepare]
"""

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.request

ML_URL = "https://huggingface.co/datasets/SWE-bench/SWE-bench_Multilingual/resolve/{rev}/data/test-00000-of-00001.parquet"
MSB_URL = "https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench/resolve/{rev}/{path}"
SOURCES = os.path.expanduser("~/.cache/boundedcode/bench-sources")


def fetch(url, dst):
    if not os.path.exists(dst):
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        urllib.request.urlretrieve(url, dst + ".part")
        os.rename(dst + ".part", dst)
    return dst


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def load_instances(manifest, cache):
    out = {}
    ml = manifest["datasets"]["swe-bench-multilingual"]
    p = fetch(ML_URL.format(rev=ml["revision"]), os.path.join(cache, "datasets", "ml-" + ml["revision"][:12] + ".parquet"))
    import pyarrow.parquet as pq

    for r in pq.read_table(p).to_pylist():
        out[r["instance_id"]] = {
            "repo": r["repo"], "base": r["base_commit"], "request": r["problem_statement"],
            "test_patch": r["test_patch"], "gold": r["patch"], "eval_script": r["eval_script"],
            "dataset": "SWE-bench/SWE-bench_Multilingual@" + ml["revision"][:12], "file_sha256": sha256(p),
        }
    msb = manifest["datasets"]["multi-swe-bench"]
    for path in msb["files"]:
        p = fetch(MSB_URL.format(rev=msb["revision"], path=path), os.path.join(cache, "datasets", "msb-" + msb["revision"][:12] + "-" + os.path.basename(path)))
        with open(p) as f:
            for line in f:
                r = json.loads(line)
                issue = r["resolved_issues"][0] if r["resolved_issues"] else {"title": r["title"], "body": r["body"]}
                out[r["instance_id"]] = {
                    "repo": r["org"] + "/" + r["repo"], "base": r["base"]["sha"],
                    "request": (issue.get("title") or "").strip() + "\n\n" + (issue.get("body") or "").strip(),
                    "test_patch": r["test_patch"], "gold": r["fix_patch"],
                    "tests": sorted(set(r["f2p_tests"]) | set(r["n2p_tests"]) | set(r["s2p_tests"])),
                    "dataset": "ByteDance-Seed/Multi-SWE-bench@" + msb["revision"][:12], "file_sha256": sha256(p),
                }
    return out


def test_command(inst):
    """The dataset's own test command (SWE-bench eval script), or for
    Multi-SWE-bench `go test` over the test patch's packages, restricted to
    the instance's fail/none-to-pass tests."""
    if "eval_script" in inst:
        m = re.search(r"Start Test Output'\n(.*?)\n: '>>>>> End", inst["eval_script"], re.S)
        cmd = m.group(1).strip()
        cmd = re.sub(r"^\((.*)\) \| cat$", r"\1", cmd)
        # pnpm is not in the sandbox image; run the same vitest directly.
        cmd = re.sub(r"^pnpm run test (\S+) --no-watch", r"node_modules/.bin/vitest run \1", cmd)
        return cmd
    pkgs = sorted({"./" + os.path.dirname(f) for f in re.findall(r"^diff --git a/(\S+)", inst["test_patch"], re.M) if f.endswith("_test.go")})
    names = sorted({t.split("/")[0] for t in inst["tests"]})
    return "go test -v %s -run '^(%s)$'" % (" ".join(pkgs), "|".join(names))


def source_dir(repo, base):
    return os.path.join(SOURCES, repo.replace("/", "__") + "@" + base[:12])


def prepare(repo, base, deps):
    d = source_dir(repo, base)
    if os.path.exists(os.path.join(d, ".bc-source-ready")):
        return d
    tmp = d + ".partial"
    shutil.rmtree(tmp, ignore_errors=True)
    os.makedirs(tmp)
    git = ["git", "-c", "core.hooksPath=/dev/null"]
    subprocess.run(git + ["init", "-q"], cwd=tmp, check=True)
    subprocess.run(git + ["fetch", "-q", "--depth", "1", "https://github.com/" + repo, base], cwd=tmp, check=True)
    subprocess.run(git + ["-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"], cwd=tmp, check=True)
    shutil.rmtree(os.path.join(tmp, ".git"))
    for step in deps:
        print("  $", step, flush=True)
        subprocess.run(step, shell=True, cwd=tmp, check=True)
    with open(os.path.join(tmp, ".bc-source-ready"), "w") as f:
        f.write("https://github.com/%s@%s\n" % (repo, base))
    shutil.rmtree(d, ignore_errors=True)
    os.rename(tmp, d)
    return d


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("manifest")
    ap.add_argument("--out", default=os.path.expanduser("~/.cache/boundedcode/bench-validation"))
    ap.add_argument("--prepare", action="store_true", help="check out sources and install dependencies (network)")
    a = ap.parse_args()
    manifest = json.load(open(a.manifest))
    insts = load_instances(manifest, a.out)
    os.makedirs(os.path.join(a.out, "tasks"), exist_ok=True)
    os.makedirs(os.path.join(a.out, "gold"), mode=0o700, exist_ok=True)
    for t in manifest["tasks"]:
        inst = insts[t["instance_id"]]
        name = inst["repo"].split("/")[1]
        spec = {
            "id": t["instance_id"], "category": t["category"],
            "source": inst["dataset"] + ":" + t["instance_id"],
            "sources": {name: "git:https://github.com/%s@%s" % (inst["repo"], inst["base"])},
            "repos": [name],
            "request": inst["request"],
            "hidden": [{"repo": name, "patch": inst["test_patch"]}],
            "checks": [{"repo": name, "run": ["sh", "-c", test_command(inst)]}],
            "timeout": t.get("timeout", "90m"),
        }
        vc = manifest.get("verification", {}).get(t["instance_id"])
        if vc:
            # Repository verification config committed into the base commit
            # (environment setup measured on the untouched base).
            with open(os.path.join(os.path.dirname(os.path.abspath(a.manifest)), vc)) as f:
                spec["setup"] = [{"repo": name, "file": ".boundedcode/verification.yaml", "old": "", "new": f.read()}]
        if t.get("interrupt_after"):
            spec["interrupt_after"] = t["interrupt_after"]
        with open(os.path.join(a.out, "tasks", t["instance_id"] + ".yaml"), "w", encoding="utf-8") as f:
            json.dump(spec, f, indent=1, ensure_ascii=False)  # JSON is valid YAML (raw UTF-8: YAML rejects surrogate escapes)
        with open(os.path.join(a.out, "gold", t["instance_id"] + ".patch"), "w") as f:
            f.write(inst["gold"])
        print(t["instance_id"], inst["repo"], inst["base"][:12], "|", spec["checks"][0]["run"][2])
        if a.prepare:
            prepare(inst["repo"], inst["base"], manifest["dependencies"].get(inst["repo"], []))


if __name__ == "__main__":
    sys.exit(main())
