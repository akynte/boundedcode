package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestContainerArgsHardening(t *testing.T) {
	c := &Container{Engine: "docker", Image: "img:1", Network: "none", Memory: "4g", UID: 1000, GID: 1000}
	args, err := c.Args(Spec{
		Argv: []string{"python", "-m", "x"}, Workdir: "/workspace", Interactive: true,
		Mounts: []Mount{{Host: "/tmp/wt", Target: "/workspace"}, {Host: "/tmp/git", Target: "/git", ReadOnly: true}},
		Masks:  []string{"/workspace/.env"}, Env: map[string]string{"B": "2", "A": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	for _, want := range []string{"--network none", "--cap-drop ALL", "no-new-privileges", "--user 1000:1000", "-i",
		"source=/tmp/git,target=/git,readonly", "destination=/workspace/.env", "-e A=1 -e B=2 img:1 python -m x"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestRefusesSensitiveMounts(t *testing.T) {
	home, _ := os.UserHomeDir()
	c := &Container{Engine: "docker", Image: "i"}
	for _, h := range []string{home, filepath.Join(home, ".ssh"), filepath.Join(home, ".aws", "credentials"), "/var/run/docker.sock", "/"} {
		if _, err := c.Args(Spec{Argv: []string{"x"}, Mounts: []Mount{{Host: h, Target: "/m"}}}); err == nil {
			t.Errorf("mount of %s should be refused", h)
		}
	}
}

func TestSensitiveEnv(t *testing.T) {
	for _, k := range []string{"AWS_SECRET_ACCESS_KEY", "OPENAI_API_KEY", "SSH_AUTH_SOCK", "MY_SERVICE_PASSWORD", "GH_TOKEN", "KUBECONFIG"} {
		if !isSensitiveEnv(k) {
			t.Errorf("%s should be sensitive", k)
		}
	}
	for _, k := range []string{"PATH", "HOME", "LANG", "GOPATH"} {
		if isSensitiveEnv(k) {
			t.Errorf("%s should not be sensitive", k)
		}
	}
	t.Setenv("FOO_API_KEY", "x")
	if slices.ContainsFunc(ScrubbedEnv(), func(kv string) bool { return strings.HasPrefix(kv, "FOO_API_KEY=") }) {
		t.Fatal("scrubbed env leaked secret")
	}
}

func TestNoneRequiresIdentityMounts(t *testing.T) {
	if _, err := (None{}).Command(t.Context(), Spec{Argv: []string{"true"}, Mounts: []Mount{{Host: "/a", Target: "/b"}}}); err == nil {
		t.Fatal("expected error")
	}
}
