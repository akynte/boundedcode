package compat

import (
	"context"
	"errors"
	"fmt"
	goversion "go/version"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/xservice"
)

// group is the needs served by one composed run: one repository's checks
// with one provider.
type group struct {
	kind, repo, provider string
	needs                []*need
}

func groupNeeds(ps []*pendingLink) []*group {
	var out []*group
	idx := map[string]*group{}
	for _, p := range ps {
		for _, n := range p.needs {
			k := n.kind + "|" + n.repo + "|" + n.provider
			g := idx[k]
			if g == nil {
				g = &group{kind: n.kind, repo: n.repo, provider: n.provider}
				idx[k] = g
				out = append(out, g)
			}
			g.needs = append(g.needs, n)
		}
	}
	return out
}

func (ev *evaluation) runGroup(ctx context.Context, g *group) {
	switch g.kind {
	case "go-cover":
		ev.runGoGroup(ctx, g)
	case "knockout":
		ev.runKnockoutGroup(ctx, g)
	}
	for _, n := range g.needs {
		n.done = true
	}
}

// containerPath is a host path as the sandbox sees it.
func (ev *evaluation) containerPath(p string) string {
	if ev.g.Verify.Sandbox != nil && ev.g.Verify.Sandbox.Isolated() {
		return sandbox.ContainerPath(p)
	}
	return p
}

// writeWork writes a go.work under root that uses the given repositories'
// trees, at the highest go version they declare.
func (ev *evaluation) writeWork(root string, repos []string) (string, error) {
	ver := "1.21"
	var b strings.Builder
	for _, r := range repos {
		_, v, _ := goModule(filepath.Join(root, r))
		if v != "" && goversion.Compare("go"+v, "go"+ver) > 0 {
			ver = v
		}
	}
	fmt.Fprintf(&b, "go %s\n\nuse (\n", ver)
	for _, r := range repos {
		fmt.Fprintf(&b, "\t./%s\n", r)
	}
	b.WriteString(")\n")
	ev.works++
	p := filepath.Join(root, fmt.Sprintf("bc-compat-%d.work", ev.works))
	return p, os.WriteFile(p, []byte(b.String()), 0o644)
}

func (ev *evaluation) composition(repos []string, head bool) map[string]string {
	out := map[string]string{}
	for _, r := range repos {
		out[r] = ev.commitOf(r, head)
	}
	return out
}

