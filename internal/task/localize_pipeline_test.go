package task_test

// LOCALIZE must survive a repository bigger than its evidence block.
//
// The unit tests beside this one show the skeleton is ranked and cut. This
// one shows the consequence that matters: a task in a large repository runs
// the whole way through, rather than stopping at INTAKE's successor with
// "repository structure exceeds localization budget" — which is how three
// imported SWE-bench tasks spent nine runs measuring nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

// capturingModel answers every phase like phaseModel and keeps the structure
// evidence LOCALIZE presented, so the test can assert on what the model was
// actually shown rather than on what the code intended to show it.
type capturingModel struct {
	llm.Provider
	inner     *phaseModel
	structure map[string]any
	files     []string
}

func (m *capturingModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{StructuredOutput: true}
}

func (m *capturingModel) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	for _, msg := range req.Messages {
		if !strings.Contains(msg.Content, `"paths_available"`) {
			continue
		}
		// The evidence block is rendered inside an origin header, so the
		// object is found by decoding from each brace until one parses.
		for i, ch := range msg.Content {
			if ch != '{' {
				continue
			}
			var body struct {
				Files     []string       `json:"files"`
				Structure map[string]any `json:"structure"`
			}
			dec := json.NewDecoder(strings.NewReader(msg.Content[i:]))
			if err := dec.Decode(&body); err == nil && body.Structure != nil {
				m.structure, m.files = body.Structure, body.Files
				break
			}
		}
	}
	return m.inner.ChatStructured(ctx, req, schema)
}

func TestLargeRepositoryRunsThroughLocalize(t *testing.T) {
	requireGo(t)

	// 4,500 non-Go files, well over the cap that used to refuse, and cheap:
	// the Go module stays two files so verification is not what this measures.
	files := map[string]string{
		"go.mod": goodModule,
		"a.go":   "package a\n\nfunc Add(x, y int) int { return x - y }\n",
	}
	const filler = 4500
	for i := range filler {
		files[fmt.Sprintf("docs/area%02d/note%04d.md", i%40, i)] = "# note\n"
	}
	repo := gitRepo(t, files)

	editor := &phaseEditor{}
	r, st := newRunner(t, editor)
	model := &capturingModel{inner: &phaseModel{accept: true}}
	r.WorkflowModel = model

	ctx := context.Background()
	id := task.NewID("bigrepo")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "correct addition", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatalf("a %d-file repository failed the run outright: %v", filler+2, err)
	}

	s, err := task.NewStore(st).LoadWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range append(append([]recipe.Result(nil), s.Results...), s.Feedback...) {
		if strings.Contains(res.Summary.Headline, "exceeds localization budget") ||
			strings.Contains(res.Summary.Headline, "evidence exceeds phase budget") {
			t.Fatalf("the phase still refuses on size: %s", res.Summary.Headline)
		}
	}
	if !out.Accepted {
		t.Fatalf("the task did not complete: %+v", out)
	}
	if editor.calls == 0 {
		t.Fatal("the editor never ran, so LOCALIZE did not hand anything on")
	}

	// And what LOCALIZE showed the model was bounded and self-describing.
	if model.structure == nil {
		t.Fatal("no structure evidence reached the model")
	}
	available := int(model.structure["paths_available"].(float64))
	retained := int(model.structure["paths_retained"].(float64))
	dropped := int(model.structure["paths_dropped_for_budget"].(float64))
	t.Logf("skeleton: %d available, %d retained, %d dropped", available, retained, dropped)
	if available < filler {
		t.Errorf("available = %d; the walk did not see the whole repository", available)
	}
	if retained != len(model.files) {
		t.Errorf("retained %d but %d path(s) were sent", retained, len(model.files))
	}
	if retained+dropped != available {
		t.Errorf("%d + %d != %d", retained, dropped, available)
	}
	if len(model.files) >= available {
		t.Errorf("all %d paths were injected; the point is that they are not", available)
	}
	if _, ok := model.structure["directories"]; !ok {
		t.Error("the directory census did not survive, so a cut tree reads as a small one")
	}
}

// A repository that always fit must behave exactly as it did.
func TestSmallRepositoryStillRunsUnchanged(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{
		"go.mod": goodModule,
		"a.go":   "package a\n\nfunc Add(x, y int) int { return x - y }\n",
	})
	editor := &phaseEditor{}
	r, st := newRunner(t, editor)
	model := &phaseModel{accept: true}
	r.WorkflowModel = model

	ctx := context.Background()
	id := task.NewID("smallrepo")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "correct addition", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	// The same counts the pre-existing acceptance test asserts.
	if !out.Accepted || model.calls != 5 || editor.calls != 1 {
		t.Fatalf("accepted=%v structured=%d edit=%d", out.Accepted, model.calls, editor.calls)
	}
}
