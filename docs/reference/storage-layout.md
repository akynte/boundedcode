# Storage layout

```
# host default: ~/.local/share/boundedcode/
# container default: /data/
  config/
    bcode.yaml            supervisor configuration
    providers.yaml     providers and role routing
    profiles/          profiles you generated (shipped ones are embedded)
    policies/
  models/              GGUF files, or a bind mount to an existing directory
  runtime/             optional pinned Prism source and CUDA llama-server build
    src/               pinned checkout (local edits are refused)
    build/             CMake output (`bin/llama-server` or `llama-server`)
    .install.lock/     transient single-builder lock; not a session process
  workspaces/<workspace_id>/
    workspace.json     the data-side record: name, root, last opened
    index.db           files, nodes, edges, chunks, FTS, embeddings, index keys
    ledger.db          requirements, tasks, operations, checkpoints, evidence, evidence chain, handoffs, leases
    telemetry.db       events, GPU samples
    artifacts/<xx>/<hash>   content-addressed evidence, read-only once written
    cache/analysis/    keyed by workspace and content manifest
    opencode/
      data/             persistent OpenCode XDG data and conversations
      state/            persistent OpenCode state and request budget
      opencode-plugin/  installed context adapter, read-only to the editor
      sessions/<id>/
        home/           ephemeral HOME/config/cache
        control/        broker capability and bcode shim, read-only to editor
        tmp/            ephemeral session temporary files
    slots/             saved prompt-cache slots, cleared on workspace switch
    tmp/               task temporary files, wiped at task end
  backups/<timestamp>/<workspace_id>/{index,ledger,telemetry}.db
  keys/                    verifier signing key (0700; private key 0600) and its public half
  provisioning/<lane>/     §6.1 lane caches: go-mod, go-build, tmp
  telemetry-aggregate.db   §2.2's optional cross-workspace counters
```

## OpenCode session state

A managed `bcode opencode` session keeps durable OpenCode data and state under
`opencode/data` and `opencode/state`, so a later session can continue the same
workspace. It creates a new `opencode/sessions/<id>` directory for HOME,
configuration, cache, temporary files, the broker capability, and the private
`bcode` shim. The session directory is removed on every exit path; the
persistent data/state directories are retained. The control and plugin paths
are readable/executable but not writable by the editor.


Everything above is per workspace except `config/`, `models/`, and these,
and each is outside for a stated reason rather than by omission.

**`keys/`** holds `verifier.ed25519`, the key that signs every record of the
evidence chain, and `verifier.ed25519.pub`, which you can hand to anyone who
needs to check a chain with `bcode task attest --pub`. It is outside every
workspace directory because nothing a task's model or its verification sandbox
is granted may reach it. It is created on first use; losing it means records
signed before cannot be told apart from forgeries, so back it up with the
ledgers.

**`provisioning/<lane>/`** holds what `bcode deps` fetches. A downloaded module is
not workspace state: two projects needing the same version should not download
it twice, and nothing about a fetched dependency identifies the project that
asked for it. A lane never opens any workspace's databases, so this shares no
boundary with §2.2's isolation contract.

**`telemetry-aggregate.db`** is the one exception §2.2 permits to
"no cross-workspace query exists in the code": *"an optional aggregate with
workspace ids only"*, holding *"counters, never content"*. It is the only
database opened outside a workspace directory, it takes no workspace handle,
nothing reads it during a task, and its row shape — a workspace id, a metric
name, a UTC day and two numbers — has nowhere to put content. It is built by
`bcode telemetry aggregate build` rather than written continuously, and deleting
it loses nothing that is not still in each workspace's own `telemetry.db`.

## Why three databases per workspace

A large index rebuild must not block the ledger, and the ledger — the crash
recovery record — should be backed up independently at high frequency.

| File | `synchronous` | Why |
|---|---|---|
| `index.db` | `NORMAL` | Rebuildable from source; speed matters more |
| `ledger.db` | `FULL` | The recovery record; must survive a hard kill |
| `telemetry.db` | `NORMAL` | Counters; a lost sample is not a problem |

All three use WAL mode with a 5-second busy timeout, foreign keys on, and
immediate write transactions so two writers fail fast instead of deadlocking.

## Requirements on the filesystem

SQLite needs a real filesystem with working `fsync`. Not a container overlay
layer, not a network share. `bcode doctor` fails with exit code 2 on either, and
the entrypoint warns before the supervisor starts.

## How isolation is enforced here

- Separate files per workspace; no cross-database query exists in the code.
- `store.OpenWorkspace(id) → *Store` is the only entry point, and
  `workspace.ID` is a distinct type.
- A build-time analyzer confines `sql.Open` and file writes to
  `internal/store` and `internal/artifacts`.
- Every content row carries its `workspace_id`, and a row surfacing from the
  wrong handle is refused.
- Each database records the workspace that created it, so restoring into the
  wrong one fails at open.

## Content addressing

Artifacts are stored under the SHA-256 of their content, with a two-level
directory fan-out, and set read-only once written. Reads verify the hash, so a
corrupted artifact is never served as evidence.

Cache keys mix the workspace id and the indexer version into the digest, so two
workspaces with identical content compute different keys, and an indexer
upgrade invalidates everything.

## Schema versions

Recorded in each database's `meta` table. Migrations are forward-only: a
database from a newer build is refused with a message pointing at the backup
and upgrade path rather than being misread.

## Sizing

The index is roughly proportional to source size — low tens of megabytes for a
100k-line repository. The ledger grows with task history. Artifacts grow with
verification runs but deduplicate by content.

```console
$ bcode doctor --json | jq '.checks[] | select(.name | startswith("index"))'
```
