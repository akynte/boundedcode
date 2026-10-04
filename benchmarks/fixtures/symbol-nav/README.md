# Fixture: symbol-nav

Two small repositories used to measure symbol-level navigation (Serena/LSP
versus the code graph) and as a base for interface-change benchmark tasks.
The code is synthetic, written for this project, and Apache-2.0. The Go
service uses only the standard library; the TypeScript app has no
dependencies.

Ground truth used by `internal/repointel/serena` tests and the overlap study:

| Question | Answer |
|---|---|
| Where is `CreatePayment`? | billing-service `internal/payment/service.go`, method `(*Service).CreatePayment` |
| Who references `Service.CreatePayment`? | `internal/api/handler.go` `(*Handler).Create`, `internal/payment/service_test.go` `TestCreatePayment` |
| Implementations of `PaymentRepository` | `PostgresRepository` (`postgres.go`), `MemoryRepository` (`memory.go`) |
| Who calls `PaymentRepository.Insert`? | `(*Service).CreatePayment` only. `audit.FileLog.Insert` is a same-named decoy on an unrelated interface |
| TS: shared type `PaymentRequest` | `web-checkout/src/types/payment.ts`, used by `services/paymentService.ts` and `api/checkoutController.ts` |
| TS: implementations of `PaymentGateway` | `gateways/cardGateway.ts` `CardGateway`, `gateways/mockGateway.ts` `MockGateway` |
