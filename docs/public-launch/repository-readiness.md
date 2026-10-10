# Repository readiness for external contributors (2026-10-10)

This page covers the GitHub configuration, contribution path, CI
transparency and release integrity of
[akynte/boundedcode](https://github.com/akynte/boundedcode).
- **What was checked:** the repository at commit `6b47963` and its GitHub
  settings, read through the GitHub API on 2026-10-10.
- **What it records:** what was checked, what was changed in the
  repository, and what only the maintainer can change.
- **What was not done:** no repository setting, label, issue or release was
  changed.

## Starting point

| Area | Found |
|---|---|
| Community files | LICENSE (Apache-2.0), CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, GOVERNANCE, DCO, PR template: GitHub's community profile reports 100%. |
| Issue templates | Three Markdown templates (bug, feature, model compatibility). Blank issues allowed. The `model-compatibility` label they apply does not exist, so GitHub silently drops it. |
| Labels | GitHub's defaults only. |
| Issues, pull requests, forks | None yet. |
| Security | Private vulnerability reporting, secret scanning and push protection are on. Dependabot alerts are off. |
| Branch protection | None on `main`; no rulesets. |
| Features | Wiki on, but it has no content (no wiki repository exists). Projects on, unused. Discussions off. No Pages site, no homepage. |
| Releases | Four pre-releases (v0.1.0-alpha.1 to alpha.4). None is marked "latest", which GitHub reserves for full releases. |
| CI | Four workflows: `ci`, `smoke`, `dco`, `secret-scan`. All green on `6b47963` after the Go 1.27.2 fix. |

## Findings and changes

### Contribution instructions, tested in a fresh clone

Every command in CONTRIBUTING.md was run in a fresh clone of `6b47963`,
with Go 1.27.2, golangci-lint v2.14.0, uv and gitleaks installed. All of
them succeeded:
- `make build` (11 s);
- `make check` (368 s);
- the Python adapter tests;
- `serenaguard`;
- the license check;
- `check-dco.sh`;
- `gitleaks`.

**Gaps found, and fixed in CONTRIBUTING.md (new "Tool versions" table):**
- **The linter version was not stated.** With golangci-lint v2.13.2 (the
  version CI pinned until `6b47963`), `make check` fails on Go 1.27.2:
  "export data version 5 is greater than maximum supported version 4".
- **gitleaks had no install instructions,** although
  `scripts/install-gitleaks.sh` installs the pinned version.
- **The race tests' requirement was not mentioned.** `make check` runs them,
  which needs cgo and a C compiler.

**Commands in the docs and templates.** Every `boundedcode` and `bcode`
command named in the Markdown documentation and the scripts was checked
against the CLI's command tree. None is missing. (`task resume` looks
missing in `--help`, but it is an alias of `task run`.)

### CI did not show what it skipped or let fail

| Problem | Change |
|---|---|
| The macOS and Windows test step is `continue-on-error`, so the job is green while 5 (macOS) and 14 (Windows) packages fail. | The step now runs through `scripts/gotestsummary`. On failure, the step is marked failed, a warning annotation names the platform, and the job summary lists the failing tests. It stays non-blocking. |
| Tests that skip because a tool is missing skip silently, for example the sandbox tests without `BC_TEST_DOCKER_IMAGE`. | The `go` job's test step lists every skipped test with its reason in the job summary. |
| The sandbox integration tests (`BC_TEST_DOCKER_IMAGE`) ran in no workflow. | The weekly `first-task` job, which already builds the image, now runs them for `internal/sandbox`, `internal/agent/openhands`, `internal/benchmark`, `internal/frontier` and `internal/verify`. Locally against the image: 156 passed and 2 skipped with stated reasons. `TestEngineGoRepo` passed only with golangci-lint v2.14.0 (first-contribution item 3). |
| CONTRIBUTING did not say what CI checks. | New "What CI checks" section: each job, whether it blocks, and what the check marks do not show. |

