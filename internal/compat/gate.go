package compat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/xservice"
)

// Repo is one task repository.
type Repo struct {
	Name     string
	Worktree string // the task worktree: git objects and commits are read through it
	Source   string // the repository's own checkout (from the ledger), for installed dependencies
	Base     string // base commit
	Head     string // candidate commit; empty = the worktree's HEAD
}

// Gate evaluates a task's affected contract links.
type Gate struct {
	DB     *sql.DB
	Verify *verify.Engine
	Rec    *telemetry.Recorder
	Log    *slog.Logger
	// Scratch is where commit trees are exported; each evaluation uses (and
	// removes) its own directory below it.
	Scratch string

	// afterLink is called after each link result is persisted (tests).
	afterLink func(LinkResult) error
}

// errUnaffected marks a link whose files changed but not the link.
var errUnaffected = errors.New("unaffected")

type repoState struct {
	Repo
	BaseDir, HeadDir string
	Hunks            fileHunks
	Changed          map[string]bool
	Module           string // head go.mod module path ("" = none at the root)
	GoVersion        string
}

type evaluation struct {
	ctx        context.Context
	g          *Gate
	taskID     string
	repos      map[string]*repoState
	order      []string
	baseRoot   string
	headRoot   string
	baseEps    []xservice.Endpoint
	headEps    []xservice.Endpoint
	headLinks  map[string]xservice.Link
	protoCache map[string]xservice.ProtoSchema
	stages     map[string][]verify.Stage
	stageErr   map[string]error
	works      int
	profiles   int
}

// Evaluate runs the gate for a task at the repositories' current commits.
// others are the endpoints of workspace repositories outside the task (from
// the index). Results already recorded for the same commits are reused.
// The returned error is a failure to evaluate at all (also recorded in the
// report); a context cancellation returns what was recorded so far.
func (g *Gate) Evaluate(ctx context.Context, taskID string, repos []Repo, others []xservice.Endpoint) (Report, error) {
	evalID, err := beginEvaluation(ctx, g.DB, taskID)
	if err != nil {
		return Report{Error: err.Error()}, err
	}
	rep, err := g.evaluate(ctx, taskID, evalID, repos, others)
	if err != nil && ctx.Err() == nil {
		rep.Error = trunc(err.Error(), 400)
	}
	if ctx.Err() == nil {
		_ = finishEvaluation(context.WithoutCancel(ctx), g.DB, evalID, rep)
		g.emit(ctx, taskID, rep)
	}
	return rep, err
}

