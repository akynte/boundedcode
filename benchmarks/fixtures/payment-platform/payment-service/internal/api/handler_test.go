package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/payment-service/internal/events"
	"example.com/payment-service/internal/store"
)

func newTestServer() (*http.ServeMux, *events.MemoryProducer) {
	prod := &events.MemoryProducer{}
	h := &Handler{Store: store.New(), Events: &events.Publisher{Producer: prod}}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, prod
}

func TestCreatePayment(t *testing.T) {
	mux, prod := newTestServer()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/payments", strings.NewReader(`{"account_id":"acc_1","amount_cents":500,"currency":"EUR"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(prod.Sent) != 1 || prod.Sent[0].Topic != events.TopicPaymentCharged {
		t.Fatalf("events = %+v", prod.Sent)
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	mux, _ := newTestServer()
	for _, body := range []string{`{`, `{"account_id":"","amount_cents":5,"currency":"EUR"}`, `{"account_id":"a","amount_cents":0,"currency":"EUR"}`} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/payments", strings.NewReader(body)))
		if rec.Code < 400 {
			t.Errorf("%s: status %d", body, rec.Code)
		}
	}
}
