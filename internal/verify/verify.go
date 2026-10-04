// Package verify is the deterministic verification engine. The LLM is never
// the judge of its own work: stages are commands with exit codes, run in the
// sandbox, with structured and persisted results.
package verify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// Scope selects which stages run.
type Scope string

// Scopes.
const (
	// Targeted runs fast, impact-selected checks during iteration.
	Targeted Scope = "targeted"
	// Full is the merge-candidate gate.
	Full Scope = "full"
)

// Stage is one verification step.
type Stage struct {
	Name string `yaml:"name"`
	// Run is argv. Placeholders: {packages} (impact-selected Go packages or
	// ./... in full scope).
	Run []string `yaml:"run"`
	// Scope: targeted, full, or always (default).
	Scope   string          `yaml:"scope"`
	Timeout config.Duration `yaml:"timeout"`
	// Optional stages are skipped (not failed) when their tool is missing.
	Optional bool `yaml:"optional"`
	// Requires lists files that must exist for the stage to apply (e.g. go.mod).
	Requires []string `yaml:"requires"`
}

// Config is a repository's verification configuration
// (.boundedcode/verification.yaml, or a preset chosen by language).
type Config struct {
	Version int     `yaml:"version"`
	Stages  []Stage `yaml:"stages"`
	// MaxChangedFiles bounds diff scope (0 = 200).
	MaxChangedFiles int `yaml:"max_changed_files"`
	// DenyPaths are extra workspace-relative globs the change must not touch.
	DenyPaths []string `yaml:"deny_paths"`
}

// StageResult is the outcome of one stage.
type StageResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // pass | fail | skipped | error
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Command    string `json:"command"`
	Output     string `json:"output,omitempty"` // tail, redacted
}

// Result is one verification run of one repository.
type Result struct {
	ID         int64         `json:"id"`
	Repository string        `json:"repository"`
	Scope      Scope         `json:"scope"`
	Passed     bool          `json:"passed"`
	Stages     []StageResult `json:"stages"`
	Started    time.Time     `json:"started"`
	Duration   time.Duration `json:"duration_ns"`
	Packages   []string      `json:"packages,omitempty"`
}

// Failures returns the failing stages.
func (r Result) Failures() []StageResult {
	var out []StageResult
	for _, s := range r.Stages {
		if s.Status == "fail" || s.Status == "error" {
			out = append(out, s)
		}
	}
	return out
}

// Signature is a stable fingerprint of the failure set, used to detect loops
// (the same failure repeating across attempts).
func (r Result) Signature() string {
	var parts []string
	for _, f := range r.Failures() {
		parts = append(parts, f.Name+":"+firstErrorLine(f.Output))
	}
	return strings.Join(parts, "|")
}

// Engine runs verification.
type Engine struct {
	Sandbox sandbox.Sandbox
	// CacheDir holds per-task build caches mounted into the sandbox.
	CacheDir string
	// GoModCache is mounted read-only so builds work with --network none.
	GoModCache string
	DB         *sql.DB
	Rec        *telemetry.Recorder
	// Gitleaks is the secret scanner binary ("" = gitleaks on PATH).
	Gitleaks string
}

// RepoTarget identifies what to verify.
type RepoTarget struct {
	Name     string
	Worktree string
	Base     string // base commit for diff scope
	TaskID   string
	// Source is the repository's own checkout (from the task ledger, never
	// derived from the agent-writable worktree). Dependencies installed there
	// (node_modules) are mounted read-only into the worktree's sandbox.
	Source string
}

// ConfigPath is the repository-relative verification config.
const ConfigPath = ".boundedcode/verification.yaml"

