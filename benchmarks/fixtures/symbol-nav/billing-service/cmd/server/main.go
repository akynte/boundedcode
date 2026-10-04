// Command server runs the billing API.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"

	"example.com/billing/internal/api"
	"example.com/billing/internal/payment"
)

func main() {
	svc := payment.NewService(payment.NewPostgresRepository(), newID)
	mux := http.NewServeMux()
	(&api.Handler{Payments: svc}).Routes(mux)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
