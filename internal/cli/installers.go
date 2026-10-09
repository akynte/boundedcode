package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/akynte/boundedcode/internal/install"
)

// exeName is a program's file name on this OS.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// installProgress prints download progress in 10% steps.
func (a *App) installProgress(what string) install.Progress {
	last := -1
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := int(done * 100 / total)
		if pct/10 != last/10 {
			last = pct
			a.printf("  %s %3d%% (%.0f MB)\n", what, pct, float64(total)/1e6)
		}
	}
}

// installTool downloads a pinned tool for this platform into the tools
// directory (which is on PATH for BoundedCode's own commands).
func (a *App) installTool(ctx context.Context, name string, pins map[string]install.Asset) error {
	asset, ok := pins[install.Platform()]
	if !ok {
		return fmt.Errorf("%s has no pinned release for %s; install it yourself and put it on PATH", name, install.Platform())
	}
	dir := a.toolsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := install.Fetch(ctx, asset, dir, a.installProgress(name))
	if err != nil {
		return err
	}
	defer os.Remove(file)
	want := exeName(name)
	if err := install.Unpack(file, asset.URL, dir, func(base string) bool { return base == want }); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		return fmt.Errorf("%s: the release archive has no %s", name, want)
	}
	a.printf("installed %s -> %s\n", name, filepath.Join(dir, want))
	return nil
}

// llamaBuildTools lists what a source build of llama.cpp needs and is
// missing (Linux, where the validated build is compiled locally).
func llamaBuildTools() []string {
	var missing []string
	for _, t := range []string{"git", "cmake", "bash"} {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if _, err := exec.LookPath("c++"); err != nil {
		if _, err := exec.LookPath("g++"); err != nil {
			missing = append(missing, "a C++ compiler")
		}
	}
	return missing
}

// useLlamaSourceBuild reports whether set-up compiles llama.cpp (the
// validated path on Linux) rather than downloading a prebuilt release. With
// an NVIDIA GPU the source build needs the CUDA toolkit (nvcc); without it,
// the prebuilt CUDA build is used instead of a build that would fail.
func useLlamaSourceBuild() bool {
	return runtime.GOOS == "linux" && len(llamaBuildTools()) == 0 && (!hasNVIDIA() || hasNVCC())
}

func hasNVIDIA() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// cudaNVCC is where scripts/build-llama-cpp.sh also looks for nvcc.
var cudaNVCC = "/usr/local/cuda/bin/nvcc"

// hasNVCC reports whether the CUDA compiler is available where
// scripts/build-llama-cpp.sh looks for it (PATH or /usr/local/cuda/bin).
func hasNVCC() bool {
	if _, err := exec.LookPath("nvcc"); err == nil {
		return true
	}
	fi, err := os.Stat(cudaNVCC)
	return err == nil && !fi.IsDir()
}

// llamaPlan describes what the inference step will install.
func llamaPlan() string {
	if useLlamaSourceBuild() {
		gpu := "CPU-only (no NVIDIA GPU found)"
		if hasNVIDIA() {
			gpu = "with CUDA"
		}
		return "Build the pinned llama.cpp " + install.LlamaTag + " from source " + gpu + " (several minutes)"
	}
	v, ok := install.PickLlama(install.Platform(), hasNVIDIA())
	if !ok {
		return "llama.cpp has no prebuilt release for " + install.Platform()
	}
	return fmt.Sprintf("Download the prebuilt llama.cpp %s (build %s, %s) from GitHub, checksum-verified", install.LlamaTag, install.LlamaBuild, v.Name)
}

// installLlamaPrebuilt downloads and unpacks the prebuilt llama.cpp for
// this machine into the runtime prefix and returns its bin directory.
func (a *App) installLlamaPrebuilt(ctx context.Context) (string, error) {
	v, ok := install.PickLlama(install.Platform(), hasNVIDIA())
	if !ok {
		return "", fmt.Errorf("llama.cpp publishes no prebuilt release for %s; build it yourself and set inference.server_binary, or use a cloud provider", install.Platform())
	}
	bin := filepath.Join(a.llamaPrefix(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return "", err
	}
	assets := []install.Asset{v.Archive}
	if v.Runtime != nil {
		assets = append(assets, *v.Runtime)
	}
	for _, as := range assets {
		file, err := install.Fetch(ctx, as, bin, a.installProgress("llama.cpp"))
		if err != nil {
			return "", err
		}
		err = install.Unpack(file, as.URL, bin, nil)
		_ = os.Remove(file)
		if err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(bin, exeName("llama-server"))); err != nil {
		return "", fmt.Errorf("the llama.cpp %s archive has no %s", v.Name, exeName("llama-server"))
	}
	a.printf("installed llama.cpp %s (%s) -> %s\n", install.LlamaBuild, v.Name, bin)
	return bin, nil
}

// containerCodex is the Linux codex build mounted into the contained
// frontier container on hosts whose own codex cannot run in it ("" on
// Linux, where the host's codex is mounted).
func (a *App) containerCodex() string {
	if runtime.GOOS == "linux" {
		return ""
	}
	return filepath.Join(a.toolsDir(), "codex-linux-"+runtime.GOARCH)
}

// needsContainerCodex reports whether the frontier set-up step applies.
func (a *App) needsContainerCodex() bool {
	f := a.Config.Frontier
	return f.Enabled && f.Provider == "codex" && f.Contain && a.containerCodex() != ""
}

// installContainerCodex downloads the pinned Linux codex for the frontier
// container.
func (a *App) installContainerCodex(ctx context.Context) error {
	asset, ok := install.CodexLinux[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("no Linux codex build is pinned for %s", runtime.GOARCH)
	}
	dir := a.toolsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := install.Fetch(ctx, asset, dir, a.installProgress("codex"))
	if err != nil {
		return err
	}
	defer os.Remove(file)
	member := install.CodexLinuxMember(runtime.GOARCH)
	if err := install.Unpack(file, asset.URL, dir, func(b string) bool { return b == member }); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(dir, member), a.containerCodex()); err != nil {
		return err
	}
	a.printf("installed the Linux codex %s for the frontier container -> %s\n", install.CodexVersion, a.containerCodex())
	return nil
}
