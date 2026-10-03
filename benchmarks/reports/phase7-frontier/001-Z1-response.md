## 1. Diagnosis

The ledger handler calls `Ledger.Post` for every delivery, so duplicate events can create duplicate entries. On the payment side, checking for an existing key before creating a payment is insufficient: concurrent requests can both pass the check. A payment committed before its event is published also creates a crash recovery risk.

## 2. Concrete approach

- **Payment service:** Treat the key as unique within the authenticated customer or merchant scope. Store it with a fingerprint of the payment request and the resulting payment ID. Enforce uniqueness in durable storage, and create the payment and key record in one transaction. A matching retry returns the stored payment with 200 or 201; reuse with a different request returns 409. Requests without the header keep their current behavior.
- Publish `PaymentCharged` only for the newly created payment. If publication is outside the database transaction, use a transactional outbox and a single logical event ID; otherwise a crash can leave a payment without an event. A replay of the HTTP request must never enqueue another event.
- **Ledger service:** Make `payment_id` unique in durable storage. In one transaction, claim that ID and post both entries. Treat a duplicate claim as success without posting again. Adapt `Ledger.Post` or add a transactional API as needed; a process-local map or a separate “seen” check is not sufficient.

## 3. Pitfalls and edge cases

Validate key length and emptiness; never let one customer’s key retrieve another customer’s payment. Define what happens while the first request is still in progress. Do not mark a ledger event processed before its entries commit. If the same `payment_id` arrives with different account, amount, or currency, report the conflict rather than silently accepting altered financial data. An outbox prevents duplicate *enqueueing* and lost events; the transport may still redeliver, which the ledger deduplication must handle.

## 4. Verify

In payment tests, send matching sequential and concurrent keyed requests and assert one payment ID and one event; test conflicting payloads and the existing no-header path. In ledger tests, deliver the same event twice, including concurrently, and assert exactly one pair of entries; test posting failure followed by retry. Then run `go test ./...` in each repository.