No check was removed or made weaker. The workflows pass `actionlint`
v1.7.12.

### Issue templates

The Markdown templates were replaced by issue forms with required fields:
- bug report;
- **installation or setup problem**;
- **platform compatibility report**;
- **wrong verification result** (false positive or false negative);
- feature proposal;
- model compatibility report;
- **evaluation result**.

`config.yml` turns off blank issues and links to private vulnerability
reporting and to CONTRIBUTING.

**Tests:**
- All eight files validate against SchemaStore's `github-issue-forms` and
  `github-issue-config` JSON schemas.
- Every repository link in them resolves.
- Every command they ask reporters to run exists in the CLI.

**Not yet tested:** GitHub's own rendering. That needs the files on the
default branch (see the checklist).

### Release integrity

- **Rebuild.** v0.1.0-alpha.4 was rebuilt from its tag with Go 1.27.1
  (`make dist`).
  - All six binaries and the licenses archive match the published
    `SHA256SUMS`.
  - **The six SBOMs did not.** The generator stamped their creation time
    and document namespace from the clock.
- **Fixed:** `scripts/sbom` now uses `SOURCE_DATE_EPOCH` (the Makefile
  exports the commit time) and puts the target OS and architecture in the
  namespace. Before, the six SBOMs of a release differed only by the second
  they were written. Two runs now give identical files (unit-tested).
- **Documented:** [docs/development/release.md](../development/release.md)
  gained "Verifying a release". It covers:
  - the checksum check;
  - the rebuild procedure and the measured result above;
  - the limits: checksums are published next to the binaries, releases are
    unsigned and have no attestation, and the SBOMs cover compiled modules
    plus pinned runtime components.
