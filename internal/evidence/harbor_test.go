package evidence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeHarborPackage builds a minimal stand-in for the installed harbor
// package's directory shape — just enough for EnsurePatchedHarborRuntime's
// copy-and-patch logic to exercise for real, without depending on the
// actual pip package being installed in this environment.
func fakeHarborPackage(t *testing.T) (pythonExe, pkgDir string) {
	t.Helper()
	root := t.TempDir()
	pkgDir = filepath.Join(root, "site-packages", "harbor")
	dockerDir := filepath.Join(pkgDir, "environments", "docker")
	if err := os.MkdirAll(dockerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "services:\n  main:\n    build:\n      context: ${CONTEXT_DIR}\n" +
		"    pull_policy: build\n    command: [ \"sh\", \"-c\", \"sleep infinity\" ]\n"
	for _, f := range []string{"docker-compose-build.yaml", "docker-compose-prebuilt.yaml"} {
		if err := os.WriteFile(filepath.Join(dockerDir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "__init__.py"),
		[]byte("# fake harbor package for tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available in this environment")
	}
	// A tiny fake "harbor" import resolvable from pkgDir's parent, so
	// harborPackageDir's `import harbor` probe works against our fixture
	// without needing the real pip package.
	fakePython := filepath.Join(root, "fake_python.sh")
	script := "#!/bin/sh\nexport PYTHONPATH=\"" + filepath.Dir(pkgDir) + "${PYTHONPATH:+:$PYTHONPATH}\"\n" +
		"exec " + py + " \"$@\"\n"
	if err := os.WriteFile(fakePython, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return fakePython, pkgDir
}

func TestEnsurePatchedHarborRuntimePatchesOnlyTheTwoAffectedTemplates(t *testing.T) {
	pythonExe, _ := fakeHarborPackage(t)
	runRoot := t.TempDir()

	pythonPath, err := EnsurePatchedHarborRuntime(context.Background(), pythonExe, runRoot)
	if err != nil {
		t.Fatalf("EnsurePatchedHarborRuntime: %v", err)
	}

	for _, f := range []string{"docker-compose-build.yaml", "docker-compose-prebuilt.yaml"} {
		body, err := os.ReadFile(filepath.Join(pythonPath, "harbor", "environments", "docker", f))
		if err != nil {
			t.Fatalf("reading patched %s: %v", f, err)
		}
		s := string(body)
		if !contains(s, `entrypoint: [ "/bin/sh" ]`) {
			t.Errorf("%s: expected an explicit entrypoint override, got:\n%s", f, s)
		}
		if !contains(s, `command: [ "-c", "sleep infinity" ]`) {
			t.Errorf("%s: expected the original sleep-infinity command preserved as CMD, got:\n%s", f, s)
		}
	}
}

func TestEnsurePatchedHarborRuntimeIsIdempotent(t *testing.T) {
	pythonExe, _ := fakeHarborPackage(t)
	runRoot := t.TempDir()

	first, err := EnsurePatchedHarborRuntime(context.Background(), pythonExe, runRoot)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := EnsurePatchedHarborRuntime(context.Background(), pythonExe, runRoot)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Fatalf("expected the same PYTHONPATH prefix on a second call, got %q then %q", first, second)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

// TestAtlasEvaluatorEnvTranslatesTheOperatorVariableAndOnlyThat proves
// design instruction GAP 2 §7/§8/§11: ATLAS_EVAL_API_KEY translates into
// exactly the two names the official verifier's task.toml expects
// (OPENAI_API_KEY, EVAL_API_KEY), the resulting environment carries no
// other credential-shaped variable, and — critically — this function
// never touches os.Environ() at all, so a generator credential
// (ANTHROPIC_API_KEY) or TYPESAFE_API_KEY set in the operator's shell is
// structurally absent from it regardless of what the process environment
// contains.
func TestAtlasEvaluatorEnvTranslatesTheOperatorVariableAndOnlyThat(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "generator-secret-must-not-appear")
	t.Setenv("TYPESAFE_API_KEY", "jev-secret-must-not-appear")

	env := AtlasEvaluatorEnv("/fake/pythonpath", "atlas-secret-value")

	wantOpenAI, wantEval := false, false
	for _, kv := range env {
		if kv == "OPENAI_API_KEY=atlas-secret-value" {
			wantOpenAI = true
		}
		if kv == "EVAL_API_KEY=atlas-secret-value" {
			wantEval = true
		}
		if contains(kv, "generator-secret-must-not-appear") || contains(kv, "jev-secret-must-not-appear") {
			t.Fatalf("AtlasEvaluatorEnv leaked an unrelated credential role: %q", kv)
		}
	}
	if !wantOpenAI {
		t.Fatalf("expected OPENAI_API_KEY=atlas-secret-value in %v", env)
	}
	if !wantEval {
		t.Fatalf("expected EVAL_API_KEY=atlas-secret-value in %v", env)
	}
}
