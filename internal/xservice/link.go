package xservice

import (
	"sort"
	"strings"
)

// LinkOptions control linking.
type LinkOptions struct {
	// SameRepoHTTP links routes and calls within one repository (off by
	// default: in-process calls are rarely contracts).
	SameRepoHTTP bool
}

// LinkAll joins endpoints from all repositories into contract links:
//   - http:  client call -> route (method-compatible, path template match),
//     across repositories;
//   - topic: producer -> consumer of the same topic, across repositories;
//   - topic_infra: provisioner (e.g. Terraform) -> producer/consumer;
//   - env:   reader -> provider (same or different repository);
//   - openapi: client call -> spec operation, across repositories;
//   - openapi_impl: route -> the spec operation it implements;
//   - grpc:  client -> server of the same service (and RPC), across
//     repositories;
//   - grpc_def: client or server -> the .proto service or rpc, across
//     repositories;
//   - proto: code importing generated code -> the .proto package, across
//     repositories;
//   - sql:   query or ORM mapping -> the schema defining its table in
//     another repository, when the querying repository does not define
//     that table itself (two services that each own a "users" table are
//     not linked).
//
// In every link From depends on To: To is the side whose change can break
// From.
func LinkAll(eps []Endpoint, opt LinkOptions) []Link {
	by := map[Kind][]Endpoint{}
	for _, e := range eps {
		by[e.Kind] = append(by[e.Kind], e)
	}
	var out []Link
	for _, c := range by[HTTPCall] {
		for _, r := range by[HTTPRoute] {
			if (c.Repo == r.Repo && !opt.SameRepoHTTP) || !pathsMatch(r.Path, c.Path) {
				continue
			}
			if r.Method != "" && c.Method != "" && r.Method != c.Method {
				continue
			}
			out = append(out, Link{Kind: "http", Contract: r.Key(), From: c, To: r})
		}
	}
	for _, p := range by[TopicProduce] {
		for _, c := range by[TopicConsume] {
			if p.Topic == c.Topic && p.Repo != c.Repo {
				out = append(out, Link{Kind: "topic", Contract: "topic " + p.Topic, From: p, To: c})
			}
		}
	}
	for _, pr := range by[TopicProvision] {
		for _, k := range []Kind{TopicProduce, TopicConsume} {
			for _, u := range by[k] {
				if pr.Topic == u.Topic {
					out = append(out, Link{Kind: "topic_infra", Contract: "topic " + pr.Topic, From: pr, To: u})
				}
			}
		}
	}
	for _, rd := range by[EnvRead] {
		for _, pv := range by[EnvProvide] {
			if rd.Env == pv.Env {
				out = append(out, Link{Kind: "env", Contract: "env " + rd.Env, From: rd, To: pv})
			}
		}
	}
	out = append(out, linkOpenAPI(by)...)
	out = append(out, linkGRPC(by)...)
	out = append(out, linkProto(by)...)
	out = append(out, linkSQL(by)...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Contract != b.Contract {
			return a.Contract < b.Contract
		}
		if a.From.Where() != b.From.Where() {
			return a.From.Where() < b.From.Where()
		}
		return a.To.Where() < b.To.Where()
	})
	return dedupe(out)
}

