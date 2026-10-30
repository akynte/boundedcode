# shop

One repository, three languages:

- `server/`: the Go HTTP API (`go test ./...` in `server/`).
- `web/`: the TypeScript client (`npm test` in `web/`). `web/src/client.gen.ts`
  is generated from `api/openapi.json` by `node tools/gen-client.mjs`; never
  edit it by hand. `node tools/gen-client.mjs --check` fails when it is stale.
- `py/`: order validation against `schema/order.schema.json`, the schema
  every service shares (`python3 -m unittest discover -s py/tests`).
