# Lease study (2026-10-26)

The plan's obligation ledger starts from a *lease*: the declarations, files
and directories a request is about, derived from the request's text before
any work. A lease check on edits (warn or refuse when the agent edits
outside it) is only worth building if derived leases are accurate, so the
deriver was measured first (`bcode bench lease`, `internal/lease`). No
model is involved.

**Method.** For the newest commits of three Go repositories that change Go
code (at most 30 non-test files), the lease is derived from the commit
message against the parent's tree and compared with what the commit
changed outside tests: declarations (from the zero-context diff mapped onto
both versions of each file), files and directories. A commit message
stands in for a request; many Go projects start it with the package path,
so it is also scored without that prefix. Repositories: golang.org/x/tools
at `86210da64`, spf13/cobra at `adbc881`, BoundedCode itself. Report:
`benchmarks/reports/20261026T181236Z-lease-study.md`.

**The deriver.** Words in the request that name something that exists:
paths and bare file names (by suffix), `Type.Method` and `pkg.Name`, and
bare names that look like code (an inner capital, an underscore, a digit)
or that the request marks as code (backticks, a call). Plain capitalised
words ("Print", "Usage", "Stream") are English as often as code and need
marking; a name defined in more than four places needs its directory.

| Repository | Message | Commits | Lease named something | Symbol precision | File precision | Dir precision | Lease holds the whole change |
|---|---|---|---|---|---|---|---|
| x/tools | as written | 300 | 96% | 38% | 40% | 63% | 65% |
| x/tools | without `pkg:` prefix | 300 | 57% | 41% | 43% | 47% | 28% |
| cobra | as written | 300 | 21% | 55% | 79% | 96% | 20% |
| boundedcode | as written | 63 | 25% | 33% | 68% | 73% | 0% |

Recall is low everywhere (symbols 0–9%, files 2–19%): a message names the
heart of a change, not its extent.

**Variants measured and dropped.** Counting plain capitalised words found
more leases but at 44% (cobra) and 18% (boundedcode) symbol precision
instead of 55% and 33%. Marking a name by its kind ("the method Entries")
recovered the direct suite's rename request but took x/tools symbol
precision from 38% to 28% ("the type checker").

**The direct suite's own requests** (passing runs' patches as the change,
five usable code tasks): three leases named something and all three held
the change; two were empty ("add a test for Ledger.Balance" style requests
without code-marked names, and a currency feature named only in prose).
Too few to weigh against the histories.

## Decision

A lease derived from the request alone is empty for a large share of
requests and, when not empty, misses part of the change in a third of
x/tools commits even with the package named, and in most others. A check
on edits built on it would warn on a large share of legitimate changes,
so the lease check is not built. The deriver and the study stay as the
measurement for any later attempt (a lease widened from the code itself,
for example the callers of the leased declarations, could be scored the
same way).
