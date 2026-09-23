package evidence

// Secret isolation for Evidence Suite v1's three credential roles — design
// instruction GAP 2 §10/§40. internal/sandbox/secret_isolation_test.go
// already proves the general mechanism (no sandbox.Runner passes host env
// through). This test is the Evidence-Suite-specific instance of it: with
// all three real role names set to fake values, the exact sandbox.Spec
// ExecuteRun constructs (see execute.go's `r.SandboxSpec = sandbox.Spec{...}`)
// never carries any of them into a sandboxed `env` command.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/container"
)

func TestEvidenceSandboxSpecNeverCarriesAnyOfTheThreeCredentialRoles(t *testing.T) {
	r := &container.Runner{}
	ok, why := r.Available(context.Background())
	if !ok {
		t.Skipf("no usable container runtime: %s", why)
	}

	fakes := map[string]string{
		"ANTHROPIC_API_KEY":  "generator-secret-must-not-leak",
		"TYPESAFE_API_KEY":   "jev-secret-must-not-leak",
		"ATLAS_EVAL_API_KEY": "atlas-secret-must-not-leak",
		"OPENAI_API_KEY":     "atlas-openai-alias-must-not-leak",
	}
	for k, v := range fakes {
		t.Setenv(k, v)
	}

	// t.TempDir() is not in Docker Desktop's shared-path allowlist on this
	// host; every other Docker-gated test in this package uses a
	// subdirectory of the already-proven-shared evidence run root instead.
	dir := "../../.boundedcode-runs/evidence-v1/test-secret-isolation"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// The exact shape execute.go's ExecuteRun builds: Image/ImageDir/
	// Network only — no Env field, so nothing from the host (including
	// os.Environ(), which Go never passes through implicitly) reaches the
	// task's sandboxed command.
	spec := sandbox.Spec{
		Image:    "busybox@sha256:dc2d74b28e4cf8984fa52af1f39bc7c3d9c73760b41a74d629f5d11b1ab28616",
		ImageDir: "/app", Network: sandbox.NetworkNone, Dir: dir,
	}
	cmd, err := r.Command(context.Background(), spec, "env")
	if err != nil {
		t.Fatalf("building the sandboxed command: %v", err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("could not run the sandboxed command (likely no network to pull busybox): %v\n%s", err, out)
	}
	for k, v := range fakes {
		if strings.Contains(string(out), v) {
			t.Fatalf("SECRET LEAK: %s's value reached the sandboxed task environment:\n%s", k, out)
		}
		if strings.Contains(string(out), k+"=") {
			t.Fatalf("SECRET LEAK: %s's variable name reached the sandboxed task environment:\n%s", k, out)
		}
	}
}
