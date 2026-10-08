package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPackageCachesApply: the host caches are mounted read-only (never the
// tool homes holding credentials), and everything a task writes goes to its
// own work directory.
func TestPackageCachesApply(t *testing.T) {
	host, work := t.TempDir(), t.TempDir()
	cargo, m2, gradle := filepath.Join(host, ".cargo"), filepath.Join(host, ".m2", "repository"), filepath.Join(host, ".gradle")
	mkdirs(t, host, ".cargo/registry/index", ".cargo/git", ".m2/repository/junit", ".gradle/caches/modules-2")
	writeFile(t, filepath.Join(cargo, "credentials.toml"), "token")
	env := map[string]string{}
	ms, err := PackageCaches{Cargo: cargo, Maven: m2, Gradle: gradle}.Apply(work, env)
	if err != nil {
		t.Fatal(err)
	}
	ro := map[string]string{}
	for _, m := range ms {
		if m.ReadOnly {
			ro[m.Host] = m.Target
		} else if m.Host != work {
			t.Errorf("writable mount outside the work dir: %+v", m)
		}
	}
	want := map[string]string{
		filepath.Join(cargo, "registry"): filepath.Join(work, "cargo-home", "registry"),
		filepath.Join(cargo, "git"):      filepath.Join(work, "cargo-home", "git"),
		m2:                               m2,
		filepath.Join(gradle, "caches"):  filepath.Join(gradle, "caches"),
	}
	for h, tg := range want {
		if ro[h] != tg {
			t.Errorf("read-only mount %s -> %q, want %q (all: %+v)", h, ro[h], tg, ms)
		}
	}
	if _, ok := ro[cargo]; ok {
		t.Error("CARGO_HOME itself (with credentials) must not be mounted")
	}
	if env["CARGO_HOME"] != filepath.Join(work, "cargo-home") || env["CARGO_NET_OFFLINE"] != "true" ||
		env["GRADLE_USER_HOME"] != filepath.Join(work, "gradle-home") || env["GRADLE_RO_DEP_CACHE"] != filepath.Join(gradle, "caches") ||
		!strings.Contains(env["MAVEN_ARGS"], "-o ") || !strings.Contains(env["MAVEN_ARGS"], "maven.repo.local.tail="+ContainerPath(m2)) ||
		env["PIP_NO_INDEX"] != "1" {
		t.Errorf("env = %v", env)
	}

	// Without host caches the tools still work offline from the work dir.
	env = map[string]string{}
	ms, err = PackageCaches{}.Apply(work, env)
	if err != nil || len(ms) != 1 || env["CARGO_HOME"] == "" || strings.Contains(env["MAVEN_ARGS"], "tail") || env["GRADLE_RO_DEP_CACHE"] != "" {
		t.Fatalf("no caches: %+v %v %v", ms, env, err)
	}
}

func TestPrepareGradleWrapper(t *testing.T) {
	gradle, project, work := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(project, "gradle/wrapper/gradle-wrapper.properties"),
		"distributionBase=GRADLE_USER_HOME\ndistributionUrl=https\\://services.gradle.org/distributions/gradle-8.10.2-bin.zip\n")
	writeFile(t, filepath.Join(gradle, "wrapper/dists/gradle-8.10.2-bin/abc/gradle-8.10.2/bin/gradle"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(gradle, "wrapper/dists/gradle-8.10.2-bin/abc/gradle-8.10.2-bin.zip.ok"), "")
	writeFile(t, filepath.Join(gradle, "wrapper/dists/gradle-7.0-bin/x/big"), "other version")
	p := PackageCaches{Gradle: gradle}
	if err := p.PrepareGradleWrapper(project, work); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(work, "gradle-home/wrapper/dists")
	if _, err := os.Stat(filepath.Join(dst, "gradle-8.10.2-bin/abc/gradle-8.10.2-bin.zip.ok")); err != nil {
		t.Fatalf("distribution not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "gradle-7.0-bin")); err == nil {
		t.Fatal("only the project's distribution is copied")
	}
	// No wrapper, or a hostile distribution URL: nothing happens.
	if err := p.PrepareGradleWrapper(t.TempDir(), work); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https\\://x/..zip", "https\\://x/a/..", "x"} {
		props := filepath.Join(t.TempDir(), "w.properties")
		writeFile(t, props, "distributionUrl="+url+"\n")
		if n := gradleDistName(props); n != "" && n != "x" {
			t.Errorf("gradleDistName(%q) = %q", url, n)
		}
	}
}

// TestDependencyMountsOtherLanguages: Python venvs, Composer and Bundler
// directories are mounted; a Go vendor directory (tracked source) and a
// directory that only has the name are not.
func TestDependencyMountsOtherLanguages(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	mkdirs(t, src, "venv-like/venv", "go/vendor/github.com/x", "ruby/vendor/bundle/ruby", "ruby/.bundle")
	writeFile(t, filepath.Join(src, ".venv/pyvenv.cfg"), "home = /usr/bin\n")
	writeFile(t, filepath.Join(src, "php/vendor/autoload.php"), "<?php\n")
	writeFile(t, filepath.Join(src, "ruby/.bundle/config"), "BUNDLE_PATH: vendor/bundle\n")
	deps, err := DependencyMounts(src, wt)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range deps.Mounts {
		rel, _ := filepath.Rel(wt, m.Target)
		got = append(got, filepath.ToSlash(rel))
		if !m.ReadOnly {
			t.Errorf("writable dependency mount %+v", m)
		}
	}
	slices.Sort(got)
	if want := []string{".venv", "php/vendor", "ruby/.bundle", "ruby/vendor"}; !slices.Equal(got, want) {
		t.Fatalf("mounts = %v, want %v", got, want)
	}
}

// TestVenvInterpreter: the interpreter a venv was made from is mounted when
// it is a self-contained installation, never a system or broad directory.
func TestVenvInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	home, _ := os.UserHomeDir()
	py := filepath.Join(t.TempDir(), "uv", "python", "cpython-3.11.9-linux-x86_64-gnu")
	mkdirs(t, py, "bin")
	venv := t.TempDir()
	for home2, want := range map[string]string{
		filepath.Join(py, "bin"):   py,
		"/usr/bin":                 "",
		"/usr/local/bin":           "",
		filepath.Join(home, "bin"): "",
		"/opt/bin":                 "",
		"relative/bin":             "",
		filepath.Join(py, "lib"):   "",
	} {
		writeFile(t, filepath.Join(venv, "pyvenv.cfg"), "include-system-site-packages = false\nhome = "+home2+"\nversion = 3.11.9\n")
		if got := venvInterpreter(venv); got != want {
			t.Errorf("home %s: prefix %q, want %q", home2, got, want)
		}
	}
	// And it is part of the venv's dependency mounts.
	src, wt := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, ".venv/pyvenv.cfg"), "home = "+filepath.Join(py, "bin")+"\n")
	deps, err := DependencyMounts(src, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(deps.Mounts, func(m Mount) bool { return m.Host == py && m.Target == py && m.ReadOnly }) {
		t.Fatalf("interpreter not mounted: %+v", deps.Mounts)
	}
}

func TestRefusesPackageCredentialMounts(t *testing.T) {
	home, _ := os.UserHomeDir()
	c := &Container{Engine: "docker", Image: "i"}
	for _, h := range []string{".cargo/credentials.toml", ".m2/settings.xml", ".gradle/gradle.properties", ".pypirc", ".gem/credentials"} {
		if _, err := c.Args(Spec{Argv: []string{"x"}, Mounts: []Mount{{Host: filepath.Join(home, h), Target: "/m"}}}); err == nil {
			t.Errorf("mount of %s should be refused", h)
		}
	}
}
