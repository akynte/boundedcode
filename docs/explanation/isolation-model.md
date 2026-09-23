# Isolation model

There are two separate concerns: keeping workspace state separate, and confining
executed repository code. Neither replaces the other.

## Workspace state

`internal/store` opens a workspace-scoped handle. Separate SQLite files live
under `$BC_DATA/workspaces/<workspace-id>/`; row stamping, cache keys and
retrieval provenance carry that identity. A database restored under the wrong
workspace identity is refused. `retrieval.Guard` rejects foreign or incomplete
slices before packet construction.

The `storescope` analyzer checks persistence boundaries, with explicit source
exemptions. `internal/store/isolation_test.go` exercises overlapping repositories,
cache separation and moved databases; `make isolation` also deliberately breaks
the cache binding in a temporary copy to establish that the tests detect it.

Workspace-switch cleanup removes local slot files. It does **not** issue
llama.cpp slot erase/save/restore requests or prove that a shared external
server's in-memory prompt cache has been purged. Do not share one unauthenticated
inference server across mutually untrusted users.

See [storage layout](../reference/storage-layout.md) for files and backup scope.

## Executed code

| Layer | Protection | Important limit |
|---|---|---|
| Native file-tool checks | Worktree path resolution, sensitive paths, exact plan write grants | Application checks in the supervisor, not a separate OS sandbox |
| Landlock | Inherited filesystem restrictions and available TCP rules on child commands | Kernel ABI dependent; no PID isolation; incomplete network-protocol coverage |
| bubblewrap plus Landlock | Additional mount/PID namespace isolation | Depends on usable user namespaces and local security policy |
| Outer Docker container | Bounds exposure to selected mounts and container privileges | Absent on a host install; shared mounts are still exposed |
| Final candidate checks | Detect out-of-scope diffs and changed evidence identity | Detection is not prevention of side effects during command execution |

`sandbox.mode: auto` selects an available implementation, trying namespace
confinement and Landlock before the container fallback. `none` selects the
container-only fallback; it is not permission for unrestricted host execution.
The runner refuses its container fallback when no container boundary is detected.
Use `bcode doctor` and actual sandbox tests to inspect what your machine can run.

The shipped container uses an unprivileged user, drops capabilities, enables
no-new-privileges and does not mount the Docker socket. Do not add that socket or
credential directories to make a task work. A trusted supervisor may need access
to several workspaces; those mounts alone do not isolate tasks from each other.

## Network limitations

Landlock rules restrict supported TCP operations by port. They are not host,
HTTP-route or credential policies. Older ABIs lack some restrictions; MPTCP and
other protocols require an independent network boundary. The test-port allowance
may also permit access to unrelated ephemeral-port services.

The optional allowlisting proxy separates operator dependency/documentation
provisioning lanes. Native task specs do not grant the proxy ports. Exact/wildcard
host rules restrict proxy destinations, but a disabled proxy does not remove the
host or container's other routes.

BoundedCode has no offline switch, and configuration checks were never a
firewall in any case. The decision plane is a required network dependency, so a
strictly network-isolated experiment is not a supported configuration of this
system: it would be BoundedCode with a required component removed. If you need
one anyway, provision dependencies first, then enforce isolation outside
BoundedCode — and expect task runs to refuse to start.

Do not disable host AppArmor/seccomp protections just to obtain an extra sandbox
layer without evaluating the tradeoff. See [trust boundaries](trust-boundaries.md)
.
