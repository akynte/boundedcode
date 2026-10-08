package xservice

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Protocol Buffers and gRPC contracts. A .proto file is parsed (comments,
// strings, nested blocks and option values are understood; messages and
// enums are skipped) for:
//   - its package, and how generated code imports it (go_package,
//     java_package, csharp_namespace, and the file's base name, which the
//     Python, JavaScript and Ruby generators use for module names):
//     one ProtoDefine endpoint per import reference;
//   - each service, as a service-level GRPCDefine, and each rpc;
//   - google.api.http annotations (grpc-gateway, Cloud Endpoints), as
//     HTTP routes, so that HTTP clients link to the RPC behind them.

type protoTok struct {
	kind byte // 'i' identifier (dotted), 's' string, 'n' number, 'p' punctuation
	text string
	line int
}

func protoLex(src string) []protoTok {
	var out []protoTok
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i < len(src) && (src[i] != '*' || i+1 >= len(src) || src[i+1] != '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '"' || c == '\'':
			var b strings.Builder
			j := i + 1
			for j < len(src) && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				b.WriteByte(src[j])
				j++
			}
			out = append(out, protoTok{kind: 's', text: b.String(), line: line})
			i = j + 1
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' && i+1 < len(src) && isProtoIdentStart(src[i+1]):
			j := i
			for j < len(src) && (isProtoIdentStart(src[j]) || src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			out = append(out, protoTok{kind: 'i', text: strings.TrimPrefix(src[i:j], "."), line: line})
			i = j
		case c >= '0' && c <= '9' || c == '-':
			j := i + 1
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' || src[j] == 'x' || src[j] >= 'a' && src[j] <= 'f' || src[j] >= 'A' && src[j] <= 'F') {
				j++
			}
			out = append(out, protoTok{kind: 'n', text: src[i:j], line: line})
			i = j
		default:
			out = append(out, protoTok{kind: 'p', text: string(c), line: line})
			i++
		}
	}
	return out
}

func isProtoIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// protoFile is what analysis needs from one .proto file.
type protoFile struct {
	pkg                          string
	pkgAt                        int
	goPackage, javaPackage, csNS string
	services                     []protoService
}

type protoService struct {
	name string
	line int
	rpcs []protoRPC
}

type protoRPC struct {
	name, req, resp string
	line            int
	http            []protoHTTP
}

type protoHTTP struct {
	method, path string
	line         int
}

// parseProto reads the declarations analysis uses. It never fails: what
// it cannot read is skipped.
func parseProto(src string) protoFile {
	t := protoLex(src)
	var f protoFile
	text := func(i int) string {
		if i >= 0 && i < len(t) {
			return t[i].text
		}
		return ""
	}
	for i := 0; i < len(t); i++ {
		tk := t[i]
		if tk.kind != 'i' {
			continue
		}
		switch tk.text {
		case "package":
			if i+1 < len(t) && t[i+1].kind == 'i' {
				f.pkg, f.pkgAt = t[i+1].text, tk.line
			}
		case "option":
			name, val, next := protoOption(t, i+1)
			switch name {
			case "go_package":
				f.goPackage = val
			case "java_package":
				f.javaPackage = val
			case "csharp_namespace":
				f.csNS = val
			}
			i = next - 1
		case "service":
			if i+2 < len(t) && t[i+1].kind == 'i' && text(i+2) == "{" {
				svc, end := parseProtoService(t, i)
				f.services = append(f.services, svc)
				i = end
			}
		case "message", "enum", "extend":
			// Skip the block: its options are not service options.
			for j := i + 1; j < len(t); j++ {
				if t[j].text == "{" {
					i = protoBlockEnd(t, j)
					break
				}
				if t[j].text == ";" {
					break
				}
			}
		}
	}
	return f
}

