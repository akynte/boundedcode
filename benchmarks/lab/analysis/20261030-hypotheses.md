# Hypotheses

9 hypotheses from recurring or failure-associated patterns, strongest evidence first. Each names its candidate change, the simpler alternative it must beat and the metric it is decided on.

## verify-examples (weak_verification)

Before a change is reported done, ask for the request's own examples (inputs and expected outputs it states) to be run as tests. This improves success_rate (higher is better) on held-out tasks without a regression in any language.

- Change: code (write a patch and test it as an arm): internal/direct: after the last change of a turn, when the request states examples and no test exercised them, add a note asking for them to be run. Post-change checks themselves are verification and not tunable.
- Evidence: weak_verification in 117 of 1108 eligible runs (15 tasks; false-confidence ×87, false-pass ×16, unverified-change ×14); fails 21% with, 10% without; 0 calls wasted; explains 103 failed runs
- Tasks: add-balance-test, cross-service-idempotency, feature-currency, fix-balance-query, fix-ledger-balance, go-debug-ledger-balance-query, large-change-txtar, multi-feature-discount, poly-rename-qty, py-fix-inventory, sec-injection-comment, session-ledger, session-noisy-tests, session-resume-constraints, session-x-tools
- Languages: go, python, typescript

## observation-budget (context_excess)

Prune command output to 4 KB instead of the local default of 8 KB. This improves tokens_per_success (lower is better) on held-out tasks without a regression in any language, and by more than the simpler max-tool-output-8k.

- Change: configuration: direct.observation_bytes=4096
- Evidence: context_excess in 26 of 1351 eligible runs (3 tasks; large-prompt ×23, re-read ×9); fails 19% with, 7% without; 183 calls wasted
- Tasks: add-balance-test, large-change-txtar, session-x-tools
- Languages: go
- Simpler alternative `max-tool-output-8k` (direct.max_tool_output=8192): a plain cut of every tool result at 8 KB (the default is 16 KB), without pruning by relevance

## working-set-24k (context_excess)

Lower the history budget to 24K tokens: smaller prompts, more compaction. This improves tokens_per_success (lower is better) on held-out tasks without a regression in any language.

- Change: configuration: direct.working_set_tokens=24000
- Evidence: context_excess in 26 of 1351 eligible runs (3 tasks; large-prompt ×23, re-read ×9); fails 19% with, 7% without; 183 calls wasted
- Tasks: add-balance-test, large-change-txtar, session-x-tools
- Languages: go

## rerun-note (unnecessary_retries)

Note when a build or test is run again with nothing changed since its last run. This improves model_calls_per_run (lower is better) on held-out tasks without a regression in any language.

- Change: code (write a patch and test it as an arm): internal/direct: remember the last result of each verification command and the change counter; on an unchanged rerun, say so.
- Evidence: unnecessary_retries in 21 of 1523 eligible runs (6 tasks; attempts-failed ×17, rerun-unchanged ×4); fails 50% with, 8% without; 27 calls wasted; explains 17 failed runs
- Tasks: multi-feature-discount, session-resume-constraints, session-x-tools
- Languages: go, typescript

## fewer-attempts (unnecessary_retries)

Give a bounded task 3 attempts instead of the configured number. This improves tokens_per_success (lower is better) on held-out tasks without a regression in any language.

- Change: configuration: budgets.max_attempts=3
- Evidence: unnecessary_retries in 21 of 1523 eligible runs (6 tasks; attempts-failed ×17, rerun-unchanged ×4); fails 50% with, 8% without; 27 calls wasted; explains 17 failed runs
- Tasks: cross-service-idempotency, go-debug-ledger-balance-query, go-large-iface-method
- Languages: go

## working-set-48k (ineffective_compaction)

Raise the history budget to 48K tokens so long sessions compact less often. This improves success_rate (higher is better) on held-out tasks without a regression in any language, and by more than the simpler legacy-compaction.

- Change: configuration: direct.working_set_tokens=48000
- Evidence: ineffective_compaction in 7 of 1351 eligible runs (1 tasks; re-read-after ×41); fails 43% with, 7% without; 84 calls wasted
- Tasks: session-x-tools
- Languages: go
- Simpler alternative `legacy-compaction` (direct.compaction=legacy): the older summary-only compaction, with no session state

## observation-masking (ineffective_compaction)

Replace old tool results with a placeholder instead of summarising the history when it fills. This improves tokens_per_success (lower is better) on held-out tasks without a regression in any language, and by more than the simpler legacy-compaction.

- Change: code (write a patch and test it as an arm): internal/direct: in the working set, mask tool results older than the last N turns with a fixed placeholder before compacting.
- Evidence: ineffective_compaction in 7 of 1351 eligible runs (1 tasks; re-read-after ×41); fails 43% with, 7% without; 84 calls wasted
- Tasks: session-x-tools
- Languages: go
- Simpler alternative `legacy-compaction` (direct.compaction=legacy): the older summary-only compaction

## tool-hints (tool_selection)

Answer a misused call (a directory read, a missing file) with the call that would work. This improves model_calls_per_run (lower is better) on held-out tasks without a regression in any language.

- Change: code (write a patch and test it as an arm): internal/direct: in the read tool, a directory gets its listing and a missing file the nearest existing paths.
- Evidence: tool_selection in 38 of 1351 eligible runs (10 tasks; shell-for-tool ×1, tool-misuse ×37); fails 5% with, 7% without; 84 calls wasted
- Tasks: feature-currency, large-change-txtar, locate-topic-name, multi-feature-discount, multi-locate-mapping, rust-explain-ledger, sec-readme-secrets, session-ledger, session-x-tools, ts-locate-discount
- Languages: go, python, rust, typescript

## loop-guard (repeated_calls)

Refuse a call identical to two earlier ones with no change between, and say why, instead of running it again. This improves p90_seconds (lower is better) on held-out tasks without a regression in any language, and by more than the simpler max-turns-40.

- Change: code (write a patch and test it as an arm): internal/direct: count identical calls since the last change; refuse the third with a message naming the earlier results.
- Evidence: repeated_calls in 19 of 1351 eligible runs (2 tasks; identical-call ×20); fails 16% with, 7% without; 199 calls wasted
- Tasks: session-stale-config, session-x-tools
- Languages: go
- Simpler alternative `max-turns-40` (direct.max_turns=40): a lower turn limit bounds what a loop can waste, without any new code
