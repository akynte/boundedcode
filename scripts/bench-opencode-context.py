#!/usr/bin/env python3
"""Measure a real OpenCode -> BoundedCode -> local model session.

The driver is OpenCode. SQLite is read only and used only to inspect OpenCode's
own recorded usage/compaction events after the run. No model HTTP calls occur.
"""

import argparse
import json
import os
import sqlite3
import subprocess
import tempfile
import time
from pathlib import Path


def scalar(command):
    try:
        return subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        return None


def gpu_used():
    value = scalar(["nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits"])
    try:
        return int(value.splitlines()[0]) if value else None
    except ValueError:
        return None


def available_ram_mib():
    for line in Path("/proc/meminfo").read_text().splitlines():
        if line.startswith("MemAvailable:"):
            return int(line.split()[1]) // 1024
    return None


def inspect(db_path, session_id):
    db = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    try:
        row = db.execute("SELECT version FROM session_v2 WHERE id=?", (session_id,)).fetchone()
        if not row:
            raise RuntimeError(f"OpenCode session {session_id} is absent from {db_path}")
        assistants = []
        compactions = []
        for seq, kind, raw in db.execute(
            "SELECT seq,type,data FROM session_message WHERE session_id=? ORDER BY seq", (session_id,)
        ):
            item = json.loads(raw)
            if kind == "compaction":
                compactions.append({"seq": seq, "status": item.get("status"), "reason": item.get("reason")})
            if kind != "assistant" or not item.get("tokens"):
                continue
            tokens = item["tokens"]
            timing = item.get("time", {})
            total_input = tokens.get("input", 0) + tokens.get("cache", {}).get("read", 0)
            elapsed = (timing.get("streamed", 0) - timing.get("created", 0)) / 1000
            assistants.append({
                "seq": seq,
                "input_tokens": total_input,
                "output_tokens": tokens.get("output", 0),
                "wall_seconds": round(elapsed, 3),
                "end_to_end_output_tps": round(tokens.get("output", 0) / elapsed, 2) if elapsed > 0 else None,
                "finish": item.get("finish"),
            })
        return {"opencode_version": row[0], "assistant_steps": assistants, "compactions": compactions}
    finally:
        db.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prompt", help="coding prompt for a new OpenCode session")
    parser.add_argument("--session", help="inspect an existing OpenCode session instead of running one")
    parser.add_argument("--expected", help="text that must appear in OpenCode's answer")
    parser.add_argument("--min-compactions", type=int, default=0,
                        help="require at least this many completed OpenCode compactions")
    parser.add_argument("--reject-immediate-recompaction", action="store_true",
                        help="fail if two completed compactions occur without an assistant model step between them")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--opencode", default="opencode")
    args = parser.parse_args()
    if bool(args.prompt) == bool(args.session):
        parser.error("provide exactly one of --prompt or --session")
    db_path = Path(os.environ.get("XDG_DATA_HOME", str(Path.home() / ".local/share"))) / "opencode/opencode.db"
    session_id = args.session
    peak_gpu = None
    min_available_ram = None
    elapsed = None
    answer = ""
    timed_out = False
    if args.prompt:
        with tempfile.TemporaryFile(mode="w+t") as output, tempfile.TemporaryFile(mode="w+t") as errors:
            cmd = [args.opencode, "run", "--standalone", "--agent", "bc-editor", "--format", "json", args.prompt]
            start = time.monotonic()
            child = subprocess.Popen(cmd, stdout=output, stderr=errors)
            while child.poll() is None:
                peak_gpu = max(filter(lambda x: x is not None, [peak_gpu, gpu_used()]), default=None)
                ram = available_ram_mib()
                min_available_ram = min(filter(lambda x: x is not None, [min_available_ram, ram]), default=None)
                if time.monotonic() - start > args.timeout:
                    child.terminate()
                    try:
                        child.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        child.kill()
                        child.wait()
                    timed_out = True
                    break
                time.sleep(0.5)
            elapsed = round(time.monotonic() - start, 3)
            output.seek(0)
            for line in output:
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                session_id = event.get("sessionID", session_id)
                if event.get("type") == "text":
                    answer += event.get("part", {}).get("text", "")
            if child.returncode != 0 and not timed_out:
                errors.seek(0)
                raise RuntimeError(f"OpenCode exited {child.returncode}: {errors.read()[-1500:]}")
    if args.expected and args.expected not in answer:
        raise RuntimeError(f"expected text {args.expected!r} was absent from the OpenCode answer")
    report = inspect(db_path, session_id)
    completed = [event["seq"] for event in report["compactions"] if event["status"] == "completed"]
    if len(completed) < args.min_compactions:
        raise RuntimeError(f"expected at least {args.min_compactions} compactions; found {len(completed)}")
    if args.reject_immediate_recompaction:
        assistant_seqs = [step["seq"] for step in report["assistant_steps"]]
        for first, second in zip(completed, completed[1:]):
            if not any(first < seq < second for seq in assistant_seqs):
                raise RuntimeError(f"OpenCode compacted again without a model step: {first}, {second}")
    report.update({
        "session_id": session_id,
        "boundedcode_commit": scalar(["git", "rev-parse", "HEAD"]),
        "prism_commit": scalar(["git", "-C", os.environ.get("BC_PRISM_DIR", ""), "rev-parse", "HEAD"])
        if os.environ.get("BC_PRISM_DIR") else None,
        "elapsed_seconds": elapsed,
        "peak_gpu_used_mib": peak_gpu,
        "minimum_available_ram_mib": min_available_ram,
        "answer_matched": bool(args.expected) if args.prompt else None,
        "timed_out": timed_out if args.prompt else None,
    })
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