// LoadConfig returns the verification config of a task worktree. It is read
// from the base commit, never from the worktree: the agent can write the
// worktree, and must not be able to redefine how its own work is judged.
// Without a config at base, the language preset for the files at base is
// used. An empty base (no task context) falls back to the working tree.
func LoadConfig(ctx context.Context, worktree, base string) (Config, error) {
	var (
		b   []byte
		err error
	)
	if base == "" {
		b, err = os.ReadFile(filepath.Join(worktree, ConfigPath))
	} else {
		var out string
		out, err = gitops.Run(ctx, worktree, "cat-file", "blob", base+":"+ConfigPath)
		b = []byte(out)
		if err != nil && !blobMissing(ctx, worktree, base, ConfigPath) {
			return Config{}, fmt.Errorf("read %s at %s: %w", ConfigPath, short(base), err)
		}
		if err != nil {
			err = os.ErrNotExist
		}
	}
	if err == nil {
		var c Config
		if err := config.DecodeStrict(b, &c); err != nil {
			return c, fmt.Errorf("%s: %w", ConfigPath, err)
		}
		return c, c.validate()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	files, err := baseFiles(ctx, worktree, base)
	if err != nil {
		return Config{}, err
	}
	return presetFor(files), nil
}

func blobMissing(ctx context.Context, worktree, base, path string) bool {
	_, err := gitops.Run(ctx, worktree, "cat-file", "-e", base+":"+path)
	if err == nil {
		return false
	}
	// cat-file -e fails both for a missing path and a bad commit; tell them apart.
	_, cerr := gitops.Run(ctx, worktree, "cat-file", "-e", base+"^{commit}")
	return cerr == nil
}

// baseFiles lists the repository files at base (or in the working tree when
// base is empty).
func baseFiles(ctx context.Context, worktree, base string) (map[string]bool, error) {
	files := map[string]bool{}
	if base == "" {
		for _, f := range []string{"go.mod", "package.json", "tsconfig.json"} {
			if _, err := os.Stat(filepath.Join(worktree, f)); err == nil {
				files[f] = true
			}
		}
		return files, nil
	}
	out, err := gitops.Run(ctx, worktree, "ls-tree", "-r", "--name-only", base)
	if err != nil {
		return nil, err
	}
	for f := range strings.SplitSeq(out, "\n") {
		if f != "" {
			files[f] = true
		}
	}
	return files, nil
}

func short(sha string) string { return sha[:min(len(sha), 12)] }

func (c Config) validate() error {
	for _, s := range c.Stages {
		if s.Name == "" || len(s.Run) == 0 {
			return fmt.Errorf("verification stage needs name and run: %+v", s)
		}
		switch s.Scope {
		case "", "always", "targeted", "full":
		default:
			return fmt.Errorf("stage %s: invalid scope %q", s.Name, s.Scope)
		}
		if err := policy.CheckCommand(s.Run); err != nil {
			return fmt.Errorf("stage %s: %w", s.Name, err)
		}
	}
	return nil
}

// presetFor returns built-in stages for the languages found in files.
// Optional stages are skipped when their tool is not installed in the
// sandbox image.
func presetFor(files map[string]bool) Config {
	c := Config{Version: 1}
	anySuffix := func(suffix string) bool {
		for f := range files {
			if strings.HasSuffix(f, suffix) {
				return true
			}
		}
		return false
	}
	if files["go.mod"] {
		c.Stages = append(c.Stages,
			Stage{Name: "gofmt", Run: []string{"sh", "-c", `out=$(gofmt -l $(git ls-files '*.go' | grep -v '^vendor/') 2>&1); [ -z "$out" ] || { echo "files need gofmt:"; echo "$out"; exit 1; }`}, Requires: []string{"go.mod"}},
			Stage{Name: "go-build", Run: []string{"go", "build", "./..."}, Requires: []string{"go.mod"}},
			Stage{Name: "go-vet", Run: []string{"go", "vet", "{packages}"}, Requires: []string{"go.mod"}},
			Stage{Name: "go-test", Run: []string{"go", "test", "-count=1", "{packages}"}, Requires: []string{"go.mod"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "golangci-lint", Run: []string{"golangci-lint", "run", "./..."}, Scope: "full", Optional: true, Requires: []string{"go.mod"}},
		)
	}
	if files["package.json"] {
		// Dependencies are never installed by verification (no network): they
		// come read-only from the repository's own checkout (see
		// sandbox.DependencyMounts). A script the project declares, with no
		// dependencies installed, fails: skipping it would pass the change
		// with no tests run. A script the project lacks is skipped.
		const missingDeps = `{ echo "node_modules missing: install the project's dependencies in the repository checkout (e.g. npm ci) so verification can run them"; exit 1; }`
		npmScript := func(name string) string {
			return `node -e 'process.exit(require("./package.json").scripts?.["` + name + `"] ? 0 : 3)'; rc=$?; ` +
				`[ $rc = 3 ] && { echo "no ` + name + ` script"; exit 127; }; [ $rc = 0 ] || exit $rc; ` +
				`[ -d node_modules ] || ` + missingDeps + `; npm run --silent ` + name
		}
		tsc := `if [ -x node_modules/.bin/tsc ]; then node_modules/.bin/tsc --noEmit; ` +
			`elif [ ! -d node_modules ] && grep -q '"typescript"' package.json; then ` + missingDeps + `; ` +
			`else echo "tsc not installed"; exit 127; fi`
		c.Stages = append(c.Stages,
			Stage{Name: "tsc", Run: []string{"sh", "-c", tsc}, Optional: true, Requires: []string{"tsconfig.json"}},
			Stage{Name: "npm-lint", Run: []string{"sh", "-c", npmScript("lint")}, Optional: true, Requires: []string{"package.json"}},
			Stage{Name: "npm-test", Run: []string{"sh", "-c", npmScript("test")}, Optional: true, Requires: []string{"package.json"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "npm-build", Run: []string{"sh", "-c", npmScript("build")}, Scope: "full", Optional: true, Requires: []string{"package.json"}},
		)
	}
	if anySuffix(".tf") {
		// Offline checks only: validate/plan need providers and credentials.
		c.Stages = append(c.Stages, Stage{Name: "terraform-fmt", Optional: true,
			Run: []string{"sh", "-c", `command -v terraform >/dev/null || exit 127; terraform fmt -check -recursive -diff`}})
	}
	if anySuffix("Chart.yaml") {
		c.Stages = append(c.Stages, Stage{Name: "helm-lint", Optional: true,
			Run: []string{"sh", "-c", `command -v helm >/dev/null || exit 127; for c in $(git ls-files '*Chart.yaml'); do helm lint "$(dirname "$c")" || exit 1; done`}})
	}
	return c
}

// Run verifies one repository at the given scope. Built-in stages
// (diff-scope, secret-scan) always run.
func (e *Engine) Run(ctx context.Context, t RepoTarget, scope Scope) (Result, error) {
	cfg, err := LoadConfig(ctx, t.Worktree, t.Base)
	if err != nil {
		return Result{}, err
	}
	res := Result{Repository: t.Name, Scope: scope, Started: time.Now().UTC(), Passed: true}
	changed, err := gitops.ChangedFiles(ctx, t.Worktree, t.Base)
	if err != nil {
		return res, err
	}
	res.Stages = append(res.Stages, diffScope(changed, cfg))
	res.Stages = append(res.Stages, e.secretScan(ctx, t, scope))

	packages := []string{"./..."}
	if scope == Targeted {
		if pk, err := e.goImpactedPackages(ctx, t, changed); err == nil && len(pk) > 0 {
			packages = pk
		}
	}
	res.Packages = packages
	for _, st := range cfg.Stages {
		if st.Scope == "full" && scope != Full || st.Scope == "targeted" && scope != Targeted {
			continue
		}
		res.Stages = append(res.Stages, e.runStage(ctx, t, st, packages))
	}
	for _, s := range res.Stages {
		if s.Status == "fail" || s.Status == "error" {
			res.Passed = false
		}
	}
	res.Duration = time.Since(res.Started)
	if e.DB != nil {
		b, _ := json.Marshal(res.Stages)
		r, err := e.DB.ExecContext(ctx, `INSERT INTO verification_runs(task_id, repository, scope, passed, stages, started_at, duration_ms) VALUES(?,?,?,?,?,?,?)`,
			t.TaskID, t.Name, string(scope), res.Passed, string(b), res.Started.Format(time.RFC3339Nano), res.Duration.Milliseconds())
		if err != nil {
			return res, err
		}
		res.ID, _ = r.LastInsertId()
	}
	e.Rec.Emit(ctx, t.TaskID, "verify.result", map[string]any{"repo": t.Name, "scope": scope, "passed": res.Passed,
		"failures": names(res.Failures()), "ms": res.Duration.Milliseconds()})
	return res, nil
}

func names(s []StageResult) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.Name)
	}
	return out
}

