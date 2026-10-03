// Package api exposes the payment HTTP API.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"example.com/payment-service/internal/events"
	"example.com/payment-service/internal/store"
)

// Handler serves /v1/payments.
type Handler struct {
	Store  *store.Store
	Events *events.Publisher
}

// CreatePaymentRequest is the JSON body of POST /v1/payments.
type CreatePaymentRequest struct {
	AccountID   string `json:"account_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// Routes registers the HTTP routes.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/payments", h.CreatePayment)
	mux.HandleFunc("GET /v1/payments/{id}", h.GetPayment)
}

// CreatePayment handles POST /v1/payments.
func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.AccountID == "" || req.AmountCents <= 0 || len(req.Currency) != 3 {
		http.Error(w, "invalid payment", http.StatusUnprocessableEntity)
		return
	}
	p := store.Payment{ID: newID(), AccountID: req.AccountID, AmountCents: req.AmountCents, Currency: req.Currency, Status: "charged"}
	if err := h.Store.InsertPayment(r.Context(), p); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if err := h.Events.PaymentCharged(events.PaymentCharged{PaymentID: p.ID, AccountID: p.AccountID, AmountCents: p.AmountCents, Currency: p.Currency}); err != nil {
		http.Error(w, "publish error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

// GetPayment handles GET /v1/payments/{id}.
func (h *Handler) GetPayment(w http.ResponseWriter, r *http.Request) {
	p, err := h.Store.GetPayment(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "pay_" + hex.EncodeToString(b[:])
}
