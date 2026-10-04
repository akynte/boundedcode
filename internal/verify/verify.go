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
}

// RepoTarget identifies what to verify.
type RepoTarget struct {
	Name     string
	Worktree string
	Base     string // base commit for diff scope
	TaskID   string
}

// LoadConfig reads <worktree>/.boundedcode/verification.yaml or falls back
// to a language preset.
func LoadConfig(worktree string) (Config, error) {
	p := filepath.Join(worktree, ".boundedcode", "verification.yaml")
	b, err := os.ReadFile(p)
	if err == nil {
		var c Config
		if err := config.DecodeStrict(b, &c); err != nil {
			return c, fmt.Errorf("%s: %w", p, err)
		}
		return c, c.validate()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	return Preset(worktree), nil
}

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

// Preset returns built-in stages for the languages detected in worktree.
func Preset(worktree string) Config {
	c := Config{Version: 1}
	exists := func(f string) bool { _, err := os.Stat(filepath.Join(worktree, f)); return err == nil }
	if exists("go.mod") {
		c.Stages = append(c.Stages,
			Stage{Name: "gofmt", Run: []string{"sh", "-c", `out=$(gofmt -l $(git ls-files '*.go' | grep -v '^vendor/') 2>&1); [ -z "$out" ] || { echo "files need gofmt:"; echo "$out"; exit 1; }`}, Requires: []string{"go.mod"}},
			Stage{Name: "go-build", Run: []string{"go", "build", "./..."}, Requires: []string{"go.mod"}},
			Stage{Name: "go-vet", Run: []string{"go", "vet", "{packages}"}, Requires: []string{"go.mod"}},
			Stage{Name: "go-test", Run: []string{"go", "test", "-count=1", "{packages}"}, Requires: []string{"go.mod"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "golangci-lint", Run: []string{"golangci-lint", "run", "./..."}, Scope: "full", Optional: true, Requires: []string{"go.mod"}},
		)
	}
	if exists("package.json") && exists("tsconfig.json") {
		c.Stages = append(c.Stages,
			Stage{Name: "tsc", Run: []string{"sh", "-c", `if [ -x node_modules/.bin/tsc ]; then node_modules/.bin/tsc --noEmit; else echo "tsc not installed (node_modules missing)"; exit 127; fi`}, Optional: true},
		)
	}
	return c
}

// Run verifies one repository at the given scope. Built-in stages
// (diff-scope, secret-scan) always run.
func (e *Engine) Run(ctx context.Context, t RepoTarget, scope Scope) (Result, error) {
	cfg, err := LoadConfig(t.Worktree)
	if err != nil {
		return Result{}, err
	}
	res := Result{Repository: t.Name, Scope: scope, Started: time.Now().UTC(), Passed: true}
	changed, err := gitops.ChangedFiles(ctx, t.Worktree, t.Base)
	if err != nil {
		return res, err
	}
	res.Stages = append(res.Stages, diffScope(changed, cfg))
	res.Stages = append(res.Stages, e.secretScan(ctx, t))

	packages := []string{"./..."}
	if scope == Targeted {
		if pk, err := e.goImpactedPackages(ctx, t.Worktree, changed); err == nil && len(pk) > 0 {
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
// network). Skipped when gitleaks is not installed.
func (e *Engine) secretScan(ctx context.Context, t RepoTarget) StageResult {
	sr := StageResult{Name: "secret-scan", Command: "git diff | gitleaks stdin"}
	bin, err := exec.LookPath("gitleaks")
	if err != nil {
		sr.Status, sr.Output = "skipped", "gitleaks not installed"
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
	cmd, err := e.Sandbox.Command(sctx, e.spec(t, argv))
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

func (e *Engine) spec(t RepoTarget, argv []string) sandbox.Spec {
	mounts := []sandbox.Mount{{Host: t.Worktree, Target: t.Worktree}}
	env := map[string]string{"GOFLAGS": "-buildvcs=false", "GOTOOLCHAIN": "local", "CI": "1"}
	if common, err := gitops.CommonDir(context.Background(), t.Worktree); err == nil {
		mounts = append(mounts, sandbox.Mount{Host: common, Target: common, ReadOnly: true})
		if admin, err := gitops.AdminDir(context.Background(), t.Worktree); err == nil && admin != common {
			mounts = append(mounts, sandbox.Mount{Host: admin, Target: admin, ReadOnly: true})
		}
	}
	if e.CacheDir != "" {
		gc := filepath.Join(e.CacheDir, "gocache")
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
	var masks []string
	if secrets, err := policy.FindSecretPaths(t.Worktree, 200); err == nil {
		for _, s := range secrets {
			masks = append(masks, filepath.Join(t.Worktree, s))
		}
	}
	return sandbox.Spec{Argv: argv, Workdir: t.Worktree, Mounts: mounts, Env: env, Masks: masks}
}

// goImpactedPackages returns the Go packages containing changed files plus
// every package in the module that (transitively) imports them, including
// via tests. Deterministic and cheap; independent of the code graph.
func (e *Engine) goImpactedPackages(ctx context.Context, worktree string, changed []string) ([]string, error) {
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
	cmd, err := e.Sandbox.Command(ctx, e.spec(RepoTarget{Worktree: worktree}, argv))
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