// protoOption reads `name = value ;` (a constant, not an aggregate).
func protoOption(t []protoTok, i int) (name, val string, next int) {
	var nb strings.Builder
	for ; i < len(t) && t[i].text != "="; i++ {
		if t[i].text == ";" || t[i].text == "{" {
			return "", "", i
		}
		nb.WriteString(t[i].text)
	}
	if i+1 >= len(t) {
		return "", "", len(t)
	}
	if t[i+1].text == "{" {
		return nb.String(), "", protoBlockEnd(t, i+1)
	}
	return nb.String(), t[i+1].text, i + 2
}

// protoBlockEnd returns the index of the brace closing the one at i.
func protoBlockEnd(t []protoTok, i int) int {
	depth := 0
	for ; i < len(t); i++ {
		switch t[i].text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(t) - 1
}

func parseProtoService(t []protoTok, i int) (protoService, int) {
	svc := protoService{name: t[i+1].text, line: t[i].line}
	end := protoBlockEnd(t, i+2)
	for j := i + 3; j < end; j++ {
		if t[j].kind != 'i' || t[j].text != "rpc" || j+1 >= end || t[j+1].kind != 'i' {
			continue
		}
		rpc := protoRPC{name: t[j+1].text, line: t[j].line}
		// rpc Name ( [stream] Req ) returns ( [stream] Resp ) ( ; | { options } )
		k := j + 2
		types := []string{}
		for k < end && len(types) < 2 {
			if t[k].text == "(" {
				k++
				stream := ""
				if k < end && t[k].text == "stream" && k+1 < end && t[k+1].kind == 'i' && t[k+1].text != ")" {
					stream = "stream "
					k++
				}
				if k < end && t[k].kind == 'i' {
					types = append(types, stream+t[k].text)
				}
			}
			if t[k].text == "{" || t[k].text == ";" {
				break
			}
			k++
		}
		if len(types) == 2 {
			rpc.req, rpc.resp = types[0], types[1]
		}
		for k < end && t[k].text != "{" && t[k].text != ";" {
			k++
		}
		if k < end && t[k].text == "{" {
			bend := protoBlockEnd(t, k)
			rpc.http = protoHTTPRules(t[k:bend])
			k = bend
		}
		svc.rpcs = append(svc.rpcs, rpc)
		j = k
	}
	return svc, end
}

// protoHTTPRules reads `option (google.api.http) = { get: "/v1/x" ...
// additional_bindings { post: "/v1/y" } }` in an rpc's option block.
func protoHTTPRules(t []protoTok) []protoHTTP {
	var out []protoHTTP
	for i := 0; i+1 < len(t); i++ {
		if t[i].kind != 'i' || t[i].text != "google.api.http" {
			continue
		}
		var start int
		for start = i; start < len(t) && t[start].text != "{"; start++ {
		}
		if start >= len(t) {
			break
		}
		end := protoBlockEnd(t, start)
		for j := start; j+2 <= end; j++ {
			key := strings.ToLower(t[j].text)
			if t[j].kind != 'i' {
				continue
			}
			k := j + 1
			if k < end && t[k].text == ":" {
				k++
			}
			switch key {
			case "get", "put", "post", "delete", "patch":
				if k < end && t[k].kind == 's' {
					out = append(out, protoHTTP{method: strings.ToUpper(key), path: grpcGatewayPath(t[k].text), line: t[k].line})
				}
			case "custom": // custom { kind: "HEAD" path: "/x" }
				var kind, p string
				line := t[j].line
				for m := k; m+2 < end && t[m].text != "}"; m++ {
					v := m + 1
					if t[v].text == ":" {
						v++
					}
					if t[m].text == "kind" && t[v].kind == 's' {
						kind = strings.ToUpper(t[v].text)
					}
					if t[m].text == "path" && t[v].kind == 's' {
						p = t[v].text
					}
				}
				if kind != "" && p != "" {
					out = append(out, protoHTTP{method: kind, path: grpcGatewayPath(p), line: line})
				}
			}
		}
		i = end
	}
	return out
}

// grpcGatewayPath turns an HTTP rule template into a path:
// "/v1/{name=shelves/*}/books/{id}:cancel" -> "/v1/shelves/{}/books/{}:cancel".
func grpcGatewayPath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '{' {
			b.WriteByte(p[i])
			continue
		}
		j := strings.IndexByte(p[i:], '}')
		if j < 0 {
			b.WriteString(p[i:])
			break
		}
		inner := p[i+1 : i+j]
		if _, pattern, ok := strings.Cut(inner, "="); ok {
			pattern = strings.ReplaceAll(pattern, "**", "{}")
			pattern = strings.ReplaceAll(pattern, "*", "{}")
			b.WriteString(pattern)
		} else {
			b.WriteString("{}")
		}
		i += j
	}
	return b.String()
}

