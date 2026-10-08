# Cross-Service Contract Analysis (`internal/xservice`)

Status: **implemented**. It closes the gaps measured in the
[repository-intelligence gap report](repointel-gap-report.md).

## What it extracts

| Contract | Languages / formats | Recognized APIs |
|---|---|---|
| HTTP routes | Go (`go/ast`) | `net/http` `Handle`/`HandleFunc` including Go 1.22 `"METHOD [host]/path"`; chi `Get/Post/…` with `Route` prefixes; gin/echo/fiber verbs with `Group` prefixes (nested); gorilla `HandleFunc(...).Methods(...)` and `PathPrefix(...).Subrouter()` |
| HTTP routes | TS/JS (lexer) | Express, Fastify, Koa-router and Hono `x.verb('/path', handler)`; NestJS `@Controller` + `@Get/@Post/…` |
| HTTP calls | Go | `http.NewRequest[WithContext]` (method constants included), `http.Get/Post/Head/PostForm`, `client.Get/Post/Head` |
| HTTP calls | TS/JS | `fetch(url, {method})`, `axios.verb`, `axios({url, method})`, `axios.create({baseURL})` instances |
| Topic produce / consume | Go | sarama (`ProducerMessage`, `ConsumerGroup.Consume`, `ConsumePartition`), kafka-go (`Writer`, `Message`, `ReaderConfig`, `GroupTopics`), franz-go (`kgo.Record`, `ConsumeTopics`), confluent (`TopicPartition`, `SubscribeTopics`), lookalike wrapper types, and `x.Topic == "…"` checks (marked partial) |
| Topic produce / consume | TS/JS | kafkajs `send/sendBatch({topic})`, `subscribe({topic \| topics})` |
| Topic provisioning | Terraform | `kafka_topic`, `confluent_kafka_topic`, `aiven_kafka_topic`, `aws_msk_topic`, `aws_sns_topic`, `aws_sqs_queue`, `google_pubsub_topic`, `azurerm_servicebus_topic`, `redpanda_topic` |
| Env read | Go / TS/JS | `os.Getenv`, `os.LookupEnv`, `env:`/`envconfig:` struct tags; `process.env.X`, `process.env["X"]`, `import.meta.env.X` |
| Env provide | YAML / Docker / templates | Kubernetes container `env`, Helm values `env` maps, compose `environment`, ConfigMap `data`, kustomize `literals`, Dockerfile `ENV`, `.env.example` (real `.env` files are never read) |
| OpenAPI operations | OpenAPI 3.x, Swagger 2.0 (YAML or JSON) | every path and method, with the base path from `basePath` or `servers` (variables at their defaults; path-level `servers` too); `operationId` as the symbol |
| Protobuf packages | `.proto` (parsed) | the package and how generated code imports it: `go_package`, `java_package` (or the package), `csharp_namespace` (or the generator's default), the file's base name (Python `_pb2`, JS `_pb`/`_grpc_pb`/`_connect` modules) |
| Protobuf use | Go, Python, Java/Kotlin/Scala, C#, TS/JS | imports of generated code (Go: imports outside the standard library and the module, once per package; matched against `go_package` when linking) |
| gRPC definitions | `.proto` | each `service` and `rpc` (streaming noted), and `google.api.http` annotations (grpc-gateway, with `additional_bindings` and `custom`) as HTTP routes |
| gRPC servers | Go, Python, Java/Kotlin, C#, Rust, Ruby, TS/JS | `RegisterXServer`, connect-go `NewXHandler`; `add_XServicer_to_server` and `XServicer` subclasses with their RPC methods; `XGrpc.XImplBase` (and Kotlin coroutine bases) with their `StreamObserver` methods; `X.XBase`; tonic `XServer::new`; `X::Service`; grpc-js `addService`, connect-es routers, nice-grpc |
| gRPC clients | same | `NewXClient` (in files using gRPC, given a connection, or from generated packages), grpc-gateway `RegisterXHandler…`; `XStub`; `XGrpc.newBlockingStub/newStub/newFutureStub`, coroutine stubs; `new X.XClient`; tonic `XClient::connect`; `X::Stub.new`; PHP `new XClient`; grpc-js clients, connect-es and nice-grpc `createClient`. RPCs called through a client variable or field (`stub.withDeadlineAfter(…).getPayment(…)`, `s.payments.Create(…)`, `pb.NewXClient(conn).Get(…)`) are RPC-level |
| SQL schema | `.sql`, Liquibase (YAML/XML), Prisma, alembic, Rails, Laravel, EF Core and knex migrations | `CREATE/ALTER/DROP/RENAME TABLE`, `CREATE INDEX`, `CREATE VIEW` (temporary tables are not contracts) |
| SQL access | string literals in every language above (concatenations, `fmt.Sprintf` formats, Python implicit joins, text blocks, heredocs, template literals), `.sql` queries, MyBatis mappers | `SELECT … FROM/JOIN`, `INSERT/MERGE/REPLACE INTO`, `UPDATE … SET`, `DELETE FROM … USING`, `TRUNCATE`, `COPY`; ORM mappings: Go `TableName()`, gorm/squirrel/goqu/bun builders, SQLAlchemy, Django, ActiveRecord, JPA `@Table`, EF `[Table]`/`ToTable`, diesel `table!`, Eloquent, TypeORM, Sequelize, knex, Prisma client |

SQL is tokenized (comments, quoted identifiers, string and dollar-quoted
literals), and each statement classified; CTE names, subqueries, set-returning
functions and function arguments (`EXTRACT(x FROM y)`, `IS DISTINCT FROM`)
are not tables, nor are system catalogs. A string counts as SQL only when it
starts with a statement keyword (upper case, unless it spans lines) and has
the statement's shape, so prose such as "delete from cart failed" is not.
Sources are lexed per language family before any pattern runs, so comments,
docstrings and other strings never produce gRPC endpoints. Generated stubs
(by name, e.g. `_pb2_grpc.py`, `Grpc.java`, `.pb.go`, or a "generated"
banner) define every service and are not servers or clients themselves;
their SQL (sqlc output) still counts. Test code (test directories and test
file names) produces no gRPC, protobuf or SQL endpoints.

Value resolution covers literals, package and local constants (across
packages of the same Go module), `+` concatenation, `fmt.Sprintf`,
`path.Join`/`url.JoinPath`, JS template literals and `const` bindings, and
`process.env.X ?? "fallback"`. Unresolvable parts become `{}` with
confidence `partial`. Endpoints carry `exact`, `resolved` or `partial`
confidence. Values that are entirely dynamic are reported as diagnostics
and are not linked.

## How contracts are linked

In every link, `From` depends on `To`.

| Link | From → To | Rule |
|---|---|---|
| `http` | call → route | method-compatible, path template match, across repositories (routes include grpc-gateway annotations) |
| `openapi` | call → spec operation | as `http`, across repositories |
| `openapi_impl` | route → spec operation | the route that implements an operation (any repository) |
| `grpc` | client → server | same service and RPC (`CreatePayment`, `createPayment` and `create_payment` are one RPC); a service-level client pairs with a service-level server, an RPC call with that RPC's implementation, or with the server's registration when it has no method-level endpoint; across repositories |
| `grpc_def` | client/server → `.proto` service or rpc | across repositories |
| `proto` | generated-code import → `.proto` package | by reference (Go import path, Java package, C# namespace, file base name), across repositories |
| `sql` | query/ORM mapping → schema of its table | across repositories, only when the querying repository does not define that table itself (two services that each own a `users` table are not linked); one link per schema file |
| `topic`, `topic_infra`, `env` | unchanged | see above |

Short service names (`PaymentService` from `NewPaymentServiceClient`) are
resolved to `pkg.PaymentService` through the `.proto` definitions; when two
packages define the same name, the generated package the code imports
decides (Go import path, Python `_pb2` module, Java package, TS module). An
RPC that the resolved service does not define (a health check's `Check`
implemented on the same class) is dropped.

## How it is used

1. **`boundedcode index`** scans every repository and stores endpoints in
   SQLite (`xservice_endpoints`). `intel links` and `intel endpoints` print
   them.
2. **Context packs.** A `CROSS-SERVICE CONTRACTS` section lists contracts
   involving the task's repositories. Contracts touched by the agent's
   changes come first, with the counterpart that may need updating. Task
   repositories are rescanned from their worktrees on every attempt.
3. **Contract check (local).** When verification passes but a touched
   contract's other side is in a task repository the agent did not change,
   the runner asks for one targeted round. This happens before any frontier
   use. HTTP, topic and gRPC contracts count from either side. Definitions
   (OpenAPI specs, `.proto` files, SQL schemas) count only when the
   definition file itself changed: changing a query does not ask for
   another service's migration to change, but changing a migration asks
   for the queries that use the table to be checked.
4. **Z3.** A touched contract whose counterpart is *outside* the task
   triggers a pre-merge frontier review, when frontier is enabled.
5. **Cross-repository compatibility gate.** For gRPC, protobuf and OpenAPI
   links, the gate decides whether the change affects each link
   (`CompareRPC`/`CompareFile` for `.proto`, an operation digest for
   OpenAPI, enclosing functions for Go code). It runs the dependent
   repository's own checks against the other repositories' candidate
   commits and records `compatible`, `broken` or `untested` for each link.
   `TASK_VERIFIED` needs every affected link to be `compatible`. See
   [cross-repo-compatibility.md](cross-repo-compatibility.md).

Disable with `repointel.cross_service: false` (this also disables the gate;
`repointel.compat_gate: false` disables only the gate).

## Evidence

* **Ground truth:** for the `payment-platform` fixture all 7 documented
  cross-service links are found with no extra links. codebase-memory-mcp
  v0.11.0 found 0 of them (gap report).
* **Ground truth, all contract types:** `testdata/contracts` is a
  six-repository workspace (shared protos with two `PaymentService`s in
  different packages and grpc-gateway annotations; a Go server with
  migrations and an OpenAPI spec; Python, Java, TypeScript and Rust
  services). `TestContractsGroundTruth` requires exactly its 24 links (gRPC,
  gRPC definitions, HTTP via grpc-gateway, OpenAPI, protobuf, SQL): no
  missing and no extra link. Its traps (a call in a comment, a server name
  in a docstring, SQL-like prose, a table owned by its own service, a raw
  string after a Rust lifetime) produce nothing.
* **Real code:** on Google's Online Boutique
  ([microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo)
  @ 38e7348e, 2026-09-18; services in Go, C#, Node.js, Python and Java,
  each scanned as its own repository) all 21 gRPC client→server edges of
  the application are found and no others. On the AWS SDK for Go and
  Kubernetes module trees no gRPC client or server is reported that is not
  one (after the fixes this validation led to: generated stubs, generic
  `NewXClient` constructors, mocks in test directories, a JS file that only
  mentioned "grpc"). Scanning cost there: +6% (AWS SDK) and +25% (Kubernetes,
  which has 2,736 OpenAPI operations and 1,696 proto packages to read) over
  HTTP/topics/env alone.
* Idiom tests cover chi, gin, gorilla, Go 1.22 patterns, sarama, kafka-go,
  franz-go, confluent, Express, NestJS, axios, kafkajs, Kubernetes,
  compose, Dockerfile and Terraform (`internal/xservice/*_test.go`).
* **Robustness:** fuzz targets for the JS lexer, the Go analyzer, path
  normalization, the SQL analyzer, the proto parser and the per-language
  source lexer. Fuzzing found and fixed one out-of-range slice on
  malformed JS.
* **Agent benefit:** the ablation on the `event-field-rename` task is in
  `benchmarks/reports/*-ablation-xservice-*`. See
  [phase9-analyzers.md](phase9-analyzers.md).

## Limits

* HTTP routes and calls, topics and env reads are analyzed in Go and
  TS/JS only; the other languages contribute gRPC, protobuf and SQL
  contracts.
* SQL contracts are per table, not per column: a migration that renames a
  column points at every query of the table. Tables named at run time
  (`fmt.Sprintf("… FROM %s", t)` with a variable) are not guessed, and
  ORM tables derived from naming conventions (a `Payment` model meaning a
  `payments` table) are not linked unless the mapping names the table.
* gRPC servers registered through reflection or a framework that hides the
  generated registration, and protobuf use without `go_package` (buf
  managed mode) in Go, are not linked; gRPC clients and servers still link
  by service name.
* Indirect clients (generated SDKs, service meshes), URLs assembled at
  runtime, and topics from configuration files are reported as unresolved
  rather than guessed.
* Helm templates (`templates/*.yaml`) are skipped, because they are not
  YAML. Values files and rendered manifests are analyzed.