func diffScope(changed []string, cfg Config) StageResult {
	sr := StageResult{Name: "diff-scope", Status: "pass", Command: "(built-in)"}
	limit := cfg.MaxChangedFiles
	if limit == 0 {
		limit = 200
	}
	var problems []string
	if len(changed) > limit {
		problems = append(problems, fmt.Sprintf("%d files changed (limit %d)", len(changed), limit))
	}
	for _, f := range changed {
		if policy.IsSecretPath(f) {
			problems = append(problems, "touches secret path: "+f)
		}
		if policy.IsProtectedPath(f) {
			problems = append(problems, "touches protected path: "+f+" (verification, CI and ownership config cannot be changed by a task)")
		}
		for _, g := range cfg.DenyPaths {
			if ok, _ := filepath.Match(g, f); ok {
				problems = append(problems, "touches denied path: "+f)
			}
		}
	}
	if len(problems) > 0 {
		sr.Status, sr.ExitCode, sr.Output = "fail", 1, strings.Join(problems, "\n")
	}
	return sr
}

// secretScan pipes the task diff to gitleaks on the host (it never needs the
// network). Without gitleaks it is skipped during iteration but is an error
// in the full gate: a merge candidate is never produced unscanned.
func (e *Engine) secretScan(ctx context.Context, t RepoTarget, scope Scope) StageResult {
	sr := StageResult{Name: "secret-scan", Command: "git diff | gitleaks stdin"}
	name := e.Gitleaks
	if name == "" {
		name = "gitleaks"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		sr.Status, sr.Output = "skipped", "gitleaks not installed"
		if scope == Full {
			sr.Status, sr.Output = "error", "gitleaks not installed: the full gate requires a secret scan (scripts/install-deps.sh installs it)"
		}
		return sr
	}
	diff, err := gitops.Diff(ctx, t.Worktree, t.Base, false)
	if err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, "stdin", "--no-banner", "--redact", "--exit-code", "1")
	cmd.Stdin = strings.NewReader(diff)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	sr.DurationMS = time.Since(start).Milliseconds()
	sr.Output = tail(telemetry.Redact(out.String()), 3000)
	var ee *exec.ExitError
	switch {
	case err == nil:
		sr.Status = "pass"
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		sr.Status, sr.ExitCode = "fail", 1
	default:
		sr.Status = "error"
	}
	return sr
}

