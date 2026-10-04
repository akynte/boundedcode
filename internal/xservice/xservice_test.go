package xservice

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func keys(eps []Endpoint, k Kind) []string {
	var out []string
	for _, e := range eps {
		if e.Kind == k {
			out = append(out, e.Key())
		}
	}
	sort.Strings(out)
	return out
}

func mustScan(t *testing.T, repo, dir string) []Endpoint {
	t.Helper()
	eps, _, err := Scan(repo, dir, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return eps
}

func assertSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got: %q\nwant: %q", what, got, want)
	}
}

func TestGoRoutesAndClients(t *testing.T) {
	eps := mustScan(t, "goroutes", "testdata/goroutes")
	assertSet(t, "routes", keys(eps, HTTPRoute), []string{
		"POST /v1/payments", "GET /v1/payments/{}", "ANY /healthz", "GET /v1/status",
		"POST /v2/orders", "GET /v2/orders/{}",
		"POST /api/v1/refunds", "DELETE /api/v1/admin/users/{}",
		"GET /v3/items/{}", "HEAD /v3/items/{}", "POST /v4/things",
	})
	assertSet(t, "calls", keys(eps, HTTPCall), []string{
		"POST /v1/payments", "GET /v1/payments/{}", "GET /v2/orders/{}", "GET /stock",
	})
	assertSet(t, "env", keys(eps, EnvRead), []string{
		"env PAYMENTS_URL", "env PAYMENTS_TIMEOUT", "env INVENTORY_TOKEN_FILE", "env REGION",
	})
	for _, e := range eps {
		if e.Kind == HTTPRoute && e.Key() == "POST /v1/payments" && e.Symbol != "h.Create" {
			t.Errorf("route symbol = %q", e.Symbol)
		}
	}
}

func TestGoKafka(t *testing.T) {
	eps := mustScan(t, "gokafka", "testdata/gokafka")
	assertSet(t, "produce", keys(eps, TopicProduce), []string{"topic orders.created", "topic audit.log", "topic shipments", "topic invoices.dlq"})
	assertSet(t, "consume", keys(eps, TopicConsume), []string{
		"topic payments.charged", "topic refunds", "topic orders.created", "topic shipments", "topic returns", "topic invoices",
	})
}

func TestJS(t *testing.T) {
	eps := mustScan(t, "jsapp", "testdata/jsapp")
	assertSet(t, "routes", keys(eps, HTTPRoute), []string{"POST /api/carts/{}/checkout", "GET /healthz", "POST /orders", "GET /orders/{}"})
	assertSet(t, "calls", keys(eps, HTTPCall), []string{"POST /v1/payments", "GET /v1/payments/{}", "DELETE /v2/orders/{}", "GET /v1/stock/{}"})
	assertSet(t, "produce", keys(eps, TopicProduce), []string{"topic orders.created"})
	assertSet(t, "consume", keys(eps, TopicConsume), []string{"topic payments.charged", "topic refunds"})
	assertSet(t, "env", keys(eps, EnvRead), []string{"env PAYMENTS_URL", "env LOG_LEVEL"})
}

func TestDeployConfig(t *testing.T) {
	eps := mustScan(t, "deploy", "testdata/deploy")
	assertSet(t, "provide", keys(eps, EnvProvide), []string{
		"env PAYMENTS_URL", "env REGION", "env LOG_LEVEL", "env PAYMENTS_TIMEOUT", "env INVENTORY_TOKEN_FILE",
		"env REGION", "env FEATURE_X", "env LEGACY_MODE",
	})
	assertSet(t, "provision", keys(eps, TopicProvision), []string{"topic orders.created"})
}

