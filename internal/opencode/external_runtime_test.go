package opencode_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/opencode"
)

func TestValidateExternalRuntimeDoesNotRequirePrismProps(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	config := `{"model":"boundedcode-local/local-model","providers":{"boundedcode-local":{"models":{"local-model":{"limit":{"context":32768,"output":8192}}}}},"compaction":{"buffer":12000,"keep":{"tokens":4000}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	budget, err := opencode.ValidateExternalRuntime(context.Background(), dir, server.URL)
	if err != nil {
		t.Fatalf("generic external endpoint was rejected: %v", err)
	}
	if budget.AdvertisedContext != 32768 || budget.OutputReserve != 8192 {
		t.Fatalf("external limits were not retained: %+v", budget)
	}
}
