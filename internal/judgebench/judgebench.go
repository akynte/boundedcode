// Package judgebench measures acceptance judges rather than solvers.
//
// The evaluation harness asks "how often does the system solve a task". This
// asks the question the product claim rests on: given a candidate change —
// from any agent, or written to be wrong in a particular way — how often does
// each judge accept work that does not actually solve the task?
//
// Every candidate is labelled by the task's hidden acceptance command, never
// by what its author intended. Then each judge decides:
//
//   - ci runs the repository's visible checks (build, vet, test, format) on
//     the patched tree and accepts when they pass. It is what a CI pipeline
//     with required checks does.
//   - bcode runs the supervisor's completion contract on an indexed repository:
//     the same visible checks, in a verification snapshot, plus the checks on
//     the tests that reach the change.
//
// The two judges run the same visible checks, so the difference between them
// is what the supervisor adds. Neither is given a hidden oracle: the only
// hidden tests a task has are the ones that label it, and judging with them
// would be circular.
package judgebench

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/worktree"
)

// Candidate is one change to judge.
type Candidate struct {
	TaskID string `yaml:"-" json:"task_id"`
	Name   string `yaml:"name" json:"name"`
	// Intent is what the author meant the candidate to be. It is reported
	// beside the result and never used to label it.
	Intent string `yaml:"intent" json:"intent"`
	// Edits describe the candidate as changes to the fixture. Patch, when set,
	// is a unified diff against the fixture instead, which is how a patch
	// produced by an external agent is judged.
	Edits []Edit `yaml:"edits,omitempty" json:"-"`
	Patch string `yaml:"-" json:"-"`
}

// Edit is one change to a fixture file.
type Edit struct {
	Path string `yaml:"path"`
	// Replace swaps the first occurrence of Old for New. An Old that is not
	// present is an error: a candidate that silently did nothing would be
	// judged as the untouched fixture.
	Old string `yaml:"old,omitempty"`
	New string `yaml:"new,omitempty"`
	// Write replaces the whole file, creating it if needed.
	Write *string `yaml:"write,omitempty"`
	// Delete removes the file.
	Delete bool `yaml:"delete,omitempty"`
}

type corpusFile struct {
	Task       string      `yaml:"task"`
	Candidates []Candidate `yaml:"candidates"`
}

