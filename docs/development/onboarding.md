# Contributor onboarding

A short path from a fresh clone to a merged change. The rules
(sign-off, licensing, security-sensitive areas) are in
[CONTRIBUTING.md](../../CONTRIBUTING.md); the code layout and test commands
are in the [development guide](guide.md).

## 1. Get a green build (about 15 minutes)

```bash
git clone https://github.com/akynte/boundedcode.git && cd boundedcode
make build                  # ./bin/boundedcode; needs only Go
go test -short ./...        # fast unit tests, about a minute
make check                  # what CI's go and lint jobs run; about 6 minutes
```

`make check` needs the tool versions listed in
[CONTRIBUTING.md](../../CONTRIBUTING.md#tool-versions). An older
golangci-lint fails on the current Go version with "export data version …
is greater than maximum supported version".

You need no model, GPU or Docker for this. Tests that need them skip
themselves and say why (`go test -v` shows the reason).

## 2. Try the product

The [getting-started guide](../usage/getting-started.md) runs a first task.
A cloud API key is the quickest way to get a model. The local model needs
about 22 GB and a capable machine.

## 3. Pick something to work on

- An issue labelled `good first issue`, or one of the
  [opportunities below](#first-contribution-opportunities).
- Anything you hit while trying the product. The issue forms ask for what
  a fix needs.
- Platform reports from macOS or Windows: the full flow has not been run
  there yet ([platform matrix](../public-launch/onboarding-validation.md#platform-compatibility-matrix)).
- Evaluation results on your own tasks or hardware
  ([how to submit](../../benchmarks/submitting-results.md)).

For anything larger than a small fix, open an issue first, so the approach
can be agreed before you write it.

## 4. Send the change

```bash
git switch -c fix/short-name
# edit, add a test
make check
git commit -s -m "area: what changed"   # -s adds the DCO sign-off; CI rejects commits without it
```

Open a pull request against `main`. The template asks for the tests you
ran and the security impact. CI then runs the checks described in
[CONTRIBUTING.md](../../CONTRIBUTING.md#what-ci-checks).

## First-contribution opportunities

Each item below is a defect observed in CI or locally. Each includes the
evidence and a definition of done. They are narrow on purpose: one root
cause each, testable in CI.

The macOS and Windows test failures come from CI run
[38051481460](https://github.com/akynte/boundedcode/actions/runs/38051481460).
Its `native` job runs the suite in report-only mode, so the job stays
green. The step itself is marked failed, and its job summary lists the
failing tests.

### 1. Sandbox-engine tests assume the Linux hint (macOS)

When the Docker daemon is unreachable, `EngineError.Hint()` correctly
advises starting Docker Desktop on macOS and Windows, and joining the
`docker` group on Linux (`internal/sandbox/engine.go`). But four tests
require "docker group" on every OS:

| Test | File |
|---|---|
| `TestCheckEngine/daemon_down` | `internal/sandbox/engine_test.go:52` |
| `TestEngineHintForEnginePath` | `internal/sandbox/engine_test.go:100` |
| `TestDoctorContainerEngine/daemon_down` | `internal/cli/doctor_test.go:78` |
| `TestBrokenEngineIsAnError/daemon_down` | `internal/verify/engine_test.go:77` |

**Done when:** the assertions expect the hint for `runtime.GOOS`, and these
four no longer appear in the macOS job summary.

### 2. Git tests compare unresolved temporary paths (macOS)

On macOS, `t.TempDir()` is under `/var`, which is a symlink to
`/private/var`. In `TestWorktreeLifecycle` (`internal/gitops/git_test.go:74`),
`CommonDir` returns the resolved `/private/var/.../repo/.git`, while the
test expects the unresolved path.

**Done when:** the test compares paths after `filepath.EvalSymlinks`, and
`internal/gitops` passes on macOS. `TestRelativeGitdir` in the same package
also fails on macOS; check whether it has the same cause before fixing it.

### 3. A verification test depends on the installed golangci-lint (any OS)

`TestEngineGoRepo` (`internal/verify/verify_test.go:44`) runs the Go
verification preset on the host, so the preset's golangci-lint stage uses
whatever `golangci-lint` is on `PATH`. If the linter is installed but too
old for the Go version, the test fails, as happened locally with
golangci-lint v2.13.2 and Go 1.27.2. CI's test job has no linter
installed, so it never notices.

**Done when:** the test no longer depends on the host's linter version.
For example, it could put a stub `golangci-lint` on `PATH`, as
`fakeDocker` does in `internal/sandbox/engine_test.go`.

### 4. An unpack test checks the Unix executable bit (Windows)

`TestFetchAndUnpackTarGz` (`internal/install/install_test.go:64`) fails on
Windows with "server not executable": Windows has no executable bit.

**Done when:** the test checks what Windows needs (the file exists, with
the expected name), and `internal/install` passes in the Windows job
summary.

## Defects that need a maintainer

These were also seen in CI run 38051481460. They touch security-sensitive
code (see CONTRIBUTING.md), so they are not first contributions:

- **Windows: secret-path detection.** `FindSecretPaths` returns
  `credentials\prod.json` where the test expects `credentials/prod.json`
  (`internal/policy/policy_test.go:174`). Its results feed the sandbox
  masks and the path policy. Decide which separator they should use before
  changing the test.
- **Windows: credential and configuration file permissions.** These tests
  read POSIX mode bits (`-rw-rw-rw-`):
  - `internal/config/config_test.go:92`;
  - `internal/secrets/secrets_test.go:30`.

  On Windows, protection comes from an access list. The tests should
  assert the access list, not be skipped.
- **macOS: frontier packet sanitization.** A path is not redacted when the
  text spells the directory differently from its resolved form (`/var/...`
  vs `/private/var/...`) (`TestSanitizeSymlinkedLocation`,
  `internal/frontier/sanitize_test.go:65`). The leaked text is a local
  path in a packet sent to a frontier model.
