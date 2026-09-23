#!/usr/bin/env python3
"""Materialize sanitized agent workspaces for the 4 selected SWE-Bench Pro
Verified tasks — design instructions §3, §4, §14, §15.

For each task:
  1. start a disposable container from the exact image preflight already
     resolved and pulled;
  2. record every future commit SHA reachable inside it (the leakage this
     whole exercise defends against — capturing these first is what lets
     verify_sanitized prove they are gone afterward, rather than merely
     asserting "no branches" and hoping);
  3. `docker cp` the repository root out to a staging directory under the
     guarded run root;
  4. sanitize it TWICE, independently, into two separate directories named
     "control" and "jev-assisted" — never one arm reusing or being copied
     from the other, so an accidental cross-arm dependency cannot exist by
     construction (design instruction §14);
  5. verify both, and verify their fingerprints are identical (§15);
  6. tear down the container and the staging copy through the guarded
     cleanup path.

Run from the repository root:
    python3 evals/evidence-v1/tooling/materialize_task.py
"""
import json
import subprocess
from pathlib import Path

import safe_cleanup
import sanitize_workspace as sw

REPO_ROOT = safe_cleanup.REPO_ROOT

# Repository root inside every SWE-Bench Pro Verified container, confirmed
# during preflight (docker run ... pwd) for all 4 selected images.
IN_CONTAINER_REPO = "/app"

SWEBENCH_PRO_IMAGES = {
    "instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-v1151a6325649aaf997cd541ebe533b53fddf1b07":
        "jefzda/sweap-images:future-architect.vuls-future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-v1151a6325649aaf997cd541ebe533b53fddf1b07",
    "instance_element-hq__element-web-ca58617cee8aa91c93553449bfdf9b3465a5119b-vnan":
        "jefzda/sweap-images:element-hq.element-element-hq__element-web-ca58617cee8aa91c93553449bfdf9b3465a5119b",
    "instance_internetarchive__openlibrary-1894cb48d6e7fb498295a5d3ed0596f6f603b784-v0f5aece3601a5b4419f7ccec1dbda2071be28ee4":
        "jefzda/sweap-images:internetarchive.openlibrary-internetarchive__openlibrary-1894cb48d6e7fb498295a5d3ed0596f6f603b784-v0f5aece3601a5b4419f7ccec1dbda",
    "instance_tutao__tutanota-b4934a0f3c34d9d7649e944b183137e8fad3e859-vbc0d9ba8f0071fbe982809910959a6ff8884dbbf":
        "jefzda/sweap-images:tutao.tutanota-tutao__tutanota-b4934a0f3c34d9d7649e944b183137e8fad3e859-vbc0d9ba8f0071fbe982809910959a6ff8884dbbf",
}


def sh(*args, timeout=600, check=True):
    return subprocess.run(args, capture_output=True, text=True, timeout=timeout, check=check)


def known_future_shas_in_container(image: str, limit: int = 25) -> list[str]:
    """Every commit `git log --all` can see inside the *original* image,
    minus HEAD itself — the exact leakage surface verify_sanitized proves
    is gone from the sanitized workspace. Capped at `limit` because a
    history-rich repository (vuls: 1709 commits reachable) does not need
    every single one probed to prove the point; the cap is applied to the
    *oldest* future commits absent, keeping the ones nearest HEAD, which
    are the ones most likely to include the gold patch itself.
    """
    # The image's own ENTRYPOINT is ["/bin/bash"] with no CMD; passing
    # "sleep 600" as the run command would be interpreted as a script
    # *argument to bash*, not "run sleep 600" — confirmed during preflight
    # (the same reason those checks used --entrypoint /bin/bash -c "..."
    # rather than a bare command). Override it explicitly here too.
    cid = start_container(image)
    try:
        head = sh("docker", "exec", cid, "git", "-C", IN_CONTAINER_REPO, "rev-parse", "HEAD").stdout.strip()
        all_shas = sh("docker", "exec", cid, "git", "-C", IN_CONTAINER_REPO,
                       "log", "--all", "--format=%H").stdout.split()
        future = [s for s in all_shas if s != head]
        return future[:limit]
    finally:
        sh("docker", "stop", "-t", "2", cid, check=False)


def start_container(image: str) -> str:
    return sh("docker", "run", "-d", "--rm", "--entrypoint", "/bin/bash", image,
              "-c", "sleep 600").stdout.strip()