func (g *Gate) evaluate(ctx context.Context, taskID string, evalID int64, repos []Repo, others []xservice.Endpoint) (Report, error) {
	var rep Report
	dir, err := os.MkdirTemp(g.Scratch, "compat-")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(dir)
	ev := &evaluation{ctx: ctx, g: g, taskID: taskID, repos: map[string]*repoState{}, baseRoot: filepath.Join(dir, "base"), headRoot: filepath.Join(dir, "head"),
		protoCache: map[string]xservice.ProtoSchema{}, stages: map[string][]verify.Stage{}, stageErr: map[string]error{}}
	for _, d := range []string{ev.baseRoot, ev.headRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return rep, err
		}
	}
	for _, r := range repos {
		if err := ev.addRepo(ctx, r); err != nil {
			return rep, err
		}
	}
	for _, name := range ev.order {
		st := ev.repos[name]
		for _, v := range []struct {
			dir string
			eps *[]xservice.Endpoint
		}{{st.BaseDir, &ev.baseEps}, {st.HeadDir, &ev.headEps}} {
			e, _, err := xservice.Scan(name, v.dir, xservice.ScanOptions{})
			if err != nil {
				return rep, fmt.Errorf("scan %s: %w", name, err)
			}
			*v.eps = append(*v.eps, e...)
		}
	}
	inTask := func(repo string) bool { return ev.repos[repo] != nil }
	var outside []xservice.Endpoint
	for _, e := range others {
		if !inTask(e.Repo) {
			outside = append(outside, e)
		}
	}
	baseLinks := gateLinks(xservice.LinkAll(append(slices.Clone(ev.baseEps), outside...), xservice.LinkOptions{}), inTask)
	headLinks := gateLinks(xservice.LinkAll(append(slices.Clone(ev.headEps), outside...), xservice.LinkOptions{}), inTask)

	ev.headLinks = headLinks
	var pend []*pendingLink
	ambiguous := ambiguousFrom(headLinks)
	for _, id := range unionIDs(baseLinks, headLinks) {
		lb, inBase := baseLinks[id]
		lh, inHead := headLinks[id]
		p, err := ev.classify(id, lb, inBase, lh, inHead, ambiguous)
		switch {
		case errors.Is(err, errUnaffected):
			rep.Unaffected = append(rep.Unaffected, p.res)
			continue
		case err != nil:
			return rep, err
		case p == nil:
			continue
		}
		pend = append(pend, p)
	}
	// Reuse results recorded for the same commits (a resumed evaluation).
	var todo []*pendingLink
	for _, p := range pend {
		p.res.Commits = ev.commits(p.involved)
		if prev, ok := lookup(ctx, g.DB, taskID, p.res.ID, commitsKey(p.res.Commits)); ok {
			rep.Links = append(rep.Links, prev)
			rep.Reused++
			_ = saveResult(ctx, g.DB, taskID, evalID, prev)
			continue
		}
		if p.res.Result != "" { // decided without running anything
			if err := ev.finish(ctx, evalID, p, &rep); err != nil {
				return rep, err
			}
			continue
		}
		todo = append(todo, p)
	}
	// Run the checks, one group (repository and composition) at a time; a
	// link is recorded as soon as all its checks have run.
	for _, grp := range groupNeeds(todo) {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		ev.runGroup(ctx, grp)
		if ctx.Err() != nil {
			// Outcomes of a cancelled run say nothing about the link: record
			// none of them (a resumed evaluation reruns the group).
			return rep, ctx.Err()
		}
		for _, p := range todo {
			if p.res.Result == "" && p.done() {
				if err := ev.finish(ctx, evalID, p, &rep); err != nil {
					return rep, err
				}
			}
		}
	}
	if ctx.Err() != nil {
		return rep, ctx.Err()
	}
	sortLinks(rep.Links)
	return rep, nil
}

func (ev *evaluation) addRepo(ctx context.Context, r Repo) error {
	if r.Head == "" {
		h, err := gitops.Run(ctx, r.Worktree, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("%s: %w", r.Name, err)
		}
		r.Head = h
	}
	if r.Base == "" {
		r.Base = r.Head
	}
	if !safeName(r.Name) {
		return fmt.Errorf("unsafe repository name %q", r.Name)
	}
	st := &repoState{Repo: r, BaseDir: filepath.Join(ev.baseRoot, r.Name), HeadDir: filepath.Join(ev.headRoot, r.Name), Changed: map[string]bool{}}
	// Trees are exported from commits, never read from the worktree: the
	// result is tied to exact commits, and the agent cannot change what is
	// checked while it runs.
	if err := gitops.ExportTree(ctx, r.Worktree, r.Base, st.BaseDir); err != nil {
		return err
	}
	if err := gitops.ExportTree(ctx, r.Worktree, r.Head, st.HeadDir); err != nil {
		return err
	}
	h, err := diffHunks(ctx, r.Worktree, r.Base, r.Head)
	if err != nil {
		return err
	}
	st.Hunks = h
	for f := range h {
		st.Changed[f] = true
	}
	st.Module, st.GoVersion, _ = goModule(st.HeadDir)
	ev.repos[r.Name] = st
	ev.order = append(ev.order, r.Name)
	return nil
}