// analyzeProto emits the endpoints of .proto files.
func analyzeProto(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	var out []Endpoint
	var diags []Diagnostic
	for _, rel := range relFiles {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 4<<20 {
			continue
		}
		f := parseProto(string(b))
		rel = filepath.ToSlash(rel)
		line := f.pkgAt
		if line == 0 {
			line = 1
		}
		for _, ref := range protoRefs(rel, f) {
			out = append(out, Endpoint{Kind: ProtoDefine, Repo: repo, File: rel, Line: line, Proto: f.pkg, Ref: ref,
				Confidence: Exact, Detail: "proto package"})
		}
		qual := func(name string) string {
			if f.pkg == "" {
				return name
			}
			return f.pkg + "." + name
		}
		for _, s := range f.services {
			out = append(out, Endpoint{Kind: GRPCDefine, Repo: repo, File: rel, Line: s.line, Symbol: s.name, Service: qual(s.name),
				Proto: f.pkg, Confidence: Exact, Detail: "service"})
			for _, r := range s.rpcs {
				detail := "rpc"
				if r.req != "" {
					detail = "rpc (" + r.req + ") returns (" + r.resp + ")"
				}
				out = append(out, Endpoint{Kind: GRPCDefine, Repo: repo, File: rel, Line: r.line, Symbol: s.name + "." + r.name,
					Service: qual(s.name), RPC: r.name, Proto: f.pkg, Confidence: Exact, Detail: detail})
				for _, h := range r.http {
					p := NormalizePath(h.path)
					if p == "" {
						diags = append(diags, Diagnostic{File: rel, Line: h.line, Message: "google.api.http: unreadable path " + h.path})
						continue
					}
					out = append(out, Endpoint{Kind: HTTPRoute, Repo: repo, File: rel, Line: h.line, Method: h.method, Path: p,
						Symbol: qual(s.name) + "/" + r.name, Confidence: Exact, Detail: "google.api.http"})
				}
			}
		}
	}
	return out, diags
}

// protoRefs are the ways generated code for a proto file is imported.
func protoRefs(rel string, f protoFile) []string {
	refs := []string{"file:" + strings.TrimSuffix(path.Base(rel), ".proto")}
	if f.goPackage != "" {
		gp, _, _ := strings.Cut(f.goPackage, ";")
		refs = append(refs, "go:"+gp)
	}
	if f.javaPackage != "" {
		refs = append(refs, "java:"+f.javaPackage)
	} else if f.pkg != "" {
		refs = append(refs, "java:"+f.pkg) // the generator's default
	}
	ns := f.csNS
	if ns == "" && f.pkg != "" {
		// The C# generator's default: each package part in PascalCase.
		parts := strings.Split(f.pkg, ".")
		for i, p := range parts {
			parts[i] = pascal(p)
		}
		ns = strings.Join(parts, ".")
	}
	if ns != "" {
		refs = append(refs, "cs:"+ns)
	}
	return refs
}

func pascal(s string) string {
	var b strings.Builder
	up := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			up = true
			continue
		}
		if up && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		up = false
		b.WriteByte(c)
	}
	return b.String()
}
