package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/recipe"
)

// PrepareRuntime derives a measurable runtime from a task's official image.
//
// The official images are built to run one instance's tests, and their
// dependency caches are only as complete as that needs. Caddy's cannot
// compile its own tree offline: `go build ./...` reaches for modules the
// image never fetched. A repository-wide verification preset is broader than
// the grader's single package, so it hits that gap.
//
// The measured run must not use the network, so the fetching happens here,
// once, before measurement — and the result is a new image with its own
// digest. That keeps both properties the report needs: the environment is
// immutable and named, and nothing is acquired while the clock is running.
//
// The warm step is chosen from what the repository is, not from which
// repository it is.
func PrepareRuntime(ctx context.Context, task Task, workDir string) (string, error) {
	if !task.Origin.PinnedRuntime() {
		return "", nil
	}
	warm := warmCommand(task.FixturePath(), task.Verification)
	if warm == "" {
		return task.Origin.RuntimeImage, nil
	}

	// The tag is derived from the base image and the warm step, so preparing
	// twice reuses the result and a changed step produces a different image.
	sum := sha256.Sum256([]byte(task.Origin.RuntimeImage + "\x00" + warm +
		"\x00" + strings.Join(task.Origin.RuntimeEnv, "\x00")))
	tag := "bc-runtime/" + strings.ToLower(task.ID) + ":" + hex.EncodeToString(sum[:6])

	if id, err := imageID(ctx, tag); err == nil && id != "" {
		return repoDigest(tag, id), nil
	}

	dir, err := os.MkdirTemp(workDir, "prepare-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The warm step must run in the same environment the verification will.
	// Without this the install lands in whichever interpreter the image's
	// default PATH points at — for these images, the base conda environment
	// rather than the one the project lives in — and the tools are still
	// missing at the moment they are needed.
	var env strings.Builder
	for _, kv := range task.Origin.RuntimeEnv {
		fmt.Fprintf(&env, "ENV %s\n", kv)
	}
	dockerfile := fmt.Sprintf("FROM %s\n%sWORKDIR %s\nRUN %s\n",
		task.Origin.RuntimeImage, env.String(), workdirOf(task), warm)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		return "", err
	}

	//nolint:gosec // the tag and the base image are values this harness computed
	cmd := exec.CommandContext(ctx, "docker", "build", "--quiet", "--tag", tag, dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("preparing the runtime for %s: %w\n%s", task.ID, err, out.String())
	}
	id, err := imageID(ctx, tag)
	if err != nil {
		return "", err
	}
	return repoDigest(tag, id), nil
}

// warmCommand is what a repository needs fetched before it can be verified
// offline, or empty when it needs nothing.
//
// It reads the repository's own manifests and its own declared presets, so a
// language this harness has never seen gets no warm step rather than a wrong
// one, and no repository is named anywhere.
func warmCommand(fixture string, level string) string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(fixture, name))
		return err == nil
	}
	switch {
	case has("vendor/modules.txt"):
		// Vendored: everything needed is already in the tree.
		return ""
	case has("go.mod"):
		// `all` rather than the build list: a verification preset runs the
		// tests too, and their dependencies are not in the build list.
		return "go mod download all"
	}
	if mods := missingPythonTools(fixture, level); len(mods) > 0 {
		// The official images install what the project's tests need, which
		// is not what its linters need: a repository that configures black
		// and mypy still gets an image without them, and the preset then
		// fails for a reason that has nothing to do with the change. Pip
		// resolves against the image's own interpreter, so the versions that
		// arrive are the ones that era supports — which is exactly what
		// installing them on the host got wrong.
		return "python -m pip install --no-input --quiet " + strings.Join(mods, " ")
	}
	return ""
}

// pythonTools maps a preset's module to the distribution that provides it,
// for the cases where the two differ.
var pythonTools = map[string]string{}

// missingPythonTools names the Python modules this repository's own presets
// invoke. Which of them the image already has is not knowable here, and
// installing one that is present is a no-op, so the list is not filtered.
func missingPythonTools(fixture string, level string) []string {
	presets, err := recipe.DiscoverPresets(fixture, recipe.Level(level))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range presets {
		// Only the `python -m <module>` shape: anything else is a program
		// whose provenance this cannot guess.
		if len(p.Argv) < 3 || !strings.HasPrefix(p.Argv[0], "python") || p.Argv[1] != "-m" {
			continue
		}
		mod := p.Argv[2]
		// compileall and unittest are standard library.
		if mod == "compileall" || mod == "unittest" {
			continue
		}
		if dist, ok := pythonTools[mod]; ok {
			mod = dist
		}
		if !seen[mod] {
			seen[mod] = true
			out = append(out, mod)
		}
	}
	sort.Strings(out)
	return out
}

func workdirOf(task Task) string {
	if task.Origin.RuntimeWorkdir != "" {
		return task.Origin.RuntimeWorkdir
	}
	return "/testbed"
}

func imageID(ctx context.Context, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", ref, "--format", "{{.Id}}").Output()
	if err != nil {
		return "", err //nolint:wrapcheck // the caller treats any error as "not built yet"
	}
	return strings.TrimSpace(string(out)), nil
}

// repoDigest renders a locally built image as name@sha256:… so the same
// pinning rule applies to it as to a pulled one.
func repoDigest(tag, id string) string {
	name, _, _ := strings.Cut(tag, ":")
	return name + "@" + id
}
