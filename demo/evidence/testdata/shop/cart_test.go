package shop

import "testing"

func TestTotalSmallCart(t *testing.T) {
	if got := Total([]Item{{PriceCents: 100, Qty: 5}}); got != 500 {
		t.Fatalf("Total = %d, want 500", got)
	}
}

func TestTotalLargeCart(t *testing.T) {
	if got := Total([]Item{{PriceCents: 100, Qty: 20}}); got != 1800 {
		t.Fatalf("Total = %d, want 1800", got)
	}
}
