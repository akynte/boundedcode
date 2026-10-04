// Package configs embeds the default configuration files shipped with the
// binary (model profiles, verification presets, policies).
package configs

import "embed"

// FS holds the embedded configuration tree.
//
//go:embed models/*.yaml
var FS embed.FS

// SerenaEnv is the locked Python environment for Serena v1.7.0
// (pyproject.toml + uv.lock), installed by `serena setup` with
// `uv sync --frozen`. See ADR-0008.
//
//go:embed serena/pyproject.toml serena/uv.lock
var SerenaEnv embed.FS
