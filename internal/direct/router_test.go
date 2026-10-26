package direct

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteRules(t *testing.T) {
	rules := RiskRules{Keywords: []string{"payment", "authentication", "schema migration"}}
	caps := Capabilities{BoundedAvailable: true}
	cases := []struct{ req, mode string }{
		{"what does Total do?", ModeChat},
		{"explain the retry loop in client.go", ModeChat},
		{"why does TestRetry flake", ModeChat},
		{"run the tests", ModeChat},
		{"fix the off-by-one in cart.go", ModeCode},
		{"add a --verbose flag to the CLI", ModeCode},
		{"rename Total to Sum and update the callers", ModeCode},
		{"can you add logging to the handler?", ModeCode},
		{"how would I add a currency field here?", ModeChat},
		{"could you add a payment surcharge to Total?", ModeVerify},
		{"add retries to the payment client", ModeVerify},
		{"implement the schema migration for orders", ModeVerify},
		{"change the login handler in auth/handler.go", ModeVerify},
		{"update api/openapi.yaml to add the new field", ModeVerify},
		{"add the field to the proto in contracts/orders.proto", ModeVerify},
		{"refactor the client, with tests", ModeVerify},
		{"make the change across all services", ModeVerify},
		{"", ModeChat},
		// A ruled-out change is not a request for one.
		{"Run the tests in ledger-service and tell me which test fails and why. Do not change anything yet.", ModeChat},
		{"Without editing any code, tell me why the build fails", ModeChat},
		// A question after a scene-setting clause, with dotted names.
		{"In go/ast/astutil, what can Apply do that ast.Inspect cannot, and which file defines Apply?", ModeChat},
		{"In package txtar, how does Parse recognise a file marker?", ModeChat},
		{"Back in go/ast/inspector: add a method Len to Inspector and test it.", ModeCode},
		{"Now add a unit test for Total in cart_test.go and run the tests.", ModeCode},
		{"what does ast.Inspect do? then fix the nil check in walk.go", ModeChat},
	}
	for _, c := range cases {
		if got := Route(c.req, rules, caps); got.Mode != c.mode {
			t.Errorf("Route(%q) = %s (%s), want %s", c.req, got.Mode, got.Reason, c.mode)
		}
	}
	d := Route("add retries to the payment client", rules, Capabilities{BoundedReason: "no sandbox image"})
	if d.Mode != ModeCode || d.Fallback != ModeVerify || !strings.Contains(d.Reason, "no sandbox image") {
		t.Fatalf("fallback: %+v", d)
	}
	if d := Route("how do I add a flag?", rules, caps); d.Confidence != "low" {
		t.Fatalf("unsure request has confidence %s", d.Confidence)
	}
}

func TestRiskyPath(t *testing.T) {
	for p, want := range map[string]bool{
		"internal/auth/handler.go": true, "db/migrations/001.sql": true, "api/openapi.yaml": true,
		"proto/orders.proto": true, ".github/workflows/ci.yml": true, "infra/main.tf": true,
		"internal/direct/tools.go": false, "README.md": false, "cmd/author/main.go": false, "pkg/accounting_test.go": false,
	} {
		if _, got := RiskyPath(p); got != want {
			t.Errorf("RiskyPath(%q) = %v", p, got)
		}
	}
}

func TestRouteWithModelOnlyWhenUnsure(t *testing.T) {
	up := &scripted{replies: []reply{{text: `{"mode":"code","reason":"wants a flag added"}`}}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	d := RouteWithModel(context.Background(), r.Gateway, "fix the typo", RiskRules{}, Capabilities{BoundedAvailable: true})
	if d.Mode != ModeCode || len(up.requests) != 0 {
		t.Fatalf("sure request consulted the model: %+v %d", d, len(up.requests))
	}
	d = RouteWithModel(context.Background(), r.Gateway, "how do I add a flag?", RiskRules{}, Capabilities{BoundedAvailable: true})
	if d.Mode != ModeCode || d.Reason != "wants a flag added" || len(up.requests) != 1 {
		t.Fatalf("model route: %+v", d)
	}
	// A bad answer keeps the rules' choice.
	up.replies = []reply{{text: "sure thing"}}
	if d = RouteWithModel(context.Background(), r.Gateway, "how do I add a flag?", RiskRules{}, Capabilities{BoundedAvailable: true}); d.Mode != ModeChat {
		t.Fatalf("bad answer: %+v", d)
	}
}

func TestChatSwitchesToCodeOnFirstEdit(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"write", map[string]any{"path": "n.txt", "content": "x"}}}},
		{text: "done"},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	r.Opts.Mode = ModeChat
	if _, err := r.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "n.txt")); err != nil {
		t.Fatal("the edit was not applied after the switch")
	}
	if got := strings.Join(kinds(*evs), ","); got != "mode,tool_start,tool_end,text,done" || (*evs)[0].Text != ModeCode {
		t.Fatalf("events: %s", got)
	}
	if r.Mode() != ModeCode {
		t.Fatal("mode not switched")
	}
	ms := up.lastMessages(t)
	if sys := ms[0]["content"].(string); strings.Contains(sys, "classified as") || strings.Contains(sys, "Environment:") {
		t.Fatal("the system prompt must stay identical across turns (no framing, no environment)")
	}
	if um := ms[1]["content"].(string); !strings.Contains(um, "classified as a request for a direct change") {
		t.Fatalf("prompt framing did not follow the switch: %q", um)
	}
}

func TestRiskyEditHandsOffBeforeWriting(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{"write", map[string]any{"path": "ok.txt", "content": "fine"}}, {"edit", map[string]any{"path": "internal/auth/login.go", "old_string": "a", "new_string": "b"}}}},
		{text: "never"},
	}}
	r, root, evs := newRunner(t, up, ApprovalAuto)
	if err := os.MkdirAll(filepath.Join(root, "internal/auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/auth/login.go"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Opts.Mode = ModeCode
	_, err := r.Send(context.Background(), "change login")
	var h *Handoff
	if !errors.As(err, &h) || h.Path != "internal/auth/login.go" || !strings.Contains(h.Reason, "security") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "internal/auth/login.go")); string(b) != "a" {
		t.Fatal("the risky file was changed")
	}
	last := (*evs)[len(*evs)-1]
	if last.Kind != EventDone {
		t.Fatalf("last event %s", last.Kind)
	}
	if (*evs)[len(*evs)-3].Kind != EventMode || (*evs)[len(*evs)-3].Text != ModeVerify {
		t.Fatalf("events: %v", kinds(*evs))
	}
	// AllowRisky (bounded mode unavailable) lets it through.
	up.replies = []reply{{calls: []call{{"edit", map[string]any{"path": "internal/auth/login.go", "old_string": "a", "new_string": "b"}}}}, {text: "done"}}
	r.Opts.AllowRisky = true
	if _, err := r.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "internal/auth/login.go")); string(b) != "b" {
		t.Fatal("AllowRisky did not apply the edit")
	}
}
