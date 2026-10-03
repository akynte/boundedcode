// Package configs embeds the default configuration files shipped with the
// binary (model profiles, verification presets, policies).
package configs

import "embed"

// FS holds the embedded configuration tree.
//
//go:embed models/*.yaml
var FS embed.FS
