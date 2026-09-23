// Package container runs a command inside a pinned container image.
//
// It exists for one problem the other runners cannot solve. bubblewrap and
// Landlock confine a process to parts of *this* machine; they do not change
// which machine it is. Verifying a repository this project did not write
// means running that repository's own checks, and those need the interpreter,
// the compiler and the installed dependencies the repository was built
// against. On the host they are whatever the operator happens to have — here,
// Python 3.14 with no pytest, and a Go toolchain four years newer than the
// tree. Every verification failed, none of the failures were about the code,
// and a benchmark built on them measures the host.
//
// A task that names the image it was built for gets it. The isolation is at
// least what the other runners give: no network, and nothing of the host
// visible but the worktree.
package container

import (
	"context"
	"fmt"
	"os/exec"
	"os/user"
	"strings"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// DefaultBinary is the container runtime. Docker is what the benchmark images
// are published for and what the official grader already requires.
const DefaultBinary = "docker"

// Runner runs commands inside spec.Image.
type Runner struct {
	// Binary is the container runtime; empty means DefaultBinary.
	Binary string
	// Inner, when set, is applied to the command that launches the container.
	// It is not applied inside it: the container is the boundary.
	Inner sandbox.Runner
}

func (r *Runner) binary() string {
	if r.Binary != "" {
		return r.Binary
	}
	return DefaultBinary
}

func (r *Runner) Name() string { return "container" }

// Layers reports what this runner applies. The container is a mount and
// network namespace of its own, which is the same guarantee bubblewrap makes
// and the reason it can stand in for it.
func (r *Runner) Layers() []sandbox.Layer {
	return []sandbox.Layer{sandbox.LayerContainer}
}

// Available reports whether the runtime is usable. It probes rather than
// looking for the binary, because an installed docker whose daemon is not
// running fails at the first task instead of at the check.
func (r *Runner) Available(ctx context.Context) (bool, string) {
	bin := r.binary()
	if _, err := exec.LookPath(bin); err != nil {
		return false, fmt.Sprintf("%s is not on PATH", bin)
	}
	cmd := exec.CommandContext(ctx, bin, "info", "--format", "{{.ServerVersion}}")
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Sprintf("the %s daemon is not reachable: %s",
			bin, strings.TrimSpace(firstLine(string(out))))
	}
	return true, ""
}

// Pinned reports whether ref names an immutable image.
//
// A tag is refused rather than resolved here. Resolving it would silently
// pick whatever the tag points at during the run, and two runs of the same
// benchmark could then measure two different environments while reporting the
// same name. The digest is resolved once, by preflight, and recorded.
func Pinned(ref string) bool {
	_, digest, ok := strings.Cut(ref, "@")
	return ok && strings.HasPrefix(digest, "sha256:") && len(digest) == len("sha256:")+64
}

// Command builds the container invocation. The command is not started.
func (r *Runner) Command(ctx context.Context, spec sandbox.Spec, argv ...string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("container: no command")
	}
	if spec.Image == "" {
		return nil, fmt.Errorf("container: the spec names no image")
	}
	if !Pinned(spec.Image) {
		return nil, fmt.Errorf("container: image %q is not pinned to a digest; "+
			"a measurement must name what it ran against, and a tag does not", spec.Image)
	}
	if spec.Dir == "" {
		return nil, fmt.Errorf("container: the spec names no working directory to mount")
	}
	dir := spec.ImageDir
	if dir == "" {
		dir = spec.Dir
	}

	args := []string{
		"run", "--rm",
		// The command to run is the argv this call was given, never the
		// image's own ENTRYPOINT. Without this, an image whose Dockerfile
		// sets one — every pinned SWE-Bench Pro Verified image does,
		// ENTRYPOINT ["/bin/bash"], with no CMD — turns the constructed
		// command into an *argument* to that entrypoint instead of the
		// command itself: `docker run image /bin/sh -c "go build ..."`
		// actually execs `/bin/bash /bin/sh -c "go build ..."`, and bash
		// treats "/bin/sh" as a script file to source, which is a binary,
		// which fails as "cannot execute binary file" — a real failure
		// this package produced against a real pinned image before this
		// fix, not a hypothetical one.
		"--entrypoint", "/bin/sh",
		// Nothing is acquired mid-verification. A check that reaches the
		// network is a check whose result depends on the network.
		"--network", "none",
		// The worktree, and nothing else of this machine.
		"--volume", spec.Dir + ":" + dir,
		"--workdir", dir,
		// The image's own environment is the point of using it, so the host's
		// is not forwarded. Only the few variables that make output readable
		// and reproducible are set, plus whatever the task declared.
		"--env", "NO_COLOR=1",
		"--env", "TERM=dumb",
	}
	for _, kv := range spec.Env {
		args = append(args, "--env", kv)
	}
	args = append(args, spec.Image)

	// The command runs as the image's own user.
	//
	// Forcing the host's uid looked safer and was wrong: these images install
	// their dependencies into root's home, and /root is not traversable by
	// anybody else, so the toolchain silently lost its module cache and tried
	// to download — inside a container with no network. The image is the
	// environment being borrowed, and that includes who it runs as.
	//
	// What that costs is ownership: files the verification creates would
	// belong to root, and the harness that has to read the diff and delete
	// the worktree afterwards does not. So ownership is handed back before
	// the command's exit status is returned, which is a change to the
	// worktree's metadata and never to its contents.
	script := shellJoin(argv)
	if spec.Prelude != "" {
		// Its output is discarded and its status ignored: it restores the
		// image's own state, and if it cannot, the command that follows will
		// say so in terms of the thing being verified.
		script = "{ " + spec.Prelude + " ; } >/dev/null 2>&1; " + script
	}
	// --entrypoint /bin/sh above makes /bin/sh the container's process
	// unconditionally; everything from here on is CMD, i.e. /bin/sh's own
	// arguments, never a second "/bin/sh" to invoke.
	if u := currentUser(); u != "" {
		args = append(args, "-c", script+"; rc=$?; chown -R "+u+" . 2>/dev/null || true; exit $rc")
	} else {
		args = append(args, "-c", script)
	}

	if r.Inner != nil {
		inner, err := r.Inner.Command(ctx, sandbox.Spec{
			Network: spec.Network, Dir: spec.Dir, TmpDir: spec.TmpDir,
		}, append([]string{r.binary()}, args...)...)
		if err != nil {
			return nil, err
		}
		return inner, nil
	}
	//nolint:gosec // the argv is the command the task declared; the container is the confinement
	cmd := exec.CommandContext(ctx, r.binary(), args...)
	cmd.Dir = spec.Dir
	return cmd, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// currentUser renders uid:gid for --user, or empty when it cannot be read.
func currentUser() string {
	u, err := user.Current()
	if err != nil || u.Uid == "" {
		return ""
	}
	return u.Uid + ":" + u.Gid
}

// shellJoin renders argv as one shell command, quoting every word so that a
// path with a space or a test name with a bracket survives.
func shellJoin(argv []string) string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		out = append(out, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(out, " ")
}
