package execbox

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/policy"
)

// toolDirs under the home directory hold toolchains: readable, not
// writable.
var toolDirs = []string{".rustup", ".nvm", ".volta", ".pyenv", ".sdkman", ".local/share/uv/python", ".local/share/pnpm", ".bun/bin",
	".deno/bin", ".dotnet", ".ghcup", ".asdf", ".local/share/mise", ".juliaup", ".local/lib", ".local/go", "go/bin", "sdk", ".cargo/bin",
	".jdks", ".gradle/jdks", ".m2/wrapper", ".local/share/coursier", ".elan", ".opam", ".swiftly", ".pub-cache/bin", ".nix-profile"}

// cacheDirs under the home directory are package and build caches:
// writable, so builds stay fast and dependencies already downloaded are
// used.
var cacheDirs = []string{".cache/go-build", "go/pkg/mod", ".npm", ".cache/pip", ".cache/uv", ".cache/pypoetry", ".cargo/registry",
	".cargo/git", ".m2/repository", ".gradle/caches", ".gradle/wrapper", ".cache/yarn", ".yarn/berry/cache", ".local/share/pnpm/store",
	".bun/install/cache", ".nuget/packages", ".cache/ms-playwright", ".cache/deno", ".gem", ".cache/bazel", ".cache/node-gyp",
	".pub-cache/hosted", ".cache/composer", ".composer/cache", ".hex", ".mix", ".cache/zig", ".cache/coursier", ".ivy2/cache", ".sbt"}

// credentialFiles inside tool and cache directories are masked.
var credentialFiles = []string{".cargo/credentials", ".cargo/credentials.toml", ".gradle/gradle.properties", ".m2/settings-security.xml",
	".m2/settings.xml", ".gem/credentials", ".pypirc", ".nuget/NuGet/NuGet.Config", ".sbt/credentials", ".ivy2/.credentials",
	".composer/auth.json", ".hex/hex.config", ".pub-cache/credentials.json", ".pub-cache/pub-tokens.json"}

// hiddenRoots are hidden as a whole: other users, removable and network
// mounts, and /run with the sockets of the user's session, the Docker
// daemon, the SSH and GPG agents and D-Bus.
var hiddenRoots = []string{"/home", "/root", "/run", "/mnt", "/media", "/srv", "/Users"}

type hostPaths struct {
	home         string
	read, write  []string // directories and files to expose
	masks        []string // files and directories to hide again
	pathBinds    []string // PATH entries under hidden roots
	gitRO, gitRW []string
}

var (
	goEnvOnce sync.Once
	goEnv     []string
)

// goDirs are GOROOT, GOMODCACHE and GOCACHE when Go is installed.
func goDirs() []string {
	goEnvOnce.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			return
		}
		out, err := exec.Command("go", "env", "GOROOT", "GOMODCACHE", "GOCACHE").Output() //nolint:noctx // quick
		if err != nil {
			return
		}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l = strings.TrimSpace(l); l != "" && filepath.IsAbs(l) {
				goEnv = append(goEnv, l)
			}
		}
	})
	return goEnv
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func hiddenByRoot(p, home string) bool {
	for _, h := range append(append([]string{}, hiddenRoots...), home) {
		if h != "" && under(p, h) {
			return true
		}
	}
	return false
}

