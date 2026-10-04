package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestContainerCancelRemovesContainer: cancelling the context must stop the
// container itself, not only the engine CLI. Set BC_TEST_DOCKER_IMAGE to run.
func TestContainerCancelRemovesContainer(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	c := &Container{Engine: "docker", Image: image, Network: "none", UID: os.Getuid(), GID: os.Getgid()}
	name := "bc-test-cancel-" + randomSuffix()
	t.Cleanup(func() { _ = c.Remove(context.Background(), name) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd, err := c.Command(ctx, Spec{Argv: []string{"sleep", "300"}, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = cmd.Run()
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("run returned after %s", d)
	}
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output()
	if err != nil {
		t.Fatal(err)
	}
	if id := strings.TrimSpace(string(out)); id != "" {
		t.Fatalf("container %s still exists after cancel (%s)", name, id)
	}
}

// TestRemoveStaleContainer: a leftover container with the same name is
// cleared so a resumed session can reuse it; removing a missing one is fine.
func TestRemoveStaleContainer(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	c := &Container{Engine: "docker", Image: image}
	name := "bc-test-stale-" + randomSuffix()
	if out, err := exec.Command("docker", "run", "-d", "--name", name, "--network", "none", image, "sleep", "300").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := c.Remove(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(t.Context(), name); err != nil {
		t.Fatalf("removing a missing container: %v", err)
	}
}
