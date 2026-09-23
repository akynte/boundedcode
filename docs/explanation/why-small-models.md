# Give the model a smaller engineering problem

BoundedCode is built around the idea that a useful local coding system can
gain capability by moving work into software outside the generator. The target
is an 8 GB GPU; “small” here means a small memory footprint, not necessarily a
small parameter count. The current generator is a compressed 27B-class model.

A repository agent has to locate code, infer a change, edit it, retain state,
and determine what the result supports. Those jobs do not all require another
generation call.

| Problem | Deterministic support | What still needs judgment |
|---|---|---|
| Find likely files | FTS5, typed graph, signatures, paged reads | Which evidence explains the defect |
| Bound the change | Validated plan, exact file grants, policy | What implementation will satisfy the request |
| Remember the investigation | Ledger, transcript, read evidence, state cards | Whether the working hypothesis is right |
| Check the patch | Compiler, tests, static analysis, candidate identity | Whether the checks cover the user's intent |
| Recover from failure | Persisted phases, intent/outcome journal, bounded retries | How to repair the observed failure |

The system keeps the repository outside context and brings selected evidence
into each phase. Context still matters: large diffs, missing relationships and
long reasoning can exceed the budget. Selective retrieval changes the shape of
the problem; it does not abolish the limit.

Review runs in a separate context from editing, which removes the editor
conversation from the review input. It runs on the same model as the editor;
the independence is of context, not of model. Optional Jev adds typed semantic signals
to some decisions; the local path retains deterministic fallbacks.

## Evidence for the argument

The current [Bonsai measurement](../benchmarks/results/2026-09-21-bonsai-runtime.md)
establishes inference on the reference card. Earlier Qwen runs establish that
the task/evidence harness can execute and that prefix reuse is observable.
They do not establish the current stack's task success.

The [graph ablation](../benchmarks/results/2026-09-17-graph-contribution.md)
did not demonstrate a task-quality gain. That result is a useful constraint on
the product story: repository structure is an implemented capability, not a
proven accuracy multiplier.

The next decisive experiment is the same Bonsai model, hardware, tasks and
budgets with and without the supervisor/retrieval components, scored by hidden
acceptance tests. Report solved rate, false acceptance, latency, token use and
whole-workflow memory together.
