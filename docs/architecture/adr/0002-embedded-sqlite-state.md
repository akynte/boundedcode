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

## Concurrency and recovery (added 2026-10-04)
* Transactions are `BEGIN IMMEDIATE` (`_txlock=immediate`), so concurrent
  writers queue on the 10 s busy timeout instead of failing on a lock
  upgrade. Migrations re-check their version under that lock, so several
  processes can open a fresh database at once (`TestConcurrentFirstOpen`).
* One process runs a task at a time: `task run` takes a lease (owner and
  heartbeat columns, renewed every 15 s). A lease not renewed for 2 minutes
  belongs to a dead process and is taken over; the new runner reconciles
  attempts the dead one left active (`TestSIGKILLResume`, a real SIGKILL).
* `Ledger.Save` never overwrites a cancellation recorded by another
  process; the runner sees it within one heartbeat and stops.
* `synchronous=NORMAL` in WAL mode can lose the last transactions on power
  loss, never corrupt the file. Lost writes are bounded by persist-then-act:
  the next run resumes from the last recorded state and git.
* Corruption: SQLite reports it on open or query. Recovery is restoring a
  copy (`sqlite3 state.db ".backup copy.db"` or `VACUUM INTO` while the CLI
  is idle) or, as a last resort, starting a fresh database: code changes
  live in git on `agent/<task-id>` branches and are not lost with it.

## Evidence
The pure-Go driver passes our store tests, including foreign-key
enforcement and idempotent migrations. Its license is BSD-3-Clause; see the
license matrix.
