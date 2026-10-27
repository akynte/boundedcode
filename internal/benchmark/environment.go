package benchmark

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
)

// Environment pins what a run depends on, so a later run can be compared
// with it, or a difference explained: the build, the machine, the model
// and its server settings, the toolchains on PATH, and hashes of the task
// definitions and fixtures.
type Environment struct {
	Build      string            `json:"build"` // version and commit of bcode
	OS         string            `json:"os"`    // GOOS/GOARCH
	CPU        string            `json:"cpu"`   // model name
	CPUs       int               `json:"cpus"`  // logical
	MemMiB     int               `json:"mem_mib,omitempty"`
	GPU        string            `json:"gpu,omitempty"`
	Model      map[string]any    `json:"model,omitempty"`  // provider, profile, file, revision, sha256, server and sampling settings
	Server     string            `json:"server,omitempty"` // model server version
	Toolchains map[string]string `json:"toolchains,omitempty"`
	TasksHash  string            `json:"tasks_sha256,omitempty"`
	Fixtures   map[string]string `json:"fixtures_sha256,omitempty"` // per fixture directory used
}

// toolchains whose version is recorded when installed.
var toolchains = [][]string{
	{"go", "version"}, {"node", "--version"}, {"python3", "--version"}, {"cargo", "--version"},
	{"rustc", "--version"}, {"javac", "-version"}, {"java", "-version"}, {"mvn", "--version"},
	{"dotnet", "--version"}, {"ruby", "--version"}, {"php", "--version"}, {"docker", "--version"},
	{"git", "--version"},
}

// captureEnvironment records the environment of a run. serverBinary may
// be "" (cloud or an external server).
func captureEnvironment(ctx context.Context, model map[string]any, serverBinary string, tasks []TaskSpec, taskDirs []string, fixturesDir string) Environment {
	e := Environment{
		Build: strings.TrimSpace(buildinfo.Version + " " + buildinfo.Commit()),
		OS:    runtime.GOOS + "/" + runtime.GOARCH,
		CPUs:  runtime.NumCPU(),
		Model: model,
	}
	e.CPU, e.MemMiB = cpuAndMemory()
	if out, err := cmdOutput(ctx, "nvidia-smi", "--query-gpu=name,memory.total,driver_version", "--format=csv,noheader"); err == nil {
		e.GPU = strings.TrimSpace(out)
	}
	if serverBinary != "" {
		if v, err := llamacpp.Version(ctx, serverBinary); err == nil {
			e.Server = "llama.cpp " + v
		} else {
			e.Server = "unknown (" + err.Error() + ")"
		}
	}
	e.Toolchains = map[string]string{}
	for _, tc := range toolchains {
		if out, err := cmdOutput(ctx, tc[0], tc[1:]...); err == nil {
			e.Toolchains[tc[0]] = firstLines(out, 1)
		}
	}
	e.TasksHash = hashFiles(taskDirs, func(p string) bool { return strings.HasSuffix(p, ".yaml") })
	used := map[string]bool{}
	for _, t := range tasks {
		if t.Fixture != "" {
			used[t.Fixture] = true
		}
	}
	if len(used) > 0 {
		e.Fixtures = map[string]string{}
		for f := range used {
			e.Fixtures[f] = hashFiles([]string{filepath.Join(fixturesDir, f)}, func(string) bool { return true })
		}
	}
	return e
}

func cmdOutput(ctx context.Context, bin string, args ...string) (string, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, path, args...).CombinedOutput()
	return string(out), err
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, " | "))
}

// cpuAndMemory reads the CPU model and total memory (Linux; empty
// elsewhere).
func cpuAndMemory() (string, int) {
	cpu := ""
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
				cpu = strings.TrimSpace(v)
				break
			}
		}
		f.Close()
	}
	mem := 0
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "MemTotal:"); ok {
				kb := 0
				for _, c := range strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")) {
					if c < '0' || c > '9' {
						break
					}
					kb = kb*10 + int(c-'0')
				}
				mem = kb / 1024
				break
			}
		}
		f.Close()
	}
	return cpu, mem
}

// hashFiles hashes the paths and contents of the regular files under
// roots that keep accepts, in a stable order ("" when there are none).
func hashFiles(roots []string, keep func(string) bool) string {
	var files []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // unreadable entries are left out
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() && keep(p) {
				files = append(files, p)
			}
			return nil
		})
	}
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		rel := p
		for _, root := range roots {
			if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
				rel = filepath.ToSlash(r)
				break
			}
		}
		_, _ = io.WriteString(h, rel+"\x00")
		if f, err := os.Open(p); err == nil {
			_, _ = io.Copy(h, f)
			f.Close()
		}
		_, _ = io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
