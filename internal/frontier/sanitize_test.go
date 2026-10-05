package frontier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/contextplan"
)

// TestSanitizeHostPathSources covers the host-path sources a packet carries.
// Found by the 2026-10-04 validation, where one surviving path silently
// blocked a frontier escalation.
func TestSanitizeHostPathSources(t *testing.T) {
	const home = "/home/dev"
	paths := PathMap{
		"/home/dev/.local/share/bc/tasks/t1/work":         ".",
		"/home/dev/.local/share/bc/tasks/t1/work/grpc-go": "grpc-go",
		"/home/dev/src/grpc-go":                           "grpc-go",
		"/home/dev/.local/share/bc/tasks/t1":              "<task-state>",
		"/home/dev/go/pkg/mod":                            "$GOMODCACHE",
		"/home/dev/.cache/bc/build":                       "<build-cache>",
	}
	cases := []struct{ name, in, want string }{
		{"workspace path", "/home/dev/.local/share/bc/tasks/t1/work/grpc-go/credentials/tls.go:12: undefined: x",
			"grpc-go/credentials/tls.go:12: undefined: x"},
		{"work dir root", "cd /home/dev/.local/share/bc/tasks/t1/work && go test", "cd . && go test"},
		{"repository checkout", "fatal: /home/dev/src/grpc-go/.git/index.lock exists", "fatal: grpc-go/.git/index.lock exists"},
		{"verification output", "open /home/dev/.local/share/bc/tasks/t1/work/grpc-go/credentials: permission denied",
			"open grpc-go/credentials: permission denied"},
		{"stack trace", "github.com/x/y.F()\n\t/home/dev/go/pkg/mod/github.com/x/y@v1.2.3/f.go:42 +0x1f",
			"github.com/x/y.F()\n\t$GOMODCACHE/github.com/x/y@v1.2.3/f.go:42 +0x1f"},
		{"build cache", "/home/dev/.cache/bc/build/gocache/ab/cd-d", "<build-cache>/gocache/ab/cd-d"},
		{"task state", "persisted in /home/dev/.local/share/bc/tasks/t1/runtime/events", "persisted in <task-state>/runtime/events"},
		{"unmapped home path", "wrote /home/dev/scratch/test_write", "wrote $HOME/scratch/test_write"},
		{"bare home", "HOME=/home/dev", "HOME=$HOME"},
		{"tool output URL", "at file:///home/dev/proj/a.js:3:1", "at file://$HOME/proj/a.js:3:1"},
		{"JSON-escaped", `{"path":"\/home\/dev\/src\/grpc-go\/x.go"}`, `{"path":"grpc-go\/x.go"}`},
		{"URL-encoded", "GET /open?f=%2Fhome%2Fdev%2Fnotes", "GET /open?f=$HOME%2Fnotes"},
		{"other user untouched", "see /home/devon/x and /home/dev_backup/y", "see /home/devon/x and /home/dev_backup/y"},
		{"repo-relative untouched", "rest/server.go:12 ./svc/x.go credentials/tls.go", "rest/server.go:12 ./svc/x.go credentials/tls.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize(c.in, paths, home)
			if got != c.want {
				t.Fatalf("Sanitize(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
			if err := CheckPacket(got, home); err != nil {
				t.Fatalf("sanitized text fails the check: %v", err)
			}
		})
	}
}

func TestSanitizeSymlinkedLocation(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "work")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got := Sanitize("panic at "+real+"/svc/main.go:9", PathMap{link: "."}, "")
	if got != "panic at ./svc/main.go:9" {
		t.Fatalf("got %q", got)
	}
}

// TestCheckPacketFailsClosed: every spelling of the home directory is
// refused, at path boundaries only, with the location in the error.
func TestCheckPacketFailsClosed(t *testing.T) {
	for _, bad := range []string{"x /home/dev/y", "x /home/dev", `"\/home\/dev\/y"`, "f=%2Fhome%2Fdev%2Fy", "HOME=/home/dev\n"} {
		err := CheckPacket(bad, "/home/dev")
		if err == nil || !strings.Contains(err.Error(), "near") {
			t.Errorf("%q not refused: %v", bad, err)
		}
	}
	for _, ok := range []string{"/home/devon/y", "/home/dev.bak", "$HOME/x", "no paths"} {
		if err := CheckPacket(ok, "/home/dev"); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	if CheckPacket("/home/dev/x", "") != nil {
		t.Error("no home: nothing to check")
	}
}

// TestBuildPacketHasNoHostPaths: a packet assembled from every section kind
// passes the check without losing repository-relative evidence.
func TestBuildPacketHasNoHostPaths(t *testing.T) {
	const home = "/home/u"
	pack := contextplan.Pack{Sections: []contextplan.Section{
		{Key: "task", Title: "TASK", Body: "fix it; log at /home/u/tmp/run.log"},
		{Key: "verification", Title: "LATEST VERIFICATION FAILURES", Body: "go-test: /home/u/w/svc/a_test.go:5: want 1\n\t/home/u/go/pkg/mod/m@v1/x.go:3"},
		{Key: "diff", Title: "CURRENT CHANGES", Body: "+const p = \"/home/u/.config/app\"\n--- a/svc/a.go"},
		{Key: "rejected", Title: "STRATEGIES", Body: "agent ran find /home/u/.cache -name x"},
	}}
	p := BuildPacket(Trigger{Z2, "stuck"}, pack, "q", PathMap{"/home/u/w": ".", "/home/u/go/pkg/mod": "$GOMODCACHE"}, home)
	if err := CheckPacket(p, home); err != nil {
		t.Fatalf("%v\n%s", err, p)
	}
	for _, want := range []string{"./svc/a_test.go:5", "$GOMODCACHE/m@v1/x.go:3", "a/svc/a.go", "$HOME/.config/app"} {
		if !strings.Contains(p, want) {
			t.Errorf("packet lacks %q", want)
		}
	}
}
