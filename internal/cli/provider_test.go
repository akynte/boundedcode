package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/model"
)

// runCLI runs one command with stdin and returns its output.
func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	app := &App{Out: &out, Err: &out, Log: newLogger(io.Discard, false)}
	root := newRoot(app)
	root.SetArgs(args)
	root.SetIn(strings.NewReader(stdin))
	err := root.ExecuteContext(context.Background())
	app.close()
	return out.String(), err
}

// fakeAnthropic answers the Models API and the Messages API, and records
// the API keys it saw.
func fakeAnthropic(t *testing.T, keys *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*keys = append(*keys, r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") != "sk-ant-good-0123456789" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/models/"):
			_, _ = io.WriteString(w, `{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","max_input_tokens":1000000,"max_tokens":128000,
				"capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true}}}}}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{}}],"has_more":false}`)
		default:
			_, _ = io.WriteString(w, `{"id":"m","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":2}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProviderCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOUNDEDCODE_HOME", home)
	t.Setenv("BOUNDEDCODE_SECRETS", "file") // never touch the developer's keychain
	var keys []string
	srv := fakeAnthropic(t, &keys)

	if _, err := runCLI(t, "", "setup", "--only", "config"); err != nil {
		t.Fatal(err)
	}
	// Selecting a cloud provider needs a model.
	if _, err := runCLI(t, "", "provider", "use", "anthropic"); err == nil || !strings.Contains(err.Error(), "model: required") {
		t.Fatalf("use without model: %v", err)
	}
	out, err := runCLI(t, "", "provider", "use", "claude", "--model", "claude-opus-5-5", "--base-url", srv.URL+"/")
	if err != nil || !strings.Contains(out, "anthropic claude-opus-5-5") || !strings.Contains(out, "no API key") {
		t.Fatalf("use: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "", "provider", "test"); err == nil || !strings.Contains(err.Error(), "no API key for anthropic") {
		t.Fatalf("test without key: %v", err)
	}
	// A rejected key is reported with its fix.
	if _, err := runCLI(t, "sk-ant-bad-0123456789\n", "provider", "key", "set", "anthropic"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "provider", "test"); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("bad key: %v", err)
	}
	if out, err = runCLI(t, "sk-ant-good-0123456789\n", "provider", "key", "set", "anthropic"); err != nil || !strings.Contains(out, "owner-only file") {
		t.Fatalf("key set: %v\n%s", err, out)
	}
	out, err = runCLI(t, "", "provider", "test")
	if err != nil || !strings.Contains(out, `"OK"`) {
		t.Fatalf("test: %v\n%s", err, out)
	}
	if out, err = runCLI(t, "", "provider", "models"); err != nil || !strings.Contains(out, "claude-opus-5-5  (context 1000000") {
		t.Fatalf("models: %v\n%s", err, out)
	}
	out, err = runCLI(t, "", "doctor")
	if !strings.Contains(out, "working context 200000 tokens") || strings.Contains(out, "llama-server") {
		t.Fatalf("doctor in cloud mode: %v\n%s", err, out)
	}
	// The key is in neither the config file nor any output.
	cfg, err := os.ReadFile(home + "/config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "sk-ant") {
		t.Fatal("API key written to config.yaml")
	}
	out, _ = runCLI(t, "", "--json", "provider", "show")
	if strings.Contains(out, "sk-ant") {
		t.Fatalf("key in provider show: %s", out)
	}
	var rows []providerRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, r := range rows {
		if r.Name == "anthropic" && (!r.Selected || r.KeySource != "file") {
			t.Fatalf("row = %+v", r)
		}
	}
	// Back to local; the anthropic settings are kept.
	if _, err := runCLI(t, "", "provider", "use", "local"); err != nil {
		t.Fatal(err)
	}
	if out, _ = runCLI(t, "", "provider", "show"); !strings.Contains(out, "▸ local") || !strings.Contains(out, "claude-opus-5-5") {
		t.Fatalf("show after switching back:\n%s", out)
	}
	if _, err := runCLI(t, "", "provider", "key", "delete", "anthropic"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home + "/config/credentials.json"); !os.IsNotExist(err) {
		t.Fatalf("credentials file left after delete: %v", err)
	}
	for _, k := range keys {
		if k != "sk-ant-good-0123456789" && k != "sk-ant-bad-0123456789" {
			t.Fatalf("unexpected key sent: %q", k)
		}
	}
}

// TestModelCommands: fetch (from a fake hub, sha256-verified), use, list
// and remove, with a user profile so no real weights are involved.
func TestModelCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BOUNDEDCODE_HOME", home)
	data := []byte(strings.Repeat("tiny-gguf", 1000))
	sum := sha256.Sum256(data)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/resolve/0123456789abcdef0123456789abcdef01234567/tiny.gguf") {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()
	old := model.HuggingFaceURL
	model.HuggingFaceURL = hub.URL
	defer func() { model.HuggingFaceURL = old }()

	if _, err := runCLI(t, "", "setup", "--only", "config"); err != nil {
		t.Fatal(err)
	}
	prof := fmt.Sprintf("name: tiny\ndisplay_name: Tiny\nfile: tiny.gguf\nstatus: experimental\nsource:\n  repo: org/tiny\n  file: tiny.gguf\n"+
		"  revision: 0123456789abcdef0123456789abcdef01234567\n  license: apache-2.0\n  size_bytes: %d\n  sha256: %s\n", len(data), hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(home+"/config/models", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home+"/config/models/tiny.yaml", []byte(prof), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "model", "fetch", "tiny", "--yes")
	if err != nil || !strings.Contains(out, "downloaded and verified") || !strings.Contains(out, "licensed apache-2.0") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(home + "/data/models/tiny.gguf"); string(got) != string(data) {
		t.Fatal("weights not in the data models dir")
	}
	if out, err = runCLI(t, "", "model", "use", "tiny"); err != nil || !strings.Contains(out, "default model: tiny") {
		t.Fatalf("use: %v\n%s", err, out)
	}
	out, _ = runCLI(t, "", "--json", "model", "list")
	var rows []modelRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	found := false
	for _, r := range rows {
		if r.Name == "tiny" {
			found = r.Present && r.Default && r.Fit != "" && r.SizeBytes == int64(len(data))
		}
	}
	if !found {
		t.Fatalf("tiny row: %s", out)
	}
	if _, err = runCLI(t, "", "model", "fetch", "laguna-xs-2.1", "--yes"); err == nil || !strings.Contains(err.Error(), "under review") {
		t.Fatalf("license-review profile fetched: %v", err)
	}
	if out, err = runCLI(t, "", "model", "remove", "tiny", "--yes"); err != nil || !strings.Contains(out, "deleted") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
}
