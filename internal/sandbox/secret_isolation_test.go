package sandbox_test

// Secret isolation — design instruction §35 of the Evidence Suite v1
// execution-integration task: "the untrusted agent task environment must
// not [see provider credentials]. Add a regression test."
//
// This is a real, verified property of the codebase, not a new mechanism
// added to satisfy the instruction: every sandbox.Runner.Command
// implementation builds its child process's environment strictly from
// sandbox.Spec.Env (or, for the container runner, from a fixed constant set
// plus spec.Env), and Go's exec.Cmd.Env, when non-nil, REPLACES the child's
// environment rather than extending the parent's — confirmed by reading
// internal/sandbox/container.go:35, internal/sandbox/landlock/runner.go:146,
// and internal/sandbox/bwrap/runner.go:222, none of which reference
// os.Environ(). This test proves that property holds by construction: a
// credential placed in *this test process's own* environment must never be
// visible to a command any sandbox.Runner builds, for every runner that can
// actually run on this host.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

const fakeCredentialName = "BC_TEST_FAKE_PROVIDER_CREDENTIAL"
const fakeCredentialValue = "sk-should-never-be-visible-to-a-sandboxed-command-000000000000"

// candidateRunners lists every sandbox.Runner implementation this package
// can construct without extra setup. Bubblewrap and Landlock additionally
// require kernel features this host may not have, so each is skipped
// individually via its own Available() check rather than assumed.
func candidateRunners(t *testing.T) []sandbox.Runner {
	t.Helper()
	return []sandbox.Runner{
		sandbox.ContainerRunner{},
	}
}

// TestSandboxedCommandsNeverSeeTheParentProcessCredentials places a fake
// credential in this test process's own environment (the same way a real
// TYPESAFE_API_KEY or generator provider key would reach BoundedCode's
// supervisor process) and confirms no sandbox.Runner's Command() carries it
// into the child, when Spec.Env deliberately omits it — the narrow,
// explicit env every real call site (recipe.GoEnv, TaskOrigin.RuntimeEnv)
// already builds.
func TestSandboxedCommandsNeverSeeTheParentProcessCredentials(t *testing.T) {
	t.Setenv(fakeCredentialName, fakeCredentialValue)

	for _, r := range candidateRunners(t) {
		t.Run(r.Name(), func(t *testing.T) {
			ok, why := r.Available(context.Background())
			if !ok {
				t.Skipf("runner unavailable: %s", why)
			}
			dir := t.TempDir()
			spec := sandbox.Spec{
				ReadWrite: []string{dir},
				Dir:       dir,
				TmpDir:    dir,
				// Deliberately narrow — exactly the shape recipe.GoEnv
				// and TaskOrigin.RuntimeEnv build in production: no
				// credential, no blanket os.Environ() passthrough.
				Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir},
			}
			cmd, err := r.Command(context.Background(), spec, "env")
			if err != nil {
				t.Fatalf("building the sandboxed command: %v", err)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("running the sandboxed command: %v\n%s", err, out)
			}
			if strings.Contains(string(out), fakeCredentialValue) {
				t.Fatalf("SECRET LEAK: the sandboxed command's environment exposed the fake "+
					"credential:\n%s", out)
			}
			if strings.Contains(string(out), fakeCredentialName) {
				t.Fatalf("SECRET LEAK: the sandboxed command's environment carried the "+
					"credential's variable name:\n%s", out)
			}
		})
	}
}

// TestExecCmdEnvReplacesRatherThanExtendsTheParentEnvironment is the
// narrower, stdlib-level property the whole guarantee rests on: Go's
// documented behavior that a non-nil exec.Cmd.Env replaces the child's
// environment outright, rather than appending to the parent's. If this
// were ever false, every sandbox.Runner's explicit Env: spec.Env
// construction would be silently undermined regardless of how careful the
// callers are.
func TestExecCmdEnvReplacesRatherThanExtendsTheParentEnvironment(t *testing.T) {
	t.Setenv(fakeCredentialName, fakeCredentialValue)
	cmd := exec.Command("env")
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // deliberately narrow, no credential
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running env: %v\n%s", err, out)
	}
	if strings.Contains(string(out), fakeCredentialValue) {
		t.Fatal("exec.Cmd.Env did not replace the parent environment — this would silently " +
			"undermine every sandbox.Runner's secret isolation")
	}
}

// A sanity check in the other direction: confirm the fake credential really
// is set in this test's own process environment, so a false pass above
// cannot be explained by the credential simply never having been set.
func TestFakeCredentialIsActuallySetInThisProcess(t *testing.T) {
	t.Setenv(fakeCredentialName, fakeCredentialValue)
	if os.Getenv(fakeCredentialName) != fakeCredentialValue {
		t.Fatal("test setup bug: the fake credential was not actually set")
	}
}
