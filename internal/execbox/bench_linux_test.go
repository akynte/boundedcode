//go:build linux

package execbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The cost one command pays for its containment: `true`, and a small Go
// build, run directly and under each backend.
func BenchmarkCommand(b *testing.B) {
	root := b.TempDir()
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n\nfunc F() int { return 1 }\n"), 0o644)
	for _, be := range []struct{ name, backend, mode string }{{"direct", "", "off"}, {"bwrap", "bwrap", ""}, {"landlock", "landlock", ""}} {
		box := New(root, Config{Backend: be.backend, Mode: be.mode, StateDir: filepath.Join(b.TempDir(), "s")})
		if be.mode == "" && box.Report().Backend != be.backend {
			continue
		}
		for _, cmd := range []struct{ name, line string }{{"true", "true"}, {"go-build", "go build ./..."}} {
			b.Run(be.name+"/"+cmd.name, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					c, err := box.Command(context.Background(), Spec{Argv: []string{"bash", "-c", cmd.line}, Dir: root})
					if err != nil {
						b.Fatal(err)
					}
					c.Stdout, c.Stderr = io.Discard, io.Discard
					if err := c.Run(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
