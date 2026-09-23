# Authored oracles

One hidden acceptance suite per task, in the format an operator installs
(`oracle.dir`), used by `bcode eval judges --oracles evals/oracles`.

## How they were written

On 2026-09-23 Claude Code 2.1.280 (`claude -p`, model `claude-opus-5-5[1m]`)
wrote each suite from the task's objective and the unfixed fixture, in a
throwaway copy with no hidden acceptance files and no candidate patch, with
every path in this repository denied. $9.83 in total. Only new `*_test.go`
files were kept. The prompt told the author to cover what the change must do
and existing behaviour it could break, to prefix every test with `TestOracle`
and every other identifier with `oracle`, and not to edit existing files.

The suites were then converted, one check per package running only the
`TestOracle` tests. Nobody reviewed them. In the product an operator would
accept or edit a drafted suite; here the draft is used as written.

## Why they are a fair oracle, and where they are not

- **Independent of the labels.** The author never saw the hidden tests that
  decide whether a candidate solved its task.
- **Not independent of the patch agent.** The same model wrote the agent
  patches, so both may read an objective the same way. On `damaged-stock-001`,
  whose objective does not name the method to add, the oracle and the patch
  both call it `SetDamaged`; the hidden test calls it `MarkDamaged`.
- **Reads were not fully audited.** Denied tool calls are logged, and none
  reached this repository. Allowed reads are not logged. The scratch area held
  the working copies of the earlier patch runs, so an author could have read a
  patch, though none was needed.
- Every suite fails on the unfixed code, which `bcode eval judges` checks before
  its verdicts are read.
