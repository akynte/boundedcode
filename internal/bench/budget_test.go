package bench

import "testing"

func TestGenerationBudgetCountsAdmissionsAndStopsAtLimit(t *testing.T) {
	budget := NewGenerationBudget(1)
	if err := budget.reserve(); err != nil {
		t.Fatal(err)
	}
	if err := budget.reserve(); err == nil {
		t.Fatal("generation budget admitted more requests than its limit")
	}
	if got := budget.Used(); got != 1 {
		t.Fatalf("used requests = %d, want 1", got)
	}
}
