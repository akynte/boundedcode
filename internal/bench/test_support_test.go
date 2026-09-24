package bench

import (
	"context"
	"os/exec"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// recordingSandbox is test-only: it records the exact spec and delegates
// execution without claiming to be a security boundary. The real
// CommandWorker selects Landlock/bubblewrap/container; this helper lets the
// self-test inspect grants on hosts where those layers cannot re-exec a test
// binary.
type recordingSandbox struct{ spec sandbox.Spec }

func (s *recordingSandbox) Name() string { return "recording-test-sandbox" }
func (s *recordingSandbox) Layers() []sandbox.Layer {
	return []sandbox.Layer{sandbox.LayerContainer, sandbox.LayerBwrap}
}
func (s *recordingSandbox) Available(context.Context) (bool, string) { return true, "" }
func (s *recordingSandbox) Command(ctx context.Context, spec sandbox.Spec, argv ...string) (*exec.Cmd, error) {
	s.spec = spec
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // test argv
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	return cmd, nil
}
