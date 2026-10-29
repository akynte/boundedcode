package direct

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/inference"
)

// BenchmarkReplyCalls measures one model reply's tool calls run in turn
// (parallel_tools 1) and together (4) on real tools: reads, searches and
// symbol lookups over Go's standard library source, and fetches from a
// local server that answers after 200 ms (a remote page's latency).
//
//	go test ./internal/direct/ -run '^$' -bench ReplyCalls -benchtime 5x
func BenchmarkReplyCalls(b *testing.B) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		b.Skip("go")
	}
	src := filepath.Join(strings.TrimSpace(string(out)), "src")
	if _, err := os.Stat(filepath.Join(src, "net", "http")); err != nil {
		b.Skip("no Go source tree")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = fmt.Fprint(w, "<html><body><p>release notes</p></body></html>")
	}))
	defer srv.Close()
	read := func(p string) call { return call{"read", map[string]any{"path": p}} }
	sets := []struct {
		name  string
		root  string
		calls []call
	}{
		{"read6", filepath.Join(src, "net", "http"), []call{read("server.go"), read("client.go"), read("transport.go"), read("request.go"), read("response.go"), read("header.go")}},
		{"grep4", src, []call{
			{"grep", map[string]any{"pattern": "func NewRequestWithContext", "output_mode": "files"}},
			{"grep", map[string]any{"pattern": "ErrBodyNotAllowed", "output_mode": "files"}},
			{"grep", map[string]any{"pattern": "type Transport struct", "output_mode": "files"}},
			{"grep", map[string]any{"pattern": "func Marshal\\(", "output_mode": "files"}},
		}},
		{"symbols4", filepath.Join(src, "net"), []call{
			{"find_symbol", map[string]any{"name": "ServeHTTP"}},
			{"find_symbol", map[string]any{"name": "RoundTrip"}},
			{"find_symbol", map[string]any{"name": "Dial"}},
			{"find_symbol", map[string]any{"name": "ParseIP"}},
		}},
		{"fetch4", b.TempDir(), []call{
			{"web_fetch", map[string]any{"url": srv.URL + "/a"}},
			{"web_fetch", map[string]any{"url": srv.URL + "/b"}},
			{"web_fetch", map[string]any{"url": srv.URL + "/c"}},
			{"web_fetch", map[string]any{"url": srv.URL + "/d"}},
		}},
	}
	for _, set := range sets {
		for _, parallel := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/parallel%d", set.name, parallel), func(b *testing.B) {
				for range b.N {
					up := &scripted{replies: []reply{{calls: set.calls}, {text: "done"}}}
					r := &Runner{
						Opts:    Options{Root: set.root, Approval: ApprovalAuto, ParallelTools: parallel, NoChecks: true, NoSeedPack: true, NoHistoryNotes: true, Retrieval: "off"},
						Session: &Session{},
					}
					r.Gateway = &inference.Gateway{Upstream: up, Model: "test"}
					r.Opts.Web.AllowHosts = []string{"127.0.0.1"}
					if _, err := r.Send(context.Background(), "go"); err != nil {
						b.Fatal(err)
					}
					r.Close()
				}
			})
		}
	}
}
