# ADR-0004: JSON-RPC over stdio to the OpenHands adapter, with LLM tunnelling

Status: accepted (2026-10-03)

## Context
The OpenHands SDK is Python and the control plane is Go. We needed to pick
an IPC mechanism. The options were JSON-RPC over stdio, a Unix domain
socket, or localhost HTTP. The agent and its tools must run in a sandbox
container (ADR-0003). On the reference machine the container engine is
**Docker Desktop**, which runs containers in a VM, so a Unix socket
bind-mounted from the host does not reach the container.

## Decision
* The adapter is a small Python program that speaks **newline-delimited
  JSON-RPC 2.0 over stdin/stdout**. stderr is used for logs.
* The protocol is symmetric. Go calls `session.*` methods. The adapter calls
  `llm.complete` on Go and sends `event` notifications.
* **LLM tunnelling:** the adapter points LiteLLM at a loopback HTTP proxy
  inside its own process. That proxy turns each chat-completion request into
  an `llm.complete` call over stdio. Go forwards the call to the inference
  runtime and records tokens and timings. The container can therefore run
  with `--network none`.
* The adapter contains no product logic. It contains no retry policy,
  verification, routing or escalation.

## Consequences
* `docker run -i` carries stdio across the Docker Desktop VM boundary, and
  local `exec` works the same way. There are no ports, socket files or
  network exposure.
* Go sees every model call. That covers metering, budgets and audit, and Go
  could later apply routing or caching.
* Streaming responses are not tunnelled. The adapter disables streaming. If
  streaming becomes necessary, `llm.complete` can grow chunk notifications.
* A crash on either side closes the pipe, which is detected immediately.
  Task state survives because it lives in SQLite, Git and the OpenHands
  persistence directory.
