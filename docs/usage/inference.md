# Inference protocol

BoundedCode talks to every model through one gateway on the host. The agent
runtime and the host's own callers send OpenAI chat-completions requests;
each provider's upstream translates them and meters the answer. This page
is the reference for what the gateway does with a request and an answer,
per provider, and which settings change it. The measurements are in
[the inference benchmark](../benchmarks/inference-2026-10.md).

## Capabilities

The gateway does not assume that every model supports every feature. Each
upstream reports what its model supports, and callers ask before they use
a feature.

| Provider | Where the capabilities come from | Streaming | Reasoning back to the model | Structured output | Prompt progress |
|---|---|---|---|---|---|
| Local llama.cpp | Read once from the server's `GET /props`: the chat template and its capability flags | yes | where the template renders `reasoning_content` | yes (`response_format`, a grammar) | yes |
| Another local OpenAI-compatible server (`external_url` without `/props`) | Assumed | yes | no (`reasoning_history: keep` turns it on) | yes; dropped if the server rejects it | no |
| OpenAI | The API's documentation | yes | no | yes (`response_format`, strict) | no |
| OpenAI-compatible | The API's documentation | yes | no (`keep` turns it on) | yes; dropped if the service rejects it | no |
| Anthropic | The API's documentation | yes | thinking blocks, replayed verbatim | yes (`output_config.format`); dropped if the model rejects it | no |
| Gemini | The API's documentation | yes | thought signatures, replayed verbatim | yes (`responseJsonSchema`); dropped if the model rejects it | no |

## Reasoning in the history

A reasoning model writes its reasoning before each answer. What happens to
that reasoning when the conversation goes back to the model on the next
call depends on the provider:

- **Anthropic and Gemini** require their thinking blocks and thought
  signatures back, verbatim. The gateway keeps them on the host, keyed by
  the turn's tool-call ids (or its text), and puts them back into each
  request. The agent's history never holds them.
- **Local llama.cpp.** Templates such as Qwen3.6's render each earlier
  assistant turn as `<think>reasoning</think>` followed by the answer. When
  the history arrives without the reasoning, the template renders an empty
  reasoning block where the model had reasoned. Two things then go wrong:
  - the model reads a history it did not write;
  - the server's cache holds the tokens the model generated, reasoning
    included, so the prompt stops matching the cache at that turn, and every
    call processes the previous answer again.

  With `inference.reasoning_history: auto` (the default) the reasoning is
  sent back to a local model whose template reads `reasoning_content`:
  - direct mode sends its own copy, which the session file keeps;
  - in bounded mode the OpenHands agent drops it for models it does not
    know, so the gateway restores it from a per-task file
    (`local-replay.jsonl` in the task directory), and a resumed run keeps it.
- **OpenAI and OpenAI-compatible services** never get it back unless
  `reasoning_history: keep` says so. Some services ask for
  `reasoning_content` in tool-call turns; their documentation says so.

Reasoning is never sent to a provider other than the one that wrote it: an
upstream that does not take it removes the field, and user and tool
messages never carry it.

How much earlier reasoning a llama.cpp template keeps is the server's
choice. `--reasoning-preserve`, the default where the template supports
it, keeps every turn's. With `server.reasoning_preserve: false` in the
model profile, only the current turn keeps its reasoning. The prompt then
grows less, but the server processes the previous turn again after every
new user message, because its rendering changes. Direct mode counts the
reasoning it sends toward the working set (`direct.working_set_tokens`).

## Structured output

The calls whose JSON the control plane parses send the reply's JSON schema
with the request (`task.structured_output`, on by default):
- the task contract;
- the check of its ambiguities against the request;
- the counterexample tests of verification.

| Provider | Sent as |
|---|---|
| llama.cpp | `response_format` with `json_schema`; the server compiles it into a grammar, so the reply is valid JSON of that shape |
| OpenAI, OpenAI-compatible | `response_format` with `json_schema` and `strict: true` |
| Anthropic | `output_config.format` |
| Gemini | `generationConfig.responseMimeType: application/json` and `responseJsonSchema` |

