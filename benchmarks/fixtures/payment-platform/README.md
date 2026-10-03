# Fixture: payment-platform

A small multi-repository microservice workspace used to measure repository
intelligence (Phase 3) and as a base for engineering benchmark tasks. Each
top-level directory becomes its own git repository via
`scripts/materialize-fixture.sh`. The code is synthetic, written for this
project, and Apache-2.0. Go services use only the standard library so they
build and test offline in the sandbox.

Cross-service relationships, which serve as the ground truth for the gap report:

| Kind | From | To |
|---|---|---|
| HTTP | gateway `src/payments.ts` `createPayment()` → `POST /v1/payments` | payment-service `internal/api.(*Handler).CreatePayment` |
| Event | payment-service `events.Publisher.PaymentCharged` → topic `payments.charged` | ledger-service `consumer.(*Consumer).HandlePaymentCharged` |
| Contract | `shared-protos/proto/payment.proto` `PaymentCharged` | producer and consumer JSON structs |
| DB | payment-service `store.InsertPayment` → table `payments` (migration 001) | none |
| DB | ledger-service `ledger.Post` → table `ledger_entries` (migration 001) | none |
| Config | payment-service `config.Load` reads `PAYMENT_DB_DSN`, `KAFKA_BROKERS` | Helm `deploy/helm/payment-service/values.yaml` env |
| Infra | Terraform `kafka_topic.payments_charged` | topic `payments.charged` |
