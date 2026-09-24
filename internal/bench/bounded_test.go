package bench

import (
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

func TestMetricsFromOutcomeCountsBudgetedAttemptsNotRecipeChecks(t *testing.T) {
	out := &task.Outcome{
		Attempts: 1,
		Results:  make([]recipe.Result, 5),
	}
	metrics := metricsFromOutcome(out)
	if metrics.VerificationAttempts == nil || *metrics.VerificationAttempts != 1 {
		t.Fatalf("verification attempts = %v, want one budgeted attempt", metrics.VerificationAttempts)
	}
	if metrics.Retries == nil || *metrics.Retries != 0 {
		t.Fatalf("retries = %v, want zero", metrics.Retries)
	}
}
