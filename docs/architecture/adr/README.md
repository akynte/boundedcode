# Architecture Decision Records

| ADR | Title | Status |
|---|---|---|
| [0001](0001-project-foundations.md) | Project foundations: an integration product, not a fork | accepted; §7 amended by 0008 |
| [0002](0002-embedded-sqlite-state.md) | Embedded SQLite for control-plane state | accepted |
| [0003](0003-container-sandbox.md) | Container sandbox for agent execution | accepted |
| [0004](0004-adapter-protocol.md) | JSON-RPC over stdio to the OpenHands adapter, with LLM tunnelling | accepted |
| [0005](0005-codebase-memory-cli.md) | Drive codebase-memory-mcp through its CLI mode | amended by 0006 |
| [0006](0006-persistent-mcp-session.md) | Persistent MCP session for codebase-memory-mcp | accepted |
| [0007](0007-default-model.md) | Default local model (Qwen3.6) | accepted |
| [0008](0008-serena-symbol-navigation.md) | Serena v1.7.0 for LSP-backed symbol navigation (amends 0001 §7) | accepted |
| [0009](0009-frontier-escalation.md) | Frontier escalation through the Codex CLI subscription | accepted |
| [0010](0010-cloud-model-providers.md) | Cloud model providers as an alternative to the local model (amends 0001) | accepted |

Template: context → decision → consequences → evidence. An ADR is superseded
by a new ADR, never edited after acceptance except for typo fixes and status
updates.
