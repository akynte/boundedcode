package graph

import "testing"

func TestNameMatches(t *testing.T) {
	method := Node{Name: "Discount", FQN: "example.com/wh/internal/pricing.BulkDiscount.Discount"}
	for symbol, want := range map[string]bool{
		"Discount":                      true,
		"BulkDiscount.Discount":         true,
		"pricing.BulkDiscount.Discount": true,
		method.FQN:                      true,
		"kDiscount.Discount":            false, // not at a separator
		"LoyaltyDiscount.Discount":      false,
		"Discount.Other":                false,
		"":                              false,
	} {
		if got := NameMatches(symbol, method); got != want {
			t.Errorf("NameMatches(%q) = %v, want %v", symbol, got, want)
		}
	}
	rust := Node{Name: "parse", FQN: "crate::config::parse"}
	if !NameMatches("config::parse", rust) {
		t.Error("a Rust path tail did not match")
	}
}

func TestSplitSymbol(t *testing.T) {
	for ref, want := range map[string][3]string{
		"RunOnce":            {"", "RunOnce", "RunOnce"},
		"Reconciler.RunOnce": {"", "Reconciler.RunOnce", "RunOnce"},
		"internal/service/user.go::UserService.Email": {"internal/service/user.go", "UserService.Email", "Email"},
		"crate::config::parse":                        {"", "crate::config::parse", "parse"},
		"app.py::Handler.get":                         {"app.py", "Handler.get", "get"},
	} {
		p, s, short := SplitSymbol(ref)
		if [3]string{p, s, short} != want {
			t.Errorf("SplitSymbol(%q) = %q %q %q, want %q", ref, p, s, short, want)
		}
	}
}

// The single-colon file prefix a plan was recorded using, and the forms that
// must not be mistaken for one.
func TestSplitSymbolSingleColonFilePrefix(t *testing.T) {
	for ref, want := range map[string][3]string{
		"internal/worker/reconcile.go:Reconciler":         {"internal/worker/reconcile.go", "Reconciler", "Reconciler"},
		"internal/worker/reconcile.go:Reconciler.RunOnce": {"internal/worker/reconcile.go", "Reconciler.RunOnce", "RunOnce"},
		"crate::config::parse":                            {"", "crate::config::parse", "parse"},
		"note: see here":                                  {"", "note", "note"}, // not a file prefix
	} {
		p, s, short := SplitSymbol(ref)
		if [3]string{p, s, short} != want {
			t.Errorf("SplitSymbol(%q) = %q %q %q, want %q", ref, p, s, short, want)
		}
	}
}

// Annotated references, each as a recorded plan wrote it.
func TestSplitSymbolAnnotatedForms(t *testing.T) {
	for ref, want := range map[string][3]string{
		"Email (method) in internal/service/user.go":             {"internal/service/user.go", "Email", "Email"},
		"ErrNotFound (internal/service/user.go:6)":               {"internal/service/user.go", "ErrNotFound", "ErrNotFound"},
		"Store.Find (internal/service/user.go:16)":               {"internal/service/user.go", "Store.Find", "Find"},
		"internal/worker/reconcile.go:func(Reconciler)RunOnce":   {"internal/worker/reconcile.go", "Reconciler.RunOnce", "RunOnce"},
		"internal/worker/reconcile.go:func (r *Reconciler) Tick": {"internal/worker/reconcile.go", "Reconciler.Tick", "Tick"},
		"Reconciler.RunOnce()":                                   {"", "Reconciler.RunOnce", "RunOnce"},
	} {
		p, s, short := SplitSymbol(ref)
		if [3]string{p, s, short} != want {
			t.Errorf("SplitSymbol(%q) = %q %q %q, want %q", ref, p, s, short, want)
		}
	}
}
