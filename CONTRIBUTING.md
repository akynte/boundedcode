# Contributing

Thanks for your interest. BoundedCode is a public alpha: interfaces and
configuration may change, and reports from real machines and repositories
are among the most useful contributions.

## Ground rules

* **Developer Certificate of Origin.** Every commit must be signed off
  (`git commit -s`), which certifies the [DCO](DCO). CI rejects commits
  without a `Signed-off-by:` trailer that matches the author. There is no CLA.
* **License.** Contributions are accepted under [Apache-2.0](LICENSE).
* **No copied upstream code** without prior discussion. Code derived from
  another project needs:
  - a provenance header (see [docs/licensing/policy.md](docs/licensing/policy.md));
  - an entry in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

  GPL, AGPL, SSPL and source-available code are not accepted in the core.
  Upstream tools (llama.cpp, the OpenHands SDK, codebase-memory-mcp, Serena)
  are integrated as separate processes or libraries, not vendored.
* **No model weights, credentials or proprietary source** in commits,
  fixtures or test logs.
* **Evidence over claims.** Performance or quality claims in docs need a
  reproducible benchmark under `benchmarks/`.

## Development setup

New here? [Contributor onboarding](docs/development/onboarding.md) walks
from a fresh clone to a pull request and lists first-contribution
opportunities. The [development guide](docs/development/guide.md) describes
the code layout and the test commands.

### Tool versions

| Tool | Version | Needed for |
|---|---|---|
| Go | the `go` line in `go.mod` | everything |
| Git | any recent | everything |
| C compiler (cgo) | any | `go test -race`, part of `make check` |
| golangci-lint | v2.14.0, as in CI (`.github/workflows/ci.yml`); older versions fail on the current Go | `make check` (skipped with a message if not installed; required in CI) |
| uv | 0.12.18, as in CI | the Python adapter tests and license checks |
| gitleaks | the pinned version: `scripts/install-gitleaks.sh DIR` | the secret scan |
| Docker or Podman | any recent | sandbox integration tests and real tasks |

```bash
make build     # ./bin/boundedcode
make check     # gofmt, go vet, go test, go test -race, golangci-lint, LICENSES/go (about 6 minutes)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

The same gates as CI:

```bash
cd adapters/openhands/python && uv run --frozen pytest -q            # Python adapter
go run ./scripts/serenaguard                                         # Serena stays on MIT v1.7.0
go run ./scripts/licensecheck -check-notices THIRD_PARTY_NOTICES.md,docs/licensing/upstream-license-matrix.md
scripts/check-dco.sh origin/main HEAD
gitleaks git --redact --no-banner .
```

Sandbox and security tests run against the real container image when
`BC_TEST_DOCKER_IMAGE` is set (build it with `boundedcode sandbox build`):

```bash
BC_TEST_DOCKER_IMAGE=boundedcode-openhands:local go test -count=1 ./...
```

Tests that need a GPU, a model, a live Codex account or experimental
infrastructure are skipped by default (environment variables or build tags).

## What CI checks

Every pull request and push to `main` runs these. None of them may be
weakened to get a change through.

| Workflow | Job | Checks | Blocking |
|---|---|---|---|
| `ci` | `go` | gofmt, vet, tests, race tests, license policy, `LICENSES/go` up to date, Serena pin, govulncheck, build, SBOM | yes |
| `ci` | `lint` | golangci-lint (pinned) | yes |
| `ci` | `cross-build` | vet and build for all six release targets | yes |
| `ci` | `adapter` | Python adapter tests, Python license inventories | yes |
| `ci` | `native` (macOS, Windows) | build and vet (blocking); unit tests **report only** | build: yes; tests: no |
| `dco` | `signoff` | `Signed-off-by` on every commit | yes |
| `secret-scan` | `gitleaks` | the full history | yes |
| `smoke` | installer jobs | install, re-install, tampered release, first run (Linux, macOS, Windows); when installer or CLI files change, and weekly | yes |
| `smoke` | `first-task` | sandbox integration tests, a task end to end in the Docker sandbox, the evidence demo; weekly and on demand | weekly |

**What is not visible from the check marks:**
- **macOS and Windows unit tests do not pass yet.** The `native` test step
  is non-blocking. When it fails, the step is marked failed, a warning
  annotation names the platform, and the failing tests are listed in the
  job summary.
- **Skips are listed.** The `go` job lists every skipped test, with its
  reason, in its job summary.
- **The sandbox integration tests need the sandbox image.** They run in
  the weekly `first-task` job, not on pull requests. A change to
  `internal/sandbox`, `internal/agent/openhands` or `internal/verify`
  should be tested locally with `BC_TEST_DOCKER_IMAGE`, as shown above.

## Security-sensitive changes

Changes to any of the following need tests that try to break them:
- the sandbox, masks and mounts (`internal/sandbox`);
- command and path policy (`internal/policy`);
- git worktree handling;
- verification integrity (`internal/verify`), including the
  cross-repository compatibility gate (`internal/compat`);
- frontier packet sanitization (`internal/frontier`).

Add a case to the adversarial suites and describe the threat in the PR. Never
weaken a test or an exclusion to make CI green. Report vulnerabilities
privately ([SECURITY.md](SECURITY.md)), not in issues or PRs.

## Adding an agent adapter

Adapters speak the JSON-RPC protocol in
[ADR-0004](docs/architecture/adr/0004-adapter-protocol.md). See
`adapters/openhands` for the reference implementation and
`internal/agent/scripted` for a test double. An adapter must:
- run inside the sandbox;
- route model calls through the control plane's tunnel;
- support resume and condensation.

## Adding a model profile

Add a YAML file to `configs/models/` with:
- the upstream repository, file and pinned revision (a commit, not a branch);
- the license;
- server settings measured with `boundedcode bench infra`.

Verify the license on the model page and record it in the
[upstream license matrix](docs/licensing/upstream-license-matrix.md). Never
commit weights. Reports of how a model behaves on your hardware are welcome
as issues ("Model compatibility").

## Benchmarks and validation

External results are welcome; see
[Submitting an evaluation result](benchmarks/submitting-results.md).

- **Keep history.** Historical results (including failures) are never
  rewritten or deleted.
- **New run or method → new report.** A new run or a methodology change gets
  a new report directory and document. Say what changed and why.
- **Development is not validation.** Development reruns on tasks used to
  drive fixes must be labelled as development evidence, not as independent
  validation.
- **No unmeasured numbers.** Numeric claims in docs must link to the raw
  report.

## Pull requests

* Keep changes small and focused, and include tests.
* Update docs and the status markers (*implemented*, *experimental*,
  *planned*) when behavior changes.
* Architectural changes need an ADR in `docs/architecture/adr/`.
* New dependencies need a license check and an entry in the notices.
