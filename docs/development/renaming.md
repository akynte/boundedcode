# Project name

The project is **BoundedCode**. Machine-facing identifiers use the lowercase
form `boundedcode`: the CLI binary, the Go module path
`github.com/akynte/boundedcode`, the `BOUNDEDCODE_` environment prefix and
the `~/.local/share/boundedcode` data directories.

`internal/buildinfo/buildinfo.go` defines the canonical constants
(`ProductName`, `Name`, `EnvPrefix`, `DataDirName`). New code that needs the
name should import `buildinfo`. Not all existing code does: a rename must
also change the places below, which spell the name out.

| Where | Form |
|---|---|
| Go module path, every import | `github.com/akynte/boundedcode/...` |
| Environment variables read by the CLI | `BOUNDEDCODE_HOME`, `BOUNDEDCODE_LOG`, `BOUNDEDCODE_SERENA_INSTANCE` (`internal/config/paths.go`, `internal/cli/app.go`, `internal/repointel/serena/proc_*.go`); tests also read `BOUNDEDCODE_SERENA` |
| Data, config and cache paths | `boundedcode` under the XDG directories (`internal/config/paths.go`, `internal/model/profile.go`, `internal/sandbox/sandbox.go`) |
| Container image tag | `boundedcode-openhands:local` (`internal/config/config.go`) |
| Git identity and commit messages of agent commits | `boundedcode-agent`, `agent@boundedcode.invalid`, `boundedcode <task>: attempt N` (`internal/gitops`, `internal/orchestrator`) |
| MCP `clientInfo.name`, Serena context file and project description | `boundedcode` (`internal/repointel/cbm`, `internal/repointel/serena`) |
| codebase-memory-mcp cache marker | `.boundedcode-configured` |
| Python packages and their variables | `bc-openhands-adapter`, `bc_openhands`, `bc-serena-env`; `BC_ADAPTER_LOG` and the `BC_*` test variables |
| Scripts, docs, CI and `Makefile` | `boundedcode` paths and binary name |

Search with `git grep -n -i -e boundedcode -e 'BC_' -e 'bc[-_]'` before a
rename.
