// Package python gives a Python repository the same kind of graph a Go one
// gets: modules, classes, functions, methods, definitions, references,
// imports and inheritance, with real source ranges and cross-file edges.
//
// It does not read Python. Reading Python well enough to resolve
// `from pkg.core import handler` across a src-layout package means
// implementing import resolution, MRO and enough of the type system to know
// what a name refers to — which is a type checker, and there is a good one.
// scip-python is Pyright with a SCIP writer on the end, so this package's job
// is to run it correctly and hand its output to internal/scipindex, which
// already turns SCIP into the canonical graph every other language uses.
//
// The three things that make that job non-trivial, and the three things this
// package is actually about:
//
//   - the indexer must be a pinned build, not whatever is on PATH, because
//     symbol naming differs between versions and a benchmark that silently
//     changed indexer changed its own measurement;
//   - the search path must be right, or a src-layout repository produces a
//     large, confident, useless index (see WriteSearchPathConfig);
//   - failure must be legible. A Python repository whose graph is filesystem
//     containment is a repository this did not index, and it says so.
package python

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/scipindex"
	"github.com/akynte/boundedcode/internal/store"
)

// Version is the scip-python build this is written against and tested with.
//
// Pinned rather than "whatever is installed" for the same reason the judgment
// model is: the indexer's symbol naming is part of the graph's identity, and
// two runs that used different indexers are not comparable while looking
// identical in every report. The binary is asked its version and a mismatch
// degrades the run rather than silently producing a different graph.
const Version = "0.6.6"

// SidecarDir is where the pinned indexer lives relative to the installation
// root, beside the TypeScript sidecar and by the same convention.
const SidecarDir = "sidecars/python"

// EnvSidecarDir names the directory holding node_modules/.bin/scip-python,
// for installations that do not keep it beside the binary — and for a
// SWE-bench task, whose indexing runs inside the instance's own environment.
const EnvSidecarDir = "BC_PYTHON_SIDECAR_DIR"

// IndexTimeout bounds one repository. Pyright is fast but Django is 5,000
// files; a repository that cannot be indexed in this much time degrades to
// the lexical layer rather than holding a task open.
const IndexTimeout = 10 * time.Minute

// Indexer implements index.SemanticIndexer for Python.
type Indexer struct {
	// Logf receives the indexer's diagnostics. Nil discards them.
	Logf func(string, ...any)
	// SidecarDir overrides discovery, for tests and for a container whose
	// sidecar is not beside the binary.
	SidecarDir string
	// Timeout overrides IndexTimeout.
	Timeout time.Duration
	// AllowUnpinned proceeds when the located indexer is not Version. It
	// exists for a developer trying a newer build, and it marks the result
	// partial so no benchmark quietly depends on it.
	AllowUnpinned bool
}

func (p *Indexer) Name() string { return "scip-python" }

func (p *Indexer) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}

// Applies reports a Python repository.
func (p *Indexer) Applies(root string) bool { return IsPython(root) }

// Enrich indexes the repository and imports the result into the graph.
func (p *Indexer) Enrich(ctx context.Context, st *store.Store, repositoryID, root string) (report index.SemanticReport, err error) {
	start := time.Now()
	det := Detect(root)
	report = index.SemanticReport{
		Language: "python", Indexer: p.Name(), Version: Version,
		Status: index.SemanticUnavailable, SourceFiles: det.SourceFiles,
	}
	// Named returns so the duration survives: an ordinary defer writing to a
	// local runs after the value has already been copied out.
	defer func() { report.Duration = time.Since(start) }()

	bin, err := p.locate()
	if err != nil {
		report.Reason = err.Error()
		return report, nil
	}
	installed, err := p.probeVersion(ctx, bin)
	if err != nil {
		report.Reason = fmt.Sprintf("%s did not report a version: %v", bin, err)
		return report, nil
	}
	report.Version = installed
	unpinned := installed != Version
	if unpinned && !p.AllowUnpinned {
		report.Reason = fmt.Sprintf("scip-python %s is installed and this is written against %s; "+
			"symbol naming differs between builds, so the graph would not be the one the tests describe",
			installed, Version)
		return report, nil
	}

	restore, wrote, err := WriteSearchPathConfig(root, det)
	if err != nil {
		report.Reason = fmt.Sprintf("writing the search path configuration: %v", err)
		return report, nil
	}
	defer restore()
	if wrote {
		p.logf("scip-python: search path %v (packages %v)", det.ImportRoots, det.Packages)
	}

	out, err := os.CreateTemp("", "bc-scip-python-*.scip")
	if err != nil {
		report.Reason = err.Error()
		return report, nil
	}
	outPath := out.Name()
	_ = out.Close()
	defer func() { _ = os.Remove(outPath) }()

	stderr, excluded, runErr := p.runWithRecovery(ctx, bin, root, outPath, det)
	if runErr != nil {
		report.Reason = fmt.Sprintf("scip-python: %v: %s", runErr, firstLines(stderr, 3))
		return report, nil
	}
	report.ExcludedFiles = excluded
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		report.Reason = "scip-python produced no index"
		return report, nil
	}

	stats, err := scipindex.Import(ctx, st, repositoryID, root, outPath)
	if err != nil {
		report.Reason = fmt.Sprintf("importing the SCIP index: %v", err)
		return report, nil
	}
	report.Documents = stats.Documents
	report.References = stats.Occurrences - stats.Symbols

	if err := p.census(ctx, st, repositoryID, &report); err != nil {
		report.Reason = fmt.Sprintf("counting the imported graph: %v", err)
		return report, nil
	}
	report.Status, report.Reason = classify(report, unpinned, firstLines(stderr, 2))
	if len(excluded) > 0 && report.Status == index.SemanticAvailable {
		report.Status = index.SemanticPartial
		report.Reason = fmt.Sprintf("%d file(s) crashed the type checker and were excluded: %s",
			len(excluded), strings.Join(excluded, ", "))
	}
	return report, nil
}

