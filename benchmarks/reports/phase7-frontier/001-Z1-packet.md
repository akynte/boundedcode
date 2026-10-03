You are a senior software architect advising a local coding agent. You cannot run code or see the repository beyond what is below. Answer concisely with: (1) diagnosis, (2) the concrete approach to take, (3) pitfalls/edge cases, (4) how to verify. Prefer precise instructions the local agent can implement. Do not write the whole implementation.

ESCALATION Z1: architectural risk: touches idempotency, idempotent, ledger, payment; changes span 2 repositories

## TASK
Task t20261003-7dbb27 (attempt 0 of 6, phase setup)

Request:
Make payment processing idempotent across services. (1) payment-service: POST /v1/payments must honour an `Idempotency-Key` request header. A repeated request with the same key must return the original payment (same id) with status 200 or 201 and must NOT publish a second PaymentCharged event. Requests without the header behave as today. (2) ledger-service: HandlePaymentCharged must be idempotent per payment_id: processing the same event twice posts only once. Add tests in both services.

Acceptance criteria:
- same Idempotency-Key returns the same payment id and publishes exactly one event
- duplicate PaymentCharged events (same payment_id) are posted once
- go test ./... passes in both services
Remaining steps: implement; verify (targeted); verify (full gate)

Repositories (each is a git worktree on the task branch):
- ledger-service: ./ledger-service (branch agent/t20261003-7dbb27)
- payment-service: ./payment-service (branch agent/t20261003-7dbb27)

## RELEVANT CODE
### ledger-service: PaymentCharged
name: PaymentCharged
qualified_name: bench.cross-service-idempotency.ledger-service.internal.consumer.PaymentCharged
label: Struct
file_path: ledger-service/internal/consumer/consumer.go
start_line: 22
end_line: 27
source_mode: full
source: |
  type PaymentCharged struct {
  	PaymentID   string `json:"payment_id"`
  	AccountID   string `json:"account_id"`
  	AmountCents int64  `json:"amount_cents"`
  	Currency    string `json:"currency"`
  }
match_method: suffix
callers: 0
callees: 0
callers:
function: PaymentCharged
direction: inbound
callers_total: 0
callers_total_relation: eq
callers: 0  (cols: qn hop)
### ledger-service: HandlePaymentCharged
name: HandlePaymentCharged
qualified_name: bench.cross-service-idempotency.ledger-service.internal.consumer.HandlePaymentCharged
label: Method
file_path: ledger-service/internal/consumer/consumer.go
start_line: 36
end_line: 48
source_mode: full
source: |
  func (c *Consumer) HandlePaymentCharged(msg *ConsumerMessage) error {
  	if msg.Topic != TopicPaymentCharged {
  		return fmt.Errorf("unexpected topic %q", msg.Topic)
  	}
  	var ev PaymentCharged
  	if err := json.Unmarshal(msg.Value, &ev); err != nil {
  		return fmt.Errorf("decode PaymentCharged: %w", err)
  	}
  	return c.Ledger.Post(
  		ledger.Entry{PaymentID: ev.PaymentID, AccountID: ev.AccountID, AmountCents: ev.AmountCents, Currency: ev.Currency},
  		ledger.Entry{PaymentID: ev.PaymentID, AccountID: SettlementAccount, AmountCents: -ev.AmountCents, Currency: ev.Currency},
  	)
  }
match_method: suffix
callers: 1
callees: 1
callers:
function: HandlePaymentCharged
direction: inbound
callers_total: 0
callers_total_relation: eq
callers: 0  (cols: qn hop)

## SPECIFIC QUESTION
Before the local agent implements this, what is the correct design? Identify contract, consistency, idempotency or security risks and the safest implementation plan.
