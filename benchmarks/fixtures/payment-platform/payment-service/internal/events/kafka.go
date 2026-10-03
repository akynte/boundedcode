// Package events publishes domain events to Kafka. The producer interface
// mirrors sarama's SyncProducer so the real client can be dropped in.
package events

import (
	"encoding/json"
	"sync"
)

// TopicPaymentCharged carries PaymentCharged events (shared-protos/proto/payment.proto).
const TopicPaymentCharged = "payments.charged"

// ProducerMessage mirrors sarama.ProducerMessage.
type ProducerMessage struct {
	Topic string
	Key   string
	Value []byte
}

// SyncProducer mirrors sarama.SyncProducer.
type SyncProducer interface {
	SendMessage(msg *ProducerMessage) (partition int32, offset int64, err error)
}

// PaymentCharged is the event payload.
type PaymentCharged struct {
	PaymentID   string `json:"payment_id"`
	AccountID   string `json:"account_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// Publisher publishes payment events.
type Publisher struct{ Producer SyncProducer }

// PaymentCharged publishes to topic "payments.charged".
func (p *Publisher) PaymentCharged(ev PaymentCharged) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, _, err = p.Producer.SendMessage(&ProducerMessage{Topic: TopicPaymentCharged, Key: ev.AccountID, Value: b})
	return err
}

// MemoryProducer records messages (tests and local runs).
type MemoryProducer struct {
	mu   sync.Mutex
	Sent []*ProducerMessage
}

// SendMessage implements SyncProducer.
func (m *MemoryProducer) SendMessage(msg *ProducerMessage) (int32, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, msg)
	return 0, int64(len(m.Sent) - 1), nil
}