// runWithRecovery indexes, and retries without whatever killed the process.
//
// Pyright can die on a single pathological file — sympy has one whose
// overload resolution recurses until V8's stack is gone — and when it does,
// the whole repository is lost rather than that file. It names the file
// first, though, which is enough to carry on without it: the exclusion is
// read out of the crash, never written down here, so this is a recovery rule
// rather than a list of repositories somebody has to maintain.
//
// Each retry must exclude something new, and there are at most a few, so a
// repository that fails everywhere fails quickly instead of looping.
func (p *Indexer) runWithRecovery(ctx context.Context, bin, root, out string, det Detected) (string, []string, error) {
	const maxExclusions = 8
	var excluded []string
	for attempt := 0; ; attempt++ {
		stderr, err := p.run(ctx, bin, root, out)
		if err == nil {
			return stderr, excluded, nil
		}
		if attempt >= maxExclusions {
			return stderr, excluded, err
		}
		culprit := crashedFile(stderr, root)
		if culprit == "" {
			return stderr, excluded, err
		}
		for _, already := range excluded {
			if already == culprit {
				// The same file twice means the exclusion did not take, and
				// another attempt would loop.
				return stderr, excluded, err
			}
		}
		excluded = append(excluded, culprit)
		p.logf("scip-python: %s crashed the type checker; retrying without it", culprit)
		if _, _, werr := WriteSearchPathConfig(root, det, excluded...); werr != nil {
			return stderr, excluded, err
		}
	}
}

