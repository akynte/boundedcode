//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunProcessKillsDescendantsAfterParentExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant-survived")
	script := fmt.Sprintf("(sleep 1; printf survived > %q) &", marker)
	cmd := exec.Command("/bin/sh", "-c", script) //nolint:gosec // test shell
	if err := runProcess(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("descendant survived normal parent exit: stat error=%v", statErr)
	}
}

func TestRunProcessKillsDescendantsOnDeadline(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant-survived")
	script := fmt.Sprintf("(sleep 1; printf survived > %q) & wait", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script) //nolint:gosec // test shell
	err := runProcess(ctx, cmd)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runProcess error = %v, want deadline exceeded", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("descendant survived process-group cleanup: stat error=%v", statErr)
	}
}
