# Cross-Repository Compatibility Gate (`internal/compat`)

Status: **experimental**. It is unit-tested and tested against the real CLI on
fixtures, but it has not been run on a real multi-repository task.

Each task repository's own checks can pass while the repositories no longer
work together. A provider renames a protobuf field and updates its own
server. The client in another repository still builds, because it builds
against its vendored or published copy of the generated code. Before
`TASK_VERIFIED`, the gate checks every gRPC, protobuf or OpenAPI link that
the change affects. It runs the dependent repository's existing checks
against the candidate commits of the repositories that repository depends
on, and it records one result for each link.

## Results

| Result | Meaning |
|---|---|
| `compatible` | Every side the link needs was built against the candidate commits of what it depends on, and checks executed it and passed. Coverage or a knock-out run shows the execution (see below). |
| `broken` | The candidate no longer defines what a dependent still uses (an RPC, service, package or operation removed). Or the dependent's checks fail against the candidate and pass with the provider's base commit. |
| `untested` | Anything else. The reason says which evidence is missing. A `gap` field marks the cases that a test in the task could settle (`not_exercised`) and wire-incompatible definition changes (`breaking_definition`). |

A link is never `compatible` on the strength of static analysis, or of passing
tests that do not execute it.

## Which links are affected

The gate exports every task repository at its base commit and at its head
commit (`git archive`; the worktree is never read). It scans both versions
with `internal/xservice` (repositories outside the task come from the index)
and links each version. Every link of kind `grpc`, `grpc_def`, `proto`,
`openapi` or `openapi_impl` whose two sides are in different repositories,
with at least one side in the task, is compared across the two versions.
Link direction is kept: `From` depends on `To`.

| Link | Affected when |
|---|---|
| `grpc` (client → server) | the Go function containing the call changed, the server's registration or the methods implementing the RPC changed, or the RPC's definition changed |
| `grpc_def` (client or server → `.proto` service/rpc) | the code side's Go function changed, or the definition changed |
| `proto` (import of generated code → `.proto` package) | anything in the `.proto` file changed structurally |
| `openapi` (call → operation), `openapi_impl` (route → operation) | the code side changed, or the operation changed |
| any | the link appears or disappears between base and head |

"Changed" is precise where the evidence allows it:

* **Go code:** the diff hunks are intersected with the enclosing function, at
  the head commit and, by name, at the base commit. Code in other languages
  counts as changed when anything in its file changed.
* **`.proto`:** messages, fields, enums and RPC signatures are parsed and
  compared (`xservice.CompareRPC`, `CompareFile`). The comparison follows
  each RPC through every message and enum it reaches. Comments, formatting,
  and changes to other RPCs or messages do not affect the RPC. A change is
  *breaking* when a field is removed, renumbered, retyped, renamed or
  relabelled, when an enum value is removed or renamed, or when an RPC's
  types or streaming change. Adding a field or an enum value is compatible.
* **OpenAPI:** each operation has a digest of the operation, its path-level
  parameters, and every local `$ref` it reaches, formatting-independent
  (`xservice.OpenAPIOperations`).

Links whose files changed but which are not affected are listed as
`unaffected` (the report shows their count).

## Static decisions

* A link that exists only at the base commits, whose dependent still uses
  the contract at head while the definition (`.proto` service or RPC,
  protobuf package, OpenAPI operation) is gone: **broken**.
* A use that the change itself removed is **untested**. The gate cannot show
  that the consumer no longer needs the contract. The same holds for a
  server registration that is no longer recognized.
* A gRPC endpoint that the analyzer links to two or more services
  (ambiguous): **untested**.
