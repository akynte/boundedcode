package xservice

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Structural comparison of .proto definitions, for the cross-repository
// compatibility gate (internal/compat). parseProto skips messages; this
// reads them too, so that a change can be attributed to the RPCs whose
// request or response it reaches, and classified: a field added is a
// compatible change; a field removed, renumbered, retyped or renamed, an
// enum value removed, or an RPC's types or streaming changed are breaking
// for code generated from the old definition. Types from other files are
// not followed (reported as a note).

// ProtoSchema is the structure of one .proto file.
type ProtoSchema struct {
	Package  string
	Services map[string]ProtoServiceDef // by qualified name
	Messages map[string]ProtoMessageDef // by qualified name (nested: Outer.Inner)
	Enums    map[string]map[int]string  // qualified name -> number -> value name
}

// ProtoServiceDef is a service and its RPCs by name.
type ProtoServiceDef struct {
	Line int
	RPCs map[string]ProtoRPCDef
}

// ProtoRPCDef is one RPC's signature.
type ProtoRPCDef struct {
	Req, Resp string // as written, "stream " prefixed when streaming
	Line      int
}

// ProtoField is a message field.
type ProtoField struct {
	Name, Type, Label string // Label: "", "repeated", "optional", "required"
	Number            int
}

// ProtoMessageDef is a message's fields by number.
type ProtoMessageDef struct {
	Fields map[int]ProtoField
	Line   int
}

// ParseProtoSchema reads services, RPCs, messages and enums. It never
// fails: what it cannot read is skipped.
func ParseProtoSchema(src string) ProtoSchema {
	t := protoLex(src)
	s := ProtoSchema{Services: map[string]ProtoServiceDef{}, Messages: map[string]ProtoMessageDef{}, Enums: map[string]map[int]string{}}
	f := parseProto(src)
	s.Package = f.pkg
	for _, sv := range f.services {
		d := ProtoServiceDef{Line: sv.line, RPCs: map[string]ProtoRPCDef{}}
		for _, r := range sv.rpcs {
			d.RPCs[r.name] = ProtoRPCDef{Req: r.req, Resp: r.resp, Line: r.line}
		}
		s.Services[s.qual(sv.name)] = d
	}
	for i := 0; i < len(t); i++ {
		switch {
		case t[i].kind == 'i' && t[i].text == "service" && i+2 < len(t) && t[i+2].text == "{":
			i = protoBlockEnd(t, i+2)
		case t[i].kind == 'i' && (t[i].text == "message" || t[i].text == "enum") && i+2 < len(t) && t[i+1].kind == 'i' && t[i+2].text == "{":
			i = s.parseBlock(t, i, "")
		}
	}
	return s
}

func (s ProtoSchema) qual(name string) string {
	if s.Package == "" {
		return name
	}
	return s.Package + "." + name
}

// parseBlock reads a message or enum starting at t[i] ("message"/"enum")
// and returns the index of its closing brace.
func (s ProtoSchema) parseBlock(t []protoTok, i int, outer string) int {
	name := t[i+1].text
	if outer != "" {
		name = outer + "." + name
	}
	end := protoBlockEnd(t, i+2)
	if t[i].text == "enum" {
		vals := map[int]string{}
		for j := i + 3; j+3 < end; j++ {
			if t[j].kind == 'i' && t[j+1].text == "=" && t[j+2].kind == 'n' && t[j].text != "option" {
				var n int
				if _, err := fmt.Sscan(t[j+2].text, &n); err == nil {
					vals[n] = t[j].text
				}
				j += 2
			}
		}
		s.Enums[s.qual(name)] = vals
		return end
	}
	m := ProtoMessageDef{Fields: map[int]ProtoField{}, Line: t[i].line}
	for j := i + 3; j < end; j++ {
		tk := t[j]
		if tk.kind != 'i' {
			continue
		}
		switch tk.text {
		case "message", "enum":
			if j+2 < end && t[j+1].kind == 'i' && t[j+2].text == "{" {
				j = s.parseBlock(t, j, name)
				continue
			}
		case "option", "reserved", "extensions":
			for j < end && t[j].text != ";" {
				j++
			}
			continue
		case "oneof":
			if j+2 < end && t[j+2].text == "{" {
				j += 2 // fields inside are read as fields of the message
				continue
			}
		}
		// [label] type name = number [options] ;
		k, label := j, ""
		switch tk.text {
		case "repeated", "optional", "required":
			label, k = tk.text, j+1
		}
		typ := ""
		switch {
		case k < end && t[k].text == "map" && k+1 < end && t[k+1].text == "<":
			var b strings.Builder
			for ; k < end && t[k].text != ">"; k++ {
				b.WriteString(t[k].text)
			}
			b.WriteString(">")
			typ = b.String()
		case k < end && t[k].kind == 'i':
			typ = t[k].text
		}
		if typ == "" || k+3 >= end || t[k+1].kind != 'i' || t[k+2].text != "=" || t[k+3].kind != 'n' {
			continue
		}
		var n int
		if _, err := fmt.Sscan(t[k+3].text, &n); err != nil {
			continue
		}
		m.Fields[n] = ProtoField{Name: t[k+1].text, Type: typ, Label: label, Number: n}
		for j = k + 3; j < end && t[j].text != ";"; j++ {
			if t[j].text == "[" { // field options may hold strings with ';'
				for j < end && t[j].text != "]" {
					j++
				}
			}
		}
	}
	s.Messages[s.qual(name)] = m
	return end
}

