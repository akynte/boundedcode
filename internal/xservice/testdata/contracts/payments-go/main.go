package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	paymentsv1 "github.com/acme/protos/gen/go/payments/v1"
	"google.golang.org/grpc"
)

type server struct {
	paymentsv1.UnimplementedPaymentServiceServer
	db      *sql.DB
	refunds paymentsv1.RefundServiceClient
}

const insertPayment = `
	INSERT INTO payments (id, account_id, amount_cents)
	VALUES ($1, $2, $3)`

func (s *server) CreatePayment(ctx context.Context, req *paymentsv1.CreatePaymentRequest) (*paymentsv1.Payment, error) {
	_, err := s.db.ExecContext(ctx, insertPayment, "id", req.AccountId, req.AmountCents)
	log.Println("delete from cart failed") // prose, not a query
	return &paymentsv1.Payment{}, err
}

func (s *server) refund(ctx context.Context, id string) error {
	_, err := s.refunds.IssueRefund(ctx, &paymentsv1.IssueRefundRequest{PaymentId: id})
	return err
}

func main() {
	conn, _ := grpc.NewClient("refunds:443")
	s := grpc.NewServer()
	srv := &server{refunds: paymentsv1.NewRefundServiceClient(conn)}
	paymentsv1.RegisterPaymentServiceServer(s, srv)
	http.HandleFunc("GET /api/v1/refunds/{id}", func(w http.ResponseWriter, r *http.Request) {})
}
