#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Gate replay (protocol.md section 5): what BoundedCode's verification gate
would have said about each baseline patch.

For every baseline result with a saved patch, a fresh BoundedCode task is
run on the same frozen task spec. Its "agent" is the scripted stand-in
(scripts/smoke/fake_model.py): in its first turn it applies the baseline's
patch, unchanged, and finishes. When BoundedCode asks for a test, it
declines (changes nothing). BoundedCode's real gate then decides: full
verification, then behavioural evidence. The harness re-runs the hidden
acceptance on the result. It must equal the baseline's own hidden result,
or the replay did not reproduce the patch.

usage: gate_replay.py [--eval DIR] [--binary PATH] [--only ID ...]
"""
import argparse
import base64
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
from run import env_for, scrub  # noqa: E402

CHUNK = 6000  # base64 characters per terminal command


def commands(repo, patch):
    data = open(patch, "rb").read()
    # The harness saved patches without their final newline (deviation D2);
    # git apply needs it.
    if not data.endswith(b"\n"):
        data += b"\n"
    b = base64.b64encode(data).decode()
    steps = ["rm -f /tmp/agent.patch.b64"]
    for i in range(0, len(b), CHUNK):
        steps.append("printf %%s '%s' >> /tmp/agent.patch.b64" % b[i:i + CHUNK])
    steps.append("cd %s && base64 -d /tmp/agent.patch.b64 > /tmp/agent.patch && git apply --whitespace=nowarn /tmp/agent.patch && git status --short" % repo)
    # Turn 1 applies the patch; turn 2 (BoundedCode's request for a test,
    # or a retry) changes nothing.
    return "\n+++\n".join(steps) + "\n---\ntrue\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval", default=os.path.expanduser("~/.cache/bc-comparative"))
    ap.add_argument("--binary", default=os.path.join(ROOT, "bin", "boundedcode"))
    ap.add_argument("--only", nargs="*")
    ap.add_argument("--runs", default="runs", help="results/ subdirectory with the baseline's runs")
    ap.add_argument("--outdir", default="replay", help="results/ subdirectory for the replays")
    a = ap.parse_args()
    frozen = json.load(open(os.path.join(HERE, "frozen-tasks.json")))
    out = os.path.join(HERE, "results", a.outdir)
    os.makedirs(out, exist_ok=True)
    env = env_for(a.eval)
    env["BOUNDEDCODE_HOME"] = os.path.join(a.eval, "home-replay")
    env["BOUNDEDCODE_OPENAI_COMPATIBLE_API_KEY"] = "replay-placeholder"  # seen only by the local stand-in
    cfg = os.path.join(env["BOUNDEDCODE_HOME"], "config")
    os.makedirs(cfg, exist_ok=True)
    # Two attempts suffice: apply the patch, then decline the request for a
    # test. More would only retry a fixed patch.
    conf = open(os.path.join(HERE, "config.yaml")).read().replace("    max_attempts: 6\n", "    max_attempts: 2\n")
    assert "max_attempts: 2" in conf
    open(os.path.join(cfg, "config.yaml"), "w").write(conf)
    for t in frozen["tasks"]:
        tid = t["id"]
        if a.only and tid not in a.only:
            continue
        res_path = os.path.join(HERE, "results", a.runs, "O-%s.json" % tid)
        stem = os.path.join(out, "R-%s" % tid)
        if not os.path.exists(res_path) or os.path.exists(stem + ".json"):
            continue
        res = json.load(open(res_path))["results"][0]
        patch = os.path.expanduser(res.get("agent_patch", ""))
        if not patch or not os.path.exists(patch) or os.path.getsize(patch) == 0:
            json.dump({"task": tid, "replayed": False, "why": "the baseline left no patch"}, open(stem + ".json", "w"), indent=1)
            continue
        repo = json.load(open(os.path.join(a.eval, "tasks", "frozen", tid + ".yaml")))["repos"][0]
        cmdfile = stem + ".commands"
        open(cmdfile, "w").write(commands(repo, patch))
        srv = subprocess.Popen([sys.executable, os.path.join(ROOT, "scripts", "smoke", "fake_model.py"), "--command-file", cmdfile,
                                "--log", stem + ".model.log"], stdout=subprocess.PIPE, text=True)
        port = srv.stdout.readline().strip()
        try:
            subprocess.run([a.binary, "provider", "use", "openai-compatible", "--base-url", "http://127.0.0.1:%s/v1" % port,
                            "--model", "replay", "--context-window", "131072"], env=env, check=True, capture_output=True)
            print(time.strftime("%H:%M:%S"), "replay", tid, flush=True)
            with open(stem + ".stderr.log", "w") as f:
                subprocess.run([a.binary, "bench", "tasks", "--tasks", os.path.join(a.eval, "tasks", "frozen"), "--only", tid,
                                "--out", stem + ".json", "--serena", "off"], env=env, stdout=f, stderr=subprocess.STDOUT, timeout=95 * 60)
        finally:
            srv.terminate()
            srv.wait()
            scrub(stem + ".json", stem + ".stderr.log")
        os.remove(cmdfile)  # it holds the patch again, already saved with the run


if __name__ == "__main__":
    main()