func dedupe(ls []Link) []Link {
	seen := map[string]bool{}
	var out []Link
	for _, l := range ls {
		k := l.Kind + "|" + l.From.Where() + "|" + l.To.Where() + "|" + l.Contract
		if !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}

// Unresolved reports endpoints whose contract value could not be determined
// well enough to link (e.g. an HTTP call whose path is entirely dynamic).
func Unresolved(eps []Endpoint) []Endpoint {
	var out []Endpoint
	for _, e := range eps {
		switch e.Kind {
		case HTTPCall, HTTPRoute:
			if e.Path == "" {
				out = append(out, e)
			}
		case TopicProduce, TopicConsume, TopicProvision:
			if e.Topic == "" {
				out = append(out, e)
			}
		}
	}
	return out
}

func methodsCompatible(a, b string) bool { return a == "" || b == "" || a == b }

func linkOpenAPI(by map[Kind][]Endpoint) []Link {
	var out []Link
	for _, op := range by[OpenAPIOperation] {
		for _, c := range by[HTTPCall] {
			if c.Repo != op.Repo && pathsMatch(op.Path, c.Path) && methodsCompatible(op.Method, c.Method) {
				out = append(out, Link{Kind: "openapi", Contract: op.Key(), From: c, To: op})
			}
		}
		for _, r := range by[HTTPRoute] {
			if r.Detail != "google.api.http" && pathsMatch(op.Path, r.Path) && methodsCompatible(op.Method, r.Method) {
				out = append(out, Link{Kind: "openapi_impl", Contract: op.Key(), From: r, To: op})
			}
		}
	}
	return out
}

// normRPC compares RPC names across generators: CreatePayment,
// createPayment and create_payment are the same RPC.
func normRPC(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }

func shortService(s string) string { return s[strings.LastIndexByte(s, '.')+1:] }

// grpcIndex resolves service names against the .proto definitions.
type grpcIndex struct {
	services map[string]Endpoint // full name -> service-level define
	rpcs     map[string]Endpoint // full name + "/" + normRPC -> rpc define
	short    map[string][]string // short name -> full names
	refPkgs  map[string][]string // proto Ref -> proto packages
}

func newGRPCIndex(by map[Kind][]Endpoint) grpcIndex {
	ix := grpcIndex{services: map[string]Endpoint{}, rpcs: map[string]Endpoint{}, short: map[string][]string{}, refPkgs: map[string][]string{}}
	for _, d := range by[GRPCDefine] {
		if d.RPC == "" {
			if _, ok := ix.services[d.Service]; !ok {
				ix.services[d.Service] = d
				ix.short[shortService(d.Service)] = append(ix.short[shortService(d.Service)], d.Service)
			}
			continue
		}
		ix.rpcs[d.Service+"/"+normRPC(d.RPC)] = d
	}
	for _, d := range by[ProtoDefine] {
		ix.refPkgs[d.Ref] = append(ix.refPkgs[d.Ref], d.Proto)
	}
	return ix
}

// resolve returns the fully qualified services an endpoint may refer to.
// A short name is narrowed by the generated package the code imports.
func (ix grpcIndex) resolve(e Endpoint) []string {
	if _, ok := ix.services[e.Service]; ok {
		return []string{e.Service}
	}
	cands := ix.short[shortService(e.Service)]
	if e.Ref != "" && len(cands) > 1 {
		var keep []string
		for _, full := range cands {
			for _, pkg := range ix.refPkgs[e.Ref] {
				if full == pkg+"."+shortService(e.Service) || pkg == "" && full == shortService(e.Service) {
					keep = append(keep, full)
				}
			}
		}
		if len(keep) > 0 {
			return keep
		}
	}
	return cands
}

// names are the service names an endpoint matches: resolved ones, or its
// short name when no definition is known.
func (ix grpcIndex) names(e Endpoint) ([]string, bool) {
	if r := ix.resolve(e); len(r) > 0 {
		return r, true
	}
	return []string{shortService(e.Service)}, false
}

func linkGRPC(by map[Kind][]Endpoint) []Link {
	ix := newGRPCIndex(by)
	hasRPCDefs := map[string]bool{}
	for k := range ix.rpcs {
		svc, _, _ := strings.Cut(k, "/")
		hasRPCDefs[svc] = true
	}
	// valid drops RPC-level endpoints whose RPC the resolved service does
	// not define (a health check's Check implemented on the same class).
	valid := func(e Endpoint) bool {
		if e.RPC == "" {
			return true
		}
		known := false
		for _, full := range ix.resolve(e) {
			if !hasRPCDefs[full] {
				return true // nothing to check against
			}
			known = true
			if _, ok := ix.rpcs[full+"/"+normRPC(e.RPC)]; ok {
				return true
			}
		}
		return !known
	}
	var calls, serves []Endpoint
	for _, e := range by[GRPCCall] {
		if valid(e) {
			calls = append(calls, e)
		}
	}
	for _, e := range by[GRPCServe] {
		if valid(e) {
			serves = append(serves, e)
		}
	}
	// A client constructor is not linked when the same file calls RPCs of
	// the service: those say more.
	callsRPC := map[string]bool{}
	for _, e := range calls {
		if e.RPC != "" {
			callsRPC[e.Repo+"|"+e.File+"|"+shortService(e.Service)] = true
		}
	}
	// Servers implementing an RPC method by method (repo|service|rpc).
	servesRPC := map[string]bool{}
	for _, e := range serves {
		if e.RPC != "" {
			servesRPC[e.Repo+"|"+shortService(e.Service)+"|"+normRPC(e.RPC)] = true
		}
	}
	// pairs: a service-level call with a service-level server; an RPC call
	// with that RPC's implementation, or with the server's registration
	// when the server has no method-level endpoint for it.
	pairs := func(c, sv Endpoint) bool {
		switch {
		case c.RPC == "":
			return sv.RPC == ""
		case sv.RPC != "":
			return normRPC(c.RPC) == normRPC(sv.RPC)
		default:
			return !servesRPC[sv.Repo+"|"+shortService(sv.Service)+"|"+normRPC(c.RPC)]
		}
	}
	contract := func(svc, rpc string) string {
		if d, ok := ix.rpcs[svc+"/"+normRPC(rpc)]; ok && rpc != "" {
			rpc = d.RPC
		}
		if rpc == "" {
			return "grpc " + svc
		}
		return "grpc " + svc + "/" + rpc
	}
	var out []Link
	for _, c := range calls {
		if c.RPC == "" && callsRPC[c.Repo+"|"+c.File+"|"+shortService(c.Service)] {
			continue
		}
		cn, cok := ix.names(c)
		for _, sv := range serves {
			if sv.Repo == c.Repo || !pairs(c, sv) {
				continue
			}
			sn, sok := ix.names(sv)
		match:
			for _, name := range cn {
				for _, other := range sn {
					if name == other || (!cok || !sok) && shortService(name) == shortService(other) {
						out = append(out, Link{Kind: "grpc", Contract: contract(name, c.RPC), From: c, To: sv})
						break match
					}
				}
			}
		}
	}
	for _, e := range append(calls, serves...) {
		if e.Kind == GRPCCall && e.RPC == "" && callsRPC[e.Repo+"|"+e.File+"|"+shortService(e.Service)] {
			continue
		}
		for _, full := range ix.resolve(e) {
			d, ok := ix.rpcs[full+"/"+normRPC(e.RPC)]
			if !ok || e.RPC == "" {
				d = ix.services[full]
			}
			if d.Repo != e.Repo {
				out = append(out, Link{Kind: "grpc_def", Contract: d.Key(), From: e, To: d})
			}
		}
	}
	return out
}

func linkProto(by map[Kind][]Endpoint) []Link {
	defs := map[string][]Endpoint{}
	for _, d := range by[ProtoDefine] {
		defs[d.Ref] = append(defs[d.Ref], d)
	}
	var out []Link
	for _, u := range by[ProtoUse] {
		for _, d := range defs[u.Ref] {
			if d.Repo != u.Repo {
				out = append(out, Link{Kind: "proto", Contract: d.Key(), From: u, To: d})
			}
		}
	}
	return out
}

func lastSegment(t string) string { return t[strings.LastIndexByte(t, '.')+1:] }

func linkSQL(by map[Kind][]Endpoint) []Link {
	schemas := map[string][]Endpoint{} // by unqualified table name
	for _, s := range by[SQLSchema] {
		schemas[lastSegment(s.Table)] = append(schemas[lastSegment(s.Table)], s)
	}
	var out []Link
	seen := map[string]bool{}
	for _, a := range by[SQLAccess] {
		k := a.Repo + "|" + a.File + "|" + a.Table
		if seen[k] {
			continue // one link per file and table
		}
		seen[k] = true
		var owners []Endpoint
		own := false
		for _, s := range schemas[lastSegment(a.Table)] {
			if !tablesMatch(a.Table, s.Table) {
				continue
			}
			if s.Repo == a.Repo {
				own = true
				break
			}
			owners = append(owners, s)
		}
		if own {
			continue
		}
		files := map[string]bool{} // one link per schema file (its first statement)
		for _, s := range owners {
			if !files[s.Repo+"/"+s.File] {
				files[s.Repo+"/"+s.File] = true
				out = append(out, Link{Kind: "sql", Contract: "table " + s.Table, From: a, To: s})
			}
		}
	}
	return out
}

// DefinitionLinks are link kinds whose To side is a published definition
// (a spec, a .proto, a schema). A change to it can break From; a change to
// From cannot break it.
var DefinitionLinks = map[string]bool{"openapi": true, "openapi_impl": true, "grpc_def": true, "proto": true, "sql": true}

// SymmetricLinks are link kinds where a change to either side can break
// the other (a route and its callers, a producer and its consumers).
var SymmetricLinks = map[string]bool{"http": true, "topic": true, "grpc": true}
