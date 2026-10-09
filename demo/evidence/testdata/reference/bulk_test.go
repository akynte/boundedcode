package shop

import "testing"

// Reference regression test, used only by the fixture tests to prove the
// bug is real and that the reference fix removes it. It is never given to
// the agent: the demo copies testdata/shop only.
func TestTotalBulkDiscountAtExactlyTen(t *testing.T) {
	if got := Total([]Item{{PriceCents: 100, Qty: 10}}); got != 900 {
		t.Fatalf("Total = %d, want 900", got)
	}
}