// resolve finds a referenced message or enum, from the scope of the type
// that refers to it (protobuf scoping: innermost first).
func (s ProtoSchema) resolve(ref, scope string) (string, bool) {
	ref = strings.TrimPrefix(strings.TrimPrefix(ref, "stream "), ".")
	known := func(n string) bool {
		_, m := s.Messages[n]
		_, e := s.Enums[n]
		return m || e
	}
	for sc := scope; ; {
		if c := sc + "." + ref; sc != "" && known(c) {
			return c, true
		}
		i := strings.LastIndexByte(sc, '.')
		if i < 0 {
			break
		}
		sc = sc[:i]
	}
	if known(s.qual(ref)) {
		return s.qual(ref), true
	}
	if known(ref) {
		return ref, true
	}
	return "", false
}

// ProtoChange describes how a definition changed between two versions.
type ProtoChange struct {
	Changed  bool     // any structural difference
	Removed  bool     // the RPC or service no longer exists
	Breaking []string // changes code generated from the old version cannot absorb
	Notes    []string // compatible changes and what was not compared
}

// CompareRPC compares one RPC (service is qualified; rpc empty compares the
// whole service) and every message and enum its request and response
// reach, between a base and a head version of a .proto file.
func CompareRPC(base, head ProtoSchema, service, rpc string) ProtoChange {
	var c ProtoChange
	bs, ok := base.Services[service]
	hs, hok := head.Services[service]
	if !ok {
		c.Changed, c.Notes = hok, []string{"service " + service + " is new"}
		return c
	}
	if !hok {
		c.Changed, c.Removed = true, true
		c.Breaking = append(c.Breaking, "service "+service+" removed")
		return c
	}
	var names []string
	for n := range bs.RPCs {
		if rpc == "" || normRPC(n) == normRPC(rpc) {
			names = append(names, n)
		}
	}
	if rpc == "" {
		for n := range hs.RPCs {
			if _, ok := bs.RPCs[n]; !ok {
				c.Changed = true
				c.Notes = append(c.Notes, "rpc "+n+" added")
			}
		}
	} else if len(names) == 0 {
		for n := range hs.RPCs {
			if normRPC(n) == normRPC(rpc) {
				c.Changed = true
				c.Notes = append(c.Notes, "rpc "+n+" is new")
			}
		}
		return c
	}
	sort.Strings(names)
	cmp := typeComparer{base: base, head: head, seen: map[string]bool{}, c: &c}
	for _, n := range names {
		br := bs.RPCs[n]
		hr, ok := hs.RPCs[n]
		if !ok {
			c.Changed = true
			c.Breaking = append(c.Breaking, "rpc "+n+" removed")
			if rpc != "" {
				c.Removed = true
			}
			continue
		}
		if br.Req != hr.Req || br.Resp != hr.Resp {
			c.Changed = true
			c.Breaking = append(c.Breaking, fmt.Sprintf("rpc %s signature changed: (%s) returns (%s) -> (%s) returns (%s)", n, br.Req, br.Resp, hr.Req, hr.Resp))
			continue
		}
		cmp.types(br.Req, hr.Req, service)
		cmp.types(br.Resp, hr.Resp, service)
	}
	return c
}

