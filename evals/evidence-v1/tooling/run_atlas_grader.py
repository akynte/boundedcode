#!/usr/bin/env python3
"""Runs one SWE Atlas grading pass through Harbor's own official
Environment API — never a re-implementation of it.

Called by internal/evidence's gradeSWEAtlas (Go) as a subprocess, with
PYTHONPATH already pointing at the entrypoint-patched harbor runtime
(EnsurePatchedHarborRuntime) so environment.start()'s docker-compose
build hits the same fix internal/evidence/harbor.go already proved.

Why a Python driver at all, rather than `harbor task start-env` (which Go
already shells out to elsewhere in this suite): start-env's own CLI command
always tears its environment down in a `finally: await environment.stop()`
block when the process exits (confirmed by reading harbor/cli/tasks.py),
regardless of --interactive/--non-interactive. There is no CLI flag to
leave it running for a second, separate process to exec into. Driving the
same public Environment.start/exec/upload_file/stop API directly, in one
process, for one grading pass, is the smallest correct way to keep a
container alive across "apply the agent's patch" and "run the official
verifier" — using Harbor's own methods, not a parallel implementation of
what they do.

Usage:
    run_atlas_grader.py <task_dir> <patch_file> <workdir> <verifier_script> <timeout_sec>

stdin: none. Reads EVAL_API_KEY / OPENAI_API_KEY from its own process
environment (set by the Go caller via AtlasEvaluatorEnv) and passes them
ONLY to the one verifier exec call — never to the earlier patch-apply
call, and never written to any file this script controls.

stdout: a single JSON object:
    {"ok": true/false, "return_code": int, "stdout": "...", "stderr": "...", "error": "..."}
"""
import asyncio
import json
import os
import sys
import tempfile
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 6:
        print(json.dumps({"ok": False, "error": "usage: run_atlas_grader.py <task_dir> <patch_file> "
                           "<workdir> <verifier_script> <timeout_sec>"}))
        return 2
    task_dir, patch_file, workdir, verifier_script, timeout_sec = sys.argv[1:]
    timeout_sec = int(timeout_sec)

    from harbor.models.environment_type import EnvironmentType
    from harbor.models.task.task import Task
    from harbor.models.trial.paths import EnvironmentPaths, TrialPaths
    from harbor.environments.factory import EnvironmentFactory

    async def run():
        task = Task(Path(task_dir))
        with tempfile.TemporaryDirectory() as temp_trial_dir:
            trial_paths = TrialPaths(trial_dir=Path(temp_trial_dir))
            trial_paths.mkdir()
            environment = EnvironmentFactory.create_environment(
                EnvironmentType.DOCKER,
                environment_dir=task.paths.environment_dir,
                environment_name=task.short_name,
                session_id=f"{task.short_name}__evidence-grader__env",
                trial_paths=trial_paths,
                task_env_config=task.config.environment,
            )
            return await run_in_environment(environment, task, patch_file, workdir, verifier_script, timeout_sec)

    async def run_in_environment(environment, task, patch_file, workdir, verifier_script, timeout_sec):
        try:
            await environment.start(force_build=True)

            # The verifier's own scripts (evaluate_tests.py and friends)
            # are not baked into the task image — Harbor's own convention
            # is to copy tests/ into the environment at /tests immediately
            # before the verifier runs (see harbor/models/trial/paths.py's
            # EnvironmentPaths docstring: "Copied over by the Verifier
            # after the agent runs"). This mirrors that exactly, never a
            # parallel convention.
            env_paths = EnvironmentPaths.for_os(task.config.environment.os)
            await environment.upload_dir(task.paths.tests_dir, str(env_paths.tests_dir))

            remote_patch = f"{workdir}/.evidence-agent-patch.diff"
            await environment.upload_file(patch_file, remote_patch)

            apply_result = await environment.exec(
                f"cd {workdir} && git apply {remote_patch}", timeout_sec=60)
            if apply_result.return_code != 0:
                return {"ok": False, "return_code": apply_result.return_code,
                        "stdout": apply_result.stdout, "stderr": apply_result.stderr,
                        "error": "patch did not apply inside the Atlas grading environment"}

            verifier_env = {}
            for name in ("EVAL_API_KEY", "OPENAI_API_KEY", "EVAL_BASE_URL", "EVAL_MODEL"):
                if os.environ.get(name):
                    verifier_env[name] = os.environ[name]

            result = await environment.exec(
                f"cd {env_paths.tests_dir} && python3 {os.path.basename(verifier_script)}",
                env=verifier_env, timeout_sec=timeout_sec)

            # The verifier's own exit code only reports whether the
            # SCRIPT ran without crashing — NOT the grading verdict.
            # evaluate_tests.py's own write_reward() writes the real
            # pass/fail signal to /logs/verifier/reward.txt (a bare
            # number), which is the official artifact this driver reads
            # back rather than scraping printed text or trusting exit 0.
            reward = None
            reward_result = await environment.exec(
                f"cat {env_paths.reward_text_path}", timeout_sec=10)
            if reward_result.return_code == 0 and reward_result.stdout is not None:
                try:
                    reward = float(reward_result.stdout.strip())
                except ValueError:
                    reward = None

            return {"ok": result.return_code == 0, "return_code": result.return_code,
                    "stdout": result.stdout, "stderr": result.stderr, "reward": reward}
        finally:
            await environment.stop(delete=True)

    try:
        outcome = asyncio.run(run())
    except Exception as e:  # noqa: BLE001 - reported as structured JSON, not a traceback
        print(json.dumps({"ok": False, "error": f"{type(e).__name__}: {e}"}))
        return 1

    print(json.dumps(outcome))
    return 0 if outcome.get("ok") else 1


if __name__ == "__main__":
    sys.exit(main())
