You are a senior software architect advising a local coding agent. You cannot run code or see the repository beyond what is below. Answer concisely with: (1) diagnosis, (2) the concrete approach to take, (3) pitfalls/edge cases, (4) how to verify. Prefer precise instructions the local agent can implement. Do not write the whole implementation.

ESCALATION Z3: high-risk change before merge: ledger-service/internal/consumer/consumer_test.go, ledger-service/internal/ledger/ledger.go, payment-service/internal/api/handler.go, payment-service/internal/api/handler_test.go, payment-service/internal/store/store.go

## TASK
Task t20261003-7dbb27 (attempt 2 of 6, phase verifying)

Request:
Make payment processing idempotent across services. (1) payment-service: POST /v1/payments must honour an `Idempotency-Key` request header. A repeated request with the same key must return the original payment (same id) with status 200 or 201 and must NOT publish a second PaymentCharged event. Requests without the header behave as today. (2) ledger-service: HandlePaymentCharged must be idempotent per payment_id: processing the same event twice posts only once. Add tests in both services.

Acceptance criteria:
- same Idempotency-Key returns the same payment id and publishes exactly one event
- duplicate PaymentCharged events (same payment_id) are posted once
- go test ./... passes in both services

Completed steps: verify (targeted)
Remaining steps: implement; verify (full gate)