// paths works out what a command may see.
func (b *Box) paths() hostPaths {
	home, _ := os.UserHomeDir()
	hp := hostPaths{home: home}
	add := func(list *[]string, p string) {
		if p != "" && exists(p) {
			*list = append(*list, p)
		}
	}
	for _, d := range toolDirs {
		add(&hp.read, filepath.Join(home, d))
	}
	gd := goDirs()
	for i, d := range gd {
		if i == 0 {
			add(&hp.read, d) // GOROOT
		} else {
			add(&hp.write, d)
		}
	}
	for _, d := range cacheDirs {
		add(&hp.write, filepath.Join(home, d))
	}
	// PATH directories hidden by the view (a toolchain's bin in the home
	// directory, /run/current-system on NixOS) stay visible. A bin's
	// parent is exposed too when it is a versioned install
	// (~/.nvm/versions/node/v24) or outside the home directory, so links
	// into ../lib work; a tool's own directory directly in the home
	// (~/.opencode, ~/.local), where credentials sit, is not.
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(d) || !isDir(d) {
			continue
		}
		for _, p := range []string{filepath.Clean(d), realPath(d)} {
			if !hiddenByRoot(p, home) || under(p, b.root) || p == home || p == "/" {
				continue
			}
			if filepath.Base(p) == "bin" && toolchainRoot(filepath.Dir(p), home) {
				p = filepath.Dir(p)
			}
			hp.pathBinds = append(hp.pathBinds, p)
		}
	}
	for _, p := range b.cfg.ReadPaths {
		add(&hp.read, p)
	}
	for _, p := range b.cfg.WritePaths {
		add(&hp.write, p)
	}
	for _, f := range credentialFiles {
		add(&hp.masks, filepath.Join(home, f))
	}
	for _, rel := range b.secretPaths() {
		add(&hp.masks, filepath.Join(b.root, rel))
	}
	hp.gitRW, hp.gitRO = gitPaths(b.root)
	hp.read, hp.write, hp.pathBinds = uniq(hp.read), uniq(hp.write), uniq(hp.pathBinds)
	return hp
}

// toolchainRoot reports whether dir, the parent of a bin on PATH, may be
// exposed whole.
func toolchainRoot(dir, home string) bool {
	if home == "" || !under(dir, home) {
		return dir != "/" && !hiddenRootItself(dir)
	}
	rel, err := filepath.Rel(home, dir)
	return err == nil && strings.Count(filepath.ToSlash(rel), "/") >= 2
}

func hiddenRootItself(dir string) bool {
	for _, h := range hiddenRoots {
		if dir == h {
			return true
		}
	}
	return false
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func uniq(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// secretPaths are the project's secret files, found once per box.
func (b *Box) secretPaths() []string {
	b.maskOnce.Do(func() {
		b.masks, _ = policy.FindSecretPaths(b.root, 500)
	})
	return b.masks
}

// projectTools are the package managers of the project's ecosystems.
func (b *Box) projectTools() []string {
	b.ecoOnce.Do(func() {
		seen := map[string]bool{}
		for _, p := range ecosystem.Detect(b.root, ecosystem.Options{MaxDepth: 2, MaxEntries: 4000}).Projects {
			t := filepath.Base(p.Tool)
			t = strings.TrimSuffix(strings.TrimPrefix(t, "./"), ".bat")
			if t != "" && !seen[t] {
				seen[t] = true
				b.ecoTools = append(b.ecoTools, t)
			}
			if p.Manager != "" && !seen[p.Manager] {
				seen[p.Manager] = true
				b.ecoTools = append(b.ecoTools, p.Manager)
			}
		}
	})
	return b.ecoTools
}

// gitPaths returns the repository's git directories (writable: the agent
// commits) and what in them runs programs (read-only: hooks, the
// configuration, attributes that name filters).
func gitPaths(root string) (rw, ro []string) {
	gitdir := filepath.Join(root, ".git")
	st, err := os.Lstat(gitdir)
	if err != nil {
		return nil, nil
	}
	if !st.IsDir() {
		b, err := os.ReadFile(gitdir)
		if err != nil {
			return nil, nil
		}
		line := strings.TrimSpace(strings.TrimPrefix(string(bytes.SplitN(b, []byte("\n"), 2)[0]), "gitdir:"))
		if line == "" {
			return nil, nil
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(root, line)
		}
		gitdir = filepath.Clean(line)
		if !under(gitdir, root) {
			rw = append(rw, gitdir)
		}
	}
	common := gitdir
	if b, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		c := strings.TrimSpace(string(b))
		if !filepath.IsAbs(c) {
			c = filepath.Join(gitdir, c)
		}
		common = filepath.Clean(c)
		if !under(common, root) {
			rw = append(rw, common)
		}
	}
	for _, d := range uniq([]string{gitdir, common}) {
		// A missing hooks directory would let a command create hooks that
		// git outside the sandbox later runs: create it, so it can be
		// mounted read-only.
		if isDir(d) && !exists(filepath.Join(d, "hooks")) {
			_ = os.Mkdir(filepath.Join(d, "hooks"), 0o755)
		}
		for _, f := range []string{"hooks", "config", "config.worktree", "info/attributes"} {
			if p := filepath.Join(d, f); exists(p) {
				ro = append(ro, p)
			}
		}
	}
	return rw, ro
}
