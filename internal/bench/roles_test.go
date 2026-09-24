package bench

import (
	"testing"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/task"
)

func TestProductionRolesMustUseTheFrozenModel(t *testing.T) {
	provider := llm.NewOpenAICompatible(llm.Options{
		Name: "coding", Model: "model-a",
		Caps: llm.Capabilities{Kind: llm.KindOpenAICompatible, MaxContext: 4096},
	})
	runner := &task.Runner{WorkflowModel: provider}
	if err := ValidateProductionModelRoles(runner, ModelConfig{Provider: "coding", Model: "model-a", ContextTokens: 4096}, false); err != nil {
		t.Fatalf("matching planning role rejected: %v", err)
	}
	if err := ValidateProductionModelRoles(runner, ModelConfig{Provider: "coding", Model: "model-b", ContextTokens: 4096}, false); err == nil {
		t.Fatal("a different planning model was accepted")
	}
}
