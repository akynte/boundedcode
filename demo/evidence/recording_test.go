package evidence

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// recordings are the published sessions (docs/assets/evidence-demo).
var recordings = []string{"../../docs/assets/evidence-demo", "../../docs/assets/evidence-demo/scripted"}

// TestRecordedClaims ties what the README and demo/evidence/README.md say
// about the recordings to the unedited logs: each step is in the log, in
// order, as the product printed it.
func TestRecordedClaims(t *testing.T) {
	for _, dir := range recordings {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, "session.log"))
			if err != nil {
				t.Fatal(err)
			}
			log := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(b), "")
			steps := []string{
				"10 items at 100 cents: total 1000 cents", // 1. the bug
				"ok  \texample.com/shop",                  // 2. existing tests pass
				"verification passed but the change adds or modifies no tests; asking the agent for a test",
				"--- FAIL: Test", // 4. the change's test fails on the original code
				"Total = 1000, want 900",
				"10 items at 100 cents: total 900 cents", // 5. passes with the change
				`"reason":"1 test(s) fail on the base and pass on the change"`,
				"verification=task_verified",
			}
			at := 0
			for _, s := range steps {
				i := strings.Index(log[at:], s)
				if i < 0 {
					t.Fatalf("the recording lacks %q (after byte %d)", s, at)
				}
				at += i + len(s)
			}
			if strings.Contains(log, "/home/") || regexp.MustCompile(`[+-]0[1-9]:00 \[`).MatchString(log) {
				t.Fatal("the recording names a local path or time zone")
			}
		})
	}
}

// TestCastIsLossless: session.cast is session.log, byte for byte, without
// script's header and footer lines, so the GIF renders exactly what ran.
func TestCastIsLossless(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required")
	}
	for _, dir := range recordings {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "s.cast")
			cmd := exec.Command("python3", "to_cast.py", filepath.Join(dir, "session.timing"), filepath.Join(dir, "session.log"), out, "--cols", "100", "--rows", "34", "--title", "BoundedCode evidence demo")
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("to_cast: %v\n%s", err, b)
			}
			fresh, _ := os.ReadFile(out)
			published, _ := os.ReadFile(filepath.Join(dir, "session.cast"))
			if !bytes.Equal(fresh, published) {
				t.Fatal("the published session.cast is not the conversion of session.log")
			}
			var text strings.Builder
			for i, line := range strings.Split(strings.TrimSpace(string(fresh)), "\n") {
				if i == 0 {
					continue
				}
				var ev []any
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Fatal(err)
				}
				text.WriteString(ev[2].(string))
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "session.log"))
			body := raw[bytes.IndexByte(raw, '\n')+1:]
			if !bytes.HasPrefix(body, []byte(text.String())) || !bytes.HasPrefix(bytes.TrimSpace(body[text.Len():]), []byte("Script done")) {
				t.Fatal("the cast's output differs from the log")
			}
		})
	}
}
