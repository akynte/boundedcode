package inference

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReachable(t *testing.T) {
	// A server without /health is still reachable.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := NewClient(srv.URL, time.Second).Reachable(t.Context()); err != nil {
		t.Fatalf("answering server: %v", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String()
	_ = l.Close()
	if err := NewClient(url, time.Second).Reachable(t.Context()); err == nil {
		t.Fatal("closed port reported reachable")
	}
}