func (ev *evaluation) profilePath(root string) (string, error) {
	ev.profiles++
	dir := filepath.Join(root, ".bc-compat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("cover-%d.out", ev.profiles)), nil
}

func (g *group) fail(reason string, checks ...Check) {
	for _, n := range g.needs {
		n.untested = reason
		n.checks = append(n.checks, checks...)
	}
}

// runGoGroup runs a Go repository's `go test` stage in a workspace that
// resolves the provider's module to its candidate tree, with coverage of
// the packages holding the sides, then decides each need:
//   - pass: exercised if coverage shows the side's code ran;
//   - fail: rerun with the base commits (control); broken if the control
//     passes and the failure names the side's package or file, untested
//     otherwise.
func (ev *evaluation) runGoGroup(ctx context.Context, g *group) {
	repos := []string{g.repo}
	if g.provider != g.repo {
		repos = append(repos, g.provider)
	}
	st := ev.repos[g.repo]
	prov := ev.repos[g.provider]
	src := verify.ComposedRun{TaskID: ev.taskID, Root: ev.headRoot, Repo: g.repo, Source: st.Source, Stage: g.needs[0].stage}
	work, err := ev.writeWork(ev.headRoot, repos)
	if err != nil {
		g.fail("could not compose the repositories: " + err.Error())
		return
	}
	src.GoWork = work
	comp := ev.composition(repos, true)
	var checks []Check

	// The provider's module must resolve to its candidate tree. A replace
	// directive or another copy (vendor/) is not what is being checked.
	if g.provider != g.repo {
		r := src
		r.Argv = []string{"go", "list", "-m", "-f", "{{.Dir}}", prov.Module}
		out, err := ev.g.Verify.ComposedOutput(ctx, r)
		c := Check{Purpose: "resolve", Repo: g.repo, Command: strings.Join(r.Argv, " "), Composition: comp, Status: "pass"}
		want := ev.containerPath(filepath.Join(ev.headRoot, g.provider))
		got := strings.TrimSpace(out)
		if err != nil || filepath.Clean(got) != filepath.Clean(want) {
			c.Status, c.Output = "fail", trunc(strings.TrimSpace(got+" "+errText(err)), 300)
			g.fail(fmt.Sprintf("missing dependency: %s does not resolve %s to %s's candidate tree (got %q)", g.repo, prov.Module, g.provider, trunc(got, 120)), c)
			return
		}
		c.Exercised = prov.Module + " => " + g.provider + "@" + short(prov.Head)
		checks = append(checks, c)
	}

	// The packages holding the sides, and every package that imports them.
	var files []string
	for _, n := range g.needs {
		for f := range n.spans {
			if !slices.Contains(files, f) {
				files = append(files, f)
			}
		}
	}
	slices.Sort(files)
	r := src
	r.Argv = []string{"go", "list", "-e", "-f", verify.GoListImportsFormat, "./..."}
	listing, err := ev.g.Verify.ComposedOutput(ctx, r)
	if err != nil {
		g.fail("could not list "+g.repo+"'s packages in the composed tree: "+trunc(err.Error(), 300), checks...)
		return
	}
	pkgs := verify.Impacted(listing, ev.containerPath(filepath.Join(ev.headRoot, g.repo)), files)
	if len(pkgs) == 0 {
		g.fail(fmt.Sprintf("no package of %s holds or imports %s", g.repo, strings.Join(files, ", ")), checks...)
		return
	}
	var cover []string
	for _, f := range files {
		if p := importPathOf(st.Module, f); !slices.Contains(cover, p) {
			cover = append(cover, p)
		}
	}
	profile, err := ev.profilePath(ev.headRoot)
	if err != nil {
		g.fail(err.Error(), checks...)
		return
	}
	r = src
	r.Argv = verify.GoTestCoverArgv(src.Stage, pkgs, cover, ev.containerPath(profile))
	sr, out, blocks, err := ev.run(ctx, "candidate", r, comp, profile)
	cand := checkOf("candidate", g.repo, sr, comp)
	switch {
	case sr.Status == "pass":
		if err != nil {
			cand.Exercised = "no coverage profile: " + err.Error()
			g.fail("the checks passed but wrote no coverage profile, so nothing shows they ran the link", append(checks, cand)...)
			return
		}
		for _, n := range g.needs {
			c := cand
			n.exercised, n.exText = exercised(blocks, st.Module, n)
			c.Exercised = n.exText
			n.checks = append(append(n.checks, checks...), c)
		}
	case sr.Status == "fail" && !strings.HasPrefix(sr.Output, "timeout after"):
		ev.attributeGoFailure(ctx, g, src, repos, pkgs, cover, sr, out, append(checks, cand))
	default:
		g.fail(fmt.Sprintf("%s's checks could not run in the composed tree (%s): %s", g.repo, sr.Status, firstLine(sr.Output)), append(checks, cand)...)
	}
}

// attributeGoFailure decides the needs of a group whose candidate run
// failed, with control runs that differ from it in one respect at a time:
//   - all base commits: if this fails too, the failure predates the task;
//   - the dependent's candidate with the provider's base commit (when both
//     changed): if this passes, the provider's change causes the failure.
//
// A failure is attributed to a side only when the output names its package
// or file. When only the dependent changed, only a build failure naming the
// side's file counts (the dependent no longer builds against the provider);
// a failing test there is the dependent's own failure, which its ordinary
// verification reports.
func (ev *evaluation) attributeGoFailure(ctx context.Context, g *group, src verify.ComposedRun, repos, pkgs, cover []string,
	sr verify.StageResult, out string, checks []Check) {
	st, prov := ev.repos[g.repo], ev.repos[g.provider]
	run := func(purpose string, root string, comp map[string]string) (verify.StageResult, Check, error) {
		r := src
		r.Root = root
		work, err := ev.writeWork(root, repos)
		if err != nil {
			return verify.StageResult{}, Check{}, err
		}
		r.GoWork = work
		profile, err := ev.profilePath(root)
		if err != nil {
			return verify.StageResult{}, Check{}, err
		}
		r.Argv = verify.GoTestCoverArgv(src.Stage, pkgs, cover, ev.containerPath(profile))
		res, _, _, _ := ev.run(ctx, purpose, r, comp, "")
		return res, checkOf(purpose, g.repo, res, comp), nil
	}
	cres, ctl, err := run("control", ev.baseRoot, ev.composition(repos, false))
	if err != nil {
		g.fail(err.Error(), checks...)
		return
	}
	checks = append(checks, ctl)
	if cres.Status != "pass" {
		g.fail(fmt.Sprintf("%s's checks fail with the candidate commits and also with the base commits (%s), so the failure is not attributable to this link",
			g.repo, cres.Status), checks...)
		return
	}
	provChanged := g.provider != g.repo && prov.Base != prov.Head
	depChanged := st.Base != st.Head
	byProvider := provChanged && !depChanged
	if provChanged && depChanged {
		mixed, err := ev.mixedTree(map[string]bool{g.repo: true, g.provider: false})
		if err != nil {
			g.fail(err.Error(), checks...)
			return
		}
		mres, mck, err := run("mixed", mixed, map[string]string{g.repo: st.Head, g.provider: prov.Base})
		if err != nil {
			g.fail(err.Error(), checks...)
			return
		}
		checks = append(checks, mck)
		if mres.Status != "pass" {
			g.fail(fmt.Sprintf("%s's checks fail with its candidate whatever %s's commit (with %s@%s too: %s): the failure is %s's own, which its verification reports",
				g.repo, g.provider, g.provider, short(prov.Base), mres.Status, g.repo), checks...)
			return
		}
		byProvider = true
	}
	digest := firstLine(sr.Digest + "\n" + sr.Output)
	for _, n := range g.needs {
		n.checks = append(n.checks, checks...)
		switch {
		case byProvider && mentions(out, st.Module, n, false):
			n.broken = fmt.Sprintf("%s's checks fail with %s's candidate (%s) and pass with %s's base commit: %s",
				g.repo, g.provider, composition(ev.composition(repos, true)), g.provider, digest)
		case !byProvider && mentions(out, st.Module, n, true):
			n.broken = fmt.Sprintf("%s's candidate no longer builds against %s (%s): %s", g.repo, g.provider, composition(ev.composition(repos, true)), digest)
		case byProvider:
			n.untested = fmt.Sprintf("%s's checks fail with %s's candidate, but in packages that do not hold this side", g.repo, g.provider)
		default:
			n.untested = fmt.Sprintf("%s's checks fail with its own candidate (%s); the failure does not involve this link and its ordinary verification reports it", g.repo, digest)
		}
	}
}

// mixedTree copies repositories into a new tree: each at its head (true)
// or base (false) commit.
func (ev *evaluation) mixedTree(head map[string]bool) (string, error) {
	ev.works++
	root := filepath.Join(filepath.Dir(ev.headRoot), fmt.Sprintf("mixed-%d", ev.works))
	for _, r := range sortedKeys(head) {
		from := ev.repos[r].BaseDir
		if head[r] {
			from = ev.repos[r].HeadDir
		}
		if err := copyTree(from, filepath.Join(root, r)); err != nil {
			return "", err
		}
	}
	return root, nil
}

// copyTree copies regular files and directories (exports hold nothing else).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			info, _ := d.Info()
			mode := os.FileMode(0o644)
			if info != nil && info.Mode()&0o111 != 0 {
				mode = 0o755
			}
			return os.WriteFile(target, b, mode)
		}
		return nil
	})
}

