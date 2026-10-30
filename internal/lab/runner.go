package lab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/benchmark"
	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
)

// RecordSchema is the version of the provenance format.
const RecordSchema = 1

// Record is an experiment run's provenance: everything needed to know
// what ran and to check that its results are the ones it produced. Its
// paths are relative to the run's directory, so that the record can move.
type Record struct {
	Schema     int       `json:"schema"`
	Experiment string    `json:"experiment"`
	Spec       FileRef   `json:"spec"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished"`
	Lab        string    `json:"lab"` // the bcode build that ran the laboratory
	Repo       struct {
		Path     string   `json:"-"`
		Revision string   `json:"revision"`        // every arm is built from it
		Dirty    []string `json:"dirty,omitempty"` // uncommitted files then (not in any arm's binary)
	} `json:"repo"`
	Env  map[string]string `json:"env,omitempty"`
	Arms []ArmRecord       `json:"arms"`
	Runs []RunRecord       `json:"runs"`
	// Inputs hashes the task definitions and fixtures before the first run
	// and after every run: a run that changed them is refused.
	Inputs string `json:"inputs_sha256"`
	// Decision and Proposals are the outputs, pinned.
	Decision  *FileRef  `json:"decision,omitempty"`
	Proposals []FileRef `json:"proposals,omitempty"`
}

// ArmRecord is how an arm was built and configured.
type ArmRecord struct {
	Name     string     `json:"name"`
	Role     string     `json:"role"`
	Settings []string   `json:"settings,omitempty"`
	Patch    *PatchInfo `json:"patch,omitempty"`
	Binary   FileRef    `json:"binary"`
	// Fingerprint hashes the arm's configuration outside the experiment
	// settings; it is the same for every arm (security policy, sandbox,
	// verification and models unchanged).
	Fingerprint string `json:"config_fingerprint"`
	Home        string `json:"home"`
	// SecurityTests is the security test run of a patched build.
	SecurityTests string `json:"security_tests,omitempty"`
}

// RunRecord is one arm's trial.
type RunRecord struct {
	Arm     string    `json:"arm"`
	Trial   int       `json:"trial"`
	Report  FileRef   `json:"report"`
	Started time.Time `json:"started"`
	Seconds float64   `json:"seconds"`
	Error   string    `json:"error,omitempty"`
}

// Runner runs experiments in isolation:
//   - each arm's binary is built from a fresh git worktree of the base
//     revision (a patch arm's with its patch applied after CheckPatch),
//     never from the working tree;
//   - each arm runs under its own BOUNDEDCODE_HOME holding a copy of the
//     user's configuration and model profiles with the arm's settings
//     applied, and nothing else (no credentials, sessions or state);
//   - its environment is an allowlist (no API keys reach it);
//   - its benchmark copies fixtures into that home, so no user project
//     and no production state is touched;
//   - inputs and every report are hashed as they are produced and checked
//     before the decision.
type Runner struct {
	Repo          string // the BoundedCode repository (a git checkout)
	LabDir        string // where experiment runs are kept
	UserConfigDir string // config.yaml and models/*.yaml are copied from here
	// ModelsDir is written into an arm's configuration when the user's has
	// none (the default is under the data directory, which an isolated home
	// does not share). The server binary needs nothing: by default a name
	// found on PATH, which arms keep.
	ModelsDir string
	// Build builds a worktree into out (default: go build ./cmd/boundedcode).
	Build func(ctx context.Context, src, out string) error
	// Test runs the security tests in a patched worktree (default: go test
	// SecurityTestPackages).
	Test func(ctx context.Context, src string) (string, error)
	// Exec runs an arm's benchmark (default: exec.CommandContext).
	Exec     func(ctx context.Context, cmd *exec.Cmd) error
	Progress func(string)
	Now      func() time.Time
	// KeepWorktrees keeps the build worktrees (they are removed after the
	// build by default; the patch and binary are kept).
	KeepWorktrees bool
}