func (e *Engine) runStage(ctx context.Context, t RepoTarget, st Stage, packages []string) StageResult {
	sr := StageResult{Name: st.Name}
	for _, req := range st.Requires {
		if _, err := os.Stat(filepath.Join(t.Worktree, req)); err != nil {
			// A stage that applied at base cannot be switched off by deleting
			// its required file.
			if t.Base != "" && !blobMissing(ctx, t.Worktree, t.Base, req) {
				sr.Status, sr.ExitCode, sr.Output = "fail", 1, req+" exists at the base commit but was removed by the change"
				return sr
			}
			sr.Status, sr.Output = "skipped", "missing "+req
			return sr
		}
	}
	var argv []string
	for _, a := range st.Run {
		if a == "{packages}" {
			argv = append(argv, packages...)
			continue
		}
		argv = append(argv, a)
	}
	sr.Command = strings.Join(argv, " ")
	if err := policy.CheckCommand(argv); err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr
	}
	timeout := st.Timeout.D()
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	spec, err := e.spec(t, argv)
	if err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr
	}
	cmd, err := e.Sandbox.Command(sctx, spec)
	if err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = 10 * time.Second
	start := time.Now()
	err = cmd.Run()
	sr.DurationMS = time.Since(start).Milliseconds()
	sr.Output = tail(telemetry.Redact(out.String()), 6000)
	var ee *exec.ExitError
	switch {
	case err == nil:
		sr.Status = "pass"
	case errors.Is(err, exec.ErrNotFound):
		// Host execution (sandbox none) of a tool that is not installed.
		if st.Optional {
			sr.Status, sr.Output = "skipped", argv[0]+" not installed"
		} else {
			sr.Status, sr.Output = "error", argv[0]+" not installed (required by stage "+st.Name+")"
		}
	case sctx.Err() != nil:
		sr.Status, sr.Output = "fail", "timeout after "+timeout.String()+"\n"+sr.Output
	case errors.As(err, &ee):
		sr.ExitCode = ee.ExitCode()
		// 127 = command not found (in the sandbox, docker reports 127 too).
		if st.Optional && (sr.ExitCode == 127 || strings.Contains(sr.Output, "executable file not found")) {
			sr.Status = "skipped"
		} else {
			sr.Status = "fail"
		}
	default:
		sr.Status = "error"
	}
	return sr
}

