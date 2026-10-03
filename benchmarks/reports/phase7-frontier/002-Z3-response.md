## 1. Diagnosis

**Do not merge yet.** The payment path does not guarantee idempotency under concurrent requests or publish failures.

- [handler.go](payment-service/internal/api/handler.go:62): Two requests can both miss `GetPaymentByKey`, create different payments, and publish two events. The key is recorded only after publishing; `InsertIdempotencyKey` even returns success for a duplicate key with the same fingerprint.
- [handler.go](payment-service/internal/api/handler.go:80): If publishing fails, the payment exists but its key is unrecorded. A retry creates another payment.
- [handler.go](payment-service/internal/api/handler.go:57): The colon joined fingerprint can collide when field values contain colons, allowing a changed request to reuse a key without a conflict.
- [ledger.go](ledger-service/internal/ledger/ledger.go:49): `Post` returns early if *any* entry has a posted ID, silently dropping new payment IDs in a mixed batch. Writing `postedIDs` also panics for the previously usable zero value `&Ledger{}`.

## 2. Concrete approach

Make key lookup and payment creation one atomic store operation. Record the key before attempting publication, and track publication state so concurrent callers cannot publish the same event; handle failed publication through a retryable state or transactional outbox. Compare structured request fields instead of a delimiter based fingerprint. In the ledger, define whether `Post` accepts one payment ID per call; enforce that contract, initialize the map under its lock, and atomically check and append.

## 3. Pitfalls and edge cases

Reject an empty `payment_id` before deduplication; otherwise unrelated malformed events all share one deduplication key. Define what a retry returns while the first request is still publishing and after publication fails. Preserve the original payment ID in every outcome.

## 4. Verify

Add simultaneous same-key request tests asserting one payment ID and one event; a publish-failure-then-retry test; a fingerprint collision test; and ledger tests for zero-value construction, mixed IDs, and concurrent duplicate delivery. Run `gofmt` on changed Go files, then `go test ./...` in both services.