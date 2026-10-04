// Package api exposes the payment service over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"example.com/billing/internal/payment"
)

// Handler serves the payments API.
type Handler struct {
	Payments *payment.Service
}

// Routes registers the handlers.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/payments", h.Create)
}

// Create handles POST /v1/payments.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req payment.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	p, err := h.Payments.CreatePayment(r.Context(), req)
	if errors.Is(err, payment.ErrInvalid) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}
