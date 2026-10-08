package verify

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// Composed runs: the cross-repository compatibility gate (internal/compat)
// runs a repository's existing test stage in a tree that holds several task
// repositories, each exported at an exact commit, so that the repository's
// checks see another repository's candidate (a provider's changed
// definitions) instead of the copy they would resolve on their own (a
// vendored or published version). The stage comes from the repository's
// verification config at its base commit, like every other stage: the
// agent cannot redefine it.

// TestStages returns the test stages (full scope) of a repository's
// verification config at base.
func TestStages(ctx context.Context, worktree, base string) ([]Stage, error) {
	cfg, err := LoadConfig(ctx, worktree, base)
	if err != nil {
		return nil, err
	}
	var out []Stage
	for _, st := range cfg.Stages {
		if st.Scope != "targeted" && isTestStage(st) {
			out = append(out, st)
		}
	}
	return out, nil
}

// IsGoTest reports whether a stage runs `go test`, the form the gate can add
// coverage flags to.
func IsGoTest(st Stage) bool { return len(st.Run) >= 2 && st.Run[0] == "go" && st.Run[1] == "test" }

// goTestValueFlags take their value as the next argument.
var goTestValueFlags = map[string]bool{"-run": true, "-skip": true, "-timeout": true, "-tags": true, "-p": true, "-parallel": true,
	"-count": true, "-bench": true, "-benchtime": true, "-cpu": true, "-exec": true, "-ldflags": true, "-gcflags": true,
	"-mod": true, "-o": true, "-covermode": true, "-coverpkg": true, "-coverprofile": true, "-vet": true, "-shuffle": true,
	"-asmflags": true, "-modfile": true, "-overlay": true, "-pkgdir": true, "-toolexec": true}

// GoTestCoverArgv rewrites a `go test` stage to test the given packages with
// set-mode coverage of coverPkgs written to profile. The stage's own flags
// are kept, except coverage flags and package arguments.
func GoTestCoverArgv(st Stage, packages, coverPkgs []string, profile string) []string {
	argv := []string{"go", "test"}
	args := st.Run[2:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, hasValue := strings.Cut(a, "=")
		switch {
		case a == "{packages}" || !strings.HasPrefix(a, "-"):
			continue // a package argument
		case name == "-cover" || name == "-covermode" || name == "-coverpkg" || name == "-coverprofile" || name == "-count":
			if !hasValue && goTestValueFlags[name] {
				i++
			}
			continue
		}
		argv = append(argv, a)
		if !hasValue && goTestValueFlags[name] && i+1 < len(args) {
			i++
			argv = append(argv, args[i])
		}
	}
	argv = append(argv, "-count=1", "-covermode=set", "-coverpkg="+strings.Join(coverPkgs, ","), "-coverprofile="+profile)
	return append(argv, packages...)
}

// ComposedRun is one command in a composed tree.
type ComposedRun struct {
	TaskID string
	Root   string // host directory holding one exported tree per repository
	Repo   string // subdirectory of Root the command runs in
	Source string // the repository's own checkout, for its installed dependencies
	Stage  Stage  // timeout, requires; Name for the result
	Argv   []string
	// GoWork is a go.work file under Root; Go commands then resolve the
	// workspace's modules to their trees under Root.
	GoWork string
}

// RunComposed runs a command of a composed tree in the sandbox, with the
// same mounts, caches, masks and outcome classification as a verification
// stage.
func (e *Engine) RunComposed(ctx context.Context, r ComposedRun) (StageResult, string) {
	sr := StageResult{Name: r.Stage.Name, Command: strings.Join(r.Argv, " ")}
	dir := filepath.Join(r.Root, r.Repo)
	for _, req := range r.Stage.Requires {
		if _, err := os.Stat(filepath.Join(dir, req)); err != nil {
			sr.Status, sr.Output = "skipped", "missing "+req
			return sr, ""
		}
	}
	if err := policy.CheckCommand(r.Argv); err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr, ""
	}
	spec, err := e.composedSpec(r)
	if err != nil {
		sr.Status, sr.Output = "error", err.Error()
		return sr, ""
	}
	return e.execStage(ctx, r.Stage, r.Argv, spec, sr)
}

// ComposedOutput runs a command of a composed tree and returns its
// standard output (go list).
func (e *Engine) ComposedOutput(ctx context.Context, r ComposedRun) (string, error) {
	if err := policy.CheckCommand(r.Argv); err != nil {
		return "", err
	}
	spec, err := e.composedSpec(r)
	if err != nil {
		return "", err
	}
	cmd, err := e.Sandbox.Command(ctx, spec)
	if err != nil {
		return "", err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", strings.Join(r.Argv, " "), err, tail(strings.TrimSpace(stderr.String()), 600))
	}
	return string(out), nil
}

func (e *Engine) composedSpec(r ComposedRun) (sandbox.Spec, error) {
	dir := filepath.Join(r.Root, r.Repo)
	spec, err := e.spec(RepoTarget{Name: r.Repo, Worktree: dir, TaskID: r.TaskID, Source: r.Source}, r.Argv)
	if err != nil {
		return spec, err
	}
	// The whole tree is visible (and writable: it is a scratch export);
	// secret paths of every repository in it are masked.
	spec.Mounts = append([]sandbox.Mount{{Host: r.Root, Target: r.Root}}, spec.Mounts...)
	secrets, err := policy.FindSecretPaths(r.Root, policy.MaxSecretMasks)
	if err != nil {
		return spec, fmt.Errorf("secret masks: %w", err)
	}
	spec.Masks = nil
	for _, s := range secrets {
		spec.Masks = append(spec.Masks, filepath.Join(r.Root, s))
	}
	if r.GoWork != "" {
		p := r.GoWork
		if e.Sandbox != nil && e.Sandbox.Isolated() {
			p = sandbox.ContainerPath(p)
		}
		spec.Env["GOWORK"] = p
		// Workspace mode accepts only -mod=readonly: modules resolve to the
		// workspace's trees, other dependencies to the module cache.
		spec.Env["GOFLAGS"] = "-buildvcs=false"
	} else {
		spec.Env["GOWORK"] = "off"
	}
	return spec, nil
}

// CoverBlock is one block of a Go coverage profile.
type CoverBlock struct {
	File               string // import path + "/" + file name
	StartLine, EndLine int
	Count              int
}

// ParseCoverProfile reads a Go coverage profile.
func ParseCoverProfile(path string) ([]CoverBlock, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []CoverBlock
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// file.go:12.34,15.2 3 1
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) != 3 {
			continue
		}
		start, end, ok := strings.Cut(fields[0], ",")
		if !ok {
			continue
		}
		sl, _ := strconv.Atoi(strings.SplitN(start, ".", 2)[0])
		el, _ := strconv.Atoi(strings.SplitN(end, ".", 2)[0])
		n, _ := strconv.Atoi(fields[2])
		out = append(out, CoverBlock{File: line[:colon], StartLine: sl, EndLine: el, Count: n})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("empty coverage profile")
	}
	return out, nil
}

// Impacted is the reverse-dependency closure of the packages holding the
// given files, from `go list -e -f '{{.ImportPath}}\t{{.Dir}}\t…imports'`
// output (see goImpactedPackages).
func Impacted(listing, root string, files []string) []string { return impacted(listing, root, files) }

// GoListImportsFormat is the go list -f template Impacted reads.
const GoListImportsFormat = "{{.ImportPath}}\t{{.Dir}}\t{{join .Imports \",\"}},{{join .TestImports \",\"}},{{join .XTestImports \",\"}}"
