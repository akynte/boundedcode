// Package server is the shop's HTTP API.
package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Item is one line of an order.
type Item struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// Order is an order as the API returns it (api/openapi.json).
type Order struct {
	ID         string `json:"id"`
	Items      []Item `json:"items"`
	TotalCents int    `json:"total_cents"`
}

// Store holds the orders.
type Store map[string]Order

// Handler serves GET /orders/{id}.
func (s Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/orders/")
		o, ok := s[id]
		if r.Method != http.MethodGet || !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(o)
	})
}
