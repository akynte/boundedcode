package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PackageCaches are the host's package caches for languages other than Go
// (whose module cache is handled separately). The sandbox has no network,
// so builds resolve dependencies from what the host has already downloaded.
// The caches are mounted read-only: never the tool's home directory itself,
// which can hold credentials (~/.cargo/credentials.toml,
// ~/.m2/settings.xml, ~/.gradle/gradle.properties).
type PackageCaches struct {
	Cargo  string // CARGO_HOME: registry/ and git/ are used
	Maven  string // the local Maven repository (~/.m2/repository)
	Gradle string // GRADLE_USER_HOME: caches/ and wrapper/dists/ are used
}

// HostPackageCaches finds the invoking user's package caches. Missing ones
// are left empty.
func HostPackageCaches() PackageCaches {
	home, _ := os.UserHomeDir()
	pick := func(env string, def ...string) string {
		p := os.Getenv(env)
		if p == "" && home != "" {
			p = filepath.Join(append([]string{home}, def...)...)
		}
		if st, err := os.Stat(p); p == "" || err != nil || !st.IsDir() {
			return ""
		}
		return p
	}
	return PackageCaches{
		Cargo:  pick("CARGO_HOME", ".cargo"),
		Maven:  pick("BC_MAVEN_REPOSITORY", ".m2", "repository"),
		Gradle: pick("GRADLE_USER_HOME", ".gradle"),
	}
}

// Apply returns the mounts and sets the environment that make the caches
// usable offline in a container. work is a writable host directory private
// to one task and one user of it (agent or verification): tool homes,
// build outputs and the writable halves of the caches go there, so nothing
// a task runs can write into the host's caches or into another task's
// builds. Missing caches are skipped.
//
//   - Cargo: CARGO_HOME is work/cargo-home with the host's registry/ and git/
//     mounted read-only inside it; CARGO_TARGET_DIR is work/cargo-target.
//   - Maven: the host repository is a read-only tail
//     (maven.repo.local.tail, Maven 3.9+) behind a writable work/m2, offline.
//   - Gradle: GRADLE_USER_HOME is work/gradle-home and the host's caches/
//     a read-only dependency cache (GRADLE_RO_DEP_CACHE). Wrapper
//     distributions are copied in by PrepareGradleWrapper.
//   - Python, Ruby and PHP package managers are told not to use the network.
func (p PackageCaches) Apply(work string, env map[string]string) ([]Mount, error) {
	if work == "" {
		return nil, nil
	}
	cargoHome := filepath.Join(work, "cargo-home")
	m2 := filepath.Join(work, "m2")
	gradleHome := filepath.Join(work, "gradle-home")
	target := filepath.Join(work, "cargo-target")
	for _, d := range []string{cargoHome, m2, gradleHome, target} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	mounts := []Mount{{Host: work, Target: work}}
	for _, sub := range []string{"registry", "git"} {
		host := filepath.Join(p.Cargo, sub)
		if p.Cargo == "" || !isDir(host) {
			continue
		}
		// The mount point must exist and belong to the user, not the engine.
		if err := os.MkdirAll(filepath.Join(cargoHome, sub), 0o700); err != nil {
			return nil, err
		}
		mounts = append(mounts, Mount{Host: host, Target: filepath.Join(cargoHome, sub), ReadOnly: true})
	}
	env["CARGO_HOME"], env["CARGO_TARGET_DIR"], env["CARGO_NET_OFFLINE"] = cargoHome, target, "true"
	env["RUSTUP_AUTO_INSTALL"] = "0"

	mavenArgs := "-B -o -Dmaven.repo.local=" + ContainerPath(m2)
	if p.Maven != "" && isDir(p.Maven) {
		mounts = append(mounts, Mount{Host: p.Maven, Target: p.Maven, ReadOnly: true})
		mavenArgs += " -Dmaven.repo.local.tail=" + ContainerPath(p.Maven)
	}
	env["MAVEN_ARGS"] = mavenArgs

	env["GRADLE_USER_HOME"] = gradleHome
	if caches := filepath.Join(p.Gradle, "caches"); p.Gradle != "" && isDir(filepath.Join(caches, "modules-2")) {
		mounts = append(mounts, Mount{Host: caches, Target: caches, ReadOnly: true})
		env["GRADLE_RO_DEP_CACHE"] = caches
	}
	env["GRADLE_OPTS"] = "-Dorg.gradle.daemon=false"

	env["PIP_NO_INDEX"], env["PIP_DISABLE_PIP_VERSION_CHECK"], env["UV_OFFLINE"] = "1", "1", "1"
	env["COMPOSER_DISABLE_NETWORK"] = "1"
	return mounts, nil
}

// PrepareGradleWrapper copies the Gradle distribution that a project's
// wrapper (gradle/wrapper/gradle-wrapper.properties under dir) uses from the
// host's GRADLE_USER_HOME into work/gradle-home (see Apply), so ./gradlew
// runs offline. It cannot be mounted read-only: the wrapper takes a lock
// file next to the distribution even when it is installed. Nothing happens
// when the project has no wrapper, the host does not have the
// distribution, or it was already copied.
func (p PackageCaches) PrepareGradleWrapper(dir, work string) error {
	if p.Gradle == "" || work == "" {
		return nil
	}
	name := gradleDistName(filepath.Join(dir, "gradle", "wrapper", "gradle-wrapper.properties"))
	if name == "" {
		return nil
	}
	src := filepath.Join(p.Gradle, "wrapper", "dists", name)
	dst := filepath.Join(work, "gradle-home", "wrapper", "dists", name)
	if !isDir(src) || isDir(dst) {
		return nil
	}
	tmp := dst + ".partial"
	_ = os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("copy gradle distribution %s: %w", name, err)
	}
	return os.Rename(tmp, dst)
}

// gradleDistName is the distribution directory name (gradle-8.5-bin) of a
// wrapper properties file, or "".
func gradleDistName(props string) string {
	b, err := os.ReadFile(props)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(k) != "distributionUrl" {
			continue
		}
		v = strings.ReplaceAll(strings.TrimSpace(v), `\:`, ":")
		base := v[strings.LastIndexAny(v, "/")+1:]
		base = strings.TrimSuffix(base, ".zip")
		if base == "" || strings.ContainsAny(base, `/\`) || base == "." || base == ".." {
			return ""
		}
		return base
	}
	return ""
}

// copyTree copies regular files and directories (symlinks are skipped).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, to, fi.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o200)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
