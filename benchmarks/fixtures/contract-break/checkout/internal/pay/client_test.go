package pay

import (
	"context"
	"testing"

	paymentsv1 "example.com/shop/protos/payments/v1"
)

// fakeConn answers Charge like the payments service does.
type fakeConn struct{ got *paymentsv1.ChargeRequest }

func (f *fakeConn) Invoke(_ context.Context, method string, args, reply any) error {
	req := args.(*paymentsv1.ChargeRequest)
	f.got = req
	out := reply.(*paymentsv1.ChargeResponse)
	out.PaymentId, out.Status = "pay_1", paymentsv1.Status_STATUS_CAPTURED
	return nil
}

func TestChargeOrderSendsTheTotal(t *testing.T) {
	conn := &fakeConn{}
	id, err := New(conn).ChargeOrder(context.Background(), "o1", 1250, "EUR")
	if err != nil || id != "pay_1" {
		t.Fatalf("ChargeOrder = %q, %v", id, err)
	}
	if conn.got.GetOrderId() != "o1" || conn.got.GetAmountCents() != 1250 || conn.got.GetCurrency() != "EUR" {
		t.Fatalf("request = %+v", conn.got)
	}
}

func TestFormatTotal(t *testing.T) {
	if got := FormatTotal(1250, "EUR"); got != "12.50 EUR" {
		t.Fatal(got)
	}
}