func (r *Runner) progress(f string, a ...any) {
	if r.Progress != nil {
		r.Progress(fmt.Sprintf(f, a...))
	}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func (r *Runner) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs an experiment and returns its directory, provenance and the
// reports by arm. specPath is the experiment file (patches resolve
// against its directory).
func (r *Runner) Run(ctx context.Context, e *Experiment, specPath string) (string, *Record, map[string][]string, error) {
	if e.Retrospective() {
		return "", nil, nil, errors.New("a retrospective experiment has nothing to run: evaluate it")
	}
	rec := &Record{Schema: RecordSchema, Experiment: e.ID, Started: r.now(), Lab: buildinfo.Version + " " + buildinfo.Commit(), Env: e.Env}
	spec, err := pin(specPath)
	if err != nil {
		return "", nil, nil, err
	}
	rec.Spec = spec
	rev, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, nil, err
	}
	rec.Repo.Path, rec.Repo.Revision = r.Repo, rev
	if st, err := r.git(ctx, "status", "--porcelain"); err == nil && st != "" {
		for _, l := range strings.Split(st, "\n") {
			rec.Repo.Dirty = append(rec.Repo.Dirty, strings.TrimSpace(l))
		}
	}
	dir := filepath.Join(r.LabDir, e.ID, rec.Started.Format("20060102T150405Z"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, nil, err
	}
	if b, err := os.ReadFile(specPath); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "experiment.yaml"), b, 0o600)
	}
	inputs, err := r.inputsHash(e)
	if err != nil {
		return "", nil, nil, err
	}
	rec.Inputs = inputs
	leaks, err := r.leakStrings(e)
	if err != nil {
		return "", nil, nil, err
	}

	// Binaries: the base once, and each patch arm's own.
	baseBin := filepath.Join(dir, "bin", "base")
	if err := r.buildAt(ctx, rev, filepath.Join(dir, "src", "base"), baseBin, nil); err != nil {
		return dir, rec, nil, fmt.Errorf("build the base revision: %w", err)
	}
	bins := map[string]string{}
	for _, a := range e.Arms {
		ar := ArmRecord{Name: a.Name, Role: a.Role, Settings: a.Set.Pairs()}
		bins[a.Name] = baseBin
		if a.Patch != "" {
			ppath := a.Patch
			if !filepath.IsAbs(ppath) {
				ppath = filepath.Join(filepath.Dir(specPath), ppath)
			}
			patch, err := os.ReadFile(ppath)
			if err != nil {
				return dir, rec, nil, fmt.Errorf("arm %s: %w", a.Name, err)
			}
			src := filepath.Join(dir, "src", a.Name)
			bin := filepath.Join(dir, "bin", a.Name)
			info, tests, err := r.buildPatched(ctx, rev, src, bin, patch, leaks, e.EditBudget)
			if err != nil {
				return dir, rec, nil, fmt.Errorf("arm %s: %w", a.Name, err)
			}
			_ = os.WriteFile(filepath.Join(dir, "bin", a.Name+".patch"), patch, 0o600)
			ar.Patch, ar.SecurityTests = &info, tests
			bins[a.Name] = bin
		}
		ref, err := pin(bins[a.Name])
		if err != nil {
			return dir, rec, nil, err
		}
		ar.Binary = FileRef{Path: relTo(dir, ref.Path), SHA256: ref.SHA256}
		home := filepath.Join(dir, "arms", a.Name, "home")
		fp, err := r.prepareHome(home, a.Set)
		if err != nil {
			return dir, rec, nil, fmt.Errorf("arm %s: %w", a.Name, err)
		}
		ar.Home, ar.Fingerprint = relTo(dir, home), fp
		rec.Arms = append(rec.Arms, ar)
	}
	for _, a := range rec.Arms[1:] {
		if a.Fingerprint != rec.Arms[0].Fingerprint {
			return dir, rec, nil, fmt.Errorf("arm %s's configuration differs from arm %s's outside the experiment settings: refused", a.Name, rec.Arms[0].Name)
		}
	}
	if err := writeJSON(filepath.Join(dir, "provenance.json"), rec); err != nil {
		return dir, rec, nil, err
	}

	// Trials, interleaved and rotated so that drift (temperature, other
	// load) falls on every arm alike.
	reports := map[string][]string{}
	for t := 1; t <= e.Trials; t++ {
		for i := range e.Arms {
			a := e.Arms[(i+t-1)%len(e.Arms)]
			out := filepath.Join(dir, "arms", a.Name, "reports", fmt.Sprintf("trial-%d.json", t))
			run := RunRecord{Arm: a.Name, Trial: t, Started: r.now()}
			r.progress("trial %d/%d: arm %s", t, e.Trials, a.Name)
			err := r.runArm(ctx, e, bins[a.Name], filepath.Join(dir, "arms", a.Name, "home"), out)
			run.Seconds = r.now().Sub(run.Started).Seconds()
			if err != nil {
				run.Error = err.Error()
			}
			if ref, perr := pin(out); perr == nil {
				run.Report = FileRef{Path: relTo(dir, out), SHA256: ref.SHA256}
				reports[a.Name] = append(reports[a.Name], out)
			} else if err == nil {
				run.Error = "no report: " + perr.Error()
			}
			rec.Runs = append(rec.Runs, run)
			if h, herr := r.inputsHash(e); herr != nil || h != rec.Inputs {
				return dir, rec, reports, fmt.Errorf("arm %s trial %d changed the task definitions or fixtures: the experiment is void", a.Name, t)
			}
			if err := writeJSON(filepath.Join(dir, "provenance.json"), rec); err != nil {
				return dir, rec, reports, err
			}
			if ctx.Err() != nil {
				return dir, rec, reports, ctx.Err()
			}
		}
	}
	rec.Finished = r.now()
	if err := VerifyRuns(rec, dir); err != nil {
		return dir, rec, reports, err
	}
	return dir, rec, reports, writeJSON(filepath.Join(dir, "provenance.json"), rec)
}