// LoadCorpus reads every *.yaml in dir, and every <task-id>/*.patch beside
// them, in a stable order.
func LoadCorpus(dir string) ([]Candidate, error) {
	var out []Candidate
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml":
			body, err := os.ReadFile(p) //nolint:gosec // a corpus file the operator named
			if err != nil {
				return err
			}
			var f corpusFile
			dec := yaml.NewDecoder(strings.NewReader(string(body)))
			dec.KnownFields(true)
			if err := dec.Decode(&f); err != nil {
				return fmt.Errorf("judgebench: %s: %w", p, err)
			}
			for _, c := range f.Candidates {
				c.TaskID = f.Task
				out = append(out, c)
			}
		case ".patch", ".diff":
			body, err := os.ReadFile(p) //nolint:gosec // a corpus file the operator named
			if err != nil {
				return err
			}
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			out = append(out, Candidate{
				TaskID: filepath.Base(filepath.Dir(p)), Name: name, Intent: "patch", Patch: string(body),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Verdict is one judge's decision on one candidate.
type Verdict struct {
	Accepted bool     `json:"accepted"`
	Reasons  []string `json:"reasons,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// Row is one candidate's labels and verdicts.
type Row struct {
	Candidate Candidate `json:"candidate"`
	Solved    bool      `json:"solved"`
	TruthErr  string    `json:"truth_error,omitempty"`
	// TruthOutput is the tail of the hidden command's output when the
	// candidate did not solve the task: what it got wrong, for a person.
	TruthOutput string             `json:"truth_output,omitempty"`
	Verdicts    map[string]Verdict `json:"verdicts"`
}

// Judge decides whether to accept a candidate applied to a fixture.
type Judge interface {
	Name() string
	Judge(ctx context.Context, task eval.Task, patch string) Verdict
}

// Options configure a run.
type Options struct {
	WorkDir string
	Logf    func(format string, args ...any)
}

// Run labels every candidate and puts it before every judge.
func Run(ctx context.Context, tasks []eval.Task, candidates []Candidate, judges []Judge, o Options) ([]Row, error) {
	byID := map[string]eval.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var rows []Row
	for _, c := range candidates {
		task, ok := byID[c.TaskID]
		if !ok {
			return nil, fmt.Errorf("judgebench: candidate %s names unknown task %s", c.Name, c.TaskID)
		}
		patch, err := candidatePatch(ctx, o.WorkDir, task, c)
		if err != nil {
			return nil, fmt.Errorf("judgebench: %s/%s: %w", c.TaskID, c.Name, err)
		}
		row := Row{Candidate: c, Verdicts: map[string]Verdict{}}
		row.Solved, row.TruthOutput, err = groundTruth(ctx, o.WorkDir, task, patch)
		if err != nil {
			row.TruthErr = err.Error()
		}
		for _, j := range judges {
			start := time.Now()
			row.Verdicts[j.Name()] = j.Judge(ctx, task, patch)
			logf("judgebench: %s/%s: %s accepted=%v (%s)", c.TaskID, c.Name, j.Name(),
				row.Verdicts[j.Name()].Accepted, time.Since(start).Round(time.Millisecond))
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Tally is one judge's confusion counts against the labels.
type Tally struct {
	Judge          string `json:"judge"`
	Solved         int    `json:"solved"`
	Unsolved       int    `json:"unsolved"`
	FalseAccepts   int    `json:"false_accepts"`
	FalseRejects   int    `json:"false_rejects"`
	Errors         int    `json:"errors"`
	CandidatesSeen int    `json:"candidates"`
}

// Split separates hand-written candidates from patches another agent
// produced. They answer different questions — whether each mechanism works,
// and how often real work fails in the way it guards against — and a tally
// over both answers neither.
func Split(rows []Row) (constructed, patches []Row) {
	for _, r := range rows {
		if r.Candidate.Patch != "" {
			patches = append(patches, r)
		} else {
			constructed = append(constructed, r)
		}
	}
	return constructed, patches
}

// Tallies summarizes rows per judge. Rows whose label could not be computed
// are excluded: a candidate nobody knows the answer for is not evidence.
func Tallies(rows []Row, judges []string) []Tally {
	var out []Tally
	for _, j := range judges {
		t := Tally{Judge: j}
		for _, r := range rows {
			if r.TruthErr != "" {
				continue
			}
			v, judged := r.Verdicts[j]
			if !judged {
				continue
			}
			t.CandidatesSeen++
			if v.Err != "" {
				// A judge that could not decide is counted, not scored: an
				// error is neither an acceptance nor a rejection.
				t.Errors++
				continue
			}
			if r.Solved {
				t.Solved++
				if !v.Accepted {
					t.FalseRejects++
				}
			} else {
				t.Unsolved++
				if v.Accepted {
					t.FalseAccepts++
				}
			}
		}
		out = append(out, t)
	}
	return out
}

// candidatePatch turns a candidate into a unified diff against its fixture.
func candidatePatch(ctx context.Context, workDir string, task eval.Task, c Candidate) (string, error) {
	if c.Patch != "" {
		return c.Patch, nil
	}
	dir, err := fixtureRepo(ctx, workDir, task)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, e := range c.Edits {
		if err := applyEdit(dir, e); err != nil {
			return "", err
		}
	}
	if _, err := git(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	patch, err := git(ctx, dir, "diff", "--cached", "--binary", "HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(patch) == "" {
		return "", errors.New("the candidate changes nothing")
	}
	return patch, nil
}

func applyEdit(dir string, e Edit) error {
	full, err := worktree.Resolve(dir, e.Path)
	if err != nil {
		return err
	}
	switch {
	case e.Delete:
		return os.Remove(full)
	case e.Write != nil:
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		return os.WriteFile(full, []byte(*e.Write), 0o644) //nolint:gosec // a fixture copy
	default:
		body, err := os.ReadFile(full) //nolint:gosec // a fixture copy
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), e.Old) {
			return fmt.Errorf("%s does not contain the text to replace: %q", e.Path, firstLine(e.Old))
		}
		return os.WriteFile(full, []byte(strings.Replace(string(body), e.Old, e.New, 1)), 0o644) //nolint:gosec // a fixture copy
	}
}

// groundTruth applies the patch, installs the hidden acceptance files and runs
// the hidden command, as the evaluation harness does. A candidate that
// changed a path the task protects has not solved it.
func groundTruth(ctx context.Context, workDir string, task eval.Task, patch string) (bool, string, error) {
	dir, err := fixtureRepo(ctx, workDir, task)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := apply(ctx, dir, patch); err != nil {
		return false, "", err
	}
	changed, err := git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	for _, line := range strings.Split(changed, "\n") {
		if len(line) > 3 {
			for _, protected := range task.Acceptance.MustNotChange {
				if strings.TrimSpace(line[3:]) == protected {
					return false, "changed " + protected + ", which the task protects", nil
				}
			}
		}
	}
	for rel, body := range task.Acceptance.Files {
		if err := worktree.WriteWithin(dir, rel, []byte(body)); err != nil {
			return false, "", err
		}
	}
	solved, out, err := runAcceptance(ctx, dir, task)
	if err != nil || solved || !collides(out) {
		return solved, out, err
	}
	// The package did not compile because a test or helper the candidate
	// wrote shares a name with a hidden one. That is a failure of the grader,
	// not of the code, so the candidate's tests in those packages are set
	// aside and the hidden tests run again. Only then: a candidate that had to
	// change an existing test — a signature change, say — needs its version.
	restored, err := isolateTests(ctx, dir, task)
	if err != nil {
		return false, "", err
	}
	for rel, body := range task.Acceptance.Files {
		if err := worktree.WriteWithin(dir, rel, []byte(body)); err != nil {
			return false, "", err
		}
	}
	solved, out, err = runAcceptance(ctx, dir, task)
	if !solved && err == nil {
		out = "test files returned to the fixture's after a name collision with the hidden tests: " +
			strings.Join(restored, ", ") + "\n" + out
	}
	return solved, out, err
}

// collides reports a compile failure caused by a declaration shared between
// a candidate's test file and a hidden one.
func collides(out string) bool {
	return strings.Contains(out, "redeclared in this block") || strings.Contains(out, "already declared at")
}

// runAcceptance runs the hidden command in dir.
func runAcceptance(ctx context.Context, dir string, task eval.Task) (bool, string, error) {
	runCtx, cancel := context.WithTimeout(ctx, task.Acceptance.Timeout())
	defer cancel()
	//nolint:gosec // the command comes from the task file, part of the evaluation set
	cmd := exec.CommandContext(runCtx, task.Acceptance.Argv[0], task.Acceptance.Argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, "", nil
	}
	const keep = 3000
	if len(out) > keep {
		out = out[len(out)-keep:]
	}
	return false, string(out), nil
}

// isolateTests returns every test file in a package that receives hidden
// acceptance files to its fixture state: added ones are removed, changed or
// deleted ones restored.
//
// The hidden tests judge the candidate's code. A candidate's own tests share
// the package with them, and a test or helper that happens to share a name
// with a hidden one stops the package compiling — a failure of the grader,
// not of the code. The evaluation harness does the same.
func isolateTests(ctx context.Context, dir string, task eval.Task) ([]string, error) {
	pkgs := map[string]bool{}
	for rel := range task.Acceptance.Files {
		pkgs[filepath.Dir(filepath.FromSlash(rel))] = true
	}
	var restored []string
	for pkg := range pkgs {
		base, err := git(ctx, dir, "ls-tree", "--name-only", "HEAD", filepath.ToSlash(pkg)+"/")
		if err != nil {
			return nil, err
		}
		atBase := map[string]bool{}
		for _, f := range strings.Split(strings.TrimSpace(base), "\n") {
			if strings.HasSuffix(f, "_test.go") {
				atBase[f] = true
			}
		}
		entries, err := os.ReadDir(filepath.Join(dir, pkg))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			rel := filepath.ToSlash(filepath.Join(pkg, e.Name()))
			if _, hidden := task.Acceptance.Files[rel]; hidden {
				continue
			}
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") && !atBase[rel] {
				if err := os.Remove(filepath.Join(dir, rel)); err != nil {
					return nil, err
				}
				restored = append(restored, rel)
			}
		}
		for f := range atBase {
			diff, err := git(ctx, dir, "status", "--porcelain", "--", f)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(diff) == "" {
				continue
			}
			if _, err := git(ctx, dir, "checkout", "HEAD", "--", f); err != nil {
				return nil, err
			}
			restored = append(restored, f)
		}
	}
	sort.Strings(restored)
	return restored, nil
}

// fixtureRepo copies a task's fixture into a fresh git repository with one
// commit, the base every candidate is a diff against.
func fixtureRepo(ctx context.Context, workDir string, task eval.Task) (string, error) {
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0o750); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(workDir, "judge-"+task.ID+"-")
	if err != nil {
		return "", err
	}
	if err := copyTree(task.FixturePath(), dir); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"-c", "user.name=judgebench", "-c", "user.email=judgebench@localhost", "commit", "-q", "-m", "fixture"},
	} {
		if _, err := git(ctx, dir, args...); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func apply(ctx context.Context, dir, patch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = strings.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git apply: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=judgebench", "GIT_AUTHOR_EMAIL=judgebench@localhost",
		"GIT_COMMITTER_NAME=judgebench", "GIT_COMMITTER_EMAIL=judgebench@localhost")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, stderr)
	}
	return string(out), nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(p) //nolint:gosec // a fixture file
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644) //nolint:gosec // a fixture copy
	})
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// LoadOracles reads one hidden acceptance suite per task from
// <dir>/<task-id>/, each loaded exactly as the supervisor loads an operator's
// suite. A task without a directory has no oracle.
func LoadOracles(dir string) (map[string]*oracle.Suite, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := map[string]*oracle.Suite{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		suite, err := oracle.Load(filepath.Join(abs, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = suite
	}
	return out, nil
}

// OracleCatchesBase reports whether a task's oracle fails on the unfixed
// fixture. An oracle that passes there says nothing about whether the task was
// done, whatever it says about a candidate.
func OracleCatchesBase(ctx context.Context, workDir string, task eval.Task, suite *oracle.Suite) (bool, error) {
	dir, err := fixtureRepo(ctx, workDir, task)
	if err != nil {
		return false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, c := range suite.Checks {
		for rel, body := range c.Files {
			if err := worktree.WriteWithin(dir, rel, []byte(body)); err != nil {
				return false, err
			}
		}
	}
	for _, c := range suite.Checks {
		runCtx, cancel := context.WithTimeout(ctx, c.Timeout())
		//nolint:gosec // the command comes from the oracle suite under test
		cmd := exec.CommandContext(runCtx, c.Argv[0], c.Argv[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local")
		err := cmd.Run()
		cancel()
		if err != nil {
			return true, nil
		}
	}
	return false, nil
}
