// Package xservice extracts cross-service contracts from source and
// configuration — HTTP routes and client calls, message-topic producers and
// consumers, environment variables read and provided — and links them across
// repositories. It complements the code graph (codebase-memory-mcp), filling
// the gaps measured in docs/design/repointel-gap-report.md.
//
// Analyzers are deterministic, parser-based where practical (go/ast for Go,
// a JS/TS lexer, YAML node trees), and never execute repository code.
package xservice

import (
	"fmt"
	"sort"
	"strings"
)

// Kind classifies an endpoint.
type Kind string

// Endpoint kinds.
const (
	HTTPRoute      Kind = "http_route"      // server handles METHOD path
	HTTPCall       Kind = "http_call"       // client calls METHOD path
	TopicProduce   Kind = "topic_produce"   // publishes to a topic/queue
	TopicConsume   Kind = "topic_consume"   // subscribes to a topic/queue
	TopicProvision Kind = "topic_provision" // infrastructure declares a topic
	EnvRead        Kind = "env_read"        // code reads an environment variable
	EnvProvide     Kind = "env_provide"     // deployment config supplies it
)

// Confidence describes how a value was obtained.
type Confidence string

// Confidence levels.
const (
	// Exact: literal value at the use site.
	Exact Confidence = "exact"
	// Resolved: value obtained by resolving constants/concatenation/format.
	Resolved Confidence = "resolved"
	// Partial: some parts could not be resolved (shown as {}), or the
	// classification is heuristic.
	Partial Confidence = "partial"
)

// Endpoint is one contract occurrence in a repository.
type Endpoint struct {
	Kind       Kind       `json:"kind"`
	Repo       string     `json:"repo"`
	File       string     `json:"file"` // repo-relative, slash-separated
	Line       int        `json:"line"`
	Symbol     string     `json:"symbol,omitempty"` // enclosing function or handler
	Method     string     `json:"method,omitempty"` // HTTP method, upper case; empty = any
	Path       string     `json:"path,omitempty"`   // normalized HTTP path template
	Topic      string     `json:"topic,omitempty"`
	Env        string     `json:"env,omitempty"`
	Confidence Confidence `json:"confidence"`
	Detail     string     `json:"detail,omitempty"` // e.g. matched API ("sarama.ProducerMessage")
}

// Key returns the contract identity used for linking and display.
func (e Endpoint) Key() string {
	switch e.Kind {
	case HTTPRoute, HTTPCall:
		m := e.Method
		if m == "" {
			m = "ANY"
		}
		return m + " " + e.Path
	case TopicProduce, TopicConsume, TopicProvision:
		return "topic " + e.Topic
	default:
		return "env " + e.Env
	}
}

// Where formats repo:file:line.
func (e Endpoint) Where() string { return fmt.Sprintf("%s:%s:%d", e.Repo, e.File, e.Line) }

// Link connects two endpoints of the same contract.
type Link struct {
	Kind     string   `json:"kind"` // http | topic | topic_infra | env
	Contract string   `json:"contract"`
	From     Endpoint `json:"from"` // caller / producer / reader / provisioner
	To       Endpoint `json:"to"`   // route / consumer / provider / user
}

func (l Link) String() string {
	return fmt.Sprintf("%-11s %-36s %s (%s) -> %s (%s)", l.Kind, l.Contract, l.From.Where(), l.From.Kind, l.To.Where(), l.To.Kind)
}

// Diagnostic records something an analyzer could not handle (skipped files,
// unresolved values). Diagnostics are informational.
type Diagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// SortEndpoints orders endpoints deterministically.
func SortEndpoints(es []Endpoint) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Key() < b.Key()
	})
}

// NormalizePath turns a URL or path (possibly with unresolved parts "{}")
// into a path template: no scheme/host/query, single slashes, no trailing
// slash, parameters as "{}".
func NormalizePath(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			s = rest[j:]
		} else {
			return "/"
		}
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	// A leading unresolved base ("{}/v1/x") means host unknown, path known.
	s = strings.TrimPrefix(s, "{}")
	if !strings.HasPrefix(s, "/") {
		return ""
	}
	var out []string
	for _, seg := range strings.Split(s, "/") {
		if seg == "" {
			continue
		}
		switch {
		case seg == "{$}":
			continue // Go 1.22 exact-match marker
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"),
			strings.HasPrefix(seg, ":"), seg == "*", strings.HasPrefix(seg, "*"),
			strings.Contains(seg, "{}"):
			out = append(out, "{}")
		default:
			out = append(out, seg)
		}
	}
	return "/" + strings.Join(out, "/")
}

// pathsMatch reports whether a route template matches a call path. "{}" in
// either matches any single segment; a trailing "{...}" wildcard (Go "{x...}")
// is treated as one segment.
func pathsMatch(route, call string) bool {
	if route == "" || call == "" {
		return false
	}
	rs, cs := strings.Split(strings.Trim(route, "/"), "/"), strings.Split(strings.Trim(call, "/"), "/")
	if len(rs) != len(cs) {
		return false
	}
	for i := range rs {
		if rs[i] == "{}" || cs[i] == "{}" {
			continue
		}
		if rs[i] != cs[i] {
			return false
		}
	}
	return true
}
