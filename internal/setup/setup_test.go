package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

func TestDiscoverRuntimeFindsThePinnedBuildLayouts(t *testing.T) {
	t.Setenv("BC_LLAMA_SERVER", "")
	t.Setenv("BC_PRISM_DIR", "")
	for _, name := range []string{"bin/llama-server", "llama-server"} {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			binary := filepath.Join(data, "runtime", "build", name)
			if err := os.MkdirAll(filepath.Dir(binary), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := DiscoverRuntime(data); got != binary {
				t.Fatalf("DiscoverRuntime() = %q, want %q", got, binary)
			}
		})
	}
}

func TestApplyAndValidateCreateACompleteLocalSetup(t *testing.T) {
	data := t.TempDir()
	models := filepath.Join(data, "models")
	if err := os.MkdirAll(models, 0o750); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(models, "bonsai.gguf")
	if err := os.WriteFile(model, []byte("model bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(data, "llama-server")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := Apply(context.Background(), data, Choice{
		DataDir: data, Profile: "bonsai-2-27b-8gb-cuda", Mode: config.ModeEmbedded,
		RuntimeBinary: runtimePath, ModelPath: model, ModelName: filepath.Base(model),
		BaseURL: "http://127.0.0.1:8080", ProviderModel: "bonsai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ConfigPath == "" || report.MarkerPath == "" {
		t.Fatalf("report did not name the generated files: %+v", report)
	}
	if !Completed(data) {
		t.Fatal("completed marker was not written")
	}
	checks, err := Validate(context.Background(), data)
	if err != nil {
		t.Fatalf("validation failed: %v\n%v", err, checks)
	}
	for _, check := range checks {
		if check.Name == "inference" && check.Status != "ok" {
			t.Fatalf("inference was not validated: %+v", checks)
		}
	}

	body, err := os.ReadFile(filepath.Join(data, "config", "bcode.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--alias") || !strings.Contains(string(body), "bonsai") {
		t.Fatalf("generated config did not retain a stable model alias:\n%s", body)
	}
}

func TestApplyExternalModeNeedsNoLocalModel(t *testing.T) {
	data := t.TempDir()
	report, err := Apply(context.Background(), data, Choice{
		DataDir: data, Profile: "external-inference", Mode: config.ModeExternal,
		BaseURL: "http://127.0.0.1:9090", ProviderModel: "local-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != string(config.ModeExternal) {
		t.Fatalf("mode = %q", report.Mode)
	}
	if _, err := Validate(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadModelIsAtomicAndCanBeValidated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downloaded model"))
	}))
	defer server.Close()
	data := t.TempDir()
	path, err := DownloadModel(context.Background(), data, server.URL, "model.gguf", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "downloaded model" {
		t.Fatalf("downloaded content = %q", body)
	}
	entries, err := os.ReadDir(filepath.Join(data, "models"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("download left %d model entries, want one", len(entries))
	}
}

func TestCredentialFileNeverUsesWorldReadablePermissions(t *testing.T) {
	data := t.TempDir()
	if err := StoreCredential(data, "TYPESAFE_API_KEY", "secret-value"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "config", CredentialFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o, want 600", info.Mode().Perm())
	}
	value, err := LoadCredential(data)
	if err != nil || value != "secret-value" {
		t.Fatalf("credential = %q, err=%v", value, err)
	}
}
