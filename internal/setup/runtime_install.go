package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PrismRuntimeRepo and PrismRuntimeRevision are the pinned source used when
// the TUI is asked to build the CUDA runtime. The binary is never downloaded
// from an unversioned "latest" URL.
const (
	PrismRuntimeRepo     = "https://github.com/PrismML-Eng/llama.cpp.git"
	PrismRuntimeRevision = "1a07bfa5f4144274c8f1c9963821dd9d9a51854b"
)

// InstallPrismRuntime builds the pinned Prism llama-server runtime under the
// installation data directory. It is intentionally explicit: compiling CUDA
// code is expensive, so the interactive TUI asks first and non-interactive
// callers must pass --install-runtime (or --yes).
func InstallPrismRuntime(ctx context.Context, dataDir string, logf func(string, ...any)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dataDir = DataDir(dataDir)
	runtimeRoot := filepath.Join(dataDir, "runtime")
	binary, err := prismRuntimeBinary(runtimeRoot)
	if err != nil {
		return "", err
	}
	if executable(binary) {
		return binary, nil
	}
	if err := CheckPrismRuntimeBuild(); err != nil {
		return "", err
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if err := os.MkdirAll(runtimeRoot, 0o750); err != nil {
		return "", fmt.Errorf("setup: create Prism runtime directory: %w", err)
	}
	release, err := acquireRuntimeInstallLock(runtimeRoot)
	if err != nil {
		return "", err
	}
	defer release()

	source := filepath.Join(runtimeRoot, "src")
	if err := preparePrismSource(ctx, source, logf); err != nil {
		return "", err
	}
	arch, err := cudaArchitecture(ctx)
	if err != nil {
		return "", err
	}
	build := filepath.Join(runtimeRoot, "build")
	logf("configuring CUDA architecture %s", arch)
	if err := runRuntimeCommand(ctx, "setup: configure Prism runtime",
		"cmake", "-S", source, "-B", build, "-G", "Ninja", "-DCMAKE_BUILD_TYPE=Release", "-DGGML_CUDA=ON", "-DCMAKE_CUDA_ARCHITECTURES="+arch); err != nil {
		return "", err
	}
	logf("building llama-server")
	buildJobs := runtime.NumCPU()
	if buildJobs > 8 {
		// CUDA compilation is memory-heavy; a many-core host should not turn
		// an explicit setup into an OOM or make every unrelated process stall.
		buildJobs = 8
	}
	if err := runRuntimeCommand(ctx, "setup: build Prism runtime",
		"cmake", "--build", build, "--target", "llama-server", "-j", strconv.Itoa(buildJobs)); err != nil {
		return "", err
	}
	binary, err = prismRuntimeBinary(runtimeRoot)
	if err != nil {
		return "", err
	}
	if !executable(binary) {
		return "", fmt.Errorf("setup: Prism build completed without an executable at %s", binary)
	}
	return binary, nil
}

// CheckPrismRuntimeBuild verifies prerequisites without changing the host. It
// is exported so the setup command can avoid downloading a large model before
// discovering that an explicitly requested runtime build cannot start.
func CheckPrismRuntimeBuild() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("setup: automatic Prism runtime installation is supported on Linux x86-64, not %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for _, tool := range []string{"git", "cmake", "ninja", "nvcc", "nvidia-smi"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("setup: automatic Prism runtime installation needs %s on PATH: %w", tool, err)
		}
	}
	return nil
}

