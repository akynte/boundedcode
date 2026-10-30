# p11-context-and-loops: the recorded run

The evidence of the experiment in `../p11-context-and-loops.yaml`, run on
2026-10-30 from 08:38 to 13:23 (local time). The write-up is in
[docs/benchmarks/lab-2026-10.md](../../../../docs/benchmarks/lab-2026-10.md#the-first-prospective-experiment).

The files are copied from the run's directory in the laboratory, with three
changes:
- **Paths:** absolute paths in `provenance.json` and `outputs.json` are made
  relative to this directory, and the repository's path is removed.
- **Left out:** each arm's home (its configuration, sessions and fixture
  copies), its binary and the build worktrees. Every binary can be rebuilt:
  revision `15ab3aa`, plus `../loop-guard.patch` for the loop-guard arm.
  Their hashes are in `provenance.json`.
- **Two decisions:**
  - `decision-at-run.{json,md}` is what the run decided. Its hash is the one
    `outputs.json` pins, and `lab evaluate` at the time reproduced it byte
    for byte.
  - `decision.{json,md}` is the same reports decided again by the committed
    laboratory, after two fixes to how decisions are reported. A predicted
    fix that got worse is now listed as such, not as "not fixed". A paired
    test with too few tasks to decide says "inconclusive", not "unchanged".
    No verdict changed.

The reports are unchanged, and their hashes are in `provenance.json`. To
check them and decide again, run from the repository root:

    bcode lab evaluate benchmarks/lab/experiments/p11-context-and-loops