func (e *Engine) spec(t RepoTarget, argv []string) (sandbox.Spec, error) {
	mounts := []sandbox.Mount{{Host: t.Worktree, Target: t.Worktree}}
	env := map[string]string{"GOFLAGS": "-buildvcs=false", "GOTOOLCHAIN": "local", "CI": "1"}
	if common, err := gitops.CommonDir(context.Background(), t.Worktree); err == nil {
		mounts = append(mounts, sandbox.Mount{Host: common, Target: common, ReadOnly: true})
		if admin, err := gitops.AdminDir(context.Background(), t.Worktree); err == nil && admin != common {
			mounts = append(mounts, sandbox.Mount{Host: admin, Target: admin, ReadOnly: true})
		}
	}
	if e.CacheDir != "" {
		// One build cache per task: a cache shared across tasks could be
		// poisoned by code one task's tests write into it.
		key := t.TaskID
		if key == "" {
			key = "_adhoc"
		}
		gc := filepath.Join(e.CacheDir, "gocache", key)
		_ = os.MkdirAll(gc, 0o700)
		mounts = append(mounts, sandbox.Mount{Host: gc, Target: gc})
		env["GOCACHE"] = gc
	}
	if e.GoModCache != "" {
		if _, err := os.Stat(e.GoModCache); err == nil {
			mounts = append(mounts, sandbox.Mount{Host: e.GoModCache, Target: e.GoModCache, ReadOnly: true})
			env["GOMODCACHE"] = e.GoModCache
			env["GOPROXY"] = "off"
			env["GOFLAGS"] = "-buildvcs=false -mod=mod"
		}
	}
	var scratch []string
	if e.Sandbox != nil && e.Sandbox.Isolated() {
		deps, err := sandbox.DependencyMounts(t.Source, t.Worktree)
		if err != nil {
			return sandbox.Spec{}, fmt.Errorf("dependency mounts: %w", err)
		}
		mounts = append(mounts, deps.Mounts...)
		scratch = deps.Scratch
	}
	var masks []string
	secrets, err := policy.FindSecretPaths(t.Worktree, policy.MaxSecretMasks)
	if err != nil {
		return sandbox.Spec{}, fmt.Errorf("secret masks: %w", err)
	}
	for _, s := range secrets {
		masks = append(masks, filepath.Join(t.Worktree, s))
	}
	return sandbox.Spec{Argv: argv, Workdir: t.Worktree, Mounts: mounts, Scratch: scratch, Env: env, Masks: masks}, nil
}

// goImpactedPackages returns the Go packages containing changed files plus
// every package in the module that (transitively) imports them, including
// via tests. Deterministic and cheap; independent of the code graph.
func (e *Engine) goImpactedPackages(ctx context.Context, t RepoTarget, changed []string) ([]string, error) {
	worktree := t.Worktree
	if _, err := os.Stat(filepath.Join(worktree, "go.mod")); err != nil {
		return nil, err
	}
	var goFiles []string
	for _, f := range changed {
		if strings.HasSuffix(f, ".go") || filepath.Base(f) == "go.mod" || filepath.Base(f) == "go.sum" {
			goFiles = append(goFiles, f)
		}
	}
	if len(goFiles) == 0 {
		return nil, nil
	}
	for _, f := range goFiles {
		if filepath.Base(f) == "go.mod" || filepath.Base(f) == "go.sum" {
			return []string{"./..."}, nil
		}
	}
	argv := []string{"go", "list", "-e", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{join .Imports \",\"}},{{join .TestImports \",\"}},{{join .XTestImports \",\"}}", "./..."}
	spec, err := e.spec(t, argv)
	if err != nil {
		return nil, err
	}
	cmd, err := e.Sandbox.Command(ctx, spec)
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return impacted(string(out), worktree, goFiles), nil
}

// impacted computes the reverse-dependency closure from `go list` output.
func impacted(listing, worktree string, changedFiles []string) []string {
	type pkg struct {
		dir     string
		imports []string
	}
	pkgs := map[string]pkg{}
	for line := range strings.SplitSeq(strings.TrimSpace(listing), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		pkgs[f[0]] = pkg{dir: f[1], imports: strings.Split(f[2], ",")}
	}
	seed := map[string]bool{}
	for _, cf := range changedFiles {
		dir := filepath.Join(worktree, filepath.Dir(cf))
		for ip, p := range pkgs {
			if p.dir == dir {
				seed[ip] = true
			}
		}
	}
	affected := map[string]bool{}
	for ip := range seed {
		affected[ip] = true
	}
	for changed := true; changed; {
		changed = false
		for ip, p := range pkgs {
			if affected[ip] {
				continue
			}
			if slices.ContainsFunc(p.imports, func(i string) bool { return affected[i] }) {
				affected[ip] = true
				changed = true
			}
		}
	}
	out := make([]string, 0, len(affected))
	for ip := range affected {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func tail(s string, n int) string {
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

func firstErrorLine(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "ok ") || strings.HasPrefix(l, "?") || strings.HasPrefix(l, "…") {
			continue
		}
		if len(l) > 120 {
			l = l[:120]
		}
		return l
	}
	return ""
}

// LoadRuns returns persisted verification results for a task.
func LoadRuns(ctx context.Context, db *sql.DB, taskID string, limit int) ([]Result, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, repository, scope, passed, stages, started_at, duration_ms FROM verification_runs
		WHERE task_id = ? ORDER BY id DESC LIMIT ?`, taskID, max(limit, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var stages, started string
		var ms int64
		if err := rows.Scan(&r.ID, &r.Repository, &r.Scope, &r.Passed, &stages, &started, &ms); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(stages), &r.Stages)
		r.Started, _ = time.Parse(time.RFC3339Nano, started)
		r.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, r)
	}
	return out, rows.Err()
}
