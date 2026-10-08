// Package server implements shop.payments.v1.PaymentService.
package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	paymentsv1 "example.com/shop/protos/payments/v1"
)

// Server captures payments in memory.
type Server struct {
	mu       sync.Mutex
	next     int
	captured map[string]int64 // payment id -> amount in cents
}

// New returns an empty server.
func New() *Server { return &Server{captured: map[string]int64{}} }

// Register exposes the server on a gRPC registrar.
func Register(r paymentsv1.ServiceRegistrar, s *Server) {
	paymentsv1.RegisterPaymentServiceServer(r, s)
}

// Charge captures a payment for an order.
func (s *Server) Charge(_ context.Context, req *paymentsv1.ChargeRequest) (*paymentsv1.ChargeResponse, error) {
	if req.GetOrderId() == "" {
		return nil, errors.New("order_id is required")
	}
	if req.GetAmountCents() <= 0 || req.GetCurrency() == "" {
		return &paymentsv1.ChargeResponse{Status: paymentsv1.Status_STATUS_DECLINED}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := fmt.Sprintf("pay_%d", s.next)
	s.captured[id] = req.GetAmountCents()
	return &paymentsv1.ChargeResponse{PaymentId: id, Status: paymentsv1.Status_STATUS_CAPTURED}, nil
}

// Refund releases a captured payment.
func (s *Server) Refund(_ context.Context, req *paymentsv1.RefundRequest) (*paymentsv1.RefundResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.captured[req.GetPaymentId()]; !ok {
		return &paymentsv1.RefundResponse{Ok: false}, nil
	}
	delete(s.captured, req.GetPaymentId())
	return &paymentsv1.RefundResponse{Ok: true}, nil
}
