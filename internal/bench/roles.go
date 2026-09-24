package bench

import (
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/task"
)

// ValidateProductionModelRoles refuses a BOUNDED run whose structured roles
// silently use a different route from the frozen coding model. Without this
// check the same suite could compare a RAW single-model command against a
// BOUNDED pipeline that planned or reviewed with another model, making the
// architecture label indistinguishable from a model change.
func ValidateProductionModelRoles(r *task.Runner, expected ModelConfig, smoke bool) error {
	if smoke || strings.TrimSpace(expected.Model) == "" || r == nil {
		return nil
	}
	if r.WorkflowModel == nil {
		return fmt.Errorf("bench: planning role is not configured")
	}
	checks := []struct {
		role string
		p    llm.Provider
	}{
		{"planning", r.WorkflowModel},
	}
	// Production falls back to the workflow model when no separate review
	// route is available. Validate that effective route rather than silently
	// treating a missing role as if it were the frozen coding model.
	review := r.ReviewModel
	if review == nil {
		review = r.WorkflowModel
	}
	checks = append(checks, struct {
		role string
		p    llm.Provider
	}{"review", review})
	if r.Critic != nil && r.Critic.Provider != nil {
		checks = append(checks, struct {
			role string
			p    llm.Provider
		}{"critic", r.Critic.Provider})
	}
	for _, check := range checks {
		if err := validateModelRoute(check.role, check.p, expected); err != nil {
			return err
		}
	}
	return nil
}

func validateModelRoute(role string, provider llm.Provider, expected ModelConfig) error {
	if expected.Provider != "" && provider.Name() != expected.Provider {
		return fmt.Errorf("bench: %s role uses provider %q, frozen provider is %q", role, provider.Name(), expected.Provider)
	}
	named, ok := provider.(interface{ ModelName() string })
	if !ok || strings.TrimSpace(named.ModelName()) == "" {
		return fmt.Errorf("bench: %s role provider %q does not expose its configured model name", role, provider.Name())
	}
	if named.ModelName() != expected.Model {
		return fmt.Errorf("bench: %s role uses model %q, frozen model is %q", role, named.ModelName(), expected.Model)
	}
	if expected.ContextTokens > 0 {
		if actual := provider.Capabilities().MaxContext; actual > 0 && actual != expected.ContextTokens {
			return fmt.Errorf("bench: %s role context window %d does not match frozen context window %d", role, actual, expected.ContextTokens)
		}
	}
	return nil
}
