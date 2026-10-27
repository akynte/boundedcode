package api

import "testing"

func TestSummarize(t *testing.T) {
	s := Summarize(Order{ID: "o1", Lines: []Line{{SKU: "a", PriceCents: 250, Qty: 2}, {SKU: "b", PriceCents: 100, Qty: 1}}, DiscountCents: 100})
	if s.SubtotalCents != 600 || s.TotalCents != 500 {
		t.Fatalf("%+v", s)
	}
	if s := Summarize(Order{ID: "o2", Lines: []Line{{PriceCents: 100, Qty: 1}}, DiscountCents: 500}); s.TotalCents != 0 {
		t.Fatalf("negative total: %+v", s)
	}
}
