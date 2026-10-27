# shop-mono

The order API (Go, `api/`) and the web client that reads its summaries
(TypeScript, `web/`). `contracts/summary.schema.json` is the JSON both sides
agree on.

- API tests: `go -C api test ./...`
- Web tests: `cd web && node --test`
