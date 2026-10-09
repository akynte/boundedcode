# Public-launch readiness audit

Date: 2026-10-09. Audited revision: `fa36de7` (main, = v0.1.0-alpha.4 plus
documentation commits). Scope: README, documentation, release artifacts, CI,
installers, tests, benchmark reports, verification and sandbox code.

Every finding below was checked against code or data in this repository, a
command run during the audit, or both. Findings that could only be reasoned
from code, without a reproduction, say so. Passing unit tests are not taken as
evidence of production readiness. The audit ran on the Linux reference machine.
It did not run a full task end to end (model plus sandbox), and it did not run
anything on macOS or Windows hardware.

## Summary

| Severity | Found | Fixed in code | Corrected in docs only | Open |
|---|---|---|---|---|
| Critical | 1 | 1 | – | – (the fix is not released) |
| High | 6 | 3 | 3 | 3 open risks, now disclosed |
| Medium | 7 | 1 | 5 | 5 |
| Low | 8 | 1 | 6 | 2 |

**Launch blocker:** the critical fix (C1) is on `main` only. Both installers
download the newest release binary, v0.1.0-alpha.4, which still has the
defect. Until a release with the fix ships, every installed copy can be
escaped from the sandbox as described in C1. Cutting that release, and
deciding whether to publish a security advisory, is the maintainer's call;
this audit did neither.

## Findings, by severity

Status values: **fixed** (code changed, regression test added), **docs**
(the claim was corrected and the behaviour is unchanged), **open** (still
present; disclosed where noted).

### Critical

**C1. Host code execution through a nested git repository in the worktree.
Fixed.**
- **The attack:** the agent controls its worktree, and the sandbox image
  ships git. Inside the worktree it creates `sub/.git` and sets
  `filter.evil.clean = <command>` in that repository's own config, plus
  `* filter=evil` in `sub/.gitattributes`.
- **What the host did:** the host-side checkpoint (`gitops.CommitAll`) ran
  `git add -A`, which recorded `sub` as a gitlink. A later `git status`,
  `git diff` or `git add`, with the nested repository dirty, then ran git
  inside `sub` with *its* config, so the filter command executed on the host.
- **Why the defences missed it:** the hardening flags (`core.hooksPath`,
  `core.fsmonitor`, ...) do not override a nested repository's filter
  config. `CheckTaskWorktree` inspected only the top-level pointer.
- **Reproduction:** with plain git 2.47.3 and the exact hardening flags, a
  canary file was created by `git status --porcelain` and by `git add -A`.
  The audit ran a controlled A/B against fresh repositories.
- **Fix:**
  - Host git now always runs with `diff.ignoreSubmodules=all`,
    `submodule.recurse=false` and `status.submoduleSummary=false`.
  - `CommitAll` refuses a worktree that holds a nested repository, whether
    it is a new directory or sits behind a gitlink the agent staged. This is
    needed because `git add -A` still recursed with the flags set (verified).
  - Both checks use git listings that do not enter the nested repository.
  - Files: `internal/gitops/git.go`.
- **Test:** `TestNestedRepositoryConfigNotExecuted` covers an untracked
  nested repository and an agent-staged gitlink. It fails on the original
  code and passes with the fix.

### High

**H1. Protected-path, secret-path and deny-path policies were bypassed by
quoted file names. Fixed.**
- `ChangedFiles` parsed `git diff --name-only` line by line. Git C-quotes
  names that contain non-ASCII characters, `"` or control characters, so
  `.github/workflows/cié.yml` reached `IsProtectedPath` as
  `"\".github/workflows/ci\\303\\251.yml\""` and passed the diff-scope gate.
- **Fix:** names are read with `-z`, and `core.quotePath=false` is set
  (`internal/gitops/git.go`). The same applies to the untracked-file
  listings used by `Diff` and `ChangedSymbols`.
- **Test:** `TestChangedFilesUnquoted` fails on the original code and passes
  now. A probe confirmed that the diff-scope stage now flags
  `.boundedcode/verification"x.yaml`.

**H2. Go behavioural evidence never checked that the test passes on the
change. Fixed for Go; open for other languages, now disclosed.**
- **The gap:** `BehaviourEvidence` compared only the base with the changed
  tests against the base without them. "Passes with the change" meant only
  that the full gate passed. A new test that fails on the base and calls
  `t.Skip` on the change therefore counted as evidence (probe:
  `Verified:true`).
