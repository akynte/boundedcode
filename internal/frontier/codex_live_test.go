package frontier

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestCodexContainedLive sends one tiny packet through the contained Codex
// provider using the user's ChatGPT sign-in. It consumes subscription quota,
// so it only runs with BC_TEST_CODEX_LIVE=1 and BC_TEST_DOCKER_IMAGE set.
func TestCodexContainedLive(t *testing.T) {
	img := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if os.Getenv("BC_TEST_CODEX_LIVE") != "1" || img == "" {
		t.Skip("set BC_TEST_CODEX_LIVE=1 and BC_TEST_DOCKER_IMAGE")
	}
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home+"/.cache", "bc-codex-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c := &Codex{Binary: "codex", Timeout: 5 * time.Minute,
		Container: &CodexContainer{Engine: "docker", Image: img, UID: os.Getuid(), GID: os.Getgid()}}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	ans, err := c.Ask(ctx, "Do not run any commands. Reply with exactly the two letters OK and nothing else.", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(ans), "OK") {
		t.Fatalf("unexpected answer: %q", ans)
	}
	t.Logf("contained codex answered %q", ans)
}
