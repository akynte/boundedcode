package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
)

// RuntimeCheck is one thing proved about a task's verification environment.
type RuntimeCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// RuntimeReport is what environment preflight established about one task.
//
// It exists because the previous batch admitted tasks on the strength of the
// official grader — gold resolves, the pre-fix state does not — and those
// checks say nothing about whether *this* system can verify the task. Six of
// eight runs then failed on the environment rather than on the work, and two
// never reached the model at all. Admission now has to prove the runtime, not
// just the instance.
type RuntimeReport struct {
	TaskID string         `json:"task_id"`
	Image  string         `json:"image,omitempty"`
	Checks []RuntimeCheck `json:"checks"`
}

// Usable reports whether every check passed.
func (r RuntimeReport) Usable() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return len(r.Checks) > 0
}

// Blockers names the checks that failed.
func (r RuntimeReport) Blockers() []string {
	var out []string
	for _, c := range r.Checks {
		if !c.OK {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return out
}

// CheckRuntime proves a task's verification environment is usable before the
// task is admitted to a batch.
//
// It runs the task's own declared presets against an untouched copy of the
// fixture, inside the runtime the task names. Nothing here consults the
// acceptance key: the presets come from the repository, and what the official
// grader would say is not this function's business.
func CheckRuntime(ctx context.Context, task Task, workDir string) RuntimeReport {
	rep := RuntimeReport{TaskID: task.ID, Image: task.Origin.Runtime()}
	add := func(name string, ok bool, detail string) {
		rep.Checks = append(rep.Checks, RuntimeCheck{Name: name, OK: ok, Detail: detail})
	}

	if !task.Origin.PinnedRuntime() {
		add("runtime declared", true, "none: this task verifies on the host")
		return rep
	}

	if !container.Pinned(task.Origin.Runtime()) {
		add("image pinned", false, "the reference is not name@sha256:…, so two runs could measure two environments")
		return rep
	}
	add("image pinned", true, task.Origin.Runtime())

	run := &container.Runner{}
	if ok, why := run.Available(ctx); !ok {
		add("container runtime", false, why)
		return rep
	}
	add("container runtime", true, run.Name())

	// Prefetched: the image must already be here. Pulling inside a measured
	// run would put a network download on the clock and make the result
	// depend on a registry.
	//nolint:gosec // the reference is a digest this harness pinned and just validated
	inspect := exec.CommandContext(ctx, "docker", "image", "inspect",
		task.Origin.Runtime(), "--format", "{{.Id}}")
	if out, err := inspect.CombinedOutput(); err != nil {
		add("image prefetched", false, strings.TrimSpace(string(out)))
		return rep
	}
	add("image prefetched", true, "present locally")

	// An untouched copy of the fixture, so nothing here can disturb the
	// fixture the solver will be given.
	dir, err := os.MkdirTemp(workDir, "runtime-")
	if err != nil {
		add("baseline copy", false, err.Error())
		return rep
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := copyTree(task.FixturePath(), dir); err != nil {
		add("baseline copy", false, err.Error())
		return rep
	}
	add("baseline copy", true, filepath.Base(dir))

	spec := sandbox.Spec{
		Image: task.Origin.Runtime(), ImageDir: task.Origin.RuntimeWorkdir,
		Dir: dir, Network: sandbox.NetworkNone, Env: task.Origin.RuntimeEnv,
		Prelude: task.Origin.RuntimePrelude,
	}

	// The worktree has to actually arrive inside the image. A container
	// runtime backed by a VM shares only the host paths it was configured
	// for, and a mount it refuses fails the run before the command starts —
	// with an error that looks nothing like a failing test. Proving it here
	// is the difference between "this task cannot be measured" and a batch of
	// results that are all the same shrug.
	probe, err := run.Command(ctx, spec, "/bin/sh", "-c", "test -r .")
	if err != nil {
		add("worktree mounted", false, err.Error())
		return rep
	}
	if out, err := probe.CombinedOutput(); err != nil {
		add("worktree mounted", false, strings.TrimSpace(firstLineOf(string(out))))
		return rep
	}
	add("worktree mounted", true, dir+" -> "+dirOrSelf(spec))

	// The toolchain the presets will invoke has to exist in the image.
	presets, err := recipe.DiscoverPresets(dir, recipe.Level(task.Verification))
	if err != nil {
		add("declared presets", false, err.Error())
		return rep
	}
	if len(presets) == 0 {
		add("declared presets", false, "the repository declares no verification preset")
		return rep
	}
	names := make([]string, 0, len(presets))
	for _, p := range presets {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	add("declared presets", true, strings.Join(names, ", "))

	// Executable: every preset's program must run inside the image. A preset
	// that cannot start is the failure mode the previous batch hit on every
	// Python task, and it is indistinguishable in the results from a change
	// that broke the build.
	var unrunnable []string
	for _, p := range presets {
		if len(p.Argv) == 0 {
			continue
		}
		cmd, err := run.Command(ctx, spec, p.Argv[0], "--version")
		if err != nil {
			unrunnable = append(unrunnable, p.Name+": "+err.Error())
			continue
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			// --version is not universal — plenty of programs exit non-zero
			// on it — so a non-zero exit is not itself a failure. What counts
			// is the program never starting, and the container runtime
			// refusing to run at all.
			text := string(out)
			switch {
			case strings.Contains(text, "executable file not found"),
				strings.Contains(text, "No module named"),
				strings.Contains(text, "command not found"),
				strings.Contains(text, "Error response from daemon"):
				unrunnable = append(unrunnable, p.Name+": "+strings.TrimSpace(firstLineOf(text)))
			}
		}
	}
	if len(unrunnable) > 0 {
		add("toolchain executable", false, strings.Join(unrunnable, "; "))
		return rep
	}
	add("toolchain executable", true, fmt.Sprintf("%d preset program(s) start", len(presets)))

	// Baseline recorded: every declared preset is run once on the untouched
	// fixture and its result kept.
	//
	// This used to demand that they all pass, and that made real
	// repositories unmeasurable — a linter whose rules postdate the tree, a
	// test the project runs through its own harness, a submodule outside the
	// build. What matters is not that the baseline is green but that it is
	// known, so a measured run can be judged on what it changed. Recording
	// it here also keeps it off the measured clock.
	r := &recipe.Runner{Sandbox: run, Spec: spec}
	base := &recipe.Baseline{
		TaskID: task.ID, FixtureDigest: FixtureDigest(task.FixturePath()),
		Entries: map[string]recipe.BaselineEntry{},
	}
	envDigest := recipe.EnvDigest(spec.Env)
	var red, unnameable []string
	for _, p := range presets {
		res := r.Run(ctx, recipe.Recipe{Name: p.Name, Kind: p.Kind, Argv: p.Argv, Dir: p.Dir},
			dir, "baseline")
		ids, ok, note := recipe.NormalizeFailures(dir, res)
		base.Entries[p.Name] = recipe.BaselineEntry{
			Preset: p.Name, Image: task.Origin.Runtime(),
			Command: recipe.CommandDigest(p), Env: envDigest,
			Status: res.Status, ExitCode: res.ExitCode,
			Failures: ids, Normalizable: ok, Note: note,
			Artifact: res.ArtifactHash,
		}
		if res.Status == recipe.Fail || res.Status == recipe.Error {
			red = append(red, p.Name)
			if !ok {
				unnameable = append(unnameable, p.Name+" ("+note+")")
			}
		}
	}
	if err := SaveBaseline(base); err != nil {
		add("baseline recorded", false, err.Error())
		return rep
	}
	detail := fmt.Sprintf("%d preset(s); %d already failing untouched", len(presets), len(red))
	if len(red) > 0 {
		detail += ": " + strings.Join(red, ", ")
	}
	add("baseline recorded", true, detail)

	// A preset whose failures cannot be named is reported, not hidden. Its
	// comparison falls back to exit status, and a reader has to know that
	// before trusting a "no regression" over it.
	if len(unnameable) > 0 {
		add("failures comparable", true,
			"these are compared by exit status only: "+strings.Join(unnameable, "; "))
	} else {
		add("failures comparable", true, "every failing preset names its failures")
	}

	return rep
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func dirOrSelf(spec sandbox.Spec) string {
	if spec.ImageDir != "" {
		return spec.ImageDir
	}
	return spec.Dir
}

// NewRuntimeWorkDir creates a scratch directory under RuntimeWorkDir and
// returns it with the function that removes it.
//
// It lives here rather than in cmd/bcode for the reason the storescope exemption
// gives: the exemption stays on one package with one justification instead of
// spreading to the whole CLI. The directory is evaluation scratch outside any
// workspace, deleted when the check finishes.
func NewRuntimeWorkDir(prefix string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp(RuntimeWorkDir(), prefix)
	if err != nil {
		return "", func() {}, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// RuntimeWorkDir is where a runtime check may copy a fixture to.
//
// It is not the system temporary directory. A container runtime backed by a
// virtual machine shares only configured host paths, and on a default Docker
// Desktop install /tmp is not one of them, so a bind mount from there is
// refused. The user's home is shared by every such default, and an override
// exists for hosts arranged differently.
func RuntimeWorkDir() string {
	if d := os.Getenv("BC_RUNTIME_TMPDIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	d := filepath.Join(home, ".cache", "boundedcode", "runtime")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return os.TempDir()
	}
	return d
}
