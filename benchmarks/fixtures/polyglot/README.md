# polyglot fixtures

Small projects in several languages for the direct-mode and bounded
suites, each with its own tests. Every repository is standard-library only,
so its tests run offline.

| Repository | Language | Build and test |
|---|---|---|
| `py-inventory` | Python | `python3 -m unittest` |
| `ts-cart` | TypeScript | `node --test` (Node 22.18 or later runs TypeScript directly) |
| `rust-ledger` | Rust | `cargo test --offline` |
| `java-orders` | Java (javac, no build tool) | `make test` |
| `shop-mono` | Go API and TypeScript web client | `go -C api test ./...`; `cd web && node --test` |
