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
  among Go modules linked into shipped binaries.
* `scripts/pylicensecheck.sh` covers the Python adapter environment.
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
unpinned. Dependency updaters must ignore `serena-agent`.

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
source repository, revision and license so users can review them before
downloading.

## SBOM

Release builds produce an SPDX JSON SBOM (`make sbom`, see the
[release process](../development/release.md)).
