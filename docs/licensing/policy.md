# Licensing and Dependency Policy

## Our code

The project's own code is Apache-2.0 ([LICENSE](../../LICENSE)).
Contributions are accepted under the [DCO](../../DCO). There is no CLA.

## Dependency license classes

| Class | Licenses | Rule |
|---|---|---|
| Allowed | Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC (also 0BSD, CC0-1.0, Unlicense) | No special review. |
| Manual review | MPL-2.0, the LGPL family, other weak copyleft, custom licenses (e.g. OpenMDW for models) | The maintainer approves and records the decision in the license matrix. |
| Not permitted in the core distribution | GPL, AGPL, SSPL, source-available, proprietary | Requires an explicit legal and architectural review. Such a component may only ever be an optional, separately installed external process. |

This is project policy. It does not claim that any of these licenses is
invalid.

Enforcement:

* `scripts/licensecheck` fails CI for denied, unknown or unreviewed licenses
  among Go modules linked into shipped binaries. With `-check-notices` it
  also fails when a linked module is missing from `THIRD_PARTY_NOTICES.md` or
  the license matrix, and CI fails when `make licenses` changes `LICENSES/`.
* `scripts/pylicensecheck.py` covers the Python environments of the adapter
  and of Serena. It fails on a denied license. For an expression with `AND`,
  every part must pass; `OR` picks the most permissive option within one
  part. CI regenerates `adapters/openhands/python/THIRD_PARTY.md` and
  `configs/serena/THIRD_PARTY.md` and fails if they change, so new
  review-class packages are reviewed and recorded in the matrix.
* `docs/licensing/upstream-license-matrix.md` covers external runtimes and
  models.

## Pinning and upgrades

Every external component records its name, source, version/tag/commit,
license, LICENSE sha256, last-verified date and update policy (see
[upstream components](../architecture/upstream-components.md)). We never
track `main`.

An upgrade requires:

1. Re-reading the LICENSE file at the new tag and comparing the sha256.
2. Running the compatibility/integration test for that boundary.
3. A benchmark where relevant (inference runtime, model, agent runtime,
   repository intelligence).
4. Updating the matrix, `THIRD_PARTY_NOTICES.md` and `LICENSES/`.

If the license changed, stop and document the discrepancy before adopting the
new version.

**Serena** is pinned to v1.7.0, its last MIT-licensed release (ADR-0008).
Serena upgrades are manual and require license review: a request, a license
review (v2 relicenses the application to GPL-3.0-or-later, which this policy
does not permit in the core), a compatibility review, `boundedcode bench
intel` on the new version, and explicit maintainer approval. CI
(`scripts/serenaguard`) fails if the pin changes without the matrix,
notices, ADR and benchmark changing with it, or if anything installs Serena
unpinned. It scans install files (scripts, Dockerfiles, CI, Python
project files) and Markdown. A Markdown line that only names the package in
prose can carry an HTML comment `serenaguard:allow`. Dependency updaters must ignore `serena-agent`. <!-- serenaguard:allow: prose, not an install -->

## Copied or derived code

Prefer writing our own adapters. If a file is derived from upstream code, add
a header like this:

```text
Derived from <project>/<path> at <tag/commit>.
Original source: <URL>
Original license: <SPDX id>. Copyright <holder>.
Modified by the boundedcode authors.
```

Also add an entry to `THIRD_PARTY_NOTICES.md`, and never remove upstream
copyright notices. **No vendored or derived upstream code currently exists
in this repository.**

## Models

Model weights are never committed or redistributed. Profiles record the
source repository, the commit (`source.revision`) and the license so users
can review them before downloading. `bcode model fetch` (and `scripts/fetch-model.sh`)
only download at an explicit commit and verify the file against the sha256
recorded in the profile (the hub's LFS sha256).

## SBOM

Release builds produce an SPDX 2.3 JSON SBOM (`make sbom`, see the
[release process](../development/release.md)); CI generates one on every
run as a smoke test. It lists:

* the Go modules linked into the binary, with licenses classified from their
  LICENSE files;
* the pinned external programs (llama.cpp, `openhands-sdk`,
  `openhands-tools`, codebase-memory-mcp, gitleaks, Serena);
* the Python distributions of the adapter and Serena environments, from
  their `uv.lock` files (version, artifact URL, sha256). Only runtime (not
  dev) dependencies that are installed in the reference Linux x86-64
  environment are listed. Their license field is `NOASSERTION`; the package
  metadata license and the classifier verdict are in each entry's comment,
  and the reviewed decisions are in the matrix.