// CompareFile compares every service, message and enum of two versions of
// a .proto file (the whole package that importing code depends on).
func CompareFile(base, head ProtoSchema) ProtoChange {
	var c ProtoChange
	if base.Package != head.Package {
		c.Changed = true
		c.Breaking = append(c.Breaking, fmt.Sprintf("package changed: %s -> %s", base.Package, head.Package))
	}
	for _, svc := range sortedKeys(base.Services) {
		sc := CompareRPC(base, head, svc, "")
		c.merge(sc)
	}
	for _, svc := range sortedKeys(head.Services) {
		if _, ok := base.Services[svc]; !ok {
			c.Changed = true
			c.Notes = append(c.Notes, "service "+svc+" added")
		}
	}
	cmp := typeComparer{base: base, head: head, seen: map[string]bool{}, c: &c}
	for _, m := range sortedKeys(base.Messages) {
		cmp.types(m, m, "")
	}
	for _, e := range sortedKeys(base.Enums) {
		cmp.types(e, e, "")
	}
	for _, m := range sortedKeys(head.Messages) {
		if _, ok := base.Messages[m]; !ok {
			c.Changed = true
			c.Notes = append(c.Notes, "message "+m+" added")
		}
	}
	c.Breaking, c.Notes = uniq(c.Breaking), uniq(c.Notes)
	return c
}

func (c *ProtoChange) merge(o ProtoChange) {
	c.Changed = c.Changed || o.Changed
	c.Breaking = append(c.Breaking, o.Breaking...)
	c.Notes = append(c.Notes, o.Notes...)
}

type typeComparer struct {
	base, head ProtoSchema
	seen       map[string]bool
	c          *ProtoChange
}

// types compares a type reference in base and head and, for messages, their
// fields recursively.
func (tc typeComparer) types(bref, href, scope string) {
	bn, bok := tc.base.resolve(bref, scope)
	hn, hok := tc.head.resolve(href, scope)
	if !bok {
		if strings.Contains(strings.TrimPrefix(bref, "stream "), ".") || !scalarTypes[strings.TrimPrefix(bref, "stream ")] {
			tc.c.Notes = append(tc.c.Notes, "type "+strings.TrimPrefix(bref, "stream ")+" is defined in another file and was not compared")
		}
		return
	}
	if !hok || bn != hn {
		tc.c.Changed = true
		tc.c.Breaking = append(tc.c.Breaking, "type "+bn+" removed")
		return
	}
	if tc.seen[bn] {
		return
	}
	tc.seen[bn] = true
	if be, ok := tc.base.Enums[bn]; ok {
		he := tc.head.Enums[bn]
		for _, n := range sortedKeys(be) {
			switch hv, ok := he[n]; {
			case !ok:
				tc.c.Changed = true
				tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("enum %s value %d (%s) removed", bn, n, be[n]))
			case hv != be[n]:
				tc.c.Changed = true
				tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("enum %s value %d renamed %s -> %s", bn, n, be[n], hv))
			}
		}
		for n := range he {
			if _, ok := be[n]; !ok {
				tc.c.Changed = true
				tc.c.Notes = append(tc.c.Notes, fmt.Sprintf("enum %s value %d (%s) added", bn, n, he[n]))
			}
		}
		return
	}
	bm, hm := tc.base.Messages[bn], tc.head.Messages[bn]
	for _, n := range sortedKeys(bm.Fields) {
		bf := bm.Fields[n]
		hf, ok := hm.Fields[n]
		switch {
		case !ok:
			tc.c.Changed = true
			tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("field %d (%s) of %s removed", n, bf.Name, bn))
			continue
		case bf.Type != hf.Type:
			tc.c.Changed = true
			tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("field %d (%s) of %s changed type %s -> %s", n, bf.Name, bn, bf.Type, hf.Type))
			continue
		case bf.Name != hf.Name:
			tc.c.Changed = true
			tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("field %d of %s renamed %s -> %s", n, bn, bf.Name, hf.Name))
		case bf.Label != hf.Label:
			tc.c.Changed = true
			tc.c.Breaking = append(tc.c.Breaking, fmt.Sprintf("field %d (%s) of %s changed label %q -> %q", n, bf.Name, bn, bf.Label, hf.Label))
		}
		if !scalarTypes[bf.Type] && !strings.HasPrefix(bf.Type, "map<") {
			tc.types(bf.Type, hf.Type, bn)
		}
	}
	for _, n := range sortedKeys(hm.Fields) {
		if _, ok := bm.Fields[n]; !ok {
			hf := hm.Fields[n]
			tc.c.Changed = true
			tc.c.Notes = append(tc.c.Notes, fmt.Sprintf("field %d (%s %s) added to %s", n, hf.Type, hf.Name, bn))
		}
	}
}

var scalarTypes = map[string]bool{"double": true, "float": true, "int32": true, "int64": true, "uint32": true, "uint64": true,
	"sint32": true, "sint64": true, "fixed32": true, "fixed64": true, "sfixed32": true, "sfixed64": true, "bool": true,
	"string": true, "bytes": true}

func sortedKeys[K int | string, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
