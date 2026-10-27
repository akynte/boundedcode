// Package api serves orders and their summaries.
package api

// Line is one product line of an order.
type Line struct {
	SKU        string `json:"sku"`
	PriceCents int64  `json:"price_cents"`
	Qty        int64  `json:"qty"`
}

// Order is an order as stored.
type Order struct {
	ID            string `json:"id"`
	Lines         []Line `json:"lines"`
	DiscountCents int64  `json:"discount_cents"`
}

// Summary is what the web client shows for an order
// (contracts/summary.schema.json).
type Summary struct {
	ID            string `json:"id"`
	SubtotalCents int64  `json:"subtotal_cents"`
	TotalCents    int64  `json:"total_cents"`
}

// Summarize totals an order. The total never goes below zero.
func Summarize(o Order) Summary {
	var sub int64
	for _, l := range o.Lines {
		sub += l.PriceCents * l.Qty
	}
	return Summary{ID: o.ID, SubtotalCents: sub, TotalCents: max(sub-o.DiscountCents, 0)}
}
