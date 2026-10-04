package payment

import "context"

// MemoryRepository is a simple repository for tests and local development.
// It is not safe for concurrent use.
type MemoryRepository struct {
	items []Payment
}

// Insert appends the payment.
func (m *MemoryRepository) Insert(_ context.Context, p Payment) error {
	m.items = append(m.items, p)
	return nil
}

// Get scans for the payment.
func (m *MemoryRepository) Get(_ context.Context, id string) (Payment, error) {
	for _, p := range m.items {
		if p.ID == id {
			return p, nil
		}
	}
	return Payment{}, ErrNotFound
}
