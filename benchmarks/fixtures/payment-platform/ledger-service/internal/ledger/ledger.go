// Package ledger implements double-entry postings (table "ledger_entries").
package ledger

import (
	"errors"
	"sync"
)

// Entry is one side of a posting.
type Entry struct {
	PaymentID   string
	AccountID   string
	AmountCents int64 // positive = credit, negative = debit
	Currency    string
}

// Ledger stores entries in memory (stand-in for SQL).
type Ledger struct {
	mu      sync.Mutex
	entries []Entry
}

// ErrUnbalanced is returned when entries do not sum to zero.
var ErrUnbalanced = errors.New("unbalanced posting")

// Post records a balanced set of entries:
// INSERT INTO ledger_entries (payment_id, account_id, amount_cents, currency) VALUES ...
func (l *Ledger) Post(entries ...Entry) error {
	var sum int64
	for _, e := range entries {
		sum += e.AmountCents
	}
	if sum != 0 {
		return ErrUnbalanced
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entries...)
	return nil
}

// Balance returns the balance of an account:
// SELECT COALESCE(SUM(amount_cents),0) FROM ledger_entries WHERE account_id = $1
func (l *Ledger) Balance(accountID string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b int64
	for _, e := range l.entries {
		if e.AccountID == accountID {
			b += e.AmountCents
		}
	}
	return b
}

// Entries returns a copy of all entries.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.entries...)
}