// crashedFile reads the path pyright reported before dying, as a path
// relative to the repository. Empty when the message names none, which is
// the signal to stop retrying rather than to guess.
func crashedFile(stderr, root string) string {
	const marker = `while type checking file "`
	i := strings.Index(stderr, marker)
	if i < 0 {
		return ""
	}
	rest := stderr[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	rel, err := filepath.Rel(root, rest[:j])
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// classify turns the census into one of the three statuses.
//
// The rule is structural and has no repository in it. An index is available
// when it covers what it was given and produced something a directory
// listing could not: definitions, and relationships between them. It is
// partial when it produced that for some of the repository — which is the
// ordinary outcome for a tree with a generated file pyright cannot parse, and
// is far more useful than nothing. It is unavailable when the graph it left
// behind is indistinguishable from containment, whatever the exit status was.
func classify(r index.SemanticReport, unpinned bool, diagnostics string) (index.SemanticStatus, string) {
	switch {
	case r.Documents == 0:
		return index.SemanticUnavailable, "the index contains no documents"
	case r.ContainmentOnly():
		return index.SemanticUnavailable,
			fmt.Sprintf("the index yielded %d definition(s) and no relationships, which is "+
				"filesystem containment by another name", r.Definitions)
	}
	// Coverage is a ratio so no threshold has to know a repository's size.
	// Below nine tenths the missing tenth is a fact a consumer of symbol
	// lookup should be told, not a failure: the edges that exist are real.
	const coveredEnough = 0.9
	switch {
	case unpinned:
		return index.SemanticPartial, fmt.Sprintf(
			"indexed by scip-python %s rather than the pinned %s", r.Version, Version)
	case r.Covered() < coveredEnough:
		reason := fmt.Sprintf("%d of %d source file(s) produced a document (%.0f%%)",
			r.Documents, r.SourceFiles, r.Covered()*100)
		if diagnostics != "" {
			reason += "; " + diagnostics
		}
		return index.SemanticPartial, reason
	}
	return index.SemanticAvailable, ""
}

// census counts what the import left in the graph.
//
// Read back from the tables rather than from the SCIP file: what matters to
// retrieval is what is in the graph, and a count taken before the import is a
// count of something else.
func (p *Indexer) census(ctx context.Context, st *store.Store, repositoryID string, r *index.SemanticReport) error {
	db := st.Index().SQL()
	scan := func(q string, dst *int, args ...any) error {
		return db.QueryRowContext(ctx, q, args...).Scan(dst)
	}
	ws := st.ID().String()
	if err := scan(`SELECT COUNT(*) FROM nodes WHERE repository_id=? AND json_extract(attrs,'$.source')='scip'`,
		&r.Definitions, repositoryID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, c := range []struct {
		kinds []string
		dst   *int
	}{
		{[]string{"class", "interface"}, &r.Classes},
		{[]string{"function", "method"}, &r.Functions},
		{[]string{"module"}, &r.Imports},
	} {
		q := `SELECT COUNT(*) FROM nodes WHERE repository_id=? AND json_extract(attrs,'$.source')='scip' AND kind IN (` +
			placeholders(len(c.kinds)) + `)`
		args := []any{repositoryID}
		for _, k := range c.kinds {
			args = append(args, k)
		}
		if err := scan(q, c.dst, args...); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	// A cross-file edge is the property that distinguishes a semantic index
	// from a per-file outline, so it is counted by comparing the endpoints'
	// files rather than trusted from the indexer's own summary.
	if err := scan(`
		SELECT COUNT(*) FROM edges e
		  JOIN nodes s ON s.node_id = e.src_id
		  JOIN nodes d ON d.node_id = e.dst_id
		 WHERE e.workspace_id=? AND e.source='scip' AND s.file_id IS NOT NULL
		   AND d.file_id IS NOT NULL AND s.file_id <> d.file_id`,
		&r.CrossFileEdges, ws); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := scan(`SELECT COUNT(*) FROM edges WHERE workspace_id=? AND source='scip' AND kind='implements'`,
		&r.InheritanceEdges, ws); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := scan(`SELECT COUNT(*) FROM edges e JOIN nodes s ON s.node_id=e.src_id
		 WHERE e.workspace_id=? AND s.repository_id=? AND e.kind='contains'`,
		&r.ContainmentEdges, ws, repositoryID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return "''"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// locate finds the pinned indexer.
func (p *Indexer) locate() (string, error) {
	var dirs []string
	if p.SidecarDir != "" {
		dirs = append(dirs, p.SidecarDir)
	}
	if env := os.Getenv(EnvSidecarDir); env != "" {
		dirs = append(dirs, env)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), SidecarDir))
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			dirs = append(dirs, filepath.Join(dir, SidecarDir))
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	for _, d := range dirs {
		bin := filepath.Join(d, "node_modules", ".bin", "scip-python")
		// The candidate directories are the installation root, an operator's
		// environment variable and this process's own ancestors — never
		// anything a model or a repository supplied — and the file name is a
		// constant.
		info, err := os.Stat(bin) //nolint:gosec // G703: operator-supplied search path, constant file name
		if err == nil && !info.IsDir() {
			return bin, nil
		}
	}
	return "", fmt.Errorf("scip-python %s is not installed; expected %s/node_modules/.bin/scip-python "+
		"beside the binary or at $%s", Version, SidecarDir, EnvSidecarDir)
}

func (p *Indexer) probeVersion(ctx context.Context, bin string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "--version") //nolint:gosec // a path this package located
	cmd.Env = hermeticEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// run executes the indexer over one repository.
func (p *Indexer) run(ctx context.Context, bin, root, out string) (string, error) {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = IndexTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	//nolint:gosec // bin is a path this package located; root and out are caller-supplied paths
	cmd := exec.CommandContext(cctx, bin, "index",
		"--cwd", root,
		"--output", out,
		"--project-name", "workspace",
		"--project-version", "0",
		"--quiet")
	cmd.Dir = root
	cmd.Env = hermeticEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	err := cmd.Run()
	if cctx.Err() != nil {
		return stderr.String(), fmt.Errorf("timed out after %s", timeout)
	}
	return stderr.String(), err
}

// hermeticEnv is the environment the indexer runs in.
//
// Nothing here reaches the network, and the environment says so rather than
// relying on it: no proxy is configured, npm is told it is offline, and
// nothing that would make a package manager run is passed through. The
// indexer needs an interpreter's PATH and a home for its cache; it does not
// need a registry, and a benchmark run that installed a package would have
// measured a different repository than the one it was given.
func hermeticEnv() []string {
	keep := map[string]bool{
		"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TMPDIR": true,
		"NODE_OPTIONS": true, "PYTHONPATH": true, "VIRTUAL_ENV": true, "CONDA_PREFIX": true,
	}
	env := []string{
		"npm_config_offline=true",
		"npm_config_audit=false",
		"npm_config_fund=false",
		"NO_UPDATE_NOTIFIER=1",
		"PYTHONNOUSERSITE=1",
		"PYTHONDONTWRITEBYTECODE=1",
		// Explicitly empty rather than absent: a tool that reads a proxy from
		// the environment gets one that goes nowhere.
		"HTTP_PROXY=", "HTTPS_PROXY=", "http_proxy=", "https_proxy=", "ALL_PROXY=", "all_proxy=",
	}
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if ok && keep[k] {
			env = append(env, kv)
		}
	}
	return env
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "; "))
}