The schema lists the fields in the order the prompt does, and llama.cpp
writes them in that order. The prompt still describes the JSON, so a
provider that rejects the schema answers without it: the request is sent
again once without the schema. The main coding loop keeps native tool
calling.

## Failures the gateway handles

| What happened | What the gateway does |
|---|---|
| A streamed answer ends with no finish reason and no end marker (the connection broke, the server stopped) | The partial answer is not used. The caller is told to discard what it showed, and the request is sent once more; a second break is a 502 |
| An error event inside a stream | An error answer with the event's status, not a 200 |
| llama-server closes a kept-alive connection as the next request arrives | Local requests carry an `Idempotency-Key`, so Go's transport sends them again on a new connection; a refused or reset connection is retried with backoff |
| llama.cpp answers 500 (most often "Failed to parse tool call arguments as JSON": the model wrote a malformed call) | The request is sent again, up to 3 attempts: a new sample usually parses |
| A 200 that is not JSON or has no choices | A 502 naming the problem (`malformed_response`) |
| A tool call without an id, type or arguments | An id and the type are added; empty arguments become `{}` |
| A prompt longer than the context, in any provider's words (llama.cpp's "exceeds the available context size", Anthropic's "prompt is too long", Gemini's "input token count … exceeds the maximum") | The error gets OpenAI's `context_length_exceeded` code, which direct mode (it summarises and retries), LiteLLM and OpenHands (they condense) recognise |
| Cloud: rate limits and overload (429, 5xx, 529) | Retried with backoff, honouring `Retry-After` (unchanged) |

## What each call records

The gateway's call log (benchmark reports) and the ledger's `model_calls`
table record for each call:
- prompt, cached, completion and reasoning tokens;
- time to the first token, the server's prefill and decode times, and the
  whole call's time;
- its status and purpose (agent turn, condenser, compaction, planner).

The call log also has:

| Field | Meaning |
|---|---|
| `evaluated_tokens` | Prompt tokens the server processed (llama.cpp `timings.prompt_n`), not served from its cache |
| `messages`, `changed` | The request's message count, and the index of the first message that differs from the previous request of the same purpose (`-1`: the history only grew). A cache miss after a changed earlier message is caused by the history's rewrite (a result set aside, a summary); a miss without one is the server's |
| `retried` | Re-sent after a broken stream |

While a local server works through a long prompt, the interface shows its
progress ("reading the conversation: 3.1k of 7.8k new tokens"), from
llama.cpp's prompt-progress events.

## llama.cpp server settings

These model-profile keys (`server.`) map to flags of the pinned
llama-server. Unset, they leave the server's default.

| Key | Flag | Server default | Notes |
|---|---|---|---|
| `ctx_checkpoints` | `--ctx-checkpoints` | 32 | Context checkpoints per slot. A model with recurrent layers (Qwen3.6) can only reuse a cached prefix up to a checkpoint |
| `checkpoint_min_step` | `--checkpoint-min-step` | 8192 | Least spacing of checkpoints, in tokens; `0` = no minimum |
| `cache_ram_mib` | `--cache-ram` | 8192 | Host-memory cache of other conversations' states; `0` disables, `-1` no limit |
| `reasoning_preserve` | `--reasoning-preserve` / `--no-reasoning-preserve` | on where the template supports it | See [Reasoning in the history](#reasoning-in-the-history) |
| `op_offload_min_batch` | `GGML_OP_OFFLOAD_MIN_BATCH` (CUDA backend) | 32 | Smallest batch for which weights kept in system RAM (the MoE experts) are computed on the GPU |
| `cache_reuse` | `--cache-reuse` | 0 | Reuse of shifted chunks of the cache. llama.cpp turns it off where the model's cache cannot be shifted |
