// Package boundedcode embeds the files an installed binary needs to set up
// its prerequisites without a source checkout: the pinned dependency
// installers and the agent sandbox build context.
package boundedcode

import "embed"

// SetupScripts are the pinned installers run by `setup`.
//
//go:embed scripts/install-deps.sh scripts/build-llama-cpp.sh
var SetupScripts embed.FS

// SandboxContext is the build context of the agent sandbox image
// (adapters/openhands without tests or local environments).
//
//go:embed adapters/openhands/Dockerfile adapters/openhands/python/pyproject.toml adapters/openhands/python/uv.lock adapters/openhands/python/src/bc_openhands/*.py
var SandboxContext embed.FS