func TestNormalizeAndMatch(t *testing.T) {
	cases := map[string]string{
		"http://h:80/v1/x?y=1": "/v1/x", "{}/v1/x/{}": "/v1/x/{}", "/a/:id/b": "/a/{}/b", "/a/{id}/": "/a/{}",
		"/files/{path...}": "/files/{}", "relative": "", "/x/{$}": "/x", "https://h": "/",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
	if !pathsMatch("/v1/payments/{}", "/v1/payments/abc") || pathsMatch("/v1/payments", "/v1/payments/x") || !pathsMatch("/v1/{}", "/v1/{}") {
		t.Fatal("pathsMatch wrong")
	}
}

func TestLinkAcrossVariants(t *testing.T) {
	var all []Endpoint
	for _, r := range []string{"goroutes", "gokafka", "jsapp", "deploy"} {
		all = append(all, mustScan(t, r, "testdata/"+r)...)
	}
	links := LinkAll(all, LinkOptions{})
	got := map[string]bool{}
	for _, l := range links {
		got[fmt.Sprintf("%s %s %s->%s", l.Kind, l.Contract, l.From.Repo, l.To.Repo)] = true
	}
	for _, want := range []string{
		"http POST /v1/payments jsapp->goroutes",
		"http GET /v1/payments/{} jsapp->goroutes",
		"topic topic orders.created jsapp->gokafka",
		"topic topic orders.created gokafka->gokafka", // excluded: same repo
		"topic_infra topic orders.created deploy->gokafka",
		"env env PAYMENTS_URL goroutes->deploy",
		"env env LOG_LEVEL jsapp->deploy",
	} {
		if strings.HasSuffix(want, "gokafka->gokafka") {
			if got[want] {
				t.Errorf("unexpected same-repo topic link %s", want)
			}
			continue
		}
		if !got[want] {
			t.Errorf("missing link %q", want)
		}
	}
	// payments.charged is consumed by both gokafka and jsapp but produced by
	// neither: no topic link may be invented.
	for k := range got {
		if strings.HasPrefix(k, "topic topic payments.charged") && !strings.HasSuffix(k, "->jsapp") && !strings.HasSuffix(k, "->gokafka") {
			t.Errorf("bogus link %s", k)
		}
	}
}

// TestPaymentPlatformGroundTruth checks the fixture's documented
// cross-service relationships (benchmarks/fixtures/payment-platform/README.md),
// none of which codebase-memory-mcp v0.11.0 links (gap report).
func TestPaymentPlatformGroundTruth(t *testing.T) {
	var all []Endpoint
	for _, r := range []string{"gateway", "payment-service", "ledger-service", "infrastructure", "shared-protos"} {
		all = append(all, mustScan(t, r, "../../benchmarks/fixtures/payment-platform/"+r)...)
	}
	links := LinkAll(all, LinkOptions{})
	got := map[string]bool{}
	for _, l := range links {
		got[fmt.Sprintf("%s|%s|%s|%s", l.Kind, l.Contract, l.From.Repo, l.To.Repo)] = true
	}
	want := []string{
		"http|POST /v1/payments|gateway|payment-service",
		"topic|topic payments.charged|payment-service|ledger-service",
		"topic_infra|topic payments.charged|infrastructure|payment-service",
		"topic_infra|topic payments.charged|infrastructure|ledger-service",
		"env|env PAYMENT_DB_DSN|payment-service|payment-service",
		"env|env KAFKA_BROKERS|payment-service|payment-service",
		"env|env LISTEN_ADDR|payment-service|payment-service",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing ground-truth link %s", w)
		}
	}
	if len(got) != len(want) {
		var extra []string
		for k := range got {
			if !contains(want, k) {
				extra = append(extra, k)
			}
		}
		t.Errorf("unexpected links (precision): %v", extra)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestScanSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "routes.go")
	_ = os.WriteFile(outside, []byte("package x\n\nimport \"net/http\"\n\nfunc init() { http.HandleFunc(\"/secret-route\", nil) }\n"), 0o644)
	_ = os.Symlink(outside, filepath.Join(root, "linked.go"))
	eps, _, err := Scan("r", root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 0 {
		t.Fatalf("scan followed a symlink: %+v", eps)
	}
}