def materialize(task_id: str, image: str) -> dict:
    result = {"task_id": task_id, "image": image}

    future_shas = known_future_shas_in_container(image)
    result["original_reachable_commits"] = len(future_shas) + 1
    result["future_shas_captured"] = len(future_shas)
    result["future_shas_captured_list"] = future_shas

    staging = safe_cleanup.task_workspace(task_id, "staging")
    # task_workspace already created the directory; docker cp refuses to
    # copy into an existing empty directory target the way we want here
    # (it copies IN_CONTAINER_REPO as a new child), so remove it first and
    # let docker cp create it fresh as the repo root itself.
    safe_cleanup.safe_remove_all(staging, safe_cleanup.RUN_ROOT)

    cid = start_container(image)
    try:
        sh("docker", "cp", f"{cid}:{IN_CONTAINER_REPO}", str(staging))
    finally:
        sh("docker", "stop", "-t", "2", cid, check=False)

    control_dir = safe_cleanup.RUN_ROOT / "agent-workspaces" / task_id / "control"
    treatment_dir = safe_cleanup.RUN_ROOT / "agent-workspaces" / task_id / "jev-assisted"
    for d in (control_dir, treatment_dir):
        if d.exists():
            safe_cleanup.safe_remove_all(d, safe_cleanup.RUN_ROOT)

    sw.sanitize_git_repo(staging, control_dir)
    sw.sanitize_git_repo(staging, treatment_dir)

    control_probe = sw.verify_sanitized(control_dir, known_future_shas=future_shas)
    treatment_probe = sw.verify_sanitized(treatment_dir, known_future_shas=future_shas)

    fp_control = sw.fingerprint_tree(control_dir)
    fp_treatment = sw.fingerprint_tree(treatment_dir)

    safe_cleanup.safe_remove_all(staging, safe_cleanup.RUN_ROOT)

    result.update({
        "control": {
            "reachable_commits": control_probe.reachable_commits,
            "remotes": len(control_probe.remotes),
            "tags": len(control_probe.tags),
            "reflog_leakage": control_probe.reflog_entries,
            "future_sha_reachable": control_probe.future_sha_reachable,
            "unreachable_object_leakage": control_probe.unreachable_future_objects,
            "object_alternates": control_probe.has_object_alternates,
            "fingerprint": fp_control,
            "passed": control_probe.passed,
            "failure_reasons": control_probe.failure_reasons,
        },
        "jev_assisted": {
            "reachable_commits": treatment_probe.reachable_commits,
            "remotes": len(treatment_probe.remotes),
            "tags": len(treatment_probe.tags),
            "reflog_leakage": treatment_probe.reflog_entries,
            "future_sha_reachable": treatment_probe.future_sha_reachable,
            "unreachable_object_leakage": treatment_probe.unreachable_future_objects,
            "object_alternates": treatment_probe.has_object_alternates,
            "fingerprint": fp_treatment,
            "passed": treatment_probe.passed,
            "failure_reasons": treatment_probe.failure_reasons,
        },
        "fingerprints_match": fp_control == fp_treatment,
        "passed": control_probe.passed and treatment_probe.passed and fp_control == fp_treatment,
    })
    return result


SANITIZE_IN_CONTAINER_SCRIPT = f"""
cd {IN_CONTAINER_REPO} &&
rm -rf .git &&
find . -name .git -exec rm -rf {{}} + 2>/dev/null;
git init --quiet &&
git config core.hooksPath /dev/null &&
git config user.name '{sw.GIT_AUTHOR_NAME}' &&
git config user.email {sw.GIT_AUTHOR_EMAIL} &&
git add -A &&
GIT_AUTHOR_DATE='{sw.GIT_COMMIT_EPOCH}' GIT_COMMITTER_DATE='{sw.GIT_COMMIT_EPOCH}' \
  git commit --quiet --no-verify -m '{sw.GIT_COMMIT_MESSAGE}'
"""

# design instruction §5: preserving dependencies without preserving
# history. materialize() above proves the sanitization algorithm correct by
# copying the repository out to the host (host-side git and Python make the
# probes in verify_sanitized easy to run and assert on); it does not carry
# with it anything installed outside the repository root inside the image
# (Go's toolchain at /usr/local/go, module caches under $GOPATH, and
# similar out-of-repo state for other languages).
#
# For a real live run, sanitize IN PLACE inside the running container
# instead — the same four git commands AgentCompass's own mitigation uses,
# executed via `docker exec`, so nothing installed in the image is ever
# copied out or lost. Verified directly for the future-architect/vuls task:
# `go build ./...` still ran successfully immediately after in-container
# sanitization, using the image's own already-installed Go 1.24.4 toolchain
# — see docs/evidence/evidence-v1.md's Anti-Leakage Isolation section.
def sanitize_in_container(cid: str) -> None:
    r = sh("docker", "exec", cid, "bash", "-c", SANITIZE_IN_CONTAINER_SCRIPT)
    if r.returncode != 0:
        raise RuntimeError(f"in-container sanitization failed: {r.stderr}")


def main():
    results = []
    for task_id, image in SWEBENCH_PRO_IMAGES.items():
        print(f"materializing {task_id} ...")
        results.append(materialize(task_id, image))
    out_path = REPO_ROOT / "evals" / "evidence-v1" / "sanitization-results.json"
    out_path.write_text(json.dumps(results, indent=2, sort_keys=True) + "\n")
    for r in results:
        print(f"  {r['task_id'][:50]:52s} passed={r['passed']} "
              f"fingerprints_match={r['fingerprints_match']} "
              f"future_shas_captured={r['future_shas_captured']}")
    print(f"\nwrote {out_path}")


if __name__ == "__main__":
    main()
