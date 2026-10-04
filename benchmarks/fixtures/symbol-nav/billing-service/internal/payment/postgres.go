package payment

import (
	"context"
	"sync"
)

// PostgresRepository is the production repository. This fixture keeps rows in
// memory; the SQL it would run is kept next to each method.
type PostgresRepository struct {
	mu   sync.Mutex
	rows map[string]Payment
}

// NewPostgresRepository returns an empty repository.
func NewPostgresRepository() *PostgresRepository {
	return &PostgresRepository{rows: map[string]Payment{}}
}

// Insert runs: INSERT INTO payments (id, account_id, amount_cents, currency, status) VALUES (...)
func (r *PostgresRepository) Insert(_ context.Context, p Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[p.ID] = p
	return nil
}

// Get runs: SELECT id, account_id, amount_cents, currency, status FROM payments WHERE id = $1
func (r *PostgresRepository) Get(_ context.Context, id string) (Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.rows[id]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return p, nil
}
