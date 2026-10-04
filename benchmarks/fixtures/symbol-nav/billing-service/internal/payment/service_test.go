package payment

import (
	"context"
	"errors"
	"testing"
)

func TestCreatePayment(t *testing.T) {
	for _, repo := range []PaymentRepository{&MemoryRepository{}, NewPostgresRepository()} {
		svc := NewService(repo, func() string { return "p1" })
		p, err := svc.CreatePayment(context.Background(), CreateRequest{AccountID: "a", AmountCents: 100, Currency: "EUR"})
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetPayment(context.Background(), p.ID)
		if err != nil || got.Status != "pending" {
			t.Fatalf("got %+v, %v", got, err)
		}
		if _, err := svc.CreatePayment(context.Background(), CreateRequest{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	}
}
