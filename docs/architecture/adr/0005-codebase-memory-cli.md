# ADR-0005: Drive codebase-memory-mcp through its CLI mode

Status: accepted (2026-10-03)

## Context
codebase-memory-mcp exposes its tools over MCP (stdio). It also has a
one-shot CLI mode, `codebase-memory-mcp cli <tool> --flags --format json`,
which prints results only on stdout. Using MCP from Go would add an SDK
dependency: the official `modelcontextprotocol/go-sdk` is Apache-2.0 and
MIT during a relicensing transition, and `mark3labs/mcp-go` is MIT.

## Decision
The control plane invokes the CLI mode for indexing, search, tracing, impact
and ADR operations. It adds no MCP SDK dependency.

The agent may separately receive the same tools over MCP inside its sandbox
through OpenHands' `mcp_config`. That path is evaluated in Phase 3.

## Consequences
* There are no long-lived MCP sessions to manage from Go, and every call is
  a process spawn. A spawn is cheap for a C binary, and the call latency is
  measured in the Phase 3 benchmark.
* If per-call spawn cost proves significant, a persistent MCP client can
  replace the implementation behind `repointel.Intelligence` without
  changing callers.
