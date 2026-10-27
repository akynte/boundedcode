package ecosystem

import "testing"

func TestIsTest(t *testing.T) {
	for rel, want := range map[string]bool{
		// Go: exactly the toolchain's rule.
		"internal/cart/cart_test.go": true,
		"internal/cart/cart.go":      false,
		"tests/helpers.go":           false,
		"internal/testutil/fake.go":  false,
		// Python
		"tests/test_api.py":        true,
		"pkg/api_test.py":          true,
		"conftest.py":              true,
		"src/app/tests/factory.py": true,
		"src/app/testing.py":       false,
		"src/app/api.py":           false,
		// JavaScript and TypeScript
		"web/src/cart.test.ts":      true,
		"web/src/Cart.spec.tsx":     true,
		"web/src/__tests__/cart.js": true,
		"test/server.mjs":           true,
		"web/src/cart.ts":           false,
		"web/src/latest.ts":         false,
		"web/src/contest/entry.ts":  false,
		// JVM
		"src/test/java/com/x/CartTest.java":       true,
		"src/main/java/com/x/Cart.java":           false,
		"app/src/main/kotlin/x/CartTests.kt":      true,
		"core/src/main/scala/x/Attestation.scala": false,
		// Rust
		"tests/cart.rs":     true,
		"src/cart.rs":       false,
		"src/cart/tests.rs": true,
		// Ruby
		"spec/models/cart_spec.rb": true,
		"test/cart_test.rb":        true,
		"app/models/cart.rb":       false,
		// PHP
		"tests/CartTest.php": true,
		"src/Cart.php":       false,
		// .NET
		"Shop.Tests/CartTests.cs":   true,
		"Shop.UnitTests/Helpers.cs": true,
		"Shop/Cart.cs":              false,
		// Swift, Elixir, Dart, C/C++, Bats
		"Tests/ShopTests/CartTests.swift": true,
		"Sources/Shop/Cart.swift":         false,
		"test/cart_test.exs":              true,
		"lib/cart.ex":                     false,
		"test/cart_test.dart":             true,
		"lib/cart.dart":                   false,
		"tests/test_cart.c":               true,
		"src/cart_unittest.cc":            true,
		"src/cart.cc":                     false,
		"test/cli.bats":                   true,
		// No convention
		"docs/test.md":    false,
		"Makefile":        false,
		"testdata/x.json": false,
	} {
		if got := IsTest(rel); got != want {
			t.Errorf("IsTest(%q) = %v, want %v", rel, got, want)
		}
	}
}
