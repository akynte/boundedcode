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

Value resolution covers literals, package and local constants (across
packages of the same Go module), `+` concatenation, `fmt.Sprintf`,
`path.Join`/`url.JoinPath`, JS template literals and `const` bindings, and
`process.env.X ?? "fallback"`. Unresolvable parts become `{}` with
confidence `partial`. Endpoints carry `exact`, `resolved` or `partial`
confidence. Values that are entirely dynamic are reported as diagnostics
and are not linked.

## How it is used

1. **`boundedcode index`** scans every repository and stores endpoints in
   SQLite (`xservice_endpoints`). `intel links` and `intel endpoints` print
   them.
2. **Context packs.** A `CROSS-SERVICE CONTRACTS` section lists contracts
   involving the task's repositories. Contracts touched by the agent's
   changes come first, with the counterpart that may need updating. Task
   repositories are rescanned from their worktrees on every attempt.
3. **Contract check (local).** When verification passes but a touched
   HTTP or topic contract's other side is in a task repository the agent
   did not change, the runner asks for one targeted round. This happens
   before any frontier use.
4. **Z3.** A touched contract whose counterpart is *outside* the task
   triggers a pre-merge frontier review, when frontier is enabled.

Disable with `repointel.cross_service: false`.

## Evidence

* **Ground truth:** for the `payment-platform` fixture all 7 documented
  cross-service links are found with no extra links. codebase-memory-mcp
  v0.11.0 found 0 of them (gap report).
* Idiom tests cover chi, gin, gorilla, Go 1.22 patterns, sarama, kafka-go,
  franz-go, confluent, Express, NestJS, axios, kafkajs, Kubernetes,
  compose, Dockerfile and Terraform (`internal/xservice/*_test.go`).
* **Robustness:** fuzz targets for the JS lexer, the Go analyzer and path
  normalization. Fuzzing found and fixed one out-of-range slice on
  malformed JS.
* **Agent benefit:** the ablation on the `event-field-rename` task is in
  `benchmarks/reports/*-ablation-xservice-*`. See
  [phase9-analyzers.md](phase9-analyzers.md).

## Limits

* Python, Java, Rust and gRPC/protobuf service definitions are not
  analyzed yet. Protobuf RPCs are covered by codebase-memory-mcp as Routes.
* Indirect clients (generated SDKs, service meshes), URLs assembled at
  runtime, and topics from configuration files are reported as unresolved
  rather than guessed.
* Helm templates (`templates/*.yaml`) are skipped, because they are not
  YAML. Values files and rendered manifests are analyzed.
