package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceCaps(t *testing.T) {
	for _, c := range []struct {
		cpus, mem       string
		maxCPU          int
		maxMem          int64
		wantCPU, wantMe string
	}{
		{"8", "8g", 4, 6 << 30, "4", "5529m"}, // Docker Desktop VM smaller than the defaults
		{"8", "8g", 16, 64 << 30, "8", "8g"},  // big host: unchanged
		{"8", "8g", 0, 0, "8", "8g"},          // unknown: unchanged
		{"2.5", "512m", 2, 1 << 30, "2", "512m"},
	} {
		if got := capCPUs(c.cpus, c.maxCPU); got != c.wantCPU {
			t.Errorf("capCPUs(%s, %d) = %s, want %s", c.cpus, c.maxCPU, got, c.wantCPU)
		}
		if got := capMemory(c.mem, c.maxMem); got != c.wantMe {
			t.Errorf("capMemory(%s, %d) = %s, want %s", c.mem, c.maxMem, got, c.wantMe)
		}
	}
}

// TestProbeResources: the engine's own CPU and memory cap the container.
func TestProbeResources(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in info) echo '4 6442450944' ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := &Container{Engine: "docker", Image: "img", Memory: "8g", CPUs: "8", UID: 1, GID: 1}
	cmd, err := c.Command(context.Background(), Spec{Argv: []string{"true"}, Workdir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--cpus 4") || !strings.Contains(args, "--memory 5529m") {
		t.Fatalf("args = %s", args)
	}
}
