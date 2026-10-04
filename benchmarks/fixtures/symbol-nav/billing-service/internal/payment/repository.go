// Package payment holds the payment domain: the repository abstraction, its
// implementations and the service that creates payments.
package payment

import (
	"context"
	"errors"
)

// Payment is a stored payment.
type Payment struct {
	ID          string
	AccountID   string
	AmountCents int64
	Currency    string
	Status      string
}

// ErrNotFound is returned for unknown payments.
var ErrNotFound = errors.New("payment not found")

// PaymentRepository persists payments.
type PaymentRepository interface {
	// Insert stores a new payment.
	Insert(ctx context.Context, p Payment) error
	// Get loads a payment by id.
	Get(ctx context.Context, id string) (Payment, error)
}
