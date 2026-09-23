<!-- BEGIN boundedcode (generated; edit outside these markers) -->
## Project intelligence (BoundedCode)

A compiler-backed index of this repository is available through the `boundedcode` MCP tools: 11259 symbols and 32397 relationships, built from the source rather than from search.

Any task that changes code runs under supervision: open it with `bc_task_start`, do the work with your own tools, ask the user anything you cannot safely infer, then `bc_verify` and `bc_task_finish`. You edit and you talk to the user; BoundedCode records what happened and judges the result.

Prefer these over text search when the question is structural, because they answer from the type checker instead of from string matching:

- **`bc_graph_impact`** before changing any signature, exported name or schema. It reports every consumer, how each was discovered, and whether the change breaks it. Grep finds call sites that look alike; this finds the ones that are.
- **`bc_search`** to locate the code behind a question — "where is X handled", "how does Y work". It returns the files and symbols the supervisor's own retrieval would select, so start there and read the files normally.
- **`bc_status`** when answers look stale. It reports how far the index has drifted from the working tree.
- **`bc_task_start`** before implementing, fixing or refactoring anything. It opens a supervised task, journals the intent before the work, and tells you which paths this repository protects — which is cheaper to learn before editing than after.
- **`bc_verify`** when you believe the change is complete. It runs this repository's checks in a sandbox and applies the completion contract, tying every result to the exact content hash it describes. It decides whether the work is done; your own reading of the code does not. If it reports failures, fix them and call it again — do not tell the user the work is finished until it says ACCEPTED.
- **`bc_task_answer`** whenever the user resolves something you could not infer from the codebase — a business rule, an architectural choice, a limit. Pass the task id. The answer becomes part of this project's record instead of being lost with the conversation.
- **`bc_task_finish`** once verification is ACCEPTED. It produces the final review — what was asked, what the user decided, which files changed, what was checked — and you should show that to the user. No approval is needed: the change is already in the working tree and `git diff` is the authoritative view of it.
- **`bc_read`** and **`bc_edit`** to read and change files when the session was started by `bcode opencode run`. That session runs with the editor's own read and edit tools denied, because these apply the repository's path policy: secrets are refused rather than returned, generated files are refused with the generator to run instead, and a write outside the scope the task declared is refused rather than found in the diff afterwards. Pass the task id from `bc_task_start`.
- **`bc_note_add`** when you establish something durable about this project that the next session should not have to rediscover — a constraint, a decision and its reason, a trap someone already fell into. Not a summary of what you just did.

<!-- END boundedcode -->