// prismRuntimeBinary returns the output path produced by the pinned Prism
// CMake build. A few CMake versions place the executable directly in build/;
// accepting both layouts keeps the installer compatible without weakening the
// pinned-source guarantee.
func prismRuntimeBinary(runtimeRoot string) (string, error) {
	candidates := []string{
		filepath.Join(runtimeRoot, "build", "bin", "llama-server"),
		filepath.Join(runtimeRoot, "build", "llama-server"),
	}
	for _, candidate := range candidates {
		if executable(candidate) {
			return candidate, nil
		}
	}
	// Return the canonical location for the eventual "missing executable"
	// error when neither build layout exists yet.
	return candidates[0], nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func acquireRuntimeInstallLock(runtimeRoot string) (func(), error) {
	path := filepath.Join(runtimeRoot, ".install.lock")
	for attempt := 0; attempt < 2; attempt++ {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			_ = os.WriteFile(filepath.Join(path, "owner"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
			return func() { _ = os.RemoveAll(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("setup: acquire Prism runtime build lock: %w", err)
		}
		// A killed builder must not make all future setup impossible. The
		// owner is informational; age is the conservative stale-lock signal.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.RemoveAll(path)
			continue
		}
		return nil, fmt.Errorf("setup: another Prism runtime installation is already running (lock %s)", path)
	}
	return nil, fmt.Errorf("setup: could not acquire Prism runtime build lock %s", path)
}

func preparePrismSource(ctx context.Context, source string, logf func(string, ...any)) error {
	info, err := os.Stat(source)
	if err == nil && !info.IsDir() {
		return fmt.Errorf("setup: Prism source path %s is not a directory", source)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("setup: inspect Prism source path: %w", err)
	}
	if os.IsNotExist(err) {
		logf("initializing pinned Prism runtime source")
		if err := runRuntimeCommand(ctx, "setup: initialize Prism runtime source", "git", "init", source); err != nil {
			return err
		}
	} else if _, statErr := os.Stat(filepath.Join(source, ".git")); statErr != nil {
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("setup: inspect Prism source checkout: %w", statErr)
		}
		logf("initializing pinned Prism runtime source")
		if err := runRuntimeCommand(ctx, "setup: initialize Prism runtime source", "git", "init", source); err != nil {
			return err
		}
	}

	// A setup-owned checkout may be resumed after an interrupted build. Check
	// cleanliness before either reusing it or changing its revision: fetching
	// and checking out a different commit must not silently carry local source
	// edits into the CUDA build.
	head, headErr := runtimeGitOutput(ctx, source, "rev-parse", "HEAD")
	if headErr == nil {
		status, err := runtimeGitOutput(ctx, source, "status", "--porcelain")
		if err != nil {
			return fmt.Errorf("setup: inspect Prism source checkout: %w", err)
		}
		if strings.TrimSpace(status) != "" {
			return fmt.Errorf("setup: Prism source checkout has local changes; remove %s or restore it before rebuilding", source)
		}
	}
	if headErr != nil || strings.TrimSpace(head) != PrismRuntimeRevision {
		logf("fetching pinned Prism runtime revision")
		if err := runRuntimeCommand(ctx, "setup: fetch Prism runtime", "git", "-C", source, "fetch", "--depth", "1", PrismRuntimeRepo, PrismRuntimeRevision); err != nil {
			return err
		}
		if err := runRuntimeCommand(ctx, "setup: check out Prism runtime", "git", "-C", source, "checkout", "--detach", PrismRuntimeRevision); err != nil {
			return err
		}
	}

	checkedOut, err := runtimeGitOutput(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("setup: verify Prism runtime revision: %w", err)
	}
	if strings.TrimSpace(checkedOut) != PrismRuntimeRevision {
		return fmt.Errorf("setup: Prism runtime checkout is %s, want pinned revision %s", strings.TrimSpace(checkedOut), PrismRuntimeRevision)
	}
	return nil
}

func runtimeGitOutput(ctx context.Context, source string, args ...string) (string, error) {
	full := append([]string{"-C", source}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) //nolint:gosec // fixed git inspection subcommand and setup-owned source
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	return string(output), nil
}

func runRuntimeCommand(ctx context.Context, label, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...) //nolint:gosec // commands and flags are fixed by the installer
	if command == "git" {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("%s: %w", label, err)
		}
		return fmt.Errorf("%s: %w: %s", label, err, detail)
	}
	return nil
}

func cudaArchitecture(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader,nounits") //nolint:gosec // fixed diagnostic command
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("setup: detect NVIDIA compute capability: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("setup: NVIDIA reported no compute capability")
	}
	value := strings.ReplaceAll(fields[0], ".", "")
	if value == "" || value == "N/A" || strings.Trim(value, "[]") == "N/A" {
		return "", fmt.Errorf("setup: NVIDIA reported no usable compute capability (got %q)", fields[0])
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("setup: NVIDIA reported an invalid compute capability %q", fields[0])
		}
	}
	return value, nil
}
