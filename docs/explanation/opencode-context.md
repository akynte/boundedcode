# OpenCode context and durable task state

OpenCode 2 is BoundedCode's supported user interface. The Go native task loop
remains in the tree as an implementation and evaluation path; its prompt budgets
are **not** applied to OpenCode model requests. OpenCode owns the live message
window and its compaction. BoundedCode owns task intent, recorded user decisions,
verification, repository indexing and the checkout.

## Measured failure and cause

The installed OpenCode is **v2.0.15**, source tag
[`6f3639d82ed0760091792189b78f8eeb44f699b1`](https://github.com/anomalyco/opencode/tree/v2.0.15).
The local PrismML fork is `9a9394a895b96003ca842a6041cb28ac49a108f7`.
In one recorded OpenCode session (`ses_f2f84cd65ffeexMhj9iGFhGTLm`), five
automatic compactions completed during a short codebase investigation. They
occurred at session message sequences 33, 67, 85, 105 and 129. This is the
reported read/search/compact loop, not a synthetic llama-server test.

The source cause is an incompatible pair of OpenCode defaults for this 32,768
token slot. `packages/core/src/session/compaction.ts` sets `DEFAULT_BUFFER =
20_000` and `DEFAULT_KEEP_TOKENS = 15_000`. With the configured 8,192 output
limit, preflight compaction starts at approximately `32,768 - 20,000 = 12,768`
estimated tokens. Keeping up to 15,000 serialized recent tokens, plus the new
summary, system prompt and tool definitions, can leave the rebuilt request over
the next trigger. The exact amount depends on OpenCode's estimate and provider
usage. The same source's `findTailStart` keeps at least the newest message even
if it alone exceeds the keep allowance.

A separate recorded session (`ses_f2fcfdb02ffeYZ1oDBgwRNGs8a`) reached 32,525
input tokens on its last successful model response. The next automatic summary
request reached 33,764 tokens and the 32,768-token Prism slot rejected it. This
shows that compaction is not a guaranteed recovery when attempted too late.

The project also had duplicate OpenCode provider definitions: a legacy
`provider` entry without limits and a native `providers` entry with limits.
OpenCode's log said it retained the native value on conflict. Setup now removes
the duplicate and keeps the native 32K/8K metadata.

## Current policy and memory boundary

For the local Bonsai model, setup writes `compaction.auto=true`,
`compaction.buffer=12000` and `compaction.keep.tokens=4000`. The nominal
preflight threshold is now 20,768 tokens. The retained tail is smaller than
the threshold by 16,768 tokens, leaving room for the summary and fixed prompt.
OpenCode's estimate is heuristic; a single very large message can still exceed
the physical window.

The project OpenCode plugin invokes `bcode opencode context --session <id>` at
each primary model request. That Go command reconstructs a small card from the
workspace ledger and current checkout. It contains the original supervised
objective, explicit requirements and constraints passed to `bc_task_start`,
recorded user decisions, changed files and last verification result. The
active card limits the decision tail to about 3 KB and the changed-path list
to about 2 KB; it reports omitted counts. `bc_task_history` pages older user
decisions from the ledger by task ID, and `git status` recovers the full changed
path list. Other large task fields, especially an unusually long original
objective or write scope, are not yet subject to a total card limit.
The
OpenCode session ID arrives with MCP calls in
`_meta["ai.opencode/sessionID"]`; `bc_task_start` records the binding.
`bc_task_resume` explicitly binds a new session when several unfinished tasks
exist. When exactly one unfinished task exists, a new session finds it without
the old OpenCode conversation. The hook recomputes the card after compaction and
process restarts. Any OpenCode generated summary from an unsupervised session
is working context, not the authority for these fields.
For a bound supervised task, the plugin also supplies a short deterministic
OpenCode compaction checkpoint. This skips the separate model summary request
and prevents recursive summaries from becoming the task record. OpenCode keeps
its configured 4K recent tail; the next primary request receives a new ledger
card. Current unrecorded reasoning or tool output outside that tail still has
to be recovered from repository evidence.

The card does **not** yet capture every fact encountered in OpenCode's read and
search results. Those results remain in OpenCode's stored history and the
repository can be searched again, but a model may fail to recall that it needs
to do so. The editor supervision path also does not inherit the native task
runner's phase cards. A user can record a consequential clarification with
`bc_task_answer`; the agent should pass explicit acceptance criteria and scope
limits to `bc_task_start` at the beginning. Therefore the current system should
not claim lossless memory or guaranteed absence of semantic drift.

The regular `bcode opencode` path was validated. The confined
`bcode opencode run` path remains blocked: OpenCode 2's private server now
starts after allowing Landlock `bind(0)`, but the context hook cannot reach the
workspace ledger from its deliberately restricted filesystem. A per-workspace
state channel is needed before this path can be called production ready.

## Effective window

The physical model slot is 32,768 tokens. OpenCode's configured output
allowance is 8,192. The 12,000-token compaction buffer starts compaction at
approximately 20,768 estimated input tokens. A request also includes
OpenCode's system prompt, current instructions, `AGENTS.md`, built-in and MCP
tool schemas, recent conversation and tool results, and the BoundedCode task
card. These sizes vary by agent and task. In a newly created OpenCode session
with the adapter, OpenCode reported 7,494 input tokens for the first model
step; this is an **observed full-request total**, not a stable fixed cost.

The model's 262K native positional capacity and Qwen3.8's advertised YaRN
extension to 1M positions are different from this machine's physical slot and
from durable task memory. No 64K, 128K, 262K or 1M production mode is enabled
or validated on this 8 GB GPU.

## Performance and reproduction

Run `scripts/bench-opencode-context.py --prompt '...'` from the project with
OpenCode, BoundedCode and the local Prism server configured. The script drives
`opencode run --standalone` and reads OpenCode's own session database afterward.
It reports input/output tokens, compaction events, elapsed request time,
end-to-end output tokens per second, and sampled GPU/RAM use. It never calls the
model server directly. `--session <id>` analyzes an existing OpenCode session.
For a repeatable compaction check, give a coding prompt that grows the session
through tool reads and add `--min-compactions 1 --reject-immediate-recompaction`.
The check fails if OpenCode compacts twice without an intervening assistant
model step; it does not prove that later compactions cannot recur after more
tool output.

The pre-change session above recorded 24.07 and 25.30 end-to-end output
tokens/s on two 1,000+ token steps, and five compactions. These rates include
request overhead; short tool calls have lower effective rates. They do not
measure pure decoder speed. The post-change run and its limits should be
reported separately rather than claiming an improvement from configuration
alone. A post-change OpenCode coding investigation (`ses_f2f515b09ffe0fVndSwg2wvh7p`)
made eight model steps and one automatic compaction. Its last response generated
868 tokens in 38.291 seconds, or 22.67 end-to-end output tokens/s. The run took
349.606 seconds, sampled 7,134 MiB peak GPU memory in use and 45,743 MiB
minimum available system RAM. It was a single run with a different prompt and
output length from the baseline, so these data show that the adapter works
through OpenCode but do not establish a throughput improvement or that the
compaction loop is impossible.

A second OpenCode run with the deterministic compaction hook
(`ses_f2f411c94ffePsVu1Shm81T95X`) completed one compaction and resumed model
steps, but did not finish its two-sentence coding answer within 480 seconds.
The checkpoint was the short BoundedCode handoff and recorded no model token
usage. The 981-, 1,307- and 627-token steps measured 25.55, 25.65 and 25.18
end-to-end output tokens/s respectively; many shorter tool-call steps were
slower. This run demonstrates that the deterministic checkpoint works and
retains the normal decode rate on longer steps. It also shows that eliminating
summary inference does not by itself make the local agent finish reliably.

The local Bonsai artifact is 5.95 GB. Its base has 64 blocks with 48 Gated
DeltaNet and 16 full-attention blocks. The Prism fork has DSpark speculative
decoding and K-cache mean-centering support, but no draft model or calibrated
Q4 cache has been validated through OpenCode on this machine. They remain off.
Increasing physical context, CPU KV placement, uncalibrated Q4 KV, speculative
drafting and academic KV eviction were not adopted without task-quality,
throughput and memory measurements through OpenCode. Prompt position support
alone is insufficient evidence of usable throughput or exact code recall.

References: [OpenCode 2.0.15 compaction source](https://github.com/anomalyco/opencode/blob/v2.0.15/packages/core/src/session/compaction.ts),
[OpenCode compaction documentation](https://opencode.ai/v2/docs/compaction),
[Linux Landlock port-zero semantics](https://www.kernel.org/doc/html/v6.12/userspace-api/landlock.html),
[Bonsai model card](https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf),
[Qwen3.8-27B model card](https://huggingface.co/Qwen/Qwen3.8-27B).
