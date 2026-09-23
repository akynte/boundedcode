package llm

import (
	"strings"
	"testing"
)

// Which model does which phase.
//
// PLAN and EDIT were the same model for as long as the workflow model was
// taken from the engine. They are separate roles so that an operator can give
// the structured planning call and the tool loop different models, and on a
// machine that holds one generation model at a time that choice has to be
// expressible in configuration rather than in whichever server happens to be
// running. The shipped configuration routes every role to one model; these
// tests hold the split to its contract for the operators who use it.

// phaseRoutingFile is a split configuration: two generation providers, and an
// unmanaged embedding provider that must never enter the generation slot.
func phaseRoutingFile() ProvidersFile {
	return ProvidersFile{
		Default: "bonsai",
		Providers: []ProviderSpec{
			{Name: "bonsai", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:8090",
				Model: "bonsai-2-27b"},
			{Name: "editor", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:8091",
				Model: "second-generator"},
			{Name: "local-embed", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:8081",
				Model: "qwen3-embedding-0.6b",
				Capabilities: &Capabilities{Kind: KindLlamaCPP, Embeddings: true,
					Local: true, MaxContext: 8192}},
		},
		Roles: map[string]string{
			"planning":  "bonsai",
			"review":    "bonsai",
			"coding":    "editor",
			"embedding": "local-embed",
		},
	}
}

// Cases 1, 2 and 3. Each phase's role resolves to the model the decision named.
func TestPhaseRolesResolveToTheirModels(t *testing.T) {
	router, err := NewRouter(phaseRoutingFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		role  Role
		model string
		phase string
	}{
		{RolePlanning, "bonsai", "PLAN"},
		{RoleCoding, "editor", "EDIT"},
		{RoleReview, "bonsai", "REVIEW"},
	} {
		p, err := router.For(c.role)
		if err != nil {
			t.Fatalf("%s: %v", c.phase, err)
		}
		if p.Name() != c.model {
			t.Errorf("%s routes to %q, want %q", c.phase, p.Name(), c.model)
		}
	}
	// A split configuration keeps PLAN and EDIT apart.
	plan, _ := router.For(RolePlanning)
	edit, _ := router.For(RoleCoding)
	if plan.Name() == edit.Name() {
		t.Errorf("PLAN and EDIT resolved to the same provider %q", plan.Name())
	}
	// PLAN and REVIEW must be the same one.
	review, _ := router.For(RoleReview)
	if plan.Name() != review.Name() {
		t.Errorf("PLAN (%s) and REVIEW (%s) should share a model", plan.Name(), review.Name())
	}
}

// Case 6. The embedding provider is routed independently and is not one of the
// generation models, so swapping those cannot disturb it.
func TestEmbeddingRoutingIsIndependentOfGeneration(t *testing.T) {
	router, err := NewRouter(phaseRoutingFile())
	if err != nil {
		t.Fatal(err)
	}
	embed, err := router.For(RoleEmbedding)
	if err != nil {
		t.Fatal(err)
	}
	if !embed.Capabilities().Embeddings {
		t.Error("the embedding role resolved to a provider that does not declare embeddings")
	}
	for _, role := range []Role{RolePlanning, RoleCoding, RoleReview} {
		gen, err := router.For(role)
		if err != nil {
			t.Fatal(err)
		}
		if gen.Name() == embed.Name() {
			t.Errorf("role %s shares a provider with embedding", role)
		}
	}
}

// An unrouted role still falls back to the declared default rather than
// failing: adding two roles must not have made the others unresolvable.
func TestUnroutedRolesStillResolve(t *testing.T) {
	router, err := NewRouter(phaseRoutingFile())
	if err != nil {
		t.Fatal(err)
	}
	p, err := router.For(RoleSummarization)
	if err != nil {
		t.Fatalf("an unrouted role did not resolve: %v", err)
	}
	if !strings.Contains(p.Name(), "bonsai") {
		t.Errorf("an unrouted role resolved to %q, want the default", p.Name())
	}
}

// A role pointing at a provider nobody declared is refused at load, not at the
// first call in the middle of a task.
func TestRoleToUndeclaredProviderIsRefused(t *testing.T) {
	f := phaseRoutingFile()
	f.Roles["coding"] = "a-model-that-does-not-exist"
	if _, err := NewRouter(f); err == nil {
		t.Fatal("a role routed to an undeclared provider was accepted")
	}
}

// Case 5, at the configuration boundary: a managed provider whose process
// cannot be validated is refused rather than quietly demoted to an unmanaged
// endpoint that something else might be serving.
func TestManagedProviderWithBadPinIsRefused(t *testing.T) {
	for name, spec := range map[string]ProviderSpec{
		"no binary hash": {Name: "edit", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:8091",
			Process: &ServerProcess{Argv: []string{"/usr/bin/llama-server"}}},
		"relative executable": {Name: "edit", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1:8091",
			Process: &ServerProcess{Argv: []string{"llama-server"}, BinarySHA256: strings.Repeat("a", 64)}},
		"no port": {Name: "edit", Kind: KindLlamaCPP, BaseURL: "http://127.0.0.1",
			Process: &ServerProcess{Argv: []string{"/usr/bin/llama-server"}, BinarySHA256: strings.Repeat("a", 64)}},
		"not loopback": {Name: "edit", Kind: KindLlamaCPP, BaseURL: "http://10.0.0.5:8091",
			Process: &ServerProcess{Argv: []string{"/usr/bin/llama-server"}, BinarySHA256: strings.Repeat("a", 64)}},
	} {
		t.Run(name, func(t *testing.T) {
			f := ProvidersFile{
				Default:   "edit",
				Providers: []ProviderSpec{spec},
			}
			if _, err := NewRouter(f); err == nil {
				t.Fatalf("a managed provider with %s was accepted", name)
			}
		})
	}
}
