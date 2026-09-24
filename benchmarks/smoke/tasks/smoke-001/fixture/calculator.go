package smoke

// Add is intentionally wrong in this infrastructure fixture. The hidden
// evaluator, not this repository text, decides whether a candidate fixed it.
func Add(a, b int) int {
	return a + b + 1
}
