package ecosystem

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// command is a project's build or test command without the script it
// stands for ("npm test (node --test)" is "npm test").
func command(s string) string {
	if i := strings.Index(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		return s[:i]
	}
	return s
}

// The commands detection finds for each project of the polyglot fixture
// build and test it with the project's native tools, in the sandbox image
// (offline; Maven and Gradle download their plugins, so they run only
// with BOUNDEDCODE_NET_TESTS=1). Projects whose toolchain the image lacks
// must fail as "not installed", never pass. Needs Docker and the image:
// BOUNDEDCODE_DOCKER_TESTS=1.
func TestNativeToolchainsInSandboxImage(t *testing.T) {
	if os.Getenv("BOUNDEDCODE_DOCKER_TESTS") == "" {
		t.Skip("set BOUNDEDCODE_DOCKER_TESTS=1 (needs Docker and boundedcode-openhands:local)")
	}
	image := "boundedcode-openhands:local"
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skip("image not built: " + image)
	}
	home, _ := os.UserHomeDir()
	work, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-native-") // Docker Desktop shares only the home directory
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := exec.Command("cp", "-r", polyglot+"/.", work).Run(); err != nil {
		t.Fatal(err)
	}
	rep := Detect(work, Options{MaxProjects: 50, LookPath: func(string) (string, error) { return "/x", nil }})
	net := os.Getenv("BOUNDEDCODE_NET_TESTS") != ""
	absent := map[string]bool{"dotnet": true, "swiftpm": true, "pub": true, "terraform": true}
	ran := 0
	for _, p := range rep.Projects {
		cmds := []string{}
		for _, c := range []string{command(p.Build), command(p.Test)} {
			if c != "" && !strings.HasPrefix(c, "./configure") {
				cmds = append(cmds, c)
			}
		}
		if len(cmds) == 0 || p.System == "node" && p.Dir == "web/admin" { // typecheck only, needs node_modules
			continue
		}
		network := "none"
		if p.System == "maven" || p.System == "gradle" {
			if !net {
				t.Logf("%s (%s): needs the network for plugins; set BOUNDEDCODE_NET_TESTS=1", p.Dir, p.System)
				continue
			}
			network = "bridge"
		}
		script := strings.Join(cmds, " && ")
		if p.System == "maven" {
			script = strings.ReplaceAll(script, "mvn ", "mvn -q -B ")
		}
		if p.System == "gradle" {
			script = strings.ReplaceAll(script, "gradle ", "gradle --no-daemon -q ")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		c := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", network, "-u", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()),
			"-e", "HOME=/tmp", "-e", "GOCACHE=/tmp/gocache", "-e", "CARGO_HOME=/tmp/cargo", "-e", "GRADLE_USER_HOME=/tmp/gradle",
			"-v", work+":/w", "-w", "/w/"+p.Dir, image, "sh", "-c", script)
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		cancel()
		ran++
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", p.Dir, err)
		}
		if absent[p.System] {
			if code != 127 && !strings.Contains(out.String(), "not found") {
				t.Errorf("%s (%s): the image has no toolchain, yet %q gave exit %d:\n%s", p.Dir, p.System, script, code, tail(out.String()))
			}
			continue
		}
		if code != 0 {
			t.Errorf("%s (%s): %q failed (exit %d):\n%s", p.Dir, p.System, script, code, tail(out.String()))
		} else {
			t.Logf("%s (%s): %s: ok", p.Dir, p.System, script)
		}
	}
	if ran < 10 {
		t.Fatalf("only %d projects ran", ran)
	}
}

func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}
