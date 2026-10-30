package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetOrder(t *testing.T) {
	s := Store{"o1": {ID: "o1", Items: []Item{{SKU: "A", Qty: 2}}, TotalCents: 500}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/orders/o1", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total_cents":500`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
