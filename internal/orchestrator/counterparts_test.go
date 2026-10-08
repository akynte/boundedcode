package orchestrator

import (
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/xservice"
)

// TestCounterpartsDirection: HTTP, topic and gRPC contracts ask for the
// other side from either side; a definition (proto, OpenAPI spec, SQL
// schema) only when the definition itself changed. Changing a query must
// not ask for another service's migration to change.
func TestCounterpartsDirection(t *testing.T) {
	ep := func(repo, file string, k xservice.Kind) xservice.Endpoint {
		return xservice.Endpoint{Repo: repo, File: file, Line: 1, Kind: k}
	}
	links := []xservice.Link{
		{Kind: "grpc", Contract: "grpc a.S/M", From: ep("client", "c.go", xservice.GRPCCall), To: ep("server", "s.go", xservice.GRPCServe)},
		{Kind: "proto", Contract: "proto a", From: ep("client", "c.go", xservice.ProtoUse), To: ep("protos", "a.proto", xservice.ProtoDefine)},
		{Kind: "sql", Contract: "table t", From: ep("client", "q.py", xservice.SQLAccess), To: ep("server", "001.sql", xservice.SQLSchema)},
		{Kind: "openapi", Contract: "GET /x", From: ep("client", "c.go", xservice.HTTPCall), To: ep("server", "openapi.yaml", xservice.OpenAPIOperation)},
	}
	task := map[string]bool{"client": true, "server": true, "protos": true}
	for _, c := range []struct {
		name    string
		files   []string
		want    []string
		outside bool
	}{
		{"server implementation changed: its gRPC client", []string{"server/s.go"}, []string{"grpc a.S/M: grpc_call side in ./client/c.go:1"}, false},
		{"client changed: the gRPC server, not the proto or the spec", []string{"client/c.go"}, []string{"grpc a.S/M: grpc_serve side in ./server/s.go:1"}, false},
		{"query changed: nothing (the schema is not its counterpart)", []string{"client/q.py"}, nil, false},
		{"migration changed: the query", []string{"server/001.sql"}, []string{"table t: sql_access side in ./client/q.py:1"}, false},
		{"proto changed: its users", []string{"protos/a.proto"}, []string{"proto a: proto_use side in ./client/c.go:1"}, false},
		{"spec changed, client outside the task", []string{"server/openapi.yaml"}, []string{"GET /x: http_call side in ./client/c.go:1"}, true},
	} {
		repos := map[string]bool{}
		var changed []string
		for _, f := range c.files {
			repo, _, _ := strings.Cut(f, "/")
			if !repos[repo] {
				repos[repo] = true
				changed = append(changed, repo)
			}
		}
		tr := task
		if c.outside {
			tr = map[string]bool{"server": true}
		}
		in, out := counterparts(links, c.files, changed, tr)
		got := in
		if c.outside {
			got = out
		}
		if !slices.Equal(got, c.want) || c.outside && len(in) > 0 || !c.outside && len(out) > 0 {
			t.Errorf("%s: in %q out %q, want %q", c.name, in, out, c.want)
		}
	}
}
