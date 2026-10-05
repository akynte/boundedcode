package benchmark

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/gitops"
)

// RunBaseline runs a task with the plain agent runtime and the same local
// model, sandbox image and iteration limit, but none of the control plane:
// one session receives the request text verbatim, with no context packs,
// code graph, Serena, verification loop, retries or frontier escalation.
// The same hidden acceptance checks decide the result.
func (s *SuiteRunner) RunBaseline(ctx context.Context, model string, spec TaskSpec) (res TaskResult) {
	res = TaskResult{ID: spec.ID, Category: spec.Category, Model: model, Attempts: 1}
	start := time.Now()
	defer func() { res.WallSeconds = time.Since(start).Seconds() }()
	dir := filepath.Join(s.WorkRoot, fmt.Sprintf("baseline-%s-%d", spec.ID, time.Now().UnixNano()))
	fixture := ""
	if spec.Fixture != "" {
		fixture = filepath.Join(s.FixturesDir, spec.Fixture)
	}
	repos, err := materialize(ctx, fixture, spec.Sources, filepath.Join(dir, "repos"), spec.Setup)
	if err != nil {
		res.Error = "materialize: " + err.Error()
		return res
	}
	if len(spec.Repos) != 1 {
		res.Error = "baseline supports single-repository tasks"
		return res
	}
	name := spec.Repos[0]
	repo := repos[name]
	base, err := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	// The agent works in a worktree of the base, as in a BoundedCode task.
	wt := filepath.Join(dir, "work", name)
	if err := gitops.EnsureWorktree(ctx, repo, wt, "baseline", base); err != nil {
		res.Error = err.Error()
		return res
	}
	common, err := gitops.CommonDir(ctx, wt)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	admin, err := gitops.AdminDir(ctx, wt)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	r, cleanup, err := s.NewRunner(ctx, filepath.Join(dir, "state"))
	if err != nil {
		res.Error = "runner: " + err.Error()
		return res
	}
	defer cleanup()
	if r.EnsureModel != nil {
		if err := r.EnsureModel(ctx); err != nil {
			res.Error = "model: " + err.Error()
			return res
		}
	}
	id := "baseline-" + strings.ReplaceAll(spec.ID, "_", "-")
	res.TaskID, res.StateDir = id, filepath.Join(dir, "state")
	gw := r.NewGateway(id, 0)
	tctx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		tctx, cancel = context.WithTimeout(ctx, spec.Timeout.D())
		defer cancel()
	}
	sess, err := r.Agent.Open(tctx, agent.OpenRequest{TaskID: id, Workspace: wt, GitCommonDirs: []string{common}, GitAdminDirs: []string{admin},
		PersistenceDir: filepath.Join(dir, "state", "runtime"), MaxIterations: r.Cfg.Agent.MaxIterations,
		MaxInputTokens: r.CtxSize, MaxOutputTokens: 8192, CondenserMaxEvents: r.Cfg.Agent.CondenserMaxEvents,
		CondenserMaxTokens: r.CtxSize * 7 / 10, Gateway: gw, LLMTimeout: r.Cfg.Inference.RequestTimeout.D()})
	if err != nil {
		res.Error = "open: " + err.Error()
		return res
	}
	out, err := sess.Send(tctx, spec.Request)
	_ = sess.Close()
	res.TaskStatus = out.Status
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		res.Error = err.Error()
	}
	res.LocalTokens, res.GeneratedTokens, res.CachedTokens = gw.Stats()
	for _, h := range spec.Hidden {
		if h.Patch == "" {
			res.Error = "baseline supports patch acceptance only"
			return res
		}
		if err := applyHiddenPatch(ctx, wt, base, h.Patch); err != nil {
			res.Error = "hidden patch: " + err.Error()
			return res
		}
	}
	res.Success = true
	for _, c := range spec.Checks {
		cs := checkSpec(ctx, spec, wt, c.Run)
		if err := withDependencies(&cs, repo, wt); err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, err.Error())
			continue
		}
		cmd, err := s.Sandbox.Command(ctx, cs)
		if err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, err.Error())
			continue
		}
		if b, err := cmd.CombinedOutput(); err != nil {
			res.Success = false
			res.FailedChecks = append(res.FailedChecks, fmt.Sprintf("%s: %s", c.Repo, tailStr(string(b), 600)))
		}
	}
	res.LocalOnly = res.Success
	res.Intel = collectIntel(ctx, r.DB, id)
	return res
}
