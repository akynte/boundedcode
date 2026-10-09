// Command quote prints the total for N items at 100 cents each.
package main

import (
	"fmt"
	"os"
	"strconv"

	"example.com/shop"
)

func main() {
	n := 10
	if len(os.Args) > 1 {
		v, err := strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "usage: quote [ITEMS]")
			os.Exit(2)
		}
		n = v
	}
	fmt.Printf("%d items at 100 cents: total %d cents\n", n, shop.Total([]shop.Item{{PriceCents: 100, Qty: n}}))
}