Decisions so far:
- [frontier] Z1 advice received (## 1. Diagnosis)

Repositories (each is a git worktree on the task branch):
- ledger-service: ./ledger-service (branch agent/t20261003-7dbb27)
- payment-service: ./payment-service (branch agent/t20261003-7dbb27)

## STRATEGIES ALREADY TRIED (do not repeat)
- attempt 1: ## Summary Implemented idempotency for both services to handle duplicate requests and events. ### Payment Service (`payment-service/`) **`internal/api/handler.go`**: - Added `Idempotency-Key` header support to `POST /v1/payments` - When a key is provided: checks if the key was already used, returns …
  rejected because: payment-service/gofmt: files need gofmt:

## CURRENT CHANGES
### ledger-service
```
internal/consumer/consumer_test.go | 42 +++++++++++++++++++++++++++++++++++++-
 internal/ledger/ledger.go          | 31 +++++++++++++++++++++++++---
 2 files changed, 69 insertions(+), 4 deletions(-)
```
```diff
diff --git a/internal/consumer/consumer_test.go b/internal/consumer/consumer_test.go
index d5c3d8f..27d35b0 100644
--- a/internal/consumer/consumer_test.go
+++ b/internal/consumer/consumer_test.go
@@ -7,7 +7,7 @@ import (
 )
 
 func TestHandlePaymentCharged(t *testing.T) {
-	l := &ledger.Ledger{}
+	l := ledger.New()
 	c := &Consumer{Ledger: l}
 	msg := &ConsumerMessage{Topic: TopicPaymentCharged, Value: []byte(`{"payment_id":"p1","account_id":"a1","amount_cents":500,"currency":"EUR"}`)}
 	if err := c.HandlePaymentCharged(msg); err != nil {
@@ -20,3 +20,43 @@ func TestHandlePaymentCharged(t *testing.T) {
 		t.Fatalf("settlement = %d", got)
 	}
 }
+
+func TestHandlePaymentChargedIdempotent(t *testing.T) {
+	l := ledger.New()
+	c := &Consumer{Ledger: l}
+	msg := &ConsumerMessage{Topic: TopicPaymentCharged, Value: []byte(`{"payment_id":"p2","account_id":"a2","amount_cents":1000,"currency":"USD"}`)}
+
+	// First delivery
+	if err := c.HandlePaymentCharged(msg); err != nil {
+		t.Fatal(err)
+	}
+	if got := l.Balance("a2"); got != 1000 {
+		t.Fatalf("balance after first = %d", got)
+	}
+
+	// Second delivery (duplicate)
+	if err := c.HandlePaymentCharged(msg); err != nil {
+		t.Fatal(err)
+	}
+	if got := l.Balance("a2"); got != 1000 {
+		t.Fatalf("balance after second = %d (should still be 1000)", got)
+	}
+	if got := l.Balance(SettlementAccount); got != -1000 {
+		t.Fatalf("settlement after second = %d (should still be -1000)", got)
+	}
+
+	// Verify only 2 entries exist (one balanced pair)
+	entries := l.Entries()
+	if len(entries) != 2 {
+		t.Fatalf("expected 2 entries, got %d", len(entries))
+	}
+}
+
+func TestHandlePaymentChargedWrongTopic(t *testing.T) {
+	l := ledger.New()
+	c := &Consumer{Ledger: l}
+	msg := &ConsumerMessage{Topic: "wrong.topic", Value: []byte(`{"payment_id":"p1","account_id":"a1","amount_cents":500,"currency":"EUR"}`)}
+	if err := c.HandlePaymentCharged(msg); err == nil {
+		t.Fatal("expected error for wrong topic")
+	}
+}
diff --git a/internal/ledger/ledger.go b/internal/ledger/ledger.go
index 690c3c5..9535602 100644
--- a/internal/ledger/ledger.go
+++ b/internal/ledger/ledger.go
@@ -16,15 +16,28 @@ type Entry struct {
 
 // Ledger stores entries in memory (stand-in for SQL).
 type Ledger struct {
-	mu      sync.Mutex
-	entries []Entry
+	mu        sync.Mutex
+	entries   []Entry
+	postedIDs map[string]bool // tracks which payment_ids have been posted
 }
 
 // ErrUnbalanced is returned when entries do not sum to zero.
 var ErrUnbalanced = errors.New("unbalanced posting")
 
+// ErrDuplicatePayment is returned when a payment_id has already been posted.
+var ErrDuplicatePayment = errors.New("payment already posted")
+
+// New creates an empty ledger.
+func New() *Ledger {
+	return &Ledger{
+		entries:   []Entry{},
+		postedIDs: map[string]bool{},
+	}
+}
+
 // Post records a balanced set of entries:
 // INSERT INTO ledger_entries (payment_id, account_id, amount_cents, currency) VALUES ...
+// If the payment_id was already posted, returns nil without posting again (idempotent).
 func (l *Ledger) Post(entries ...Entry) error {
 	var sum int64
 	for _, e := range entries {
@@ -33,9 +46,21 @@ func (l *Ledger) Post(entries ...Entry) error {
 	if sum != 0 {
 		return ErrUnbalanced
 	}
+
+	// Check if any payment_id was already posted
 	l.mu.Lock()
-	defer l.mu.Unlock()
+	for _, e := range entries {
+		if l.postedIDs[e.PaymentID] {
+			l.mu.Unlock()
+			return nil
+		}
+	}
+	// Mark all payment_ids as posted before appending entries
+	for _, e := range entries {
+		l.postedIDs[e.PaymentID] = true
+	}
 	l.entries = append(l.entries, entries...)
+	l.mu.Unlock()
 	return nil
 }
```
### payment-service
```
internal/api/handler.go      | 57 ++++++++++++++++++++++++++++++++
 internal/api/handler_test.go | 77 ++++++++++++++++++++++++++++++++++++++++++++
 internal/store/store.go      | 49 +++++++++++++++++++++++++++-
 3 files changed, 182 insertions(+), 1 deletion(-)
```
```diff
diff --git a/internal/api/handler.go b/internal/api/handler.go
index c40cfdc..2a624fe 100644
--- a/internal/api/handler.go
+++ b/internal/api/handler.go
@@ -5,6 +5,8 @@ import (
 	"crypto/rand"
 	"encoding/hex"
 	"encoding/json"
+	"errors"
+	"fmt"
 	"net/http"
 
 	"example.com/payment-service/internal/events"
@@ -41,6 +43,61 @@ func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {
 		http.Error(w, "invalid payment", http.StatusUnprocessableEntity)
 		return
 	}
+
+	idempKey := r.Header.Get("Idempotency-Key")
+
+	if idempKey != "" {
+		// Validate idempotency key length
+		if len(idempKey) < 1 || len(idempKey) > 255 {
+			http.Error(w, "invalid idempotency key", http.StatusBadRequest)
+			return
+		}
+
+		fingerprint := fmt.Sprintf("%s:%d:%s", req.AccountID, req.AmountCents, req.Currency)
+
+		// Check if this key was already used
+		if existingID, existingFingerprint, ok := h.Store.GetPaymentByKey(idempKey); ok {
+			if existingFingerprint != fingerprint {
+				http.Error(w, "idempotency key conflict", http.StatusConflict)
+				return
+			}
+			p, err := h.Store.GetPayment(r.Context(), existingID)
+			if err != nil {
+				http.Error(w, "store error", http.StatusInternalServerError)
+				return
+			}
+			w.Header().Set("Content-Type", "application/json")
+			w.WriteHeader(http.StatusOK)
+			_ = json.NewEncoder(w).Encode(p)
+			return
+		}
+
+		// Create new payment
+		p := store.Payment{ID: newID(), AccountID: req.AccountID, AmountCents: req.AmountCents, Currency: req.Currency, Status: "charged"}
+		if err := h.Store.InsertPayment(r.Context(), p); err != nil {
+			http.Error(w, "store error", http.StatusInternalServerError)
+			return
+		}
+		if err := h.Events.PaymentCharged(events.PaymentCharged{PaymentID: p.ID, AccountID: p.AccountID, AmountCents: p.AmountCents, Currency: p.Currency}); err != nil {
+			http.Error(w, "publish error", http.StatusInternalServerError)
+			return
+		}
+		// Record the idempotency key (after payment and event are committed)
+		if err := h.Store.InsertIdempotencyKey(idempKey, p.ID, fingerprint); err != nil {
+			if errors.Is(err, store.ErrIdempotencyConflict) {
+				http.Error(w, "idempotency key conflict", http.StatusConflict)
+				return
+			}
+			http.Error(w, "store error", http.StatusInternalServerError)
+			return
+		}
+		w.Header().Set("Content-Type", "application/json")
+		w.WriteHeader(http.StatusCreated)
+		_ = json.NewEncoder(w).Encode(p)
+		return
+	}
+
+	// No idempotency key: behave as before
 	p := store.Payment{ID: newID(), AccountID: req.AccountID, AmountCents: req.AmountCents, Currency: req.Currency, Status: "charged"}
 	if err := h.Store.InsertPayment(r.Context(), p); err != nil {
 		http.Error(w, "store error", http.StatusInternalServerError)
diff --git a/internal/api/handler_test.go b/internal/api/handler_test.go
index 4ea8bc1..efec6e9 100644
--- a/internal/api/handler_test.go
+++ b/internal/api/handler_test.go
@@ -1,6 +1,7 @@
 package api
 
 import (
+	"encoding/json"
 	"net/http"
 	"net/http/httptest"
 	"strings"
@@ -40,3 +41,79 @@ func TestCreatePaymentValidation(t *testing.T) {
 		}
 	}
 }
+
+func TestCreatePaymentIdempotency(t *testing.T) {
+	mux, prod := newTestServer()
+
+	// First request with idempotency key
+	body := `{"account_id":"acc_1","amount_cents":500,"currency":"EUR"}`
+	rec := httptest.NewRecorder()
+	req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body))
+	req.Header.Set("Idempotency-Key", "key-1")
+	mux.ServeHTTP(rec, req)
+	if rec.Code != http.StatusCreated {
+		t.Fatalf("first request status = %d", rec.Code)
+	}
+	var firstPayment store.Payment
+	if err := json.Unmarshal(rec.Body.Bytes(), &firstPayment); err != nil {
+		t.Fatal(err)
+	}
+	firstID := firstPayment.ID
+	if len(prod.Sent) != 1 {
+		t.Fatalf("expected 1 event, got %d", len(prod.Sent))
+	}
+
+	// Second request with same key and same body
+	rec = httptest.NewRecorder()
+	req = httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body))
+	req.Header.Set("Idempotency-Key", "key-1")
+	mux.ServeHTTP(rec, req)
+	if rec.Code != http.StatusOK {
+		t.Fatalf("second request status = %d: %s", rec.Code, rec.Body)
+	}
+	var secondPayment store.Payment
+	if err := json.Unmarshal(rec.Body.Bytes(), &secondPayment); err != nil {
+		t.Fatal(err)
+	}
+	if secondPayment.ID != firstID {
+		t.Fatalf("payment IDs differ: %s vs %s", secondPayment.ID, firstID)
+	}
+	if len(prod.Sent) != 1 {
+		t.Fatalf("expected 1 event after retry, got %d", len(prod.Sent))
+	}
+}
+
+func TestCreatePaymentIdempotencyConflict(t *testing.T) {
+	mux, _ := newTestServer()
+
+	// First request with idempotency key
+	body := `{"account_id":"acc_1","amount_cents":500,"currency":"EUR"}`
+	rec := httptest.NewRecorder()
+	req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body))
+	req.Header.Set("Idempotency-Key", "key-2")
+	mux.ServeHTTP(rec, req)
+	if rec.Code != http.StatusCreated {
+		t.Fatalf("first request status = %d", rec.Code)
+	}
+
+	// Second request with same key but different body
+	body2 := `{"account_id":"acc_1","amount_cents":999,"currency":"EUR"}`
+	rec = httptest.NewRecorder()
+	req = httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body2))
+	req.Header.Set("Idempotency-Key", "key-2")
+	mux.ServeHTTP(rec, req)
+	if rec.Code != http.StatusConflict {
+		t.Fatalf("conflict status = %d: %s", rec.Code, rec.Body)
+	}
+}
+
+func TestCreatePaymentIdempotencyEmptyKey(t *testing.T) {
+	mux, _ := newTestServer()
+	rec := httptest.NewRecorder()
+	req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(`{"account_id":"acc_1","amount_cents":500,"currency":"EUR"}`))
+	req.Header.Set("Idempotency-Key", "")
+	mux.ServeHTTP(rec, req)
+	if rec.Code != http.StatusCreated {
+		t.Fatalf("empty key should behave normally, status = %d", rec.Code)
+	}
+}
diff --git a/internal/store/store.go b/internal/store/store.go
index dda39bd..4336f0d 100644
--- a/internal/store/store.go
+++ b/internal/store/store.go
@@ -19,15 +19,32 @@ type Payment struct {
 // ErrNotFound is returned for unknown payments.
 var ErrNotFound = errors.New("payment not found")
 
+// ErrIdempotencyConflict is returned when a key is reused with a different payload.
+var ErrIdempotencyConflict = errors.New("idempotency key conflict: payload differs from original request")
+
 // Store is an in-memory stand-in for the SQL store. The SQL it would run is
 // kept next to each method so the schema relationship stays visible.
 type Store struct {
 	mu       sync.Mutex
 	payments map[string]Payment
+	// idemKeys maps an idempotency key to the payment ID and the request fingerprint
+	// (account_id:amount_cents:currency).
+	idemKeys map[string]struct {
+		PaymentID   string
+		Fingerprint string
+	}
 }
 
 // New returns an empty store.
-func New() *Store { return &Store{payments: map[string]Payment{}} }
+func New() *Store {
+	return &Store{
+		payments: map[string]Payment{},
+		idemKeys: map[string]struct {
+			PaymentID   string
+			Fingerprint string
+		}{},
+	}
+}
 
 // InsertPayment runs: INSERT INTO payments (id, account_id, amount_cents, currency, status) VALUES (...)
 func (s *Store) InsertPayment(_ context.Context, p Payment) error {
@@ -47,3 +64,33 @@ func (s *Store) GetPayment(_ context.Context, id string) (Payment, error) {
 	}
 	return p, nil
 }
+
+// InsertIdempotencyKey records an idempotency key with the payment ID and a fingerprint
+// of the request. Returns ErrIdempotencyConflict if the same key was used with
+// a different payload.
+func (s *Store) InsertIdempotencyKey(key string, paymentID string, fingerprint string) error {
+	s.mu.Lock()
+	defer s.mu.Unlock()
+	if existing, ok := s.idemKeys[key]; ok {
+		if existing.Fingerprint != fingerprint {
+			return ErrIdempotencyConflict
+		}
+		return nil
+	}
+	s.idemKeys[key] = struct {
+		PaymentID   string
+		Fingerprint string
+	}{PaymentID: paymentID, Fingerprint: fingerprint}
+	return nil
+}
+
+// GetPaymentByKey returns the payment ID and fingerprint for an idempotency key.
+func (s *Store) GetPaymentByKey(key string) (string, string, bool) {
+	s.mu.Lock()
+	defer s.mu.Unlock()
+	v, ok := s.idemKeys[key]
+	if !ok {
+		return "", "", false
+	}
+	return v.PaymentID, v.Fingerprint, true
+}
```

## IMPACT OF CURRENT CHANGES
### ledger-service
base: main
merge_base: 49aa98d8e9abac866f00bb19395a8d35087e4faf
direction: inbound
changed_total: 0
changed_returned: 0
changed_has_more: false
seed_symbols: 0
impacted_total: 0
impacted_total_relation: eq
impacted_shown: 0
impacted_has_more: false
module_total: 0
module_total_relation: eq
module_returned: 0
module_has_more: false
### payment-service
base: main
merge_base: b22ba3262b178acdd75552c51f0cbf33741ae9a6
direction: inbound
changed_total: 0
changed_returned: 0
changed_has_more: false
seed_symbols: 0
impacted_total: 0
impacted_total_relation: eq
impacted_shown: 0
impacted_has_more: false
module_total: 0
module_total_relation: eq
module_returned: 0
module_has_more: false

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
Review this change before merge. List concrete correctness or security problems (with file and line) that must be fixed, or state that it is acceptable.
