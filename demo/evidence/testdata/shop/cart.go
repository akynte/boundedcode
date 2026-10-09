// Package shop prices shopping carts.
package shop

// Item is one line of a cart.
type Item struct {
	PriceCents int
	Qty        int
}

// Total returns the cart total in cents. Orders of 10 or more items get a
// 10% bulk discount.
func Total(items []Item) int {
	sum, count := 0, 0
	for _, it := range items {
		sum += it.PriceCents * it.Qty
		count += it.Qty
	}
	if count > 10 {
		sum = sum * 90 / 100
	}
	return sum
}