- **Supported-platform statements are accurate.**
  - They match the CI evidence and the
    [onboarding validation](onboarding-validation.md#platform-compatibility-matrix):
    Linux validated, macOS and Windows experimental.
  - The platform table in getting-started now also states that unit tests
    fail on macOS (5 packages) and Windows (14 packages).

### Contributor onboarding and first contributions

- **Onboarding guide.** [Contributor onboarding](../development/onboarding.md)
  is a short path from a fresh clone to a pull request.
- **First contributions.** It lists four first-contribution opportunities.
  Each is a defect observed in CI run 38051481460 or locally, with the file,
  the line and a definition of done.
- **Defects for the maintainer.** It lists three more, separately, because
  they touch security-sensitive code: Windows secret-path separators,
  Windows file-permission tests, and macOS frontier-packet path redaction.
  The four first-contribution items were filed as issues
  [#1](https://github.com/akynte/boundedcode/issues/1)–[#4](https://github.com/akynte/boundedcode/issues/4)
  on 2026-10-10. The three others are not filed.

### External evaluation results

There was no way to submit one.
[Submitting an evaluation result](../../benchmarks/submitting-results.md)
defines what a result must include:
- a selection rule fixed before running;
- every run;
- hidden acceptance;
- the raw `bench tasks` output;
- the environment;
- deviations.

Results can be submitted in two ways:
- **A pull request** into `benchmarks/community/`.
- **The "Evaluation result" issue form.**

Submitted numbers are merged as written and labelled external. A
maintainer's rerun is published next to them as a separate report.

## Recommended metadata

**Description.** Keep the one proposed in the
[README review](readme-review.md#proposed-github-metadata). It is still
accurate after the comparative evaluation, because it claims a mechanism,
not superiority:

> Control plane for AI coding agents: runs OpenHands in a network-less
> sandbox and reports a change as verified only when a test it adds fails on
> the original code and passes with the change. Local llama.cpp model by
> default, or a cloud API. Public alpha.

**Topics.** Also as proposed there:

`coding-agent`, `llm`, `local-llm`, `llama-cpp`, `openhands`,
`software-testing`, `sandbox`, `developer-tools`, `golang`, `mcp`

**Homepage.** Leave it empty. There is no site. Pointing it at the
repository itself or at a docs folder adds nothing over the README.

## GitHub Discussions: not now

**Recommendation:** keep Discussions off for now. With one maintainer and
no issues yet, Discussions would split the first feedback across two
places that both need watching. The issue forms already cover the expected
kinds of feedback:
- defects;
- installation;
- platforms;
- verification accuracy;
- models;
- evaluation results;
- proposals.

They ask for the facts a reply needs, and issues can be labelled, linked
from commits and closed when resolved.

**Revisit** when open-ended questions or show-and-tell posts start arriving
as issues, or when a second maintainer can share triage.

**The wiki.** It is on but empty, and by default any GitHub user can edit
it. It would duplicate `docs/`. Turn it off.

## Maintainer checklist

These need repository settings or public actions, so they were not done
when this audit was written. They are listed in suggested order.

**Status on 2026-10-10.** Done by the maintainer:
- items 1 and 3–9: the labels, the `main` ruleset, web sign-off,
  Dependabot alerts, the description and topics, the wiki turned off,
  head-branch deletion, and issues #1–#4;
- item 10: decided as option A, the release workflow with build
  attestation (`.github/workflows/release.yml`).

Still open:
- item 2: check the issue forms in a browser;
- item 11: the next release.

| # | Action | Why | How |
|---|---|---|---|
| 1 | Create the labels the issue forms apply | Without them, GitHub silently drops the labels | `gh label create installation -c 1d76db -d "Installer, setup or build problem"`; `gh label create platform -c 5319e7 -d "Platform compatibility"`; `gh label create verification-accuracy -c b60205 -d "False positive or negative verification result"`; `gh label create model-compatibility -c 0e8a16 -d "Local model behaviour"`; `gh label create evaluation-result -c fbca04 -d "Submitted evaluation result"` |
| 2 | After merging, open each form once at `/issues/new/choose` | GitHub's rendering cannot be tested offline | Check that each form renders and that the security link opens private reporting |
| 3 | Protect `main` with a ruleset | Nothing stops a direct push that skips CI | Require these status checks: `go`, `lint`, `adapter`, all six `cross-build` jobs, both `native` jobs (build and vet block; their tests are report-only), `signoff` and `gitleaks`. Do not require the `smoke` jobs, which run only when their paths change. Block force pushes and deletion. Allow the maintainer to bypass while there is one maintainer. |
| 4 | Require sign-off on web-based commits | Edits made in the GitHub web editor otherwise lack the DCO trailer that `dco` requires | Settings → General → "Require contributors to sign off on web-based commits" |
| 5 | Turn on Dependabot alerts, not version-update PRs | There is no Python vulnerability scan (SECURITY.md). Alerts cover Go and Python lock files. `serenaguard` deliberately rejects `dependabot.yml` so that nothing bumps Serena automatically. | Settings → Code security → Dependabot alerts |
| 6 | Apply the description and topics above | Discovery and an accurate first impression | `gh repo edit --description "…"`, `--add-topic` and `--remove-topic` |
| 7 | Turn off the wiki | Empty, editable by anyone, duplicates `docs/` | `gh repo edit --enable-wiki=false` |
| 8 | Turn on "Automatically delete head branches" | Tidier fork and branch list once pull requests arrive | `gh repo edit --delete-branch-on-merge` |
| 9 | File the four first-contribution items as issues, labelled `good first issue` | They exist only in the onboarding guide | Copy each section of [onboarding](../development/onboarding.md#first-contribution-opportunities) into an issue, with the CI run link |
| 10 | Decide on release signing or build attestation | Checksums alone do not detect a replaced release | For example, a release workflow with `actions/attest-build-provenance`. That needs a decision about building releases in CI instead of locally. |
| 11 | Publish the next release with the SBOM fix | Makes SBOMs reproducible from the tag | The release process in [release.md](../development/release.md) |

Projects (enabled, unused) can stay as they are. Turning them off is
optional.
