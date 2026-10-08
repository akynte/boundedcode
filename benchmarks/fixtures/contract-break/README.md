# Fixture: contract-break

This fixture holds a cross-service break that ordinary single-repository
tests miss. It is a three-repository shop:

| Repository | Role |
|---|---|
| `protos` | Go module `example.com/shop/protos`: `payments/v1/payments.proto` (service `shop.payments.v1.PaymentService`, RPCs `Charge` and `Refund`) and its generated Go package `paymentsv1` |
| `payments` | the gRPC server: `internal/server.(*Server).Charge`/`Refund`, registered with `RegisterPaymentServiceServer`; vendors `protos` |
| `checkout` | a client: `internal/pay.(*Client).ChargeOrder` calls `Charge`; vendors `protos`; its test uses a fake connection |

Each directory becomes its own git repository. The code is synthetic,
written for this project, and Apache-2.0. The generated files are a
standard-library-only stand-in with the API that protoc-gen-go and
protoc-gen-go-grpc produce: messages, getters, the client interface and
constructor, the server interface and registration. `ClientConnInterface`
and `ServiceRegistrar` stand in for the `google.golang.org/grpc` interfaces
of the same names, so every repository builds and tests offline.

`internal/xservice` finds 5 links:

```text
grpc      grpc shop.payments.v1.PaymentService/Charge  checkout:internal/pay/client.go:23 -> payments:internal/server/server.go:25
grpc_def  grpc shop.payments.v1.PaymentService         payments:internal/server/server.go:25 -> protos:payments/v1/payments.proto:8
grpc_def  grpc shop.payments.v1.PaymentService/Charge  checkout:internal/pay/client.go:23 -> protos:payments/v1/payments.proto:9
proto     proto shop.payments.v1                       checkout:internal/pay/client.go:8 -> protos:payments/v1/payments.proto:3
proto     proto shop.payments.v1                       payments:internal/server/server.go:10 -> protos:payments/v1/payments.proto:3
```

## The break

In the change, `ChargeRequest.amount_cents` is renamed to `amount_minor`
(same field number) in `protos`, the code is regenerated, and `payments`
(server, test and vendored copy) is updated. `checkout` is not touched. The
result:

* `go test ./...` passes in all three repositories, and so does
  BoundedCode's per-repository verification. `checkout` builds against its
  vendored copy, which still has `AmountCents`.
* Against the candidate `protos`, `checkout` no longer compiles
  (`unknown field AmountCents in struct literal`).

`internal/compat`'s `TestBreakMissedBySingleRepositoryTests` asserts both
points. Every repository's targeted verification passes. The gate reports
the three `checkout` links as `broken`. The control run with `protos` at its
base commit passes. The two `payments` links are `untested` because the
rename is wire- and source-incompatible for code built from the old
definition. `docs/design/cross-repo-compatibility.md` describes the gate.

The compatible variant used by the tests adds `string idempotency_key = 4`
instead. All five links come out `compatible`.
