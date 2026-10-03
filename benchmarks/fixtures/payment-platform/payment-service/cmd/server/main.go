// Command server runs payment-service.
package main

import (
	"log"
	"net/http"

	"example.com/payment-service/internal/api"
	"example.com/payment-service/internal/config"
	"example.com/payment-service/internal/events"
	"example.com/payment-service/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	h := &api.Handler{Store: store.New(), Events: &events.Publisher{Producer: &events.MemoryProducer{}}}
	mux := http.NewServeMux()
	h.Routes(mux)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
