package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLlamaSourceBuildNeedsCUDAToolkit: with an NVIDIA driver but no CUDA
// toolkit, set-up downloads the prebuilt CUDA build instead of starting a
// source build that stops at "nvcc not found".
func TestLlamaSourceBuildNeedsCUDAToolkit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the source build is the Linux path")
	}
	bin := t.TempDir()
	tool := func(name string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"git", "cmake", "bash", "c++", "nvidia-smi"} {
		tool(n)
	}
	t.Setenv("PATH", bin)
	old := cudaNVCC
	cudaNVCC = filepath.Join(t.TempDir(), "nvcc")
	t.Cleanup(func() { cudaNVCC = old })
	if useLlamaSourceBuild() {
		t.Fatal("source build chosen without nvcc")
	}
	if p := llamaPlan(); !strings.Contains(p, "prebuilt") || !strings.Contains(p, "cuda") {
		t.Fatalf("plan = %q, want the prebuilt CUDA build", p)
	}
	tool("nvcc")
	if !useLlamaSourceBuild() {
		t.Fatal("source build not chosen with nvcc")
	}
}