// exercised reports whether coverage shows a need's code ran.
func exercised(blocks []verify.CoverBlock, module string, n *need) (bool, string) {
	var missed []string
	for _, f := range sortedKeys(n.spans) {
		key := importPathOf(module, f) + "/" + path.Base(f)
		ran := func(s span) (bool, bool) { // ran, any block in span
			any := false
			for _, b := range blocks {
				if b.File == key && b.StartLine <= s.Hi && s.Lo <= b.EndLine {
					any = true
					if b.Count > 0 {
						return true, true
					}
				}
			}
			return false, any
		}
		spans := n.spans[f]
		if spans == nil {
			if ok, _ := ran(span{1, 1 << 30}); ok {
				return true, "statements of " + f + " ran"
			}
			missed = append(missed, "no statement of "+f+" ran")
			continue
		}
		for _, s := range spans {
			ok, seen := ran(s)
			if !seen && n.fallback[f] != (span{}) {
				ok, _ = ran(n.fallback[f])
			}
			if ok {
				return true, fmt.Sprintf("%s:%d-%d ran", f, s.Lo, s.Hi)
			}
		}
		missed = append(missed, fmt.Sprintf("%s:%d-%d never ran", f, spans[0].Lo, spans[len(spans)-1].Hi))
	}
	return false, strings.Join(missed, "; ")
}

