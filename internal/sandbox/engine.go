package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
)

// Container engine problems, distinguished so each gets its own fix. Match
// them with errors.Is on the *EngineError CheckEngine returns.
var (
	ErrEngineMissing     = errors.New("container engine not installed")
	ErrEngineUnreachable = errors.New("container engine not usable")
	ErrImageMissing      = errors.New("sandbox image not built")
)

// engineCheckTimeout bounds each engine probe; a hung daemon must not hang
// the command that asked.
const engineCheckTimeout = 15 * time.Second

// EngineError is a container engine that cannot run the sandbox, with the
// step that fixes it.
type EngineError struct {
	Kind   error // ErrEngineMissing, ErrEngineUnreachable or ErrImageMissing
	Engine string
	Image  string
	// Detail is the engine's own explanation (first line of its output).
	Detail string
}

// Problem states what is wrong, without the fix.
func (e *EngineError) Problem() string {
	switch {
	case errors.Is(e.Kind, ErrEngineMissing):
		return e.Engine + " is not installed (the agent sandbox needs Docker or Podman)"
	case errors.Is(e.Kind, ErrEngineUnreachable):
		p := e.Engine + " is installed but not usable: is the daemon running, and may your user use it?"
		if e.Detail != "" {
			p += " (" + e.Detail + ")"
		}
		return p
	default:
		return "sandbox image " + e.Image + " is not built"
	}
}

// Hint is the fix, naming the command as the user invoked it.
func (e *EngineError) Hint() string {
	setup := "`" + buildinfo.Command() + " setup --only sandbox`"
	switch {
	case errors.Is(e.Kind, ErrEngineMissing):
		return "install Docker (https://docs.docker.com/engine/install/) or Podman, then run " + setup
	case errors.Is(e.Kind, ErrEngineUnreachable):
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			return "start Docker Desktop (or, with Podman, `podman machine start`), then run " + setup
		}
		if filepath.Base(e.Engine) == "docker" {
			return "start it (e.g. `sudo systemctl start docker`) and add your user to the docker group (`sudo usermod -aG docker $USER`, then log in again)"
		}
		return "start " + e.Engine + " and check that your user may run containers with it"
	default:
		return "run " + setup + " to build it"
	}
}

func (e *EngineError) Error() string { return e.Problem() + "; " + e.Hint() }

// Unwrap returns Kind.
func (e *EngineError) Unwrap() error { return e.Kind }

// CheckEngine reports whether engine can run containers from image ("" skips
// the image check). It distinguishes an engine that is not installed, one
// whose daemon is down or refuses the user, and a missing image; nil means
// ready. setup, doctor and task runs share it.
func CheckEngine(ctx context.Context, engine, image string) error {
	bin, err := exec.LookPath(engine)
	if err != nil {
		return &EngineError{Kind: ErrEngineMissing, Engine: engine, Image: image}
	}
	pctx := ctx
	ctx, cancel := context.WithTimeout(ctx, engineCheckTimeout)
	defer cancel()
	if image != "" {
		if exec.CommandContext(ctx, bin, "image", "inspect", image).Run() == nil {
			return nil
		}
		if pctx.Err() != nil {
			return pctx.Err()
		}
	}
	// The image may be missing or the daemon unreachable: `info` tells which.
	if out, err := exec.CommandContext(ctx, bin, "info").CombinedOutput(); err != nil {
		if pctx.Err() != nil {
			return pctx.Err()
		}
		detail := engineErrorLine(string(out))
		if ctx.Err() != nil {
			detail = fmt.Sprintf("no answer within %s", engineCheckTimeout)
		}
		return &EngineError{Kind: ErrEngineUnreachable, Engine: engine, Image: image, Detail: detail}
	}
	if image == "" {
		return nil
	}
	return &EngineError{Kind: ErrImageMissing, Engine: engine, Image: image}
}

// Checker is a sandbox that can diagnose whether it is able to run
// commands (Container). Callers use it to explain a command that failed to
// start.
type Checker interface {
	Check(ctx context.Context) error
}

var _ Checker = (*Container)(nil)

// Check is CheckEngine for this container's engine and image.
func (c *Container) Check(ctx context.Context) error {
	return CheckEngine(ctx, c.Engine, c.Image)
}

// engineErrorLine picks the line of engine output that explains a failure
// (`docker info` prints the client section before the daemon error).
func engineErrorLine(out string) string {
	var first string
	for l := range strings.SplitSeq(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first == "" {
			first = l
		}
		low := strings.ToLower(l)
		if strings.Contains(low, "cannot connect") || strings.Contains(low, "failed to connect") || strings.Contains(low, "permission denied") || strings.HasPrefix(low, "error") {
			return truncate(l, 200)
		}
	}
	return truncate(first, 200)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// EngineFailure reports whether a container command's exit code and output
// show that the engine itself failed to start the command (daemon error,
// missing image, invalid mount) rather than the command failing. Docker and
// Podman exit 125 for their own errors; Docker prefixes them with its name
// or the daemon's response, Podman with "Error:".
func EngineFailure(engine string, exitCode int, output string) bool {
	if exitCode != 125 {
		return false
	}
	name := filepath.Base(engine)
	for l := range strings.SplitSeq(output, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		return strings.HasPrefix(l, name+":") || strings.HasPrefix(l, "Error response from daemon") ||
			strings.HasPrefix(l, "Unable to find image") || strings.Contains(name, "podman") && strings.HasPrefix(l, "Error:")
	}
	return false
}
