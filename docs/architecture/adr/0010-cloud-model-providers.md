# ADR-0010: Cloud model providers as an alternative to the local model

Status: accepted (2026-10-07). Amends ADR-0001 (local models for most token
volume) by making the local model a choice, not a requirement.

## Context
BoundedCode was built and validated on one machine (RTX 4060 8 GB, 64 GB RAM)
with one local model (Qwen3.6-35B-A3B, ADR-0007). Many users' machines cannot
run that model, and some prefer a hosted model. The maintainer asked for
OpenAI-compatible APIs, Anthropic's Messages API and Google's Gemini API as
alternatives to the local model, configurable without editing files
([multiplatform-plan.md](../../development/multiplatform-plan.md)).

Every agent model call already leaves the sandbox the same way: the
OpenHands adapter sends OpenAI chat-completions requests over its stdio
JSON-RPC tunnel to the host-side gateway (ADR-0004), which forwards them and
meters usage. The sandbox has no network.

Anthropic and Gemini do not speak the OpenAI format, and both attach
provider-specific state to a model turn that must be sent back unchanged:
Anthropic's thinking blocks (with signatures bound to the conversation) and
Gemini's thought signatures (a missing one ends a turn with
`MISSING_THOUGHT_SIGNATURE`). The agent keeps its history in the OpenAI
format, which has no place for either.

## Decision
1. **One provider per configuration, local by default.**
   `inference.provider` is `local` (llama.cpp, unchanged behaviour) or one of
   `openai`, `anthropic`, `gemini`, `openai-compatible`. Each cloud provider
   keeps its own settings under `inference.providers.<name>` (model, base URL,
   context window and limit, effort, optional prices), so switching keeps them.
2. **Keys never leave the host process that uses them.** API keys are stored
   in the OS credential store (Secret Service, macOS Keychain, Windows
   Credential Manager) through `github.com/zalando/go-keyring`, with an
   owner-only `credentials.json` fallback, or given explicitly as
   `BOUNDEDCODE_<PROVIDER>_API_KEY`. Vendor variables such as
   `ANTHROPIC_API_KEY` are not read, and the Anthropic SDK's environment
   autoload is disabled: a key is used only when it was given to BoundedCode.
   Keys are never written to `config.yaml`, never passed as command-line
   arguments (`provider key set` reads stdin without echo), and every key read
   is registered for exact-value redaction. Only the gateway holds a key; the
   sandbox keeps `--network none` and sees only the tunnel.
3. **Translation in the gateway.** An `inference.Upstream` per provider takes
   the OpenAI request and returns an OpenAI response:
   * OpenAI and compatible services: passed through with a bearer key;
     llama.cpp-only fields are removed, and a parameter the provider rejects
     as unsupported (OpenAI's reasoning models reject `temperature` and
     `max_tokens`) is dropped or renamed once and stays dropped.
   * Anthropic: through the official Go SDK (`anthropic-sdk-go`, MIT), with
     the request body built from the OpenAI history. Adaptive thinking is
     enabled where the Models API reports it; sampling parameters are not
     sent; forced tool choice becomes `auto`; automatic prompt caching is on;
     server-side refusal fallbacks (`fallbacks: "default"`) are requested for
     the models that accept them on the default endpoint.
   * Gemini: `generateContent` (v1beta REST) with the key in the
     `x-goog-api-key` header; tool schemas go to `parametersJsonSchema`.
4. **Native turns are replayed verbatim.** Each provider-native assistant turn
   is stored on the host, keyed by its tool-call ids (or its text), in the
   task directory (`provider-replay.jsonl`, owner-only), and substituted back
   when that turn returns in the history. The agent's history condensation
   rewrites earlier turns, which invalidates Anthropic thinking blocks that
   follow; requests therefore ask the API to drop such blocks instead of
   failing (`thinking.block_binding.prefix_mismatch_behavior: "drop_block"`,
   beta `thinking-binding-controls-2026-08-01`), and a 400 that names a stale
   block is retried once without thinking blocks.
5. **Context and cost.** The working context is the model's window (from the
   provider, or configured where the API does not report it) capped by
   `context_limit` (default 200,000), so a 1M-token window is not refilled on
   every turn. Every model call records its provider; `stats` reports usage
   per provider and an estimated cost when the user has entered prices. No
   prices are built in.
6. **Failures.** A rejected key (401/403) blocks the task with the fix and
   does not count as an attempt. Rate limits and outages are retried with
   backoff in the gateway (Retry-After honoured) and then by the runner,
   without burning attempts.
7. **The frontier is unchanged.** Frontier escalation (ADR-0009) still uses
   the Codex subscription sign-in and still refuses API keys; it is a separate
   path from the agent's provider.

## Consequences
* The agent can run on machines without a GPU, and on any of the providers.
  The agent SDK sees a neutral model alias (`boundedcode-cloud`) so it does
  not switch on provider-specific parameters by model name; the gateway sets
  the real model.
* Cloud use costs money per token. It happens only after the user selects a
  provider and stores a key; the interface shows the provider and model.
* A turn that cannot be found in the replay store (for example after the
  store file is removed) is rebuilt from the OpenAI history without thinking
  blocks or signatures; for Gemini this can end that turn with
  `MISSING_THOUGHT_SIGNATURE`, which is reported to the agent as text.
* None of this is validated on real tasks yet: the translations are tested
  against recorded request and response shapes and fake servers. Live calls
  need keys the maintainer provides.

## Evidence
* `internal/inference/providers_test.go`: request and response translation,
  key handling (the environment key is ignored), replay of thinking blocks and
  thought signatures, retries, parameter adaptation, the stale-thinking
  recovery.
* `internal/secrets/secrets_test.go`, `internal/cli/provider_test.go`
  (end-to-end `provider` commands against a fake Anthropic server; the key is
  in no file or output), `internal/orchestrator/crash_test.go`
  (`TestCloudFailures`), `internal/stats/stats_test.go` (`TestProviders`).
* API shapes: the Anthropic API reference bundled with the `claude-api` skill
  (2026-09-25 cache) and the Gemini API discovery document (revision
  20261006).
