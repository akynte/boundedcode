package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagedServerHelper(t *testing.T) {
	helper := false
	port := ""
	for i, arg := range os.Args {
		if arg == "--managed-helper" {
			helper = true
		}
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	if !helper {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestManagedProfilesUnloadBeforeSwap(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	listener.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	file.Close()
	makeProvider := func(label string) Provider {
		p, err := build(ProviderSpec{Name: label, Kind: KindLlamaCPP, BaseURL: endpoint, Process: &ServerProcess{Argv: []string{binary, "-test.run=TestManagedServerHelper", "--", "--managed-helper", label}, BinarySHA256: hex.EncodeToString(hash.Sum(nil)), ReadyTimeoutSeconds: 5}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	daily, deep := makeProvider("daily"), makeProvider("deep")
	defer daily.Close()
	defer deep.Close()
	if _, err := daily.Chat(context.Background(), ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	first := localSlot.cmd.Process.Pid
	if _, err := deep.Chat(context.Background(), ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	second := localSlot.cmd.Process.Pid
	if first == second || syscall.Kill(first, 0) != syscall.ESRCH {
		t.Fatal("previous model still resident after swap")
	}
	if _, err := deep.Chat(context.Background(), ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if localSlot.cmd.Process.Pid != second {
		t.Fatal("unchanged profile restarted")
	}
	if err := deep.Close(); err != nil {
		t.Fatal(err)
	}
	if localSlot.cmd != nil {
		t.Fatal("gateway leaked its model process")
	}
}

func TestManagedProfileRejectsUnpinnedOrRemoteServer(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:9000", "http://example.com:9000"} {
		_, err := build(ProviderSpec{Name: "bad", Kind: KindLlamaCPP, BaseURL: endpoint, Process: &ServerProcess{Argv: []string{"/bin/false"}, BinarySHA256: strings.Repeat("0", 1)}})
		if err == nil {
			t.Fatal("unsafe managed profile accepted")
		}
	}
}

// A managed provider must advertise its own startup budget.
//
// The recorded failure: cmd/bcode bounded the pre-flight Health probe at 15s.
// For a managed provider Health *starts the server*, and prepare derives its
// ready deadline from that same context — so ready_timeout_seconds: 600 was
// capped to 15s and an 18.6 GB model could not load from a cold page cache.
// Two tasks in an evidence run died on it before the model was warm enough to
// start in time.
func TestManagedProviderAdvertisesItsStartupBudget(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	p, err := build(ProviderSpec{
		Name: "big", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:9", Model: "m",
		Process: &ServerProcess{
			Argv:         []string{bin},
			BinarySHA256: hex.EncodeToString(sum[:]),
			// The operator's configured budget: minutes, for a large model.
			ReadyTimeoutSeconds: 600,
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	b, ok := p.(interface{ StartupBudget() time.Duration })
	if !ok {
		t.Fatal("a managed provider does not advertise a startup budget; " +
			"callers will bound Health with their own short timeout")
	}
	if got := b.StartupBudget(); got != 600*time.Second {
		t.Errorf("StartupBudget = %v, want 600s (the configured ready_timeout_seconds)", got)
	}
}

// An external provider advertises nothing, so a caller keeps its own bound.
func TestAnExternalProviderHasNoStartupBudget(t *testing.T) {
	var p Provider = &stubProvider{}
	_ = p
	if _, ok := p.(interface{ StartupBudget() time.Duration }); ok {
		t.Error("an unmanaged provider claims a startup budget; nothing is being started")
	}
}

// stubProvider is the minimum a managed provider can wrap.
type stubProvider struct{}

func (*stubProvider) Name() string                 { return "stub" }
func (*stubProvider) Capabilities() Capabilities   { return Capabilities{} }
func (*stubProvider) Health(context.Context) error { return nil }
func (*stubProvider) Close() error                 { return nil }
func (*stubProvider) Chat(context.Context, ChatRequest) (*ChatResponse, error) {
	return nil, &UnsupportedError{}
}
func (*stubProvider) ChatStructured(context.Context, ChatRequest, json.RawMessage) (*ChatResponse, error) {
	return nil, &UnsupportedError{}
}
func (*stubProvider) Embed(context.Context, EmbedRequest) (*EmbedResponse, error) {
	return nil, &UnsupportedError{}
}
func (*stubProvider) Infill(context.Context, InfillRequest) (*ChatResponse, error) {
	return nil, &UnsupportedError{}
}
