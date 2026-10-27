package api

import (
	"encoding/json"
	"net/http"
)

// Store looks orders up by id.
type Store interface {
	Order(id string) (Order, bool)
}

// SummaryHandler serves GET /orders/{id}/summary.
func SummaryHandler(s Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.Order(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Summarize(o))
	})
}
