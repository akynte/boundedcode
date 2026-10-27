# Direct-mode command sandbox

Direct mode runs commands in your checkout on your machine. These include
the model's shell commands, background shells, the `` !`cmd` `` commands
in command templates, and the syntax and build checks after each edit.
The [security policy](security-policy.md) decides which operations need
your authorization. The sandbox limits what a command can reach once it
runs, whoever wrote it and whatever language it is in. That covers a test
suite, a build script, a `postinstall` hook, or a script that was already
in the repository.

Check what you get on this machine with `bcode doctor`. Its
"direct-mode sandbox" line reports, resource by resource, what is
enforced. Each session also reports it before its first command.

## What is contained, by platform

| | Linux with bubblewrap | Linux with Landlock only | macOS, Windows, other |
|---|---|---|---|
| Level | **strong** | partial | none |
| Reading files | system read-only; home directories, `/run`, `/mnt`, `/media` and other users hidden; toolchains read-only; package caches and the project writable | home directory, `/run` and other users unreadable except toolchains and caches | not confined |
| Secret files in the project (`.env`, keys, …) | masked | **not** masked | not masked |
| Writing files | only the project, a private `/tmp` and package caches | only the project, `/tmp` and caches | not confined |
| `.git/hooks`, `.git/config`, `.git/info/attributes` | read-only (`git add`, `commit`, `checkout` work) | **not** protected | not protected |
| Network | none; allowed hosts only through BoundedCode's proxy | **not** confined | not confined |
| Processes | own pid namespace: everything ends with the command | process group; signals outside it blocked | process group (Unix); job object (Windows) |
| Unix sockets (Docker, SSH agent, D-Bus) | unreachable | **reachable** | reachable |
| Credentials in the environment | removed | removed | removed (unless `mode: off`) |

Secret-looking environment variables (`*TOKEN*`, `*SECRET*`, `*API_KEY*`,
`*PASSWORD*`, `AWS_*` credential settings, `SSH_AUTH_SOCK`, …) are removed
on every platform, unless `direct.sandbox.mode` is `off`. `direct.sandbox.env` passes named ones through, for
example `GH_TOKEN` for `gh`. Your git name and e-mail are passed as
`GIT_AUTHOR_*` and `GIT_COMMITTER_*`, so commits work while your git
configuration stays hidden.

## Weaker containment needs your authorization

When the containment here is weaker than strong, commands beyond
read-only file utilities need your go-ahead. Read-only utilities are
`ls`, `cat`, `grep`, `find` without `-exec`, `cp`, `mv`, and the like.
You are asked once per session. `direct.sandbox.fallback` decides:

| Value | What happens |
|---|---|
| `ask` (default) | Asked once per session, with the capability report. With no one to ask (`bcode chat -p` in a script), commands are refused. |
| `refuse` | Commands are refused; read-only utilities still run. |
| `allow` | Commands run; each session's decision is in the audit log. |

A weaker setting you choose in the configuration is that authorization.
This covers `direct.sandbox.mode: off`, `direct.sandbox.network: host`,
and forcing `backend: landlock` or `none`. Nothing in a repository can
change these settings: a workspace or project file cannot set `direct`.

For untrusted code on macOS or Windows, use bounded mode (`/task`). It
runs in a container with no network.

## Network

In the default `proxy` mode a command has no network interface except
loopback. The proxy listens on a Unix socket bound into the sandbox, and
a bridge inside forwards `127.0.0.1:3128` to it. `HTTP(S)_PROXY`, npm,
yarn, pip and Java proxy settings point at the bridge. A program that
ignores those settings, opens a raw socket, sends UDP or resolves names
itself has no network at all.

The proxy admits only:
- the hosts in `direct.sandbox.allow_hosts`;
- with `direct.sandbox.registries` (default on), the package registries
  of the project's ecosystems and of the package managers the command
  runs. Examples: `proxy.golang.org`, `registry.npmjs.org`, `pypi.org`,
  `crates.io`, Maven Central, `api.nuget.org`, `rubygems.org`. So
  `go mod download`, `npm ci` and `pip install -r requirements.txt` work;
- hosts that the command itself names and that you authorized, by your
  words, a `network(host)` rule or your approval
  ([security policy](security-policy.md#authorization)).

It never connects to loopback, private, link-local (including cloud
metadata at `169.254.169.254`) or carrier-grade NAT addresses, whatever
a name resolves to.

Each refused connection is recorded (`direct.sandbox` in the audit log).
It is also named to the model after the command, so the model can ask
you instead of retrying.

`network: none` refuses every host. `network: host` gives commands your
network unconfined (partial containment).

## Lifetimes

A command's processes end when it ends, when it times out, or when you
cancel it. This includes processes it puts in the background with `&`,
`nohup` or `setsid`. Use `bash` with `run_in_background` for servers
that must keep running. Those shells run in their own sandbox, end with
`kill_shell`, and end with the session.

## Mechanisms evaluated

| Mechanism | Used | Why |
|---|---|---|
| bubblewrap (namespaces) | yes, Linux strong | Mount, pid, network, ipc and uts namespaces from an unprivileged, widely packaged tool. On Ubuntu 23.10+ AppArmor blocks unprivileged user namespaces for ordinary programs but permits `bwrap` through its own profile, so `unshare` fails where `bwrap` works. Verified on Ubuntu 26.04, kernel 7.0, bwrap 0.11. |
| Landlock | yes, Linux fallback | Unprivileged and available without namespaces, but path rules only add access, so a secret cannot be hidden beneath an allowed directory. Pathname Unix sockets and other processes' `/proc` entries are not mediated, and TCP is filtered by port only. Hence "partial". Verified with ABI 8. |
| seccomp | no | Inside the namespaces, bwrap already gives no capabilities (Ubuntu's `unpriv_bwrap` profile denies them) and `--new-session` stops terminal injection. A syscall filter would add maintenance for little additional containment here. |
| macOS `sandbox-exec` (Seatbelt) | no | Documented as deprecated, and it cannot be verified on the machines this project is tested on. No protection is claimed. |
| Windows AppContainer, Windows Sandbox | no | Not implemented or verified yet. Windows gets job objects for process trees only. |
| Containers for direct mode | no | Bounded mode already runs untrusted work in a container. For direct mode, container start-up per command and the gap between host and container toolchains would cost the everyday speed this mode exists for. |

## What runs outside the sandbox

- Hooks: your own configuration. They still end with their process tree.
- MCP servers: yours, or project servers you trusted
  ([security policy](security-policy.md#mcp-servers)).
- Serena and codebase-memory language servers. Some language servers
  execute project code; rust-analyzer, for example, runs build scripts
  and procedural macros.
- BoundedCode's own `git status` and `git log`. These run with
  `core.fsmonitor`, hooks and external diff disabled, so a repository's
  configuration cannot make them run a program.

## Residual risks

- Strong containment still lets a command:
  - read the system (`/etc`, `/usr`) and the project;
  - write the project and the package caches. A command could plant a
    malicious package in a shared cache, which later builds outside the
    sandbox would use.
- Allowed registries can be used to send data: in a URL to the registry's
  operator, or by publishing with credentials found in the project.
  Credentials outside the project are hidden.
- A repository whose `.git/config` includes a file from the working tree
  (`include.path`) lets a command change git configuration through that
  file.
- Submodules' `.git/modules/*/hooks` are not made read-only.
- Secret files are found when the session's first command runs. A secret
  file created later is not masked; it was created inside the sandbox.
- Landlock and "none" leave the network, Unix sockets and project secrets
  open. That is why they ask first.
