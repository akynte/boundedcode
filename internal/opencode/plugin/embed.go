// Package plugin embeds the OpenCode context-hook adapter into the bcode
// binary, so it exists wherever bcode is installed rather than only inside a
// checkout of this repository's own source tree.
package plugin

import "embed"

// FS holds the plugin's own files, unpacked at setup time by
// [github.com/akynte/boundedcode/internal/opencode.InstallPlugin] into a
// stable, absolute path that opencode.json can name from any project.
//
//go:embed index.js package.json
var FS embed.FS
