package smoke

import "testing"

func TestCalculatorAddUsesThePublicContract(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
	if got := Add(-4, 1); got != -3 {
		t.Fatalf("Add(-4, 1) = %d, want -3", got)
	}
}
