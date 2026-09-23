# Start, stop and update

## Native reference workflow

Start the pinned Bonsai server as shown in [Install](install.md). It is an
operator-owned external process. Then run CLI tasks directly; `bcode api` is not
required for `bcode index` or `bcode task run`.

For the optional status service:

```bash
bcode api
```

In another terminal:

```bash
curl -fsS http://127.0.0.1:7777/healthz
curl -fsS http://127.0.0.1:7777/readyz
```

`healthz` reports service liveness; `readyz` reports essential-child readiness.
Neither proves that an independently running CLI task has completed, nor that
the reference model passes a coding evaluation.

With `inference.mode: embedded`, the API's process manager starts and monitors
the configured server. With `external`, it does not own or stop the server.

## Stop and recover

Interrupt the foreground CLI/service cleanly. A container receives SIGTERM
through `docker stop`; its grace period should exceed
`api.shutdown_grace_seconds` (30 by default). The shipped Compose configuration
uses 40 seconds.

Managed children are stopped through the process manager. Independently launched
CLI tasks and external inference servers have their own lifecycles; stopping the
status API is not a machine-wide task/GPU shutdown.

After a crash or interruption, inspect persisted state:

```bash
bcode task list
bcode task recover
bcode task show TASK_ID
```

Recovery inspects uncertain operations and stale evidence rather than assuming a
side effect succeeded. A hard kill is not a promise of safe external side effects
or uncorrupted repository logic. See [recovery](../explanation/crash-recovery.md).

## Update deliberately

Back up workspace state before upgrading; schemas migrate forward:

```bash
bcode backup --all
```

Stop active tasks, inspect the release/change log, update source and rebuild with
`make build`. Keep the Bonsai runtime revision, artifact checksum and profile
recorded separately from the BoundedCode version.

For a container deployment, review image provenance and recreate the container
with the same data/repository mounts and network restrictions. Do not copy a
`latest`-tag command and assume it reproduces the measured Bonsai installation.

Downgrades across database schema versions are unsupported. Restore a compatible
backup or upgrade the binary. See [backups](backup-and-restore.md).