* A side in a repository outside the task: **untested** ("add it to the
  task").

## Checks

### gRPC and protobuf: Go sides, built against the candidate

For each side that must be exercised (the client for `grpc_def`, the
importing code for `proto`, and both client and server for `grpc`):

1. The side must be Go, in a repository with `go.mod` at its root. Its
   verification config at the **base commit** must have a `go test` stage.
2. The side imports the generated Go package (`go_package`). That package
   must belong to a task repository's module, the provider. The provider can
   be the `.proto` repository or another one. If the provider is the side's
   own repository while the `.proto` lives elsewhere, the side uses a private
   copy of the generated code. The gate does not regenerate it, so the link
   is untested.
3. If the `.proto` changed but no Go file in the provider's generated package
   changed, the link is untested ("regenerate it"). The gate never runs
   `protoc`.
4. The commit trees are composed with a generated `go.work` (`use` the
   side's repository and the provider) and `GOWORK` set explicitly. Workspace
   mode resolves the provider's module to its candidate tree, so the side's
   `vendor/` and any `replace` directive do not apply.
   `go list -m` must report the provider's candidate directory, or the link
   is untested.
5. The repository's own `go test` stage runs in the sandbox. It is rewritten
   only to test the packages that hold the side and every package that
   imports them, with `-covermode=set -coverpkg=<the side's packages>`.
6. **Exercised** means that the coverage profile shows the side ran: the
   block holding the call line (or its function), the methods implementing
   the RPC on the server, or any statement of the importing file for
   `proto`.
7. When the candidate run fails, the gate runs controls. It runs the same
   command with all base commits; if that fails too, the failure predates
   the task and the link is untested. When both the side and the provider
   changed, it also runs the side's candidate with the provider's **base**
   commit; if that fails too, the failure belongs to the side itself, which
   its ordinary verification reports. The link is broken only when the
   failure follows the provider's change and the output names the side's
   package or file. When only the side changed, only a compiler error in the
   side's file counts.

A link with breaking definition changes whose task repositories all pass is
**untested** (`breaking_definition`). Code built from the base definition
(services already deployed, consumers outside the task) is not tested.

### OpenAPI: a knock-out shows that the check reads the operation

For the code side of an `openapi` or `openapi_impl` link, the gate does the
following:

1. It finds the dependent repository's test files that mention the spec's
   file name. If there are none, the link is untested (`not_exercised`).
2. It runs that repository's test stages (from the base-commit config; Go
   stages only for the packages of those test files) in a tree that holds
   the dependent and the spec repository at their candidate commits, in the
   same layout as the task's work directory. Relative paths between
   checkouts therefore resolve.
3. It runs them again with the operation removed from the spec. The side is
   **exercised** only if removing the operation makes a check fail. A test
   that merely loads the file does not count.
4. Failures go through the same control as for gRPC.

### Not supported (reported untested)

* Sides in languages other than Go for gRPC and protobuf. Python, Java, C#,
  TypeScript, Rust and Ruby sides are linked by `xservice` but not checked.
* Nested Go modules, and repositories whose test stage is not `go test`
  (for example `make test`): their coverage cannot be measured.
* HTTP links without a spec, topics, SQL and env (out of the gate's scope).
* End-to-end checks: the client is never run against the real server.
  `compatible` for a `grpc` link means that both sides compile against the
  same candidate definition and their own tests execute their side.

## Persistence, resume and staleness

* `compat_evaluations` holds one row per evaluation: its state, any error,
  and the unaffected links. `compat_results` holds one row per link result,
  with the `base..head` commits of every task repository that the result
  depends on. The same table holds each completed check run (`run:` keys).
  A run's key includes the commits of its composition and its command.
* An interrupted evaluation leaves the evaluation unfinished. The next
  evaluation reuses every link result and check run recorded for the same
  commits. It reruns only what is missing (`TestGateResume`).
* A result whose repositories have moved is **stale**. `task status` and the
  TUI mark it, and it is never reused (`TestGateStaleEvidence`).
* Each evaluation emits an audit event, `compat.result`.

## In the run loop

After the full gate passes in every repository:

1. If the gate's state is `broken`, verification fails. The attempt is
   rejected with a `compat:` signature, which loop and Z2 detection use. The
   next pack's advice lists each broken link, its commits and the failing
   output.
2. Otherwise the existing contract check, the Z3 review and the
   behavioural-evidence request run as before.
3. If links are untested with `gap: not_exercised`, the agent is asked once
   (`compat.evidence_requested`) for tests that execute them.
4. `TASK_VERIFIED` additionally requires the gate's state to be `none` (no
   affected link) or `compatible`. Otherwise the task ends `tests_green`,
   and a decision says why. The gate's summary is recorded as a task
   decision.

Turn it off with `repointel.compat_gate: false`. It is also off when
`repointel.cross_service` is off. With the gate off, verification behaves as
before.

## Gate integrity

* Trees are exported from commits. The gate checks exactly the recorded
  commits, and edits made during a run do not affect it.
* Stages come from each repository's verification config at its **base**
  commit (`.boundedcode/` is a protected path), with the same command
  policy, sandbox, mounts, masks and redaction as verification.
* `GOWORK` is set by the gate. A `go.work` in a repository, a `replace`
  directive or a vendored copy cannot redirect the provider's module, and
  `go list -m` confirms where it resolves (`TestGateBypassAttempts`).
* A consumer that switches to a private copy of the generated code loses
  its link to the definition. That link becomes untested, never compatible.
* Residual risk, the same as for verification: test code is written by the
  agent and runs inside the check. It could, for example, overwrite its own
  coverage profile. Only review catches an adversarial test.

## Evidence

* `internal/compat`, using the
  [`contract-break` fixture](../../benchmarks/fixtures/contract-break/README.md):
  * a compatible change (all 5 links compatible);
  * a breaking change that every repository's own verification passes
    (`TestBreakMissedBySingleRepositoryTests`);
  * untested cases: not exercised, missing dependency, not regenerated,
    outside the task, Python side;
  * an unrelated change;
  * resume after a kill;
  * stale evidence;
  * three bypass attempts.
* OpenAPI: the knock-out marks a change compatible, a test that ignores the
  operation untested, and a removed operation that is still served broken.
* `internal/orchestrator/compat_test.go`: the run loop fails the attempt on
  a broken link and retries with the report. A task with an untested link
  ends `tests_green` even with behavioural evidence. With the gate off, the
  same task is `TASK_VERIFIED`.
* The real CLI with the Docker sandbox (`verify --full`, `task status`) on
  the fixture.
