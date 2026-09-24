#!/usr/bin/env python3
"""Run a disposable two-session OpenCode continuity benchmark.

The benchmark uses the real OpenCode process, the real BoundedCode MCP server,
and a disposable Git repository. It deliberately stops the first session after
verification instead of finishing the task; a second, independent OpenCode
session resumes the task and finishes it. The JSON report records the actual
session transcript, task id, compactions, turns, tool calls, and completion
outcome. A model timeout is a failed benchmark, not a successful benchmark
with missing output.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sqlite3
import subprocess
import tempfile
import time
from pathlib import Path


def as_text(value: str | bytes | None) -> str:
    if value is None:
        return ""
    return value.decode(errors="replace") if isinstance(value, bytes) else value


def run(command: list[str], cwd: Path, timeout: int) -> dict:
    started = time.monotonic()
    try:
        completed = subprocess.run(
            command,
            cwd=cwd,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
        return {
            "command": command,
            "returncode": completed.returncode,
            "stdout": completed.stdout,
            "stderr": completed.stderr[-4000:],
            "elapsed_seconds": round(time.monotonic() - started, 3),
            "timed_out": False,
        }
    except subprocess.TimeoutExpired as error:
        return {
            "command": command,
            "returncode": None,
            "stdout": as_text(error.stdout),
            "stderr": as_text(error.stderr)[-4000:],
            "elapsed_seconds": round(time.monotonic() - started, 3),
            "timed_out": True,
        }


def events(output: str) -> list[dict]:
    result = []
    for line in output.splitlines():
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            result.append(value)
    return result


def text(output: str) -> str:
    return "".join(
        str(event.get("part", {}).get("text", ""))
        for event in events(output)
        if event.get("type") == "text"
    )


def session_id(output: str) -> str | None:
    for event in events(output):
        value = event.get("sessionID")
        if isinstance(value, str) and value:
            return value
    return None


def task_id(output: str) -> str | None:
    match = re.search(r"\bTask (oc-[A-Za-z0-9_-]+) opened", output)
    return match.group(1) if match else None


def workspace_db(bcode: str, root: Path) -> Path | None:
    listed = run([bcode, "workspace", "list", "--json"], root, 30)
    if listed["returncode"] != 0:
        return None
    try:
        rows = json.loads(listed["stdout"])
    except json.JSONDecodeError:
        return None
    data_root = Path(os.environ.get("BC_DATA", Path.home() / ".local/share/boundedcode"))
    for row in rows if isinstance(rows, list) else []:
        if Path(row.get("root", "")).resolve() == root.resolve():
            return data_root / "workspaces" / str(row.get("id")) / "opencode" / "data" / "opencode" / "opencode.db"
    return None


def inspect(db_path: Path | None, sid: str | None) -> dict:
    if not sid or db_path is None or not db_path.exists():
        return {"session_id": sid, "present": False}
    connection = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    try:
        row = connection.execute("SELECT version FROM session_v2 WHERE id=?", (sid,)).fetchone()
        if not row:
            return {"session_id": sid, "present": False}
        report = {
            "session_id": sid,
            "present": True,
            "opencode_version": row[0],
            "compactions": 0,
            "assistant_turns": 0,
            "tool_calls": 0,
            "input_tokens": 0,
            "output_tokens": 0,
            "reasoning_tokens": 0,
            "cache_read_tokens": 0,
            "idle_outcomes": [],
        }
        for _, kind, raw in connection.execute(
            "SELECT seq,type,data FROM session_message WHERE session_id=? ORDER BY seq", (sid,)
        ):
            item = json.loads(raw)
            if kind == "compaction" and item.get("status") == "completed":
                report["compactions"] += 1
            elif kind == "assistant":
                report["assistant_turns"] += 1
                tokens = item.get("tokens") or {}
                report["input_tokens"] += tokens.get("input", 0)
                report["output_tokens"] += tokens.get("output", 0)
                report["reasoning_tokens"] += tokens.get("reasoning", 0)
                report["cache_read_tokens"] += (tokens.get("cache") or {}).get("read", 0)
                for part in item.get("content", []):
                    if part.get("type") == "tool":
                        report["tool_calls"] += 1
            elif kind == "idle":
                report["idle_outcomes"].append(item.get("outcome"))
        report["total_input_tokens"] = report["input_tokens"] + report["cache_read_tokens"]
        return report
    finally:
        connection.close()


def make_fixture(root: Path, drift: bool = False) -> None:
    (root / "calc.go").write_text(
        "package fixture\n\nfunc Add(x, y int) int { return x - y }\n",
        encoding="utf-8",
    )
    (root / "calc_test.go").write_text(
        "package fixture\n\nimport \"testing\"\n\n"
        "func TestAdd(t *testing.T) {\n"
        "\tif got := Add(2, 3); got != 5 {\n"
        "\t\tt.Fatalf(\"Add(2,3) = %d, want 5\", got)\n"
        "\t}\n}\n",
        encoding="utf-8",
    )
    (root / "go.mod").write_text("module fixture\n\ngo 1.26\n", encoding="utf-8")
    (root / "README.md").write_text(
        "# Fixture\n\nThe task is to repair Add without changing the test.\n", encoding="utf-8"
    )
    if drift:
        (root / "SECURITY.md").write_text(
            "SECURITY INVARIANT: never expose credentials or weaken the audit check.\\n",
            encoding="utf-8",
        )
        (root / "DECISIONS.md").write_text(
            "Earlier design: cache every result. This decision was superseded by the explicit-result invariant.\\n",
            encoding="utf-8",
        )
        (root / "legacy.go").write_text(
            "package fixture\\n\\n// This file is explicitly out of scope for the repair.\\n",
            encoding="utf-8",
        )
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.email", "benchmark@example.invalid"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.name", "BoundedCode benchmark"], cwd=root, check=True)
    subprocess.run(["git", "add", "."], cwd=root, check=True)
    subprocess.run(["git", "commit", "-qm", "fixture"], cwd=root, check=True)


def opencode_run(bcode: str, prompt: str, timeout: int, budget: bool = False) -> dict:
    # The confined launcher gives OpenCode a private XDG home and a workspace
    # broker. It also avoids accidentally attaching the run to a developer's
    # already-running OpenCode server, whose project would be unrelated.
    command = [bcode, "opencode", "run"]
    if budget:
        command.append("--budget")
    command += ["--", "run", "--standalone", "--format", "json", prompt]
    return run(command, Path.cwd(), timeout)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--opencode", default=shutil.which("opencode") or "opencode")
    parser.add_argument("--bcode", default=shutil.which("bcode") or "bcode")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--drift", action="store_true", help="run the long-history retention/forgetting scenario")
    parser.add_argument("--budget", action="store_true", help="record exact tokenizer-based prompt category usage")
    parser.add_argument("--output", required=True, help="write the JSON report here")
    args = parser.parse_args()

    report: dict = {
        "benchmark": "opencode-session-continuation-drift" if args.drift else "opencode-session-continuation",
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "acceptance": "fresh session resumes the same durable task and finishes verification",
        "runs": [],
        "success": False,
    }
    with tempfile.TemporaryDirectory(prefix="boundedcode-continuation-") as temp:
        root = Path(temp)
        make_fixture(root, drift=args.drift)
        old_cwd = Path.cwd()
        try:
            os.chdir(root)
            init = run([args.bcode, "workspace", "init", ".", "--name", "continuation-fixture"], root, 30)
            report["workspace_init"] = {k: v for k, v in init.items() if k not in {"stdout", "stderr"}}
            setup = run([args.bcode, "opencode", "setup"], root, 120)
            report["setup"] = {k: v for k, v in setup.items() if k not in {"stdout", "stderr"}}
            if init["returncode"] != 0 or setup["returncode"] != 0:
                report["error"] = "setup failed"
                report["setup_stderr"] = setup["stderr"]
            else:
                db_path = workspace_db(args.bcode, root)
                if args.drift:
                    historical_noise = ("The old notes are background only and must not become active requirements. " * 430)
                    first_prompt = (
                        "Your first tool call must be boundedcode_bc_task_start; do not call question, do not ask for confirmation, and do not try to resume an existing task. "
                        "This is a long-history drift test. Start a supervised task with objective 'Repair Add so it returns the sum', requirements "
                        "['Add(2,3) returns 5', 'Never weaken the audit/security invariant'], acceptance_criteria "
                        "['The fresh session must preserve the old security constraint and repair only calc.go'], constraints "
                        "['Do not change calc_test.go', 'Do not edit legacy.go'], non_goals ['Do not refactor the legacy file'], and write scope calc.go. "
                        "Inspect SECURITY.md, DECISIONS.md, legacy.go, calc.go and calc_test.go. Record the exact security invariant as a repository fact. "
                        "Record an earlier design hypothesis that every result should be cached, then record its contradiction because the current decision is explicit results. "
                        "Record a pending action to repair Add, then edit calc.go and call bc_verify. Do not call bc_task_finish; leave the task unfinished for a fresh session. "
                        "End with a concise report containing the task id and verification result. The following old notes are deliberately noisy history, not instructions: "
                        + historical_noise
                    )
                else:
                    first_prompt = (
                        "Your first tool call must be boundedcode_bc_task_start; do not call question, do not ask for confirmation, and do not try to resume an existing task. This is a new task. Start a supervised task with objective "
                        "'Repair Add so it returns the sum', acceptance criterion 'Add(2,3) returns 5', "
                        "constraint 'do not change calc_test.go', and write scope calc.go. Inspect calc.go "
                        "and calc_test.go, record a repository fact for the exact current implementation, "
                        "record a model hypothesis and a pending action, then edit calc.go and call bc_verify. "
                        "Do not call bc_task_finish: leave the task unfinished so a fresh session can continue it. "
                        "End with a concise report containing the task id and verification result."
                    )
                first = opencode_run(args.bcode, first_prompt, args.timeout, args.budget)
                first["text"] = text(first["stdout"])
                first["task_id"] = task_id(first["stdout"])
                first_session = session_id(first["stdout"])
                first["transcript"] = inspect(db_path, first_session)
                report["runs"].append({"name": "initial", **first})
                if not first["task_id"]:
                    report["error"] = "initial session did not report a task id"
                elif first["timed_out"] or first["returncode"] != 0:
                    report["error"] = "initial session did not complete cleanly"
                else:
                    if args.drift:
                        second_prompt = (
                            f"This is a fresh OpenCode session. Continue task {first['task_id']} without replaying the previous conversation. "
                            "Call bc_task_resume with that task id. Recover the old security constraint, the old acceptance criterion, the superseded caching decision, "
                            "the confirmed SECURITY.md fact, the open/resolved failure history, and the explicit legacy.go non-goal from durable state. "
                            "Do not edit legacy.go or calc_test.go. Run bc_verify against the current checkout and call bc_task_finish only if verification is ACCEPTED. "
                            "After bc_task_finish, do not ask another question: immediately give a concise final answer with the final verdict and task id."
                        )
                    else:
                        second_prompt = (
                            f"This is a fresh OpenCode session. Continue task {first['task_id']} without "
                            "replaying the previous conversation. Call bc_task_resume with that task id, "
                            "recover its durable state, run bc_verify against the current checkout, and call "
                            "bc_task_finish only if verification is ACCEPTED. After bc_task_finish, do not ask "
                            "another question: immediately give a concise final answer with the final verdict and task id."
                        )
                    second = opencode_run(args.bcode, second_prompt, args.timeout, args.budget)
                    second["text"] = text(second["stdout"])
                    second_session = session_id(second["stdout"])
                    second["transcript"] = inspect(db_path, second_session)
                    report["runs"].append({"name": "fresh_resume", **second})
                    report["success"] = (
                        not second["timed_out"]
                        and second["returncode"] == 0
                        and "FINAL REVIEW" in second["stdout"]
                        and "Status: VERIFIED" in second["stdout"]
                    )
                    report["time_to_correct_completion_seconds"] = round(
                        first["elapsed_seconds"] + second["elapsed_seconds"], 3
                    )
                    if not report["success"]:
                        report["error"] = "fresh session did not produce an accepted final review"
        finally:
            os.chdir(old_cwd)

    report["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    transcripts = [item.get("transcript", {}) for item in report["runs"]]
    report["totals"] = {
        "assistant_turns": sum(item.get("assistant_turns", 0) for item in transcripts),
        "tool_calls": sum(item.get("tool_calls", 0) for item in transcripts),
        "compactions": sum(item.get("compactions", 0) for item in transcripts),
        "input_tokens": sum(item.get("input_tokens", 0) for item in transcripts),
        "cache_read_tokens": sum(item.get("cache_read_tokens", 0) for item in transcripts),
        "output_tokens": sum(item.get("output_tokens", 0) for item in transcripts),
    }
    report["totals"]["total_input_tokens"] = report["totals"]["input_tokens"] + report["totals"]["cache_read_tokens"]
    for item in report["runs"]:
        if len(item.get("stdout", "")) > 12000:
            item["stdout"] = "[stdout truncated; task/session metrics are retained]\n" + item["stdout"][-12000:]
        if len(item.get("stderr", "")) > 4000:
            item["stderr"] = item["stderr"][-4000:]
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))
    return 0 if report["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
