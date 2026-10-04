package payment

import (
	"context"
	"errors"
	"fmt"
)

// CreateRequest is the input of CreatePayment.
type CreateRequest struct {
	AccountID   string
	AmountCents int64
	Currency    string
}

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid payment request")

// Service implements payment use cases on top of a PaymentRepository.
type Service struct {
	repo  PaymentRepository
	newID func() string
}

// NewService returns a service storing payments in repo.
func NewService(repo PaymentRepository, newID func() string) *Service {
	return &Service{repo: repo, newID: newID}
}

// CreatePayment validates the request and stores a pending payment.
func (s *Service) CreatePayment(ctx context.Context, req CreateRequest) (Payment, error) {
	if req.AccountID == "" || req.AmountCents <= 0 || len(req.Currency) != 3 {
		return Payment{}, fmt.Errorf("%w: %+v", ErrInvalid, req)
	}
	p := Payment{ID: s.newID(), AccountID: req.AccountID, AmountCents: req.AmountCents, Currency: req.Currency, Status: "pending"}
	if err := s.repo.Insert(ctx, p); err != nil {
		return Payment{}, fmt.Errorf("store payment: %w", err)
	}
	return p, nil
}

// GetPayment loads a payment.
func (s *Service) GetPayment(ctx context.Context, id string) (Payment, error) {
	return s.repo.Get(ctx, id)
}
