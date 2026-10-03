# OpenHands adapter

A thin Python process that connects the boundedcode control plane to the
[OpenHands Software Agent SDK](https://github.com/OpenHands/software-agent-sdk)
(MIT, pinned `1.51.0`). It contains no product logic; see
[ADR-0004](../../docs/architecture/adr/0004-adapter-protocol.md).

* `python/src/bc_openhands/rpc.py` is a symmetric JSON-RPC 2.0 peer over stdio.
* `python/src/bc_openhands/llmproxy.py` is a loopback HTTP proxy. It tunnels
  LiteLLM's OpenAI-compatible calls to Go as `llm.complete`.
* `python/src/bc_openhands/main.py` maps `session.*` methods onto one
  `Conversation`.

## Protocol

| Direction | Method | Kind | Purpose |
|---|---|---|---|
| Go → adapter | `session.open` | request | Start a conversation, or resume `conversation_id` from `persistence_dir` |
| Go → adapter | `session.send` | request | Send a user message and run until the agent stops. Returns status, final message and stuck flag. |
| Go → adapter | `session.condense` | request | Force context condensation |
| Go → adapter | `session.interrupt`, `session.state`, `shutdown` | request | |
| adapter → Go | `llm.complete` | request | Chat completion, metered and forwarded by Go |
| adapter → Go | `ready`, `event` | notification | Startup signal and event summaries |

stdout carries only protocol messages. The adapter re-points fd 1 at stderr
before importing the SDK.

## Development

```bash
cd adapters/openhands/python
uv sync
uv run --group dev pytest -q
go test -run TestAdapterRunAndResume ./internal/agent/openhands/   # real SDK, scripted LLM
```
