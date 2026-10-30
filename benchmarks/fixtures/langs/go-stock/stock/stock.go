// Package stock tracks stock levels per warehouse.
package stock

import (
	"errors"
	"sort"
)

// ErrInsufficient is returned when a removal exceeds what is in stock.
var ErrInsufficient = errors.New("insufficient stock")

// Level is the quantity of one SKU in one warehouse.
type Level struct {
	Warehouse string
	SKU       string
	Quantity  int
}

// Book holds the stock levels.
type Book struct {
	levels map[[2]string]int
}

// NewBook returns an empty book.
func NewBook() *Book { return &Book{levels: map[[2]string]int{}} }

// Add puts qty units of sku into warehouse.
func (b *Book) Add(warehouse, sku string, qty int) {
	b.levels[[2]string{warehouse, sku}] += qty
}

// Remove takes qty units of sku out of warehouse.
func (b *Book) Remove(warehouse, sku string, qty int) error {
	k := [2]string{warehouse, sku}
	if b.levels[k] < qty {
		return ErrInsufficient
	}
	b.levels[k] -= qty
	return nil
}

// Levels lists every non-zero level, sorted by warehouse then SKU.
func (b *Book) Levels() []Level {
	var out []Level
	for k, q := range b.levels {
		if q != 0 {
			out = append(out, Level{Warehouse: k[0], SKU: k[1], Quantity: q})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Warehouse != out[j].Warehouse {
			return out[i].Warehouse < out[j].Warehouse
		}
		return out[i].SKU < out[j].SKU
	})
	return out
}