func safeName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\`) && !strings.HasPrefix(n, ".")
}

// gateLinks keeps cross-repository links of the gate's kinds that involve a
// task repository, by identity.
func gateLinks(ls []xservice.Link, inTask func(string) bool) map[string]xservice.Link {
	out := map[string]xservice.Link{}
	for _, l := range ls {
		if !GateKinds[l.Kind] || l.From.Repo == l.To.Repo || !inTask(l.From.Repo) && !inTask(l.To.Repo) {
			continue
		}
		out[linkID(l.Kind, l.Contract, side(l.From, ""), side(l.To, ""))] = l
	}
	return out
}

func unionIDs(a, b map[string]xservice.Link) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]xservice.Link{a, b} {
		for id := range m {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ambiguousFrom finds gRPC endpoints linked to more than one service: the
// analyzer could not decide which definition they use.
func ambiguousFrom(links map[string]xservice.Link) map[string][]string {
	svcs := map[string]map[string]bool{}
	for _, l := range links {
		if l.Kind != "grpc" && l.Kind != "grpc_def" {
			continue
		}
		k := l.From.Where() + "|" + string(l.From.Kind)
		if svcs[k] == nil {
			svcs[k] = map[string]bool{}
		}
		svc, _ := contractService(l.Contract)
		svcs[k][svc] = true
	}
	out := map[string][]string{}
	for k, m := range svcs {
		if len(m) > 1 {
			out[k] = sortedKeys(m)
		}
	}
	return out
}

// contractService splits "grpc pkg.Service/Rpc".
func contractService(c string) (service, rpc string) {
	s, r, _ := strings.Cut(strings.TrimPrefix(c, "grpc "), "/")
	return s, r
}

func side(e xservice.Endpoint, commit string) Side {
	return Side{Repo: e.Repo, File: e.File, Line: e.Line, Kind: string(e.Kind), Commit: commit}
}

func (ev *evaluation) commitOf(repo string, head bool) string {
	st := ev.repos[repo]
	if st == nil {
		return ""
	}
	if head {
		return st.Head
	}
	return st.Base
}

// commits are the base..head commits of the task repositories involved.
func (ev *evaluation) commits(involved []string) map[string]string {
	out := map[string]string{}
	for _, r := range involved {
		if st := ev.repos[r]; st != nil {
			out[r] = st.Base + ".." + st.Head
		}
	}
	return out
}

func commitsKey(m map[string]string) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// finish decides a link from its checks (unless already decided), records
// it and adds it to the report.
func (ev *evaluation) finish(ctx context.Context, evalID int64, p *pendingLink, rep *Report) error {
	if p.res.Result == "" {
		p.decide()
	}
	p.res.EvaluatedAt = store.Now()
	if err := saveResult(context.WithoutCancel(ctx), ev.g.DB, ev.taskID, evalID, p.res); err != nil {
		return err
	}
	rep.Links = append(rep.Links, p.res)
	if ev.g.afterLink != nil {
		return ev.g.afterLink(p.res)
	}
	return nil
}

func (g *Gate) emit(ctx context.Context, taskID string, rep Report) {
	if g.Rec == nil {
		return
	}
	var links []map[string]any
	for _, l := range rep.Links {
		links = append(links, map[string]any{"kind": l.Kind, "contract": l.Contract, "from": l.From.Repo, "to": l.To.Repo,
			"result": l.Result, "reason": trunc(l.Reason, 200)})
	}
	g.Rec.Emit(ctx, taskID, "compat.result", map[string]any{"state": rep.State(), "broken": rep.Count(Broken), "untested": rep.Count(Untested),
		"compatible": rep.Count(Compatible), "unaffected": len(rep.Unaffected), "reused": rep.Reused, "error": rep.Error, "links": links})
}

func sortLinks(ls []LinkResult) {
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Kind != ls[j].Kind {
			return ls[i].Kind < ls[j].Kind
		}
		if ls[i].Contract != ls[j].Contract {
			return ls[i].Contract < ls[j].Contract
		}
		return ls[i].From.where() < ls[j].From.where()
	})
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
