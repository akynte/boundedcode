package bench

import (
	"context"
	"fmt"
	"os"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/bwrap"
	"github.com/akynte/boundedcode/internal/sandbox/landlock"
)

// SelectCommandSandbox returns the strongest process boundary available on
// the host for benchmark-owned commands. A container-only runner is considered
// only when the harness itself is actually inside a container; on a host
// install it is not a filesystem or network boundary and must not be presented
// as one.
func SelectCommandSandbox(ctx context.Context) sandbox.Runner {
	ll, _ := landlock.New()
	candidates := []sandbox.Runner{bwrap.New(ll)}
	if ll != nil {
		candidates = append(candidates, ll)
	}
	if realContainerMarker() {
		candidates = append(candidates, sandbox.ContainerRunner{})
	}
	runner, _ := sandbox.Select(ctx, candidates)
	return runner
}

// RequireConfinedRunner rejects a runner that cannot provide the boundary the
// benchmark requested. In particular, NetworkNone is accepted on Landlock
// only when the running kernel actually supports its network rules.
func realContainerMarker() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

func RequireConfinedRunner(runner sandbox.Runner, network sandbox.Network) error {
	if runner == nil {
		return fmt.Errorf("bench: no process sandbox is available")
	}
	layers := runner.Layers()
	hasBwrap, hasLandlock, hasContainer := false, false, false
	for _, layer := range layers {
		switch layer {
		case sandbox.LayerBwrap:
			hasBwrap = true
		case sandbox.LayerLandlock:
			hasLandlock = true
		case sandbox.LayerContainer:
			hasContainer = true
		}
	}
	if hasBwrap {
		return nil
	}
	if hasContainer {
		if realContainerMarker() {
			return nil
		}
		return fmt.Errorf("bench: selected %s has no host filesystem boundary; refusing an unconfined benchmark command", runner.Name())
	}
	if hasLandlock {
		if network != sandbox.NetworkNone {
			return nil
		}
		if supported, reason := landlock.SupportsNetwork(); supported {
			return nil
		} else {
			return fmt.Errorf("bench: Landlock cannot enforce network_policy=none: %s", reason)
		}
	}
	return fmt.Errorf("bench: selected %s has no host filesystem boundary", runner.Name())
}
