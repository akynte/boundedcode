#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Writes the held-out agent-level locate tasks (bcode bench direct).

From the held-out split of tasks.json (prepare.py), per language, the first
instances by sha256(seed + id) whose fix changes one or two existing code
files: the issue text becomes a read-only question ("which files would need
to change?") and the answer must name one of those files. The rule was
fixed before any agent run. Issue texts come from SWE-bench Multilingual
and SWE-bench Lite (MIT).

usage: python3 agent_tasks.py [--tasks ~/.cache/bc-retrieval/tasks.json] [--out DIR]
"""
import argparse
import hashlib
import json
import os
import re

HERE = os.path.dirname(os.path.abspath(__file__))
PER_LANGUAGE = {"go": 2, "python": 2, "javascript": 1, "typescript": 1, "rust": 2, "java": 2, "php": 2, "ruby": 2, "c": 2}
CODE = re.compile(r"\.(go|py|js|jsx|mjs|cjs|ts|tsx|rs|java|kt|php|rb|c|h|cc|cpp|hpp)$")
QUESTION = ("\n\n---\nInvestigate the code of this repository and answer: which file or files would need to change "
            "to fix the issue above? Give their paths relative to the repository root. Do not change any files.")


def gold(patch):
    files = []
    for block in re.split(r"(?m)^(?=diff --git )", patch):
        m = re.match(r"diff --git a/(\S+) b/(\S+)", block)
        if not m or "\nnew file mode" in block or not CODE.search(m.group(1)):
            continue
        files.append(m.group(1))
    return files


def yaml_str(s):
    return json.dumps(s)  # a JSON string is a valid YAML scalar


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks", default=os.path.expanduser("~/.cache/bc-retrieval/tasks.json"))
    ap.add_argument("--out", default=os.path.join(HERE, "agent-tasks"))
    a = ap.parse_args()
    m = json.load(open(os.path.join(HERE, "manifest.json")))
    tasks = [t for t in json.load(open(a.tasks)) if t["split"] == "heldout"]
    tasks.sort(key=lambda t: hashlib.sha256((m["seed"] + "agent:" + t["id"]).encode()).hexdigest())
    os.makedirs(a.out, exist_ok=True)
    taken = {}
    for t in tasks:
        lang = t["language"]
        if taken.get(lang, 0) >= PER_LANGUAGE.get(lang, 0):
            continue
        g = gold(t["patch"])
        if not 1 <= len(g) <= 2:
            continue
        taken[lang] = taken.get(lang, 0) + 1
        repo = t["repo"].split("/")[1]
        expect = "(?:" + "|".join(re.escape(f) for f in g) + ")"
        with open(os.path.join(a.out, "locate-" + t["id"] + ".yaml"), "w") as f:
            f.write("# Held-out locate task (benchmarks/retrieval/agent_tasks.py) from %s.\n" % t["id"])
            f.write("# The fix changed: %s.\n" % ", ".join(g))
            f.write("id: locate-%s\ncategory: locate-heldout\nlanguage: %s\nread_only: true\n" % (t["id"], lang))
            f.write("source: %s\n" % yaml_str("swe-bench " + t["id"]))
            f.write("sources:\n  %s: git:https://github.com/%s@%s\n" % (repo, t["repo"], t["base"]))
            f.write("repos: [%s]\n" % repo)
            f.write("request: %s\n" % yaml_str(t["request"].replace("\r\n", "\n") + QUESTION))
            f.write("expect:\n  - %s\n" % yaml_str(expect))
            f.write("timeout: 15m\n")
    print(taken, sum(taken.values()))


if __name__ == "__main__":
    main()
