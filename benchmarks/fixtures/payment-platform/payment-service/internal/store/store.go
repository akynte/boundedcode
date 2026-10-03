// Package store persists payments (table "payments", see migrations/001).
package store

import (
	"context"
	"errors"
	"sync"
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

// Store is an in-memory stand-in for the SQL store. The SQL it would run is
// kept next to each method so the schema relationship stays visible.
type Store struct {
	mu       sync.Mutex
	payments map[string]Payment
}

// New returns an empty store.
func New() *Store { return &Store{payments: map[string]Payment{}} }

// InsertPayment runs: INSERT INTO payments (id, account_id, amount_cents, currency, status) VALUES (...)
func (s *Store) InsertPayment(_ context.Context, p Payment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payments[p.ID] = p
	return nil
}

// GetPayment runs: SELECT id, account_id, amount_cents, currency, status FROM payments WHERE id = $1
func (s *Store) GetPayment(_ context.Context, id string) (Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.payments[id]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return p, nil
}
