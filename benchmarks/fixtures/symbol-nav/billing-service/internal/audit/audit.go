// Package audit records audit events. Its Insert method shares a name with
// payment.PaymentRepository.Insert but is unrelated.
package audit

import (
	"context"
	"fmt"
	"io"
)

// Event is one audit record.
type Event struct {
	Actor  string
	Action string
}

// Sink stores audit events.
type Sink interface {
	Insert(ctx context.Context, e Event) error
}

// FileLog writes events as lines.
type FileLog struct {
	W io.Writer
}

// Insert writes one event.
func (f *FileLog) Insert(_ context.Context, e Event) error {
	_, err := fmt.Fprintf(f.W, "%s %s\n", e.Actor, e.Action)
	return err
}
