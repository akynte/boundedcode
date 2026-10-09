#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Environment-only verification configs (deviations.md D1).

For each frozen task: BoundedCode's own preset for the base (dumped by
`basecheck -preset` into env-configs/preset/), minus the checks measured to
fail or time out on the untouched base in the offline sandbox. Where the
runner can exclude single tests, only those tests are excluded. Every
exclusion below cites the base measurement that justifies it; none was
chosen from an agent run or a reference patch.

The result is written to env-configs/<id>.yaml and committed into the
task's base as .boundedcode/verification.yaml (the earlier validations'
ENVIRONMENT_ONLY set-up), so both systems see the same repository.

usage: uv run --with pyyaml python env_configs.py PRESETS.yaml
"""
import json
import os
import sys

import yaml

HERE = os.path.dirname(os.path.abspath(__file__))

FLUENTD_TESTOPTS = " ".join([
    "--ignore-name=/not.writable/", "--ignore-name=/not.redable/", "--ignore-name=/should.be.writable/",
    "--ignore-name=/can.scrub.characters/", "--ignore-name=/ignore_network_errors_at_startup/", "--ignore-name=/this.is.a/",
    "--ignore-testcase=/HttpHelperTest/", "--ignore-testcase=/ObjectSpaceInputTest/",
    "--ignore-name=/test_EACCES_error_after_setup_watcher/", "--ignore-testcase=/TestFluentPluginGenerator/",
])

# id -> (why, edit(stages) -> stages)
EDITS = {
    "gin-gonic__gin-3820": (
        "go-test: TestRunTLS and TestPusher fail on the base: they read testdata/certificate/{cert,key}.pem, which BoundedCode's secret masking hides in the sandbox (TestPusher's failure shows once TestRunTLS, which panics, is skipped).",
        lambda st: sub_run(st, "go-test", ["go", "test", "-count=1", "{packages}"], ["go", "test", "-count=1", "-skip", "^(TestRunTLS|TestPusher)$", "{packages}"])),
    "gin-gonic__gin-4003": (
        "go-test: TestRunTLS, TestPusher and TestRunQUIC fail on the base (masked certificate files, as for gin-3820; each failure shows once the previous one, which panics, is skipped).",
        lambda st: sub_run(st, "go-test", ["go", "test", "-count=1", "{packages}"], ["go", "test", "-count=1", "-skip", "^(TestRunTLS|TestPusher|TestRunQUIC)$", "{packages}"])),
    "briannesbitt__carbon-3103": (
        "php-test: CarbonPeriod CreateTest::testStartAndEndFallback (expects 2024 dates; the clock reads 2026) and TestingAidsTest::testCreateFromPartialFormat (2 errors) fail on the base; excluded with a PHPUnit filter.",
        lambda st: sub_cmd(st, "php-test", 'php vendor/bin/phpunit "$@"',
                           "php vendor/bin/phpunit --filter '/^(?!.*(testStartAndEndFallback|testCreateFromPartialFormat)).*$/' \"$@\"")),
    "phpoffice__phpspreadsheet-3463": (
        "php-test: the full PHPUnit suite (19,726 tests) times out after the preset's 20 minutes on the base; the stage is removed, so no test stage remains.",
        lambda st: [s for s in st if s["name"] != "php-test"]),
    "fluent__fluentd-3616": (
        "ruby-test: rake test fails on the base and is not stable offline: four base runs gave 14 failures and 3 errors; then, with those excluded, 2 failures; "
        "0 (cut short by the plugin generator test ending the process); and, with that excluded too, 56 failures and 962 errors in later test files. "
        "Excluding single tests is not viable, so the stage is removed and no test stage remains.",
        lambda st: [s for s in st if s["name"] != "ruby-test"]),
    "immutable-js__immutable-js-2006": (
        "npm-test: the project's test script runs format, lint, type-check, build and unit-test; type-check:ts (dtslint downloads TypeScript versions) and build:stats (bundlephobia.com) "
        "need the network and fail offline. The stage runs, in the script's order, the offline parts that pass on the base: the build without build:stats (the jest tests import the built dist/), "
        "type-check:flow and unit-test (jest). npm-build: the same build without build:stats.",
        lambda st: replace_stage(replace_stage(st, "npm-test",
            "[ -d node_modules ] || { echo \"node_modules missing\"; exit 1; }; npm run --silent build:clean && npm run --silent build:dist && npm run --silent build:esm && npm run --silent build:copy && npm run --silent build:prepare && npm run --silent type-check:flow && npm run --silent unit-test"),
            "npm-build", "[ -d node_modules ] || { echo \"node_modules missing\"; exit 1; }; npm run --silent build:clean && npm run --silent build:dist && npm run --silent build:esm && npm run --silent build:copy && npm run --silent build:prepare")),
    "sharkdp__bat-2393": (
        "cargo-test: the integration test no_args_doesnt_break aborts its test binary on the base under Rust 1.99 (\"IO Safety violation: owned file descriptor already closed\"); skipped by exact name.",
        lambda st: sub_cmd(st, "cargo-test", "cargo test --workspace --no-fail-fast", "cargo test --workspace --no-fail-fast -- --skip no_args_doesnt_break --exact")),
    "tokio-rs__axum-691": (
        "cargo-test: axum-debug's ui (trybuild) test fails on the base under Rust 1.99 (compiler diagnostics differ from its snapshots); skipped by exact name.",
        lambda st: sub_cmd(st, "cargo-test", "cargo test --workspace --no-fail-fast", "cargo test --workspace --no-fail-fast -- --skip ui --exact")),
}


def stage(st, name):
    s = [x for x in st if x["name"] == name]
    assert len(s) == 1, name
    return s[0]


def sub_run(st, name, old, new):
    s = stage(st, name)
    assert s["run"] == old, (name, s["run"])
    s["run"] = new
    return st


def sub_cmd(st, name, old, new):
    s = stage(st, name)
    assert s["run"][:2] == ["sh", "-c"] and s["run"][2].count(old) == 1, (name, old)
    s["run"][2] = s["run"][2].replace(old, new)
    return st


def replace_stage(st, name, cmd):
    stage(st, name)["run"] = ["sh", "-c", cmd]
    return st


def main():
    docs = open(sys.argv[1]).read().split("\n---\n")
    out = os.path.join(HERE, "env-configs")
    os.makedirs(os.path.join(out, "preset"), exist_ok=True)
    for d in docs:
        d = d.strip()
        if not d:
            continue
        tid = d.splitlines()[0].lstrip("# ").strip()
        body = "\n".join(d.splitlines()[1:]) + "\n"
        open(os.path.join(out, "preset", tid + ".yaml"), "w").write(body)
        cfg = yaml.safe_load(body)
        why, edit = EDITS[tid]
        stages = edit(cfg["stages"])
        for s in stages:  # keep only set keys, as a repository would write it
            for k in [k for k, v in s.items() if v in ("", None, False, [], "0s")]:
                del s[k]
        header = ("# Environment-only verification config for this evaluation task (deviations.md D1),\n"
                  "# committed into the task's base. BoundedCode's preset for this repository\n"
                  "# (env-configs/preset/), minus what fails or times out on the untouched base in\n"
                  "# the offline sandbox (measured before any frozen run, 2026-10-09):\n")
        header += "".join("#   " + line + "\n" for line in wrap(why))
        text = header + yaml.safe_dump({"version": 1, "stages": stages}, sort_keys=False, width=1000)
        open(os.path.join(out, tid + ".yaml"), "w").write(text)
        print("wrote", tid)


def wrap(s, n=86):
    words, line, out = s.split(), "", []
    for w in words:
        if len(line) + len(w) + 1 > n:
            out.append(line)
            line = w
        else:
            line = (line + " " + w).strip()
    return out + [line]


if __name__ == "__main__":
    main()
