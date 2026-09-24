package mcp

import (
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
)

// The cap used to be 500 characters, tight enough that a real acceptance
// criterion or security constraint routinely exceeded it — forcing the model
// to paraphrase the user's own stated requirement down to fit, which is a
// model-authored rewording silently standing in for what the user actually
// said. 2000 is large enough to hold a genuine paragraph without that
// pressure, while the total-character budget still bounds how much this can
// add to every request.
func TestValidateTaskDetailsAllowsAGenuineParagraphPerItem(t *testing.T) {
	paragraph := strings.Repeat("This constraint must be preserved word for word. ", 30) // ~1500 chars
	if len(paragraph) <= 500 || len(paragraph) > maxItemChars {
		t.Fatalf("test fixture is %d chars; want something between 500 and %d to prove the old cap would have rejected it and the new one does not", len(paragraph), maxItemChars)
	}
	if err := validateTaskDetails([]string{paragraph}); err != nil {
		t.Errorf("a single faithful paragraph was rejected: %v", err)
	}
}

// An item over the new, larger cap must still be refused — this is a budget,
// not an invitation to paste an unbounded document into a task's durable
// record — but the message must say which item and by how much, not just
// repeat the rule, so a retry is not a blind guess.
func TestValidateTaskDetailsNamesTheOffendingItem(t *testing.T) {
	ok := "a legitimate requirement"
	tooLong := strings.Repeat("x", maxItemChars+1)
	err := validateTaskDetails([]string{ok, tooLong})
	if err == nil {
		t.Fatal("an oversized item was accepted")
	}
	for _, want := range []string{"item 2", "do not silently paraphrase"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not identify the offending item and how to fix it (%q): %v", want, err)
		}
	}
}

func TestValidateTaskDetailsEnforcesTheGroupSizeLimit(t *testing.T) {
	group := make([]string, maxItemsPerGroup+1)
	for i := range group {
		group[i] = "x"
	}
	if err := validateTaskDetails(group); err == nil {
		t.Error("a group over the item-count limit was accepted")
	}
}

func TestValidateTaskDetailsEnforcesTheTotalBudgetAcrossGroups(t *testing.T) {
	// Individually under maxItemChars, but their sum exceeds maxTotalChars.
	item := strings.Repeat("y", maxItemChars)
	requirements := []string{item, item}
	constraints := []string{item, item}
	if requirements[0] == "" || (len(requirements[0])+len(requirements[1])+len(constraints[0])+len(constraints[1])) <= maxTotalChars {
		t.Fatal("test fixture does not exceed the total budget")
	}
	err := validateTaskDetails(requirements, constraints)
	if err == nil {
		t.Fatal("a set of items within each per-item cap but over the combined budget was accepted")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("error does not explain the combined budget: %v", err)
	}
}

func TestValidateTaskDetailsRejectsAnEmptyItem(t *testing.T) {
	if err := validateTaskDetails([]string{"real one", "  "}); err == nil {
		t.Error("a blank item was accepted")
	}
}

func TestVerificationLevelCannotBeWeakenedForASupervisedTask(t *testing.T) {
	for _, tc := range []struct {
		name     string
		got      recipe.Level
		required recipe.Level
		allowed  bool
	}{
		{"standard satisfies standard", recipe.Standard, recipe.Standard, true},
		{"high satisfies standard", recipe.High, recipe.Standard, true},
		{"low does not satisfy standard", recipe.Low, recipe.Standard, false},
		{"standard does not satisfy high", recipe.Standard, recipe.High, false},
		{"low satisfies low", recipe.Low, recipe.Low, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verificationLevelAtLeast(tc.got, tc.required); got != tc.allowed {
				t.Fatalf("verificationLevelAtLeast(%q, %q) = %t, want %t", tc.got, tc.required, got, tc.allowed)
			}
		})
	}
}
