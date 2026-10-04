package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/mux"
)

const ordersPath = "/orders"

func Std(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /v1/payments", h.Create)
	mux.HandleFunc("GET /v1/payments/{id}", h.Get)
	mux.Handle("/healthz", h)
	mux.HandleFunc("GET api.example.com/v1/status/{$}", h.Status)
}

func Chi(r chi.Router, h *Handler) {
	r.Route("/v2", func(r chi.Router) {
		r.Post(ordersPath, h.Create)
		r.Get(ordersPath+"/{orderID}", h.Get)
	})
}

func Gin(e *gin.Engine, h *Handler) {
	v1 := e.Group("/api/v1")
	admin := v1.Group("/admin")
	v1.POST("/refunds", h.Refund)
	admin.DELETE("/users/:id", h.Delete)
}

func Gorilla(r *mux.Router, h *Handler) {
	r.HandleFunc("/v3/items/{id}", h.Get).Methods("GET", "HEAD")
	s := r.PathPrefix("/v4").Subrouter()
	s.HandleFunc("/things", h.Create).Methods(http.MethodPost)
}

type Handler struct{}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request)    {}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request)       {}
func (h *Handler) Status(w http.ResponseWriter, r *http.Request)    {}
func (h *Handler) Refund(c *gin.Context)                            {}
func (h *Handler) Delete(c *gin.Context)                            {}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
