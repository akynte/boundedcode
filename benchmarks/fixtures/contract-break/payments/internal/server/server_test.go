package server

import (
	"context"
	"testing"

	paymentsv1 "example.com/shop/protos/payments/v1"
)

func TestChargeCapturesAndRefunds(t *testing.T) {
	s := New()
	resp, err := s.Charge(context.Background(), &paymentsv1.ChargeRequest{OrderId: "o1", AmountCents: 1250, Currency: "EUR"})
	if err != nil || resp.GetStatus() != paymentsv1.Status_STATUS_CAPTURED || resp.GetPaymentId() == "" {
		t.Fatalf("charge = %+v, %v", resp, err)
	}
	r, err := s.Refund(context.Background(), &paymentsv1.RefundRequest{PaymentId: resp.GetPaymentId()})
	if err != nil || !r.GetOk() {
		t.Fatalf("refund = %+v, %v", r, err)
	}
}

func TestChargeDeclinesNonPositiveAmounts(t *testing.T) {
	resp, err := New().Charge(context.Background(), &paymentsv1.ChargeRequest{OrderId: "o2", AmountCents: 0, Currency: "EUR"})
	if err != nil || resp.GetStatus() != paymentsv1.Status_STATUS_DECLINED {
		t.Fatalf("charge = %+v, %v", resp, err)
	}
}
