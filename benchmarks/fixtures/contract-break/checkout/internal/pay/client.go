// Package pay charges orders through the payments service.
package pay

import (
	"context"
	"fmt"

	paymentsv1 "example.com/shop/protos/payments/v1"
)

// Client is checkout's view of the payments service.
type Client struct {
	api paymentsv1.PaymentServiceClient
}

// New returns a client over a gRPC connection.
func New(cc paymentsv1.ClientConnInterface) *Client {
	return &Client{api: paymentsv1.NewPaymentServiceClient(cc)}
}

// ChargeOrder charges an order total and returns the payment id.
func (c *Client) ChargeOrder(ctx context.Context, orderID string, totalCents int64, currency string) (string, error) {
	resp, err := c.api.Charge(ctx, &paymentsv1.ChargeRequest{OrderId: orderID, AmountCents: totalCents, Currency: currency})
	if err != nil {
		return "", fmt.Errorf("charge order %s: %w", orderID, err)
	}
	if resp.GetStatus() != paymentsv1.Status_STATUS_CAPTURED {
		return "", fmt.Errorf("charge order %s: payment status %d", orderID, resp.GetStatus())
	}
	return resp.GetPaymentId(), nil
}

// FormatTotal renders an order total for receipts.
func FormatTotal(cents int64, currency string) string {
	return fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency)
}
