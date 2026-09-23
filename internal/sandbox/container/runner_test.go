package container_test

// This package had no tests before the Evidence Suite v1 execution-
// integration audit found a real bug here: Command() never overrode the
// target image's own ENTRYPOINT, so an image whose Dockerfile sets one to a
// shell — every pinned SWE-Bench Pro Verified image does, ENTRYPOINT
// ["/bin/bash"], with no CMD — turned the constructed command into an
// *argument* to that entrypoint instead of the command itself: `docker run
// image /bin/sh -c "go build ..."` actually exec'd `/bin/bash /bin/sh -c
// "go build ..."`, and bash tried to source "/bin/sh" as a script file,
// which is a binary, and failed with "cannot execute binary file" — a real
// failure reproduced against the real pinned future-architect/vuls image
// before the fix (--entrypoint /bin/sh, added to Command's args) and gone
// after it (see internal/eval/evidencesuite_pipeline_test.go, which runs
// the real Runner against that real image end to end).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
)

// dockerSharableTempDir returns a fresh directory under this repository's
// own .boundedcode-runs/ scratch root — reliably within Docker Desktop's
// file-sharing allowlist on a host where the OS temp dir is not, unlike
// t.TempDir().
func dockerSharableTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
	scratch := filepath.Join(dir, ".boundedcode-runs", "container-runner-test", t.Name())
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch) })
	return scratch
}

const pinnedRef = "example.test/image@sha256:0000000000000000000000000000000000000000000000000000000000000000"

func TestCommandOverridesTheImagesOwnEntrypoint(t *testing.T) {
	r := &container.Runner{}
	cmd, err := r.Command(context.Background(),
		sandbox.Spec{Image: pinnedRef, Dir: "/host/worktree", ImageDir: "/app"},
		"go", "build", "./...")
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	if !containsAdjacent(args, "--entrypoint", "/bin/sh") {
		t.Fatalf("expected --entrypoint /bin/sh in the docker invocation, got: %v", args)
	}
}

func TestCommandNeverPassesArgvDirectlyAsCMD(t *testing.T) {
	// The pre-fix bug specifically came from a branch that, absent a
	// Prelude and absent a resolvable current user, appended the task's
	// own argv straight after the image name with no shell wrapper and no
	// entrypoint override — which is exactly what breaks against a
	// shell-entrypoint image. Every path must now go through `-c <script>`
	// under the explicit /bin/sh entrypoint.
	r := &container.Runner{}
	cmd, err := r.Command(context.Background(),
		sandbox.Spec{Image: pinnedRef, Dir: "/host/worktree"},
		"go", "vet", "./...")
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	found := false
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			found = true
			// shellJoin quotes each argv word individually ('go' 'vet'
			// './...'), so check for the quoted form rather than an
			// unquoted substring.
			if !strings.Contains(args[i+1], "'go'") || !strings.Contains(args[i+1], "'vet'") {
				t.Fatalf("expected the -c script to contain the requested command, got %q", args[i+1])
			}
		}
	}
	if !found {
		t.Fatalf("expected a -c <script> pair in the docker invocation, got: %v", args)
	}
	// The bare-argv tail ("append(args, argv...)" with no wrapper at all)
	// must not appear: every element after the image name is part of the
	// -c script's own single string argument, not a repetition of argv's
	// individual words as separate docker CLI arguments.
	imageIdx := indexOf(args, pinnedRef)
	if imageIdx < 0 {
		t.Fatalf("image reference not found in args: %v", args)
	}
	tail := args[imageIdx+1:]
	if len(tail) != 2 || tail[0] != "-c" {
		t.Fatalf("expected exactly [-c, <script>] after the image, got: %v", tail)
	}
}

func TestCommandRefusesAnUnpinnedImage(t *testing.T) {
	r := &container.Runner{}
	_, err := r.Command(context.Background(),
		sandbox.Spec{Image: "example.test/image:latest", Dir: "/host/worktree"}, "go", "build")
	if err == nil {
		t.Fatal("expected an error for an unpinned (tag, not digest) image reference")
	}
}

func TestCommandRefusesNoImage(t *testing.T) {
	r := &container.Runner{}
	_, err := r.Command(context.Background(), sandbox.Spec{Dir: "/host/worktree"}, "go", "build")
	if err == nil {
		t.Fatal("expected an error when spec.Image is empty")
	}
}

func containsAdjacent(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

// TestCommandActuallyExecutesAgainstAShellEntrypointImage is the live,
// Docker-gated proof: not just that the args look right, but that a real
// command actually runs against a real image whose own ENTRYPOINT is a
// shell. It uses a minimal, always-available public image rather than the
// large pinned benchmark images, so it does not require materialize_task.py
// to have run first.
func TestCommandActuallyExecutesAgainstAShellEntrypointImage(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable")
	}
	// A tiny, always-pullable image whose ENTRYPOINT is exactly the shape
	// that broke: a shell, so this test is a general regression guard, not
	// specific to the SWE-Bench Pro Verified images.
	const shellEntrypointImage = "docker.io/library/busybox:latest"
	pull := exec.Command("docker", "pull", "-q", shellEntrypointImage)
	if err := pull.Run(); err != nil {
		t.Skip("could not pull busybox:latest (no network in this environment)")
	}
	digestOut, err := exec.Command("docker", "inspect", shellEntrypointImage,
		"--format", "{{index .RepoDigests 0}}").Output()
	if err != nil || len(digestOut) == 0 {
		t.Skip("could not resolve a digest for busybox:latest")
	}
	pinned := strings.TrimSpace(string(digestOut))

	// A plain t.TempDir() (under the OS temp dir) is often outside Docker
	// Desktop's file-sharing allowlist on a dev host — confirmed directly
	// by this test failing with "mounts denied" against it. Use a
	// directory under the repository itself, which is reliably shared.
	dir := dockerSharableTempDir(t)
	r := &container.Runner{}
	cmd, err := r.Command(context.Background(), sandbox.Spec{Image: pinned, Dir: dir},
		"echo", "regression-guard-ok")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed against a real shell-entrypoint image: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "regression-guard-ok") {
		t.Fatalf("expected output to contain the echoed marker, got: %s", out)
	}
}