// mentions reports whether a failing go test output names the side's
// package or file, or the generated package it uses. buildOnly accepts only
// build errors (compiler positions in the side's file).
func mentions(out, module string, n *need, buildOnly bool) bool {
	for f := range n.spans {
		if strings.Contains(out, f+":") && (!buildOnly || goBuildError(out, f)) {
			return true
		}
		if buildOnly {
			continue
		}
		p := importPathOf(module, f)
		if strings.Contains(out, "FAIL\t"+p+"\t") || strings.Contains(out, "FAIL\t"+p+" ") || strings.Contains(out, "# "+p+"\n") ||
			strings.Contains(out, "# "+p+" ") {
			return true
		}
	}
	return !buildOnly && n.pkg != "" && strings.Contains(out, "# "+n.pkg)
}

// goBuildError reports whether the output has a compiler error in file
// (path:line:col: message).
func goBuildError(out, file string) bool {
	for l := range strings.SplitSeq(out, "\n") {
		l = strings.TrimSpace(l)
		rest, ok := strings.CutPrefix(l, file+":")
		if !ok {
			continue
		}
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) == 3 && isDigits(parts[0]) && isDigits(parts[1]) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// runKnockoutGroup runs a repository's test stages that read a
// specification: with the candidate spec, then once per operation with the
// operation removed. A side is exercised only if removing its operation
// makes a stage fail.
func (ev *evaluation) runKnockoutGroup(ctx context.Context, g *group) {
	repos := []string{g.repo}
	if g.provider != g.repo {
		repos = append(repos, g.provider)
	}
	st := ev.repos[g.repo]
	comp := ev.composition(repos, true)
	// variant distinguishes runs with the same command and commits but a
	// different tree (the operation knocked out), for the run record.
	run := func(root string, runs []stageRun, purpose, variant string, c map[string]string) ([]Check, string) {
		var checks []Check
		worst := "pass"
		for _, sr := range runs {
			res, _, _, _ := ev.run(ctx, strings.TrimSpace(purpose+" "+variant), verify.ComposedRun{TaskID: ev.taskID, Root: root, Repo: g.repo, Source: st.Source, Stage: sr.stage, Argv: sr.argv}, c, "")
			checks = append(checks, checkOf(purpose, g.repo, res, c))
			switch {
			case res.Status == "fail" && strings.HasPrefix(res.Output, "timeout after"), res.Status == "error":
				worst = "error"
			case res.Status == "fail" && worst == "pass":
				worst = "fail"
			case res.Status == "skipped" && worst == "pass":
				worst = "skipped"
			}
		}
		return checks, worst
	}
	var runs []stageRun
	for _, n := range g.needs {
		for _, r := range n.runs {
			if !slices.ContainsFunc(runs, func(x stageRun) bool { return slices.Equal(x.argv, r.argv) }) {
				runs = append(runs, r)
			}
		}
	}
	cand, status := run(ev.headRoot, runs, "candidate", "", comp)
	switch status {
	case "pass":
	case "fail":
		ctl, cstatus := run(ev.baseRoot, runs, "control", "", ev.composition(repos, false))
		for _, n := range g.needs {
			n.checks = append(append(n.checks, cand...), ctl...)
			if cstatus == "pass" {
				n.broken = fmt.Sprintf("%s's checks that read %s fail against the candidate (%s) and pass with the base commits", g.repo, n.specFile, composition(comp))
			} else {
				n.untested = fmt.Sprintf("%s's checks fail with the candidate commits and also with the base commits (%s), so the failure is not attributable to this link", g.repo, cstatus)
			}
		}
		return
	default:
		g.fail(fmt.Sprintf("%s's checks could not run in the composed tree (%s)", g.repo, status), cand...)
		return
	}
	for _, n := range g.needs {
		specPath := filepath.Join(ev.headRoot, g.provider, filepath.FromSlash(n.specFile))
		orig, err := os.ReadFile(specPath)
		if err != nil {
			n.untested = "cannot read the candidate specification: " + err.Error()
			n.checks = append(n.checks, cand...)
			continue
		}
		ko, err := xservice.OpenAPIKnockOut(orig, n.specFile, n.method, n.opPath)
		if err != nil {
			n.untested = fmt.Sprintf("cannot remove %s %s from %s for the knock-out check: %v", n.method, n.opPath, n.specFile, err)
			n.checks = append(n.checks, cand...)
			continue
		}
		var kruns []stageRun
		kruns = append(kruns, n.runs...)
		werr := os.WriteFile(specPath, ko, 0o644)
		kchecks, kstatus := cand, "error"
		if werr == nil {
			kchecks, kstatus = run(ev.headRoot, kruns, "knockout", g.provider+"/"+n.specFile+" "+n.method+" "+n.opPath, comp)
		}
		if err := os.WriteFile(specPath, orig, 0o644); err != nil || werr != nil {
			n.untested = "could not prepare the knock-out specification"
			continue
		}
		n.checks = append(append(n.checks, cand...), kchecks...)
		for i := range n.checks[len(cand):] {
			n.checks[len(cand)+i].Exercised = fmt.Sprintf("without %s %s: %s", n.method, n.opPath, kstatus)
		}
		switch kstatus {
		case "fail":
			n.exercised, n.exText = true, fmt.Sprintf("its checks fail when %s %s is removed from %s", n.method, n.opPath, n.specFile)
		case "pass":
			n.exText = fmt.Sprintf("its checks still pass with %s %s removed from %s", n.method, n.opPath, n.specFile)
		default:
			n.untested = fmt.Sprintf("the knock-out run could not complete (%s)", kstatus)
		}
	}
}

// run runs a composed command, or returns its recorded outcome: a run is
// recorded with the commits of its composition, so an interrupted
// evaluation resumes without repeating the checks it completed, and a new
// commit in any repository of the composition invalidates it. A run that
// could not start (status error) or was cancelled is not recorded. profile,
// when set, is the coverage profile the command writes.
func (ev *evaluation) run(ctx context.Context, purpose string, r verify.ComposedRun, comp map[string]string, profile string) (verify.StageResult, string, []verify.CoverBlock, error) {
	key := runKey(purpose, r, ev.commits(sortedKeys(comp)), comp)
	if rec, ok := lookupRun(ctx, ev.g.DB, ev.taskID, key); ok {
		var err error
		if profile != "" && rec.Blocks == nil {
			err = errors.New("no coverage recorded")
		}
		return rec.Result, rec.Output, rec.Blocks, err
	}
	sr, out := ev.g.Verify.RunComposed(ctx, r)
	var blocks []verify.CoverBlock
	var err error
	if profile != "" && sr.Status == "pass" {
		blocks, err = verify.ParseCoverProfile(profile)
	}
	if ctx.Err() == nil && sr.Status != "error" {
		_ = saveRun(context.WithoutCancel(ctx), ev.g.DB, ev.taskID, key, runRecord{Result: sr, Output: tailStr(out, 256<<10), Blocks: blocks})
	}
	return sr, out, blocks, err
}

// runKey identifies a run: its purpose, repository, command (without the
// scratch coverage path), and the commits of every repository it composed.
func runKey(purpose string, r verify.ComposedRun, ranges, comp map[string]string) string {
	var argv []string
	for _, a := range r.Argv {
		if !strings.HasPrefix(a, "-coverprofile=") {
			argv = append(argv, a)
		}
	}
	return strings.Join([]string{"run", purpose, r.Repo, strings.Join(argv, "\x00"), commitsKey(ranges), commitsKey(comp), r.Stage.Timeout.D().String()}, "\x01")
}

func tailStr(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func checkOf(purpose, repo string, sr verify.StageResult, comp map[string]string) Check {
	c := Check{Purpose: purpose, Repo: repo, Command: sr.Command, Composition: comp, Status: sr.Status, ExitCode: sr.ExitCode, DurationMS: sr.DurationMS}
	if sr.Status != "pass" {
		out := sr.Digest
		if out == "" {
			out = sr.Output
		}
		c.Output = trunc(strings.TrimSpace(out), 800)
	}
	return c
}

func firstLine(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "…"))
		if l != "" && !strings.HasPrefix(l, "#") && l != "FAIL" {
			return trunc(l, 200)
		}
	}
	return ""
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
