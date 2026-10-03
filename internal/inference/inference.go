// Package inference defines the boundary to local inference runtimes and an
// OpenAI-compatible client. Runtimes are supervised external processes; this
// project never implements inference itself.
package inference

import (
	"context"
	"time"

	"github.com/akynte/boundedcode/internal/model"
)

// Runtime manages a local inference server.
type Runtime interface {
	// Name identifies the runtime (e.g. "llama.cpp").
	Name() string
	// Ensure makes a server for profile available and returns its endpoint.
	// It reuses a healthy server already serving the same profile.
	Ensure(ctx context.Context, p model.Profile) (Endpoint, error)
	// Stop stops a server this runtime started. It is a no-op for external
	// servers.
	Stop(ctx context.Context) error
	// Status reports the current server, if any.
	Status(ctx context.Context) (Status, error)
}

// Endpoint is an OpenAI-compatible server.
type Endpoint struct {
	BaseURL string `json:"base_url"` // e.g. http://127.0.0.1:8765 (no /v1 suffix)
	Model   string `json:"model"`    // model name to send in requests
}

// Status describes a running (or absent) server.
type Status struct {
	Running   bool      `json:"running"`
	Healthy   bool      `json:"healthy"`
	Managed   bool      `json:"managed"`
	PID       int       `json:"pid,omitempty"`
	Endpoint  Endpoint  `json:"endpoint"`
	Profile   string    `json:"profile,omitempty"`
	StartedAt time.Time `json:"started_at,omitzero"`
	Version   string    `json:"version,omitempty"`
	CtxSize   int       `json:"ctx_size,omitempty"`
	RSSMiB    int       `json:"rss_mib,omitempty"`
	Args      []string  `json:"args,omitempty"`
	LogPath   string    `json:"log_path,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}
