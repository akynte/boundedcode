# ADR-0006: Persistent MCP session for codebase-memory-mcp

Status: accepted (2026-10-03). Amends ADR-0005.

## Context
ADR-0005 chose one-shot CLI calls and planned to revisit if spawn cost
mattered. Phase 3 measured it: a one-shot call costs about 4 s wall-clock,
and a warm daemon still costs about 1.5 s. A persistent MCP stdio session
costs about 4 s once and then about 12 ms per query. Context planning makes
tens of queries per turn, so the CLI cost would dominate.

## Decision
`cbm.Client.Open` starts `codebase-memory-mcp` in MCP stdio mode and speaks
MCP (`initialize`, `tools/call`) over our existing `internal/jsonrpc` peer.
This adds no SDK dependency. Calls use the session when open and fall back
to the CLI otherwise.

Spawned processes use a private `CBM_CACHE_DIR` with the web UI and file
watchers disabled (see the gap report).

## Consequences
* One long-lived child process per command or task, closed explicitly.
* We implement only the MCP subset we need (`initialize`,
  `notifications/initialized`, `tools/call`). If we need more, the official
  Go SDK (Apache-2.0/MIT) is the candidate.