- **Fix:** each attributed Go test is now run on the change, in the sandbox,
  with `-run '^(names)$' -v`. It counts only with a top-level `--- PASS` and
  no FAIL or SKIP (`internal/verify/delta.go`).
- **Tests:** two new cases in `TestBehaviourEvidenceGoCases` ("test skipped
  on the change", "test fails on the change too"). Both fail on the original
  code. All existing positive and negative cases still pass.
- **Cost:** one extra sandboxed `go test` run per verified Go evidence
  check. On Docker Desktop each container start took 20–40 s in earlier e2e
  runs.
- **Still open:** for non-Go stages, the change side is still the whole
  stage passing. The README now says so.

**H3. Masked secrets are readable through the git object store. Open,
disclosed (docs).**
- Each repository's git common dir is mounted read-only into the agent
  container (`internal/agent/openhands/runtime.go:103-107`). Masks cover
  worktree paths only (`internal/sandbox/sandbox.go:211`).
- As a result, a tracked `.env`, a key on another branch or in the stash, and
  `.git/config` (including `https://user:token@…` remote URLs) are readable
  with git inside the sandbox.
- This was confirmed from the code; it was not run in a container during the
  audit.
- The README "Secrets" row and SECURITY.md said these files were masked,
  without that qualification. Both are now corrected, and the sandbox
  document has a new residual risk 12.
- A fix needs a different git mount design (for example a filtered object
  store), so it is not made here.

**H4. Python and JavaScript tests of newly added API count as evidence.
Open, disclosed (docs).**
- The README said a test that "does not compile or load on the base" is not
  evidence. That holds for Go preset stages only.
- **Probes (unchanged code path):**
  - pytest/unittest: `calc.absval(-1)`, which raises `AttributeError` on the
    base, and a function-level `import calcnew` both gave `Verified:true`.
  - JavaScript: `lib.abs(-1)`, which raises `TypeError`, gave
    `Verified:true`.
- **Cause:** per-test attribution (`delta.go`, named-failure path) runs
  before the load-failure check, which applies only to whole-stage
  failures.
- The README now states the gap.

**H5. Stale and incorrect release statements in the README. Fixed (docs).**
- The README said "Release binaries for macOS and Windows ship from the next
  release on". v0.1.0-alpha.3 and alpha.4 already ship darwin and windows
  amd64/arm64 binaries (`gh release view`).
- It called two evidence-check changes "unreleased". Commit `263ae59` is in
  v0.1.0-alpha.3 and alpha.4 (`git tag --contains`).

**H6. Windows installer kills the user's PowerShell session on any error.
Fixed.**
- **The defect:** `Die` called `exit 1`. Under the documented
  `irm … | iex`, that exits the host shell, so the window closes before the
  error (for example "Git for Windows is required") can be read. The
  installer also left `$ErrorActionPreference = 'Stop'` and
  `Set-StrictMode -Version Latest` set in the user's session.
- **Reproduction:** PowerShell 7.4.6 on Linux, with
  `Get-Content -Raw install.ps1 | Invoke-Expression`, followed by a further
  statement. The session died, or later commands failed under the leaked
  strict mode.
- **Fix:**
  - The body runs in a script block, and `Die` throws.
  - The error is printed in red.
  - `exit 1` is used only when the script runs as a file (`-File`).
- **Verified with pwsh 7.4.6:**
  - The iex failure path keeps the session alive, with no leaked settings.
  - The `-File` failure path still exits 1.
  - The download and checksum path installs alpha.4.
  - The script parses without errors.
  - Not run on Windows itself.

### Medium

**M1. Refused frontier packets are visible to the networked Codex
container. Fixed.**
- Blocked packets (refused because they still contain host paths) were
  written into the task's `frontier/` directory. The Codex container
  bind-mounts that directory, and the container has network access
  (`internal/frontier/provider.go:189`).
- They now go to a sibling `frontier-blocked/` directory
  (`internal/orchestrator/escalation.go`). `TestBlockedEscalationIsRecorded`
  asserts the new location.
- The docs said that only "the packet and an empty workdir" are in the
  container. They are corrected: the container also sees earlier
  sent packets and answers, and all of `~/.codex`, mounted read-write.

**M2. Flaky tests can become evidence. Open, disclosed.**
- Each evidence run happens once. In a probe, a pre-existing Go test that
  fails only on its first run, together with a change that touches only
  `testdata/`, gave `Verified:true`. The JavaScript equivalent did too.
- Also, for non-Go stages with named failures, none of them attributed and a
  passing control, the code still falls through to whole-stage evidence
  (`delta.go`, `if found || c.Status != "pass"`).

**M3. Custom Go stages lose the "does not compile on base" guard. Open,
disclosed.**
- A stage counts as Go only when `requires[0]` is `go.mod`. A
  user-configured `go test` stage without it is compared per stage. In a
  probe, a test of a new function plus any touched test-data file gave
  `Verified:true`.

**M4. Window between the worktree check and host git. Open, disclosed;
reasoned from code, not reproduced.**
- The agent container keeps running during the checkpoint
  (`CheckTaskWorktree`, then `CommitAll`).
- A background process could rewrite the agent-writable admin dir's
  `commondir` in between. This reopens the redirect that control 8 closes.
- Sandbox doc, residual risk 13.

**M5. Undisclosed methodology factors in the held-out validation. Docs.**
- All 6 held-out tasks are from the same 6 repositories as the development
  corpus (gin, caddy, go-zero, axios, vue, prometheus).
- The derivability screen removes exactly the failure classes seen in
  development.
- The candidate list and screening first appear in git (`5b3c54a`) 14 s
  after the frozen product commit (`4b31742`), so pre-registration cannot be
  shown from history.
- These are public issues that may be in the model's training data.
- 5/6 has an exact 95% interval of about 36–99.6%.
- All of this is now in README Known limitation 1. No results were changed.

**M6. macOS and Windows unit tests fail in CI. Open, already disclosed;
figures refreshed.**
- In CI run 37834095378 at `fa36de7`, 5 of 32 packages fail on macOS and 14
  of 32 on Windows. The job is `continue-on-error`.
- In the sample examined, most failures are test assumptions:
  - `/var` versus `/private/var` symlinks;
  - no Docker daemon on the runners;
  - expected `/` separators on Windows;
  - Serena's `venv/bin` path; Serena is documented as unsupported on
    Windows.
- Some may still be product defects that a real user would hit; the failures
  are not fully triaged:
  - `TestSanitizeSymlinkedLocation`: a host path under a symlinked directory
    is not rewritten in frontier packets on macOS or Windows;
  - Windows `TestFileFallback` and `TestSaveRoundTrip`: these check Unix
    file modes; the code applies Windows ACLs instead (not verified on
    Windows).

**M7. Secret-name matching gaps. Open, disclosed.**
- Matching is case-sensitive: `.ENV` and `ID_RSA` are neither masked nor
  denied (`internal/policy/policy.go`).
- `vendor/`, `node_modules/` and `.venv/` are not searched for secrets to
  mask.
- Sandbox doc, residual risk 15.

### Low

- **L1. The frontier wording was ambiguous. Docs.** "4 frontier calls were
  sent and no task was accepted": one first-validation task was accepted
  (axios-6539), without a frontier call. The text now reads "none of the
  tasks that made them was accepted" (3 places).
- **L2. Wrong memory unit. Docs.** The README said the model server peaked
  at 29.3 GiB. The raw logs give 29,310 MiB, which is 28.6 GiB. Minimum
  available memory was 33,128 MiB, which is 32.4 GiB, not the 33.1 GiB the
  report states. The historical report
  (`docs/benchmarks/second-independent-validation-2026-10.md:259,262`) was
  left unchanged as instructed; it needs an erratum line if the maintainer
  agrees.
- **L3. Docker 29 error not recognised. Fixed.** The container-engine check
  showed "(Client: Docker Engine - Community)" as the reason when the daemon
  was unreachable. Docker 29 says "failed to connect to the docker API",
  which `engineErrorLine` did not match. `TestEngineErrorLine` covers it now.
- **L4. `sandbox.network: bridge` was undocumented. Docs.** It silently
  turns off every "no network" claim. It is now mentioned in the README,
  SECURITY.md and the sandbox doc.
- **L5. "CI workflows" overstated protection. Docs.** Only
  `.github/workflows/` and `.gitlab-ci.yml` are protected (plus
  `.boundedcode/`, `.gitmodules` and CODEOWNERS). `.github/actions/`,
  `.circleci/`, `Jenkinsfile` and similar files are not. The README row now
  lists exactly what is protected.
- **L6. "Only the task worktree is writable" was too narrow. Docs.** The
  persistence dir, the admin dir and caches are also writable. The README
  row is corrected.
- **L7. Installer location was wrong for Windows. Docs.** Getting-started
  said `~/.local/bin` for all platforms. It now gives the Windows path too.
- **L8. The demo duration "1m43s" has no run record in the repository.
  Open.** The source is the message of commit `4a531d0`. Add the run record
  or soften the caption.

### Checked and found accurate

Each of these was recomputed from raw records or read in the code:

- **Second validation:**
  - 5/6 strict `TASK_VERIFIED`; 6/6 hidden tests passed; 0 frontier calls.
  - 0 self-verified tasks that failed the hidden tests.
  - Context use 18.3–35.0 K tokens; shares 1.60% and 29.13%.
  - Screening: 10 of 24 candidates rejected (5 AMBIGUOUS + 5 NO per
    `screening.md`).
  - Hidden tests are applied only after the agent finishes, on a
    `--depth 1` clone (`internal/benchmark/tasks.go`).
- **First validation and development runs:**
  - 0/8, then 1/8.
  - 4 frontier calls sent plus 1 blocked.
  - Development false passes: 3 (failure-driven) and 2 (targeted).
  - Baseline slower in both runs.
  - Ablation 3/3 vs 0/3.
- **Verification gate:**
  - It reads its config from the base commit.
  - The control run without the changed tests is really performed.
  - Go compile-failure and timeout exclusion work.
  - Error paths fail closed.
  - The "ask once" request for a test works.
- **Sandbox:** `--network none` by default, `--cap-drop ALL`,
  no-new-privileges, pids limit, private tmpfs home, and a refusal list for
  credential mounts.
- **API keys:** kept off command lines and out of the configuration, and
  redacted in telemetry.
- **Upstream components:** the "implemented vs integrated" table in the
  README matches the code layout. The llama.cpp `v0.5.0` pin exists
  upstream.
- **Linux installer:** `install.sh`, run into an isolated home, picked
  v0.1.0-alpha.4, verified its checksum and installed `boundedcode` and
  `bcode`.
- **First run on Linux:** `setup --check`, `setup --only config -y` and
  `doctor` gave accurate, actionable output in a fresh home.

## Remaining risks and how to validate them

| Risk | What would validate or close it |
|---|---|
| C1/H1/H2/M1 fixes are unreleased; installers ship alpha.4 | Cut a release from `main` that includes these fixes. Then run `scripts/install.sh` in a clean home and confirm that `boundedcode version` reports the new tag. Decide whether C1 needs a GitHub security advisory. |
| H2 fix not yet run in a real container | Run a Go task end to end with `sandbox.kind: docker` and check that the `verify.evidence` event lists the test and that an extra `go test -run … -v` stage ran. Re-run one second-validation Go task (gin-1805 or caddy-6370) and confirm that it still ends `TASK_VERIFIED`. |
| C1 fix in a real container | In a Docker-sandboxed task, have the agent create `sub/.git` with a clean filter (the scripted agent in `internal/agent/scripted` can do this). Expect the task to block with "nested git repository" and no host canary. |
| H3 git history / `.git/config` exposure | Needs a design change: for example, mount a filtered object store or a `--reference` clone without secrets, and mask `config`. Validate with an adversarial test that runs `git show HEAD:.env` in the sandbox and expects failure. |
| H4 dynamic-language evidence | Apply load-failure and attribute-error detection per named test (Python `AttributeError`/`ModuleNotFoundError`/`ImportError`, JavaScript `TypeError: … is not a function`/`Cannot find module`). Add the audit's probes (pytest, unittest, node) as regression tests. |
| M2 flaky evidence | Re-run the base-with-changed-tests run and the control 2–3 times, and count only failures that are stable on the base. Validate with the flaky probes from this audit. |
| M3 custom Go stages | Detect Go stages by argv (`go test`), not by `requires[0]`. Validate with a custom-stage probe. |
| M4 race | Pause or stop the agent container around `CheckTaskWorktree` + `CommitAll`. Validate with a background loop in the container that flips `commondir`. |
| M6 macOS/Windows | Triage each failing package, separating test assumptions from defects. Run the full flow (install → setup → one task → verify) once on a Mac and once on Windows 11 with Docker Desktop, and record it as was done for Linux in `benchmarks/reports/publication-20261005/fresh-clone-test.md`. |
| H6 installer on real Windows | Run `irm … \| iex` on Windows 10/11 PowerShell 5.1 and 7, without Git (expect a red message and an open window) and with Git (expect an install, a PATH update and `bcode version`). |
| Validation scope (M5) | A held-out set from repositories outside the development corpus, chosen by a recorded rule committed *before* screening, run more than once per task. |

## Changes made

Code:
- `internal/gitops/git.go`:
  - hardening flags for submodules and quoting;
  - NUL-separated name parsing (`ChangedFiles`, `untrackedFiles`,
    `splitNUL`);
  - nested-repository refusal in `CommitAll` (`checkNoNestedRepos`).
- `internal/verify/delta.go`: Go evidence must pass on the change
  (`goTestsPass`), with a new reason message.
- `internal/orchestrator/escalation.go`: blocked packets are kept outside
  the mounted frontier directory.
- `internal/sandbox/engine.go`: recognise Docker 29's connection error.
- `scripts/install.ps1`:
  - scoped settings;
  - errors no longer exit the session;
  - `exit 1` only when run as a file.

Tests:
- `internal/gitops/git_test.go`: `TestNestedRepositoryConfigNotExecuted`,
  `TestChangedFilesUnquoted`.
- `internal/verify/delta_test.go`: two new Go evidence cases.
- `internal/orchestrator/packet_test.go`: location assertion for blocked
  packets.
- `internal/sandbox/engine_test.go`: `TestEngineErrorLine`.

Documentation:
- `README.md`: release binaries, "unreleased" items, the verification
  definition and its limits, security table rows, frontier wording, memory
  figure, platform CI figures, methodology disclosures.
- `SECURITY.md`: network opt-out, git history exposure, frontier container
  contents.
- `docs/design/sandbox.md`: controls 8 and 12, the adversarial-test row,
  residual risks 8 and 12–15.
- `docs/usage/getting-started.md`: Windows install location.
- `CHANGELOG.md`: an "Unreleased" section.
- `docs/public-launch/readiness-audit.md`: this report.

Not changed:
- historical benchmark reports and results;
- ADR texts;
- release artifacts and tags.

Nothing was committed, pushed or released.

## Tests and validation executed

All on the Linux reference machine (Go 1.27.1, git 2.47.3):

- `make fmt`, `go vet ./...`, `go test -count=1 ./...`: all packages pass.
- `go test -race -count=1` on `internal/gitops`, `internal/verify`,
  `internal/sandbox` and `internal/orchestrator`: pass.
- `golangci-lint run ./...` (v2 config in the repository): 0 issues.
- Cross-OS `go vet` for linux/amd64, darwin/arm64 and windows/amd64 on the
  changed packages: pass.
- Each new regression test was confirmed to fail against the original file
  (`git show HEAD:…`) and to pass with the fix.
- The verification audit's probe tests were run against the fixed code:
  - skip-on-change: now rejected;
  - quoted protected path: now flagged;
  - H4, M2, M3 probes and the adversarial evidence-directory probe: still
    `Verified:true`, as documented above.
- Installers:
  - `scripts/install.sh` into an isolated `HOME`: release download and
    checksum OK.
  - `scripts/install.ps1` with PowerShell 7.4.6 on Linux: iex and `-File`
    failure paths, the download and checksum path, and a parse check.
- First run: `boundedcode setup --check`, `setup --only config -y` and
  `doctor` in an isolated home.
- CI: logs of the native macOS and Windows jobs of run 37834095378 were
  read for per-package results.

Not executed:
- an end-to-end task with the local model and the Docker sandbox;
- anything on macOS or Windows hardware;
- the Python adapter tests (the adapter was not changed).

## Unresolved blockers

1. **Release the fixes.** C1 is a sandbox escape present in every published
   binary. Until a release that includes it ships, the README's
   containment claims hold for `main` only.
2. **H3 (git history readable in the sandbox)** is disclosed, but users who
   keep secrets in history are exposed. It needs a design decision before
   it can be called contained.
3. **macOS and Windows** remain experimental and unrun; the documentation
   says so.
