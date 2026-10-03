package consumer

import (
	"testing"

	"example.com/ledger-service/internal/ledger"
)

func TestHandlePaymentCharged(t *testing.T) {
	l := &ledger.Ledger{}
	c := &Consumer{Ledger: l}
	msg := &ConsumerMessage{Topic: TopicPaymentCharged, Value: []byte(`{"payment_id":"p1","account_id":"a1","amount_cents":500,"currency":"EUR"}`)}
	if err := c.HandlePaymentCharged(msg); err != nil {
		t.Fatal(err)
	}
	if got := l.Balance("a1"); got != 500 {
		t.Fatalf("balance = %d", got)
	}
	if got := l.Balance(SettlementAccount); got != -500 {
		t.Fatalf("settlement = %d", got)
	}
}
