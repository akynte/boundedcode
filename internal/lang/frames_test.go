package lang

import (
	"fmt"
	"strings"
	"testing"
)

// TestParseFrames: the traces each runtime prints, innermost frame first.
func TestParseFrames(t *testing.T) {
	cases := []struct {
		name, out string
		want      []string // file:line[ symbol], innermost first
	}{
		{"go panic", "panic: runtime error: index out of range [3] with length 3\n\ngoroutine 1 [running]:\nexample.com/shop/internal/cart.Total(...)\n\t/home/u/shop/internal/cart/cart.go:42 +0x1d\nmain.main()\n\t/home/u/shop/cmd/shop/main.go:12 +0x25\nexit status 2",
			[]string{"/home/u/shop/internal/cart/cart.go:42", "/home/u/shop/cmd/shop/main.go:12"}},
		{"python", "Traceback (most recent call last):\n  File \"/srv/app/inventory/cli.py\", line 9, in main\n    reorder(items)\n  File \"/srv/app/inventory/stock.py\", line 31, in reorder\n    return qty / per_box\nZeroDivisionError: division by zero",
			[]string{"/srv/app/inventory/stock.py:31 reorder", "/srv/app/inventory/cli.py:9 main"}},
		{"node", "TypeError: Cannot read properties of undefined (reading 'price')\n    at applyDiscount (/app/src/cart/discount.ts:18:22)\n    at Object.<anonymous> (/app/src/cart/discount.test.ts:7:5)\n    at node:internal/main:1:1",
			[]string{"/app/src/cart/discount.ts:18 applyDiscount", "/app/src/cart/discount.test.ts:7 Object.<anonymous>"}},
		{"node anonymous", "Error: boom\n    at file:///app/lib/index.mjs:3:9", []string{"/app/lib/index.mjs:3"}},
		{"rust", "thread 'main' panicked at src/ledger.rs:27:9:\nattempt to subtract with overflow\nstack backtrace:\n   0: rust_begin_unwind\n             at ./src/ledger.rs:27:9",
			[]string{"src/ledger.rs:27"}},
		{"rust old", "thread 'tests::transfer' panicked at 'insufficient funds', src/bank.rs:40:13", []string{"src/bank.rs:40"}},
		{"java", "java.lang.NullPointerException: total\n\tat com.acme.orders.Orders.paidTotal(Orders.java:42)\n\tat com.acme.orders.OrdersTest.paid(OrdersTest.java:17)\n\tat java.base/jdk.internal.reflect.Method.invoke(Method.java:580)",
			[]string{"Orders.java:42 com.acme.orders.Orders.paidTotal", "OrdersTest.java:17 com.acme.orders.OrdersTest.paid", "Method.java:580 java.base/jdk.internal.reflect.Method.invoke"}},
		{"kotlin", "\tat app.Billing.charge(Billing.kt:8)", []string{"Billing.kt:8 app.Billing.charge"}},
		{"csharp", "   at Shop.Cart.Total() in /src/Shop/Cart.cs:line 21\n   at Shop.Tests.CartTests.Sum() in /src/Shop.Tests/CartTests.cs:line 12",
			[]string{"/src/Shop/Cart.cs:21 Shop.Cart.Total", "/src/Shop.Tests/CartTests.cs:12 Shop.Tests.CartTests.Sum"}},
		{"php", "PHP Fatal error:  Uncaught TypeError: x in /var/www/src/Cart.php:14\nStack trace:\n#0 /var/www/src/Checkout.php(30): Cart->total()\n#1 {main}",
			[]string{"/var/www/src/Cart.php:14", "/var/www/src/Checkout.php:30 Cart->total"}},
		{"ruby", "/app/lib/cart.rb:12:in `total': undefined method `+' for nil (NoMethodError)\n\tfrom /app/lib/checkout.rb:5:in `Checkout#run'",
			[]string{"/app/lib/cart.rb:12 total", "/app/lib/checkout.rb:5 run"}},
		{"dart", "#0      Cart.total (package:shop/cart.dart:9:5)\n#1      main (file:///app/bin/main.dart:4:3)",
			[]string{"cart.dart:9 Cart.total", "/app/bin/main.dart:4 main"}},
		{"elixir", "    (shop 0.1.0) lib/shop/cart.ex:12: Shop.Cart.total/1\n    test/cart_test.exs:8: (test)",
			[]string{"lib/shop/cart.ex:12 Shop.Cart.total/1"}},
		{"native", "    #0 0x4f1a2b in parse_header src/http/parser.c:88:12\n    #1 0x4f0000 in main src/main.c:10", []string{"src/http/parser.c:88 parse_header", "src/main.c:10 main"}},
		{"swift", "Fatal error: Index out of range: file Shop/Cart.swift, line 14", []string{"Shop/Cart.swift:14"}},
		{"no trace", "ok  \texample.com/shop\t0.01s\nPASS", nil},
	}
	for _, c := range cases {
		var got []string
		for _, f := range ParseFrames(c.out, 0) {
			s := fmt.Sprintf("%s:%d", f.File, f.Line)
			if f.Symbol != "" {
				s += " " + f.Symbol
			}
			got = append(got, s)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
