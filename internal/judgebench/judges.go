package judgebench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

// goEnv is the toolchain environment both judges run checks with, so they
// differ in what they check, never in how the toolchain behaves.
func goEnv(workDir string) []string {
	modCache := filepath.Join(workDir, "gomodcache")
	if out, err := exec.Command("go", "env", "GOMODCACHE").Output(); err == nil {
		modCache = strings.TrimSpace(string(out))
	}
	return recipe.GoEnv(filepath.Join(workDir, "gocache"), modCache, filepath.Join(workDir, "tmp"))
}

// CI is a pipeline with required checks: the repository's visible build, vet,
// test and format checks on the patched tree, accepted when all pass.
type CI struct{ WorkDir string }

func (CI) Name() string { return "ci" }

func (j CI) Judge(ctx context.Context, t eval.Task, patch string) Verdict {
	dir, err := fixtureRepo(ctx, j.WorkDir, t)
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := apply(ctx, dir, patch); err != nil {
		return Verdict{Err: err.Error()}
	}
	tmp := filepath.Join(j.WorkDir, "tmp")
	_ = os.MkdirAll(tmp, 0o750)
	env := goEnv(j.WorkDir)
	rw, ro := recipe.GoSandboxPaths(filepath.Join(j.WorkDir, "gocache"), "", tmp)
	runner := &recipe.Runner{Sandbox: sandbox.ContainerRunner{}, Spec: sandbox.Spec{
		Dir: dir, TmpDir: tmp, Env: env,
		ReadWrite: append(append([]string{dir}, rw...), recipe.DeviceFiles()...), ReadOnly: ro,
	}}
	v := Verdict{Accepted: true}
	for _, res := range runner.RunAll(ctx, recipe.GoRecipes(recipe.Standard), dir, "") {
		if res.Status == recipe.Fail || res.Status == recipe.Error {
			v.Accepted = false
			v.Reasons = append(v.Reasons, res.Recipe+": "+res.Summary.Headline+res.Err)
		}
	}
	return v
}

// Bcode is the supervisor's completion contract on an indexed repository, with
// the repository's own policies and the task's scope. With Oracles set it is
// the bcode+oracle judge: each task's hidden acceptance suite is installed as
// an operator would install it, and a task with no suite cannot be judged.
type Bcode struct {
	WorkDir string
	Oracles map[string]*oracle.Suite
}

func (j Bcode) Name() string {
	if j.Oracles != nil {
		return "bcode+oracle"
	}
	return "bcode"
}

func (j Bcode) Judge(ctx context.Context, t eval.Task, patch string) Verdict {
	dir, err := fixtureRepo(ctx, j.WorkDir, t)
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	data, err := os.MkdirTemp(j.WorkDir, "bcode-data-")
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	defer func() { _ = os.RemoveAll(data) }()
	root, err := store.OpenRoot(data)
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	defer func() { _ = root.CloseAll() }()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", "judgebench-"+t.ID))
	if err != nil {
		return Verdict{Err: err.Error()}
	}

	// A repository under the supervisor is indexed, so the graph describes
	// the base the candidate is a diff against.
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{ID: "r", Name: "r", Path: dir, DefaultBranch: "main"}); err != nil {
		return Verdict{Err: err.Error()}
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		return Verdict{Err: err.Error()}
	}

	r, err := task.NewRunner(st, patchEngine{patch: patch}, sandbox.ContainerRunner{}, "judgebench")
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	// No judgment service: every site stays at its logged default, which is
	// also the shipped state.
	r.Judge = &judgment.Fake{}
	if j.Oracles != nil {
		suite, ok := j.Oracles[t.ID]
		if !ok {
			return Verdict{Err: "no oracle for this task"}
		}
		r.Oracle = suite
		// One attempt, so the feedback budget never binds; it is set to the
		// minimum anyway so the judge cannot be read as having relied on it.
		r.HiddenFeedbackRounds = 1
	}
	tmp := filepath.Join(data, "tmp")
	_ = os.MkdirAll(tmp, 0o750)
	r.SandboxSpec = sandbox.Spec{TmpDir: tmp, Env: goEnv(j.WorkDir)}
	if r.Policies, err = policy.Load(filepath.Join(dir, "policies")); err != nil {
		return Verdict{Err: err.Error()}
	}

	id := task.NewID("judge")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: strings.TrimSpace(t.Objective), Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 10 * time.Minute, Scope: t.Scope},
	}); err != nil {
		return Verdict{Err: err.Error()}
	}
	out, err := r.Run(ctx, id, dir)
	if err != nil {
		return Verdict{Err: err.Error()}
	}
	v := Verdict{Accepted: out.Accepted}
	if !out.Accepted {
		for _, reason := range out.Reasons {
			if !strings.Contains(reason, " passed: ") {
				v.Reasons = append(v.Reasons, reason)
			}
		}
	}
	return v
}

// patchEngine applies a fixed patch in one step: the candidate stands in for
// whatever agent produced it.
type patchEngine struct{ patch string }

func (patchEngine) Name() string                 { return "judgebench-patch" }
func (patchEngine) Health(context.Context) error { return nil }
func (patchEngine) Close() error                 { return nil }

func (e patchEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	if err := apply(ctx, req.Worktree, e.patch); err != nil {
		return nil, err
	}
	return &engine.Response{Summary: "applied the candidate patch", ClaimsDone: true}, nil
}