// VerifyRuns checks that every report is still what its run produced
// (dir is the run's directory).
func VerifyRuns(rec *Record, dir string) error {
	var bad []string
	for _, run := range rec.Runs {
		if run.Report.Path == "" {
			continue
		}
		now, err := pin(resolve(dir, run.Report.Path))
		if err != nil || now.SHA256 != run.Report.SHA256 {
			bad = append(bad, run.Report.Path)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("reports changed after their runs: %s", strings.Join(bad, ", "))
	}
	return nil
}

// buildAt builds the revision in a fresh worktree at src, with patch
// applied when given.
func (r *Runner) buildAt(ctx context.Context, rev, src, out string, patch []byte) error {
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		return err
	}
	if _, err := r.git(ctx, "worktree", "add", "--detach", src, rev); err != nil {
		return err
	}
	if !r.KeepWorktrees {
		defer func() { _, _ = r.git(context.WithoutCancel(ctx), "worktree", "remove", "--force", src) }()
	}
	if patch != nil {
		cmd := exec.CommandContext(ctx, "git", "-C", src, "apply", "--whitespace=nowarn", "-")
		cmd.Stdin = bytes.NewReader(patch)
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(string(b)))
		}
	}
	return r.build(ctx, src, out)
}

func (r *Runner) build(ctx context.Context, src, out string) error {
	if r.Build != nil {
		return r.Build(ctx, src, out)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/boundedcode")
	cmd.Dir = src
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// buildPatched checks a patch against the base revision, applies it in its
// own worktree, checks what changed there, runs the security tests and
// builds.
func (r *Runner) buildPatched(ctx context.Context, rev, src, out string, patch []byte, leaks []string, budget EditBudget) (PatchInfo, string, error) {
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		return PatchInfo{}, "", err
	}
	if _, err := r.git(ctx, "worktree", "add", "--detach", src, rev); err != nil {
		return PatchInfo{}, "", err
	}
	if !r.KeepWorktrees {
		defer func() { _, _ = r.git(context.WithoutCancel(ctx), "worktree", "remove", "--force", src) }()
	}
	info, err := CheckPatch(patch, src, budget, leaks)
	if err != nil {
		return info, "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", src, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = bytes.NewReader(patch)
	if b, err := cmd.CombinedOutput(); err != nil {
		return info, "", fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(string(b)))
	}
	// What git sees changed must be what the patch declared.
	changed, err := exec.CommandContext(ctx, "git", "-C", src, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return info, "", err
	}
	for _, l := range strings.Split(string(changed), "\n") {
		if len(l) < 4 {
			continue
		}
		// "XY path", or "XY old -> new" for a rename.
		f := strings.TrimSpace(l[3:])
		if _, after, ok := strings.Cut(f, " -> "); ok {
			f = after
		}
		if why := pathRefusal(f); why != "" {
			return info, "", fmt.Errorf("after applying, %s changed: %s", f, why)
		}
	}
	tests := ""
	if r.Test != nil {
		tests, err = r.Test(ctx, src)
	} else {
		c := exec.CommandContext(ctx, "go", append([]string{"test", "-count=1"}, SecurityTestPackages...)...)
		c.Dir = src
		var b []byte
		b, err = c.CombinedOutput()
		tests = lastLines(string(b), 12)
	}
	if err != nil {
		return info, tests, fmt.Errorf("the patched build fails the security tests: %w\n%s", err, tests)
	}
	return info, tests, r.build(ctx, src, out)
}

// prepareHome writes an arm's isolated home: the user's configuration with
// the arm's settings applied, and the model profiles. It returns the
// configuration's fingerprint.
func (r *Runner) prepareHome(home string, s config.ExperimentSettings) (string, error) {
	cdir := filepath.Join(home, "config")
	if err := os.MkdirAll(filepath.Join(cdir, "models"), 0o700); err != nil {
		return "", err
	}
	src, err := os.ReadFile(filepath.Join(r.UserConfigDir, "config.yaml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if len(bytes.TrimSpace(src)) == 0 {
		src = []byte(fmt.Sprintf("version: %d\n", config.CurrentVersion))
	}
	b, err := applySettings(src, s)
	if err != nil {
		return "", err
	}
	// An isolated home has no models of its own: point at the user's.
	cur, err := config.Load(filepath.Join(r.UserConfigDir, "config.yaml"))
	if err != nil {
		return "", err
	}
	doc, err := parseYAML(b)
	if err != nil {
		return "", err
	}
	if cur.ModelsDir == "" && r.ModelsDir != "" {
		_ = setPath(doc, "models_dir", r.ModelsDir)
	}
	if b, err = renderYAML(doc); err != nil {
		return "", err
	}
	path := filepath.Join(cdir, "config.yaml")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	profiles, _ := filepath.Glob(filepath.Join(r.UserConfigDir, "models", "*.yaml"))
	for _, p := range profiles {
		pb, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(cdir, "models", filepath.Base(p)), pb, 0o600); err != nil {
			return "", err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "", fmt.Errorf("the arm's configuration: %w", err)
	}
	return Fingerprint(cfg), nil
}

// armEnv is the environment an arm runs with: an allowlist of the
// laboratory's own, the experiment's server variables and its home.
func armEnv(home string, extra map[string]string) []string {
	keep := regexp.MustCompile(`^(PATH|HOME|USER|LOGNAME|LANG|LC_[A-Z]+|TERM|TMPDIR|TZ|SHELL|XDG_RUNTIME_DIR|DOCKER_HOST|GOPATH|GOCACHE|GOMODCACHE|GOFLAGS|GOROOT|GOTOOLCHAIN)=`)
	var out []string
	for _, kv := range os.Environ() {
		if keep.MatchString(kv) {
			out = append(out, kv)
		}
	}
	for _, k := range sortedKeys(extra) {
		out = append(out, k+"="+extra[k])
	}
	return append(out, buildinfo.EnvPrefix+"HOME="+home)
}

func (r *Runner) runArm(ctx context.Context, e *Experiment, bin, home, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return err
	}
	var args []string
	switch e.Suite {
	case "direct":
		args = []string{"bench", "direct"}
		for _, d := range e.Tasks.Dirs {
			args = append(args, "--tasks", d)
		}
	default:
		args = []string{"bench", "tasks", "--tasks", e.Tasks.Dirs[0]}
	}
	if len(e.Tasks.Only) > 0 {
		args = append(args, "--only", strings.Join(e.Tasks.Only, ","))
	}
	if e.Tasks.Fixtures != "" {
		args = append(args, "--fixtures", e.Tasks.Fixtures)
	}
	if e.Model != "" {
		args = append(args, "--model", e.Model)
	}
	args = append(args, "--out", out)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.Repo
	cmd.Env = armEnv(home, e.Env)
	logf, err := os.Create(strings.TrimSuffix(out, ".json") + ".log")
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd.Stdout, cmd.Stderr = logf, logf
	if r.Exec != nil {
		return r.Exec(ctx, cmd)
	}
	return cmd.Run()
}

// inputsHash hashes the task definitions and fixtures an experiment uses.
func (r *Runner) inputsHash(e *Experiment) (string, error) {
	dirs := append([]string(nil), e.Tasks.Dirs...)
	fixtures := e.Tasks.Fixtures
	if fixtures == "" {
		fixtures = "benchmarks/fixtures"
	}
	dirs = append(dirs, fixtures)
	h := sha256.New()
	for _, d := range dirs {
		root := d
		if !filepath.IsAbs(root) {
			root = filepath.Join(r.Repo, d)
		}
		var files []string
		err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !de.IsDir() {
				files = append(files, p)
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		sort.Strings(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			rel, _ := filepath.Rel(r.Repo, f)
			fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// leakStrings are the benchmark's own strings a patch may not contain: task
// ids, hidden file paths and expected answers.
func (r *Runner) leakStrings(e *Experiment) ([]string, error) {
	var dirs []string
	for _, d := range e.Tasks.Dirs {
		if !filepath.IsAbs(d) {
			d = filepath.Join(r.Repo, d)
		}
		dirs = append(dirs, d)
	}
	tasks, err := TaskIndex(dirs)
	if err != nil {
		return nil, err
	}
	return LeakStrings(tasks), nil
}

// LeakStrings lists task ids, hidden file paths and expected-answer
// patterns of at least six characters.
func LeakStrings(tasks map[string]benchmark.TaskSpec) []string {
	set := map[string]bool{}
	for id, t := range tasks {
		set[id] = true
		for _, h := range t.Hidden {
			set[h.Path] = true
		}
		for _, x := range t.Expect {
			if p := pathFromPattern(x); p != "" {
				set[p] = true
			} else if len(x) >= 6 && regexp.QuoteMeta(x) == x {
				set[x] = true
			}
		}
	}
	var out []string
	for k := range set {
		if len(k) >= 6 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// relTo is p relative to dir when it is inside it, else p.
func relTo(dir, p string) string {
	if r, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

// resolve is a recorded path read from dir.
func resolve(dir, p string) string {
	if filepath.IsAbs(p) || dir == "" {
		return p
	}
	return filepath.Join(dir, filepath.FromSlash(p))
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{"… " + strconv.Itoa(len(lines)-n) + " lines"}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
