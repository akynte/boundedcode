// Package consumer consumes payment events from Kafka.
package consumer

import (
	"encoding/json"
	"fmt"

	"example.com/ledger-service/internal/ledger"
)

// TopicPaymentCharged is consumed by this service.
const TopicPaymentCharged = "payments.charged"

// ConsumerMessage mirrors sarama.ConsumerMessage.
type ConsumerMessage struct {
	Topic string
	Key   []byte
	Value []byte
}

// PaymentCharged mirrors the producer's payload (shared-protos PaymentCharged).
type PaymentCharged struct {
	PaymentID   string `json:"payment_id"`
	AccountID   string `json:"account_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// SettlementAccount receives the debit side of every charge.
const SettlementAccount = "settlement"

// Consumer applies payment events to the ledger.
type Consumer struct{ Ledger *ledger.Ledger }

// HandlePaymentCharged posts a charge as a balanced pair of entries.
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
