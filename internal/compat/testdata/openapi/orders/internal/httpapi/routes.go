// Package httpapi serves the orders API described by api/openapi.json.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Route is one served operation.
type Route struct{ Method, Path string }

// Routes lists the operations this service implements.
func Routes() []Route {
	return []Route{{"GET", "/v1/orders/{id}"}, {"POST", "/v1/orders"}}
}

// Register mounts the handlers.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/orders/{id}", getOrder)
	mux.HandleFunc("POST /v1/orders", createOrder)
}

func getOrder(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "total_cents": 0})
}

func createOrder(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }
