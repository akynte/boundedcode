package verify

import (
	"strings"
	"testing"
)

// TestFailureDigestSurvivesLogFlood is the regression test for the caddy
// rerun: the failing assertion came before ~12 KB of JSON logs, the stored
// tail kept only the logs, and the retry pack never said what failed.
func TestFailureDigestSurvivesLogFlood(t *testing.T) {
	var b strings.Builder
	b.WriteString("=== RUN   TestCaddyfileAdaptToJSON\n")
	b.WriteString("    caddytest.go:387: adapting config using caddyfile adapter: wrong argument count or unexpected line ending after 'expression', at Caddyfile:2\n")
	b.WriteString("    caddyfile_adapt_test.go:54: failed to adapt expression_quotes.caddyfiletest\n")
	b.WriteString("--- FAIL: TestCaddyfileAdaptToJSON (0.04s)\n")
	for i := 0; i < 200; i++ {
		b.WriteString(`{"level":"info","ts":1791192438.64,"logger":"admin.api","msg":"received request","method":"GET","uri":"/config/"}` + "\n")
	}
	b.WriteString("FAIL\tgithub.com/caddyserver/caddy/v2/caddytest/integration\t1.161s\nok  \tgithub.com/caddyserver/caddy/v2/cmd\t0.006s\n")
	out := b.String()
	if strings.Contains(tail(out, 6000), "caddyfile_adapt_test.go:54") {
		t.Fatal("test premise: the tail alone must lose the assertion")
	}
	d := FailureDigest(out, 3000)
	for _, want := range []string{"--- FAIL: TestCaddyfileAdaptToJSON", "caddyfile_adapt_test.go:54: failed to adapt", "wrong argument count", "FAIL\tgithub.com/caddyserver/caddy/v2/caddytest/integration"} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %q:\n%s", want, d)
		}
	}
	if strings.Count(d, `"logger":"admin.api"`) > 6 || len(d) > 3000 {
		t.Errorf("digest carries the log flood (%d bytes)", len(d))
	}
	if h := (StageResult{Digest: d, Output: tail(out, 6000)}).Headline(); !strings.Contains(h, "caddytest.go:387") && !strings.Contains(h, "TestCaddyfileAdaptToJSON") {
		t.Errorf("headline = %q", h)
	}
}

func TestFailureDigestOtherRunners(t *testing.T) {
	cases := map[string]string{
		"src/a.ts(68,25): error TS18048: 'atRule.nodes' is possibly 'undefined'.\n": "error TS18048",
		" FAIL  packages/x/__tests__/a.spec.ts > scoped CSS > nesting\nAssertionError: expected 'a' to match 'b'\n": "AssertionError",
		"  1) issues 4999 should not fail\n     Error: getaddrinfo EAI_AGAIN\n": "1) issues 4999",
	}
	for out, want := range cases {
		if d := FailureDigest(out, 3000); !strings.Contains(d, want) {
			t.Errorf("digest of %q lacks %q: %q", out, want, d)
		}
	}
	if FailureDigest("all good\nok  \tpkg\t0.1s\n", 3000) != "" {
		t.Error("no digest for passing output")
	}
}
