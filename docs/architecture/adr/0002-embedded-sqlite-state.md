# ADR-0002: Embedded SQLite for control-plane state

Status: accepted (2026-10-03)

## Context
The control plane needs durable, transactional state: tasks, workspaces,
attempts, verification results, escalations, model calls, audit events and
benchmarks. The tool must stay easy to install on one developer machine.

## Decision
Use SQLite through `modernc.org/sqlite`, a pure-Go driver with no cgo, in
WAL mode with foreign keys and a busy timeout. The database lives at
`$XDG_DATA_HOME/boundedcode/state.db`. Schema changes are append-only
numbered migrations in `internal/store/migrations.go`. We do not use
PostgreSQL.

## Consequences
* The binary stays static and cross-compilable.
* WAL lets a long-running `task run` and concurrent CLI queries coexist.
* Upstream components keep their own stores, isolated from ours:
  codebase-memory-mcp's SQLite cache and the OpenHands persistence
  directory. We store only references to them.

## Evidence
The pure-Go driver passes our store tests, including foreign-key
enforcement and idempotent migrations. Its license is BSD-3-Clause; see the
license matrix.
