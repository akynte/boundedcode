package xservice

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Contracts in languages without a dedicated analyzer (Python, Java,
// Kotlin, Scala, Rust, C#, Ruby, PHP), and the idioms of JavaScript/
// TypeScript that are clearest as patterns: gRPC servers and clients by
// the names generated stubs have in each language (XGrpc.newBlockingStub,
// add_XServicer_to_server, XClient::connect, ...), protobuf imports, SQL in
// string literals, ORM table mappings and schema migrations. Source is
// first lexed per language family so that comments are ignored and string
// literals (including triple-quoted, raw, verbatim and heredoc strings) are
// read whole; adjacent literals joined by +, . or juxtaposition are merged,
// so a query split over several lines is analyzed as one.

// srcLang is a source language family.
type srcLang string

const (
	langPython srcLang = "py"
	langJVM    srcLang = "jvm" // Java, Kotlin, Scala, Groovy
	langRust   srcLang = "rs"
	langCSharp srcLang = "cs"
	langRuby   srcLang = "rb"
	langPHP    srcLang = "php"
	langJS     srcLang = "js"
)

// sourceLang returns the family of a file, or "".
func sourceLang(name string) srcLang {
	switch strings.ToLower(path.Ext(name)) {
	case ".py":
		return langPython
	case ".java", ".kt", ".kts", ".scala", ".groovy":
		return langJVM
	case ".rs":
		return langRust
	case ".cs":
		return langCSharp
	case ".rb":
		return langRuby
	case ".php":
		return langPHP
	}
	return ""
}

// srcString is a string literal (merged with adjacent ones).
type srcString struct {
	val  string
	line int
}

// srcFile is lexed source: code with comments blanked (same length, same
// newlines, strings kept), bare with string contents blanked too (for
// patterns that must not match inside strings and docstrings), and its
// string literals.
type srcFile struct {
	code, bare string
	strs       []srcString
	lineStarts []int
}

func (f *srcFile) lineAt(off int) int {
	return sort.Search(len(f.lineStarts), func(i int) bool { return f.lineStarts[i] > off })
}

// lexSource blanks comments and collects string literals.
func lexSource(lang srcLang, src string) *srcFile {
	b, bare := []byte(src), []byte(src)
	f := &srcFile{lineStarts: []int{0}}
	for i, c := range b {
		if c == '\n' {
			f.lineStarts = append(f.lineStarts, i+1)
		}
	}
	blank := func(buf []byte, from, to int) {
		for k := max(from, 0); k < to && k < len(buf); k++ {
			if buf[k] != '\n' {
				buf[k] = ' '
			}
		}
	}
	lastEnd := -1
	depth := 0 // bracket nesting (Python joins literals across lines only inside brackets)
	addStr := func(start, end int, val string) {
		blank(bare, start+1, end-1) // keep the quotes, drop the contents
		if lastEnd >= 0 && joinable(lang, src[lastEnd:start], depth > 0) && len(f.strs) > 0 {
			f.strs[len(f.strs)-1].val += val
		} else {
			f.strs = append(f.strs, srcString{val: val, line: f.lineAt(start)})
		}
		lastEnd = end
	}
	comment := func(from, to int) {
		blank(b, from, to)
		blank(bare, from, to)
	}
	n := len(src)
	lineStart := true
	for i := 0; i < n; {
		c := src[i]
		atLineStart := lineStart
		lineStart = c == '\n' || (lineStart && (c == ' ' || c == '\t'))
		switch {
		// Comments.
		case (lang != langPython && lang != langRuby) && c == '/' && i+1 < n && src[i+1] == '/',
			(lang == langPython || lang == langRuby) && c == '#',
			lang == langPHP && c == '#' && (i+1 >= n || src[i+1] != '['):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				j = n - i
			}
			comment(i, i+j)
			i += j
		case lang != langPython && lang != langRuby && c == '/' && i+1 < n && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			end := n
			if j >= 0 {
				end = i + 2 + j + 2
			}
			comment(i, end)
			i = end
		case lang == langRuby && atLineStart && strings.HasPrefix(src[i:], "=begin"):
			j := strings.Index(src[i:], "\n=end")
			end := n
			if j >= 0 {
				end = i + j + 5
			}
			comment(i, end)
			i = end
		// Heredocs: Ruby <<~ID / <<-ID / <<ID, PHP <<<ID.
		case (lang == langRuby || lang == langPHP) && c == '<' && strings.HasPrefix(src[i:], "<<"):
			if end, val, ok := heredoc(lang, src, i); ok {
				addStr(i, end, val)
				i = end
				continue
			}
			i++
		// Triple-quoted strings (Python, Kotlin, Scala, Java text blocks, C# raw).
		case (c == '"' || c == '\'' && lang == langPython) && strings.HasPrefix(src[i:], strings.Repeat(string(c), 3)) &&
			(lang == langPython || lang == langJVM || lang == langCSharp):
			q := strings.Repeat(string(c), 3)
			j := strings.Index(src[i+3:], q)
			end := n
			if j >= 0 {
				end = i + 3 + j + 3
			}
			addStr(i, end, src[i+3:max(i+3, end-3)])
			i = end
		// C# verbatim @"..." and Rust raw r"..." / r#"..."#.
		case lang == langCSharp && c == '@' && i+1 < n && src[i+1] == '"':
			j := i + 2
			for j < n && (src[j] != '"' || j+1 < n && src[j+1] == '"') {
				if src[j] == '"' {
					j++
				}
				j++
			}
			addStr(i, min(j+1, n), strings.ReplaceAll(src[i+2:min(j, n)], `""`, `"`))
			i = j + 1
		case lang == langRust && c == 'r' && i+1 < n && (src[i+1] == '"' || src[i+1] == '#') && (i == 0 || !isIdentByte(src[i-1])):
			k := i + 1
			for k < n && src[k] == '#' {
				k++
			}
			if k >= n || src[k] != '"' {
				i++
				continue
			}
			closing := `"` + strings.Repeat("#", k-i-1)
			j := strings.Index(src[k+1:], closing)
			end := n
			if j >= 0 {
				end = k + 1 + j + len(closing)
			}
			addStr(i, end, src[k+1:max(k+1, end-len(closing))])
			i = end
		// Character literals and Rust lifetimes ('a).
		case c == '\'' && (lang == langJVM || lang == langRust || lang == langCSharp):
			switch {
			case i+2 < n && src[i+1] == '\\':
				j := strings.IndexByte(src[i+2:min(n, i+12)], '\'')
				if j >= 0 {
					i += 2 + j + 1
					continue
				}
				i++
			case i+2 < n && src[i+2] == '\'':
				i += 3
			default:
				i++
			}
		case c == '"' || c == '\'' || c == '`' && lang == langJS:
			// Python string prefixes (r, b, f, u and pairs) are identifiers
			// just before the quote; raw strings keep backslashes.
			raw := lang == langPython && i > 0 && (src[i-1] == 'r' || src[i-1] == 'R')
			j := i + 1
			var sb strings.Builder
			for j < n && src[j] != c {
				if src[j] == '\n' && c != '`' && lang != langRuby && lang != langPHP {
					break // unterminated
				}
				if src[j] == '\\' && j+1 < n && !raw {
					sb.WriteByte(src[j+1])
					j += 2
					continue
				}
				sb.WriteByte(src[j])
				j++
			}
			addStr(i, min(j+1, n), sb.String())
			i = j + 1
		default:
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth = max(depth-1, 0)
			}
			i++
		}
	}
	f.code, f.bare = string(b), string(bare)
	return f
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// joinable reports whether the text between two string literals joins
// them into one value: whitespace with + (most languages), . (PHP) or
// nothing (Python's implicit concatenation, which crosses a line break only
// inside brackets or after a backslash).
func joinable(lang srcLang, between string, inBrackets bool) bool {
	ops := 0
	for i := 0; i < len(between); i++ {
		switch c := between[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\\':
		case c == '+' || c == '.' && lang == langPHP:
			ops++
		default:
			return false
		}
	}
	if ops == 0 && lang == langPython {
		return inBrackets || !strings.Contains(between, "\n") || strings.Contains(between, "\\")
	}
	return ops == 1
}

var (
	rubyHeredocRE = regexp.MustCompile(`^<<([~-]?)(['"]?)([A-Za-z_]\w*)['"]?`)
	phpHeredocRE  = regexp.MustCompile(`^<<<[ \t]*(['"]?)([A-Za-z_]\w*)['"]?\r?\n`)
)

// heredoc reads a heredoc starting at i: its end offset and body.
func heredoc(lang srcLang, src string, i int) (int, string, bool) {
	var id string
	if lang == langRuby {
		m := rubyHeredocRE.FindStringSubmatch(src[i:])
		if m == nil {
			return 0, "", false
		}
		id = m[3]
	} else {
		m := phpHeredocRE.FindStringSubmatch(src[i:])
		if m == nil {
			return 0, "", false
		}
		id = m[2]
	}
	nl := strings.IndexByte(src[i:], '\n')
	if nl < 0 {
		return 0, "", false
	}
	bodyStart := i + nl + 1
	for p := bodyStart; p < len(src); {
		e := strings.IndexByte(src[p:], '\n')
		lineEnd := len(src)
		if e >= 0 {
			lineEnd = p + e
		}
		l := strings.TrimSpace(src[p:lineEnd])
		if l == id || strings.HasPrefix(l, id+";") || strings.HasPrefix(l, id+")") || strings.HasPrefix(l, id+",") {
			return lineEnd, src[bodyStart:p], true
		}
		p = lineEnd + 1
	}
	return 0, "", false
}

// sourceTestPath reports test code, which is not a contract.
func sourceTestPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, seg := range strings.Split(rel, "/")[:strings.Count(rel, "/")] {
		switch strings.ToLower(seg) {
		case "test", "tests", "spec", "specs", "__tests__", "testing", "integration-tests", "e2e":
			return true
		}
	}
	b := path.Base(rel)
	stem := strings.TrimSuffix(b, path.Ext(b))
	return strings.HasPrefix(b, "test_") || strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, "_spec") ||
		b == "conftest.py" || strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests") || strings.HasSuffix(stem, "IT") ||
		strings.HasSuffix(stem, "Spec") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")
}

// analyzeSources runs the pattern analyzers over source files of the
// languages above (and JS/TS idioms when lang is langJS).
func analyzeSources(repo, root string, relFiles []string, lang func(string) srcLang) []Endpoint {
	var out []Endpoint
	for _, rel := range relFiles {
		l := lang(rel)
		if l == "" || sourceTestPath(rel) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(b) > 2<<20 {
			continue
		}
		out = append(out, analyzeSource(repo, filepath.ToSlash(rel), l, string(b))...)
	}
	return out
}

// generatedNameRE matches files that protobuf and gRPC generators write.
var generatedNameRE = regexp.MustCompile(`(_pb2(_grpc)?\.pyi?|_pb2_grpc\.py|_pb\.(js|ts|d\.ts)|_grpc_pb\.(js|ts|d\.ts)|_grpc_web_pb\.(js|ts)|` +
	`_connect(web)?\.(js|ts)|\.pb\.(cc|h|go|rs)|Grpc\.(java|cs|kt)|GrpcKt\.kt|OuterClass\.java|\.g\.cs)$`)

// generatedSource reports generated code: by the generators' file names, or
// a "generated" banner in the first lines.
func generatedSource(rel, src string) bool {
	if generatedNameRE.MatchString(path.Base(rel)) {
		return true
	}
	head := src[:min(len(src), 2048)]
	return strings.Contains(head, "DO NOT EDIT") || strings.Contains(head, "@generated") ||
		strings.Contains(head, "Generated by the protocol buffer compiler") || strings.Contains(head, "Generated by the gRPC")
}

// sourceScanner emits endpoints for one lexed file.
type sourceScanner struct {
	repo, rel string
	lang      srcLang
	f         *srcFile
	out       []Endpoint
}

func (s *sourceScanner) emit(off int, e Endpoint) {
	e.Repo, e.File, e.Line = s.repo, s.rel, s.f.lineAt(off)
	s.out = append(s.out, e)
}

func analyzeSource(repo, rel string, lang srcLang, src string) []Endpoint {
	s := &sourceScanner{repo: repo, rel: rel, lang: lang, f: lexSource(lang, src)}
	if !generatedSource(rel, src) {
		// Generated stubs define every service's server base and client:
		// they are not servers or clients themselves. (Their SQL is real:
		// sqlc output is the queries the service runs.)
		s.grpc()
		s.protoImports()
	}
	s.orm()
	if lang != langJS { // JS literals come from the JS lexer (template literals)
		for _, str := range s.f.strs {
			if looksLikeSQL(str.val) {
				s.out = append(s.out, sqlEndpoints(repo, rel, str.line, str.val, Resolved, "", "query")...)
			}
		}
	}
	return s.out
}

// grpcIdiom is a regular expression whose first group is a service name
// (or a dotted path ending in one).
type grpcIdiom struct {
	re     *regexp.Regexp
	kind   Kind
	detail string
	guard  *regexp.Regexp // the code must match this (an import), when set
	strip  string         // a suffix the generator adds to the service name
}

// Guards: the imports that make a generic name a gRPC one.
var (
	pyGRPCGuard    = regexp.MustCompile(`_pb2_grpc`)
	rustGRPCGuard  = regexp.MustCompile(`\btonic\b`)
	phpGRPCGuard   = regexp.MustCompile(`\bGrpc\\`)
	jsGRPCGuard    = regexp.MustCompile(`['"](?:@grpc/grpc-js|grpc|grpc-web|@improbable-eng/grpc-web|nice-grpc(?:-web)?|@connectrpc/connect(?:-web|-node)?|@bufbuild/connect(?:-web|-node)?)['"]`)
	jsConnectGuard = regexp.MustCompile(`['"]@(?:connectrpc|bufbuild)/connect(?:-web|-node)?['"]`)
	jsNiceGuard    = regexp.MustCompile(`['"]nice-grpc(?:-web)?['"]`)
)

var grpcIdioms = map[srcLang][]grpcIdiom{
	langPython: {
		{re: regexp.MustCompile(`\badd_(\w+)Servicer_to_server\s*\(`), kind: GRPCServe, detail: "add_Servicer_to_server"},
		{re: regexp.MustCompile(`\bclass\s+\w+\s*\(\s*(?:[\w.]+\.)?(\w+)Servicer\s*\)`), kind: GRPCServe, detail: "Servicer subclass"},
		{re: regexp.MustCompile(`\b(?:\w+_pb2_grpc\.)(\w+)Stub\s*\(`), kind: GRPCCall, detail: "Stub"},
		{re: regexp.MustCompile(`(?:^|[^.\w])(\w+)Stub\s*\(`), kind: GRPCCall, detail: "Stub", guard: pyGRPCGuard},
	},
	langJVM: {
		{re: regexp.MustCompile(`\b(\w+)Grpc\s*\.\s*\w+ImplBase\b`), kind: GRPCServe, detail: "ImplBase"},
		{re: regexp.MustCompile(`\b(\w+)GrpcKt\s*\.\s*\w+CoroutineImplBase\b`), kind: GRPCServe, detail: "CoroutineImplBase"},
		{re: regexp.MustCompile(`\b(\w+)Grpc\s*\.\s*new(?:Blocking|Future|BlockingV2)?Stub\s*\(`), kind: GRPCCall, detail: "newStub"},
		{re: regexp.MustCompile(`\b(\w+)GrpcKt\s*\.\s*\w+CoroutineStub\s*\(`), kind: GRPCCall, detail: "CoroutineStub"},
	},
	langRust: {
		{re: regexp.MustCompile(`\b(\w+)Server\s*::\s*(?:new|with_interceptor|from_arc)\s*\(`), kind: GRPCServe, detail: "tonic Server", guard: rustGRPCGuard},
		{re: regexp.MustCompile(`\b(\w+)Client\s*::\s*(?:connect|new|with_interceptor|with_origin)\s*\(`), kind: GRPCCall, detail: "tonic Client", guard: rustGRPCGuard},
	},
	langCSharp: {
		{re: regexp.MustCompile(`:\s*(?:[\w.]+\.)?(\w+)\.(\w+)Base\b`), kind: GRPCServe, detail: "ServiceBase"},
		{re: regexp.MustCompile(`\bnew\s+(?:[\w.]+\.)?(\w+)\.(\w+)Client\s*\(`), kind: GRPCCall, detail: "Client"},
	},
	langRuby: {
		{re: regexp.MustCompile(`<\s*(?:[\w:]+::)?(\w+)::Service\b`), kind: GRPCServe, detail: "Service"},
		{re: regexp.MustCompile(`\b(?:[\w:]+::)?(\w+)::Stub\.new\b`), kind: GRPCCall, detail: "Stub"},
	},
	langPHP: {
		{re: regexp.MustCompile(`\bnew\s+\\?(?:[\w\\]+\\)?(\w+)Client\s*\(`), kind: GRPCCall, detail: "Client", guard: phpGRPCGuard},
	},
	langJS: {
		{re: regexp.MustCompile(`\.addService\s*\(\s*([\w$.]+)`), kind: GRPCServe, detail: "addService"},
		{re: regexp.MustCompile(`\bnew\s+([\w$.]+?Client)\s*\(`), kind: GRPCCall, detail: "grpc client", guard: jsGRPCGuard, strip: "Client"},
		{re: regexp.MustCompile(`\bnew\s+(\w+(?:\.\w+)+)\s*\(\s*[^,()]+,\s*(?:grpc\.)?credentials\b`), kind: GRPCCall, detail: "grpc client"},
		{re: regexp.MustCompile(`\bcreate(?:Promise|Callback)?Client\s*\(\s*(\w+)\s*,`), kind: GRPCCall, detail: "createClient", guard: jsConnectGuard, strip: "Definition"},
		{re: regexp.MustCompile(`\bcreate(?:Promise|Callback)?Client\s*\(\s*(\w+)\s*,`), kind: GRPCCall, detail: "createClient", guard: jsNiceGuard, strip: "Definition"},
		{re: regexp.MustCompile(`\.service\s*\(\s*(\w+)\s*,`), kind: GRPCServe, detail: "connect router", guard: jsConnectGuard},
		{re: regexp.MustCompile(`\bserver\.add\s*\(\s*(\w+)\s*,`), kind: GRPCServe, detail: "nice-grpc", guard: jsNiceGuard, strip: "Definition"},
	},
}

// Client bindings and the calls made through them, per language: group 1
// is the variable, group 2 the service.
var grpcClientBindings = map[srcLang][]*regexp.Regexp{
	langPython: {regexp.MustCompile(`(?m)^\s*((?:self\.)?\w+)\s*=\s*(?:[\w.]+\.)?(\w+)Stub\s*\(`)},
	langJVM: {
		regexp.MustCompile(`\b((?:this\.)?\w+)\s*=\s*(\w+)Grpc\s*\.\s*new\w*Stub\s*\(`),
		regexp.MustCompile(`\b((?:this\.)?\w+)\s*=\s*(\w+)GrpcKt\s*\.\s*\w+CoroutineStub\s*\(`),
	},
	langRust:   {regexp.MustCompile(`\blet\s+(?:mut\s+)?(\w+)\s*=\s*(?:[\w:]+::)?(\w+)Client\s*::\s*(?:connect|new|with_interceptor|with_origin)\s*\(`)},
	langCSharp: {regexp.MustCompile(`\b(\w+)\s*=\s*new\s+(?:[\w.]+\.)?\w+\.(\w+)Client\s*\(`)},
	langRuby:   {regexp.MustCompile(`(@?\w+)\s*=\s*(?:[\w:]+::)?(\w+)::Stub\.new\b`)},
	langPHP:    {regexp.MustCompile(`(\$\w+(?:->\w+)?)\s*=\s*new\s+\\?(?:[\w\\]+\\)?(\w+)Client\s*\(`)},
	langJS: {
		regexp.MustCompile(`\b(?:const|let|var)\s+(\w+)\s*=\s*new\s+[\w$.]*?(\w+)Client\s*\(`),
		regexp.MustCompile(`\b(this\.\w+)\s*=\s*new\s+[\w$.]*?(\w+)Client\s*\(`),
		regexp.MustCompile(`\b(?:const|let|var)\s+(\w+)\s*=\s*create(?:Promise|Callback)?Client\s*\(\s*(\w+)`),
	},
}

// grpcNotRPC are client methods that are not RPCs.
var grpcNotRPC = map[string]bool{"close": true, "Close": true, "waitForReady": true, "getChannel": true, "channel": true,
	"Dispose": true, "dispose": true, "clone": true, "to_owned": true, "max_decoding_message_size": true,
	"max_encoding_message_size": true, "send_compressed": true, "accept_compressed": true, "wait": true}

func (s *sourceScanner) grpc() {
	code := s.f.bare // patterns never match inside strings or comments
	for _, id := range grpcIdioms[s.lang] {
		if id.guard != nil && !id.guard.MatchString(s.f.code) {
			continue
		}
		for _, m := range id.re.FindAllStringSubmatchIndex(code, -1) {
			name := code[m[2]:m[3]]
			if s.lang == langCSharp && name != code[m[4]:m[5]] {
				continue // Svc.SvcBase / Svc.SvcClient name the service twice
			}
			switch {
			case id.detail == "addService":
				name = grpcJSServiceArg(name)
			case id.strip == "" && strings.Contains(name, "."):
				name = dropRoot(name)
			default:
				name = strings.TrimSuffix(name, id.strip)
			}
			if svc := jsServiceName(name); svc != "" {
				s.emit(m[0], Endpoint{Kind: id.kind, Service: svc, Ref: s.grpcRef(svc), Confidence: Partial, Detail: id.detail})
			}
		}
	}
	// Server methods (method-level serve endpoints).
	switch s.lang {
	case langPython:
		s.serverMethods(pyServicerClassRE, pyServicerMethodRE)
	case langJVM:
		s.serverMethods(jvmImplBaseRE, jvmServerMethodRE)
	}
	// Calls through client variables.
	if s.lang == langJS && !jsGRPCGuard.MatchString(s.f.code) {
		return
	}
	for _, re := range grpcClientBindings[s.lang] {
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			v, svc := m[1], jsServiceName(m[2])
			if svc == "" {
				continue
			}
			for _, c := range grpcCallRE(s.lang, v).FindAllStringSubmatchIndex(code, -1) {
				rpc := code[c[2]:c[3]]
				if grpcNotRPC[rpc] || strings.HasPrefix(rpc, "with") {
					continue
				}
				if s.lang == langCSharp {
					rpc = strings.TrimSuffix(rpc, "Async")
				}
				s.emit(c[2], Endpoint{Kind: GRPCCall, Service: svc, RPC: rpc, Ref: s.grpcRef(svc), Confidence: Partial, Detail: "stub call"})
			}
		}
	}
}

// grpcCallRE matches calls through a client variable v, allowing call
// options in between (stub.withDeadlineAfter(...).getPayment(...)). A field
// bound as this.x / self.x / @x is also matched without the receiver.
func grpcCallRE(lang srcLang, v string) *regexp.Regexp {
	sep := `\s*\.\s*`
	if lang == langPHP {
		sep = `\s*->\s*`
	}
	name, receiver := v, ""
	for _, recv := range []string{"this.", "self.", "@"} {
		if n, ok := strings.CutPrefix(v, recv); ok {
			name, receiver = n, `(?:(?:this|self)\s*\.\s*|@)?`
		}
	}
	opts := `(?:` + sep + `with\w*\((?:[^()]|\([^()]*\))*\))*`
	return regexp.MustCompile(`(?:^|[^\w$.>@])` + receiver + regexp.QuoteMeta(name) + opts + sep + `(\w+)\s*\(`)
}

var (
	pyServicerClassRE  = regexp.MustCompile(`(?m)^\s*class\s+\w+\s*\(\s*(?:[\w.]+\.)?(\w+)Servicer\s*\)`)
	pyServicerMethodRE = regexp.MustCompile(`(?m)^\s+(?:async\s+)?def\s+([A-Z]\w*)\s*\(\s*self\s*,\s*\w+\s*,\s*\w+`)
	jvmImplBaseRE      = regexp.MustCompile(`\b(\w+)Grpc\s*\.\s*\w+ImplBase\b`)
	jvmServerMethodRE  = regexp.MustCompile(`\b(\w+)\s*\(\s*[\w.<>]+\s+\w+\s*,\s*(?:io\.grpc\.stub\.)?StreamObserver\s*<`)
)

// serverMethods emits each RPC a server class implements: methods matching
// method after a class matching class, until the next class.
func (s *sourceScanner) serverMethods(class, method *regexp.Regexp) {
	code := s.f.bare
	classes := class.FindAllStringSubmatchIndex(code, -1)
	for i, c := range classes {
		end := len(code)
		if i+1 < len(classes) {
			end = classes[i+1][0]
		}
		svc := code[c[2]:c[3]]
		for _, m := range method.FindAllStringSubmatchIndex(code[c[1]:end], -1) {
			rpc := code[c[1]+m[2] : c[1]+m[3]]
			s.emit(c[1]+m[2], Endpoint{Kind: GRPCServe, Service: svc, RPC: rpc, Ref: s.grpcRef(svc), Confidence: Partial, Detail: "server method"})
		}
	}
}

// jsServiceName keeps a dotted service path (pkg.Service) or a name.
func jsServiceName(name string) string {
	name = strings.Trim(name, ".$")
	if name == "" || strings.ContainsAny(name, " ()") {
		return ""
	}
	return name
}

// grpcJSServiceArg reads the service from server.addService's first
// argument: proto-loader's pkg.v1.PaymentService.service (a dotted path),
// or the static generator's services.PaymentServiceService (the service
// name plus "Service").
func grpcJSServiceArg(arg string) string {
	if p, ok := strings.CutSuffix(arg, ".service"); ok {
		return dropRoot(p)
	}
	name := arg[strings.LastIndexByte(arg, '.')+1:]
	if strings.HasSuffix(name, "ServiceService") {
		return strings.TrimSuffix(name, "Service")
	}
	return strings.TrimSuffix(name, "Definition")
}

// dropRoot removes the variable a proto-loader path starts from
// (proto.acme.shop.v2.OrderService -> acme.shop.v2.OrderService); a short
// path (pkg.Service) is kept, and linking falls back to its last part.
func dropRoot(p string) string {
	if strings.Count(p, ".") >= 2 {
		return p[strings.IndexByte(p, '.')+1:]
	}
	return p
}

// grpcRef is the proto reference of the generated code a service's stubs
// come from, used to qualify its short name at link time: the Python _pb2
// module, the Java package of XGrpc, or the JS/TS module the service is
// imported from.
func (s *sourceScanner) grpcRef(svc string) string {
	code := s.f.code
	short := shortService(svc)
	switch s.lang {
	case langPython:
		if m := pyPB2RE.FindStringSubmatch(s.f.bare); m != nil {
			return "file:" + m[1]
		}
	case langJVM:
		re := regexp.MustCompile(`(?m)^\s*import\s+([\w.]+)\.` + regexp.QuoteMeta(short) + `Grpc(?:Kt)?\b`)
		if m := re.FindStringSubmatch(code); m != nil {
			return "java:" + m[1]
		}
	case langJS:
		re := regexp.MustCompile(`\bimport\s*\{[^}]*\b` + regexp.QuoteMeta(short) + `(?:Client|Service|Definition)?\b[^}]*\}\s*from\s*['"]([^'"]+)['"]`)
		if m := re.FindStringSubmatch(code); m != nil {
			base := strings.TrimSuffix(path.Base(m[1]), path.Ext(m[1]))
			if g := generatedPBRE.FindStringSubmatch(base); g != nil {
				return "file:" + g[1]
			}
		}
	}
	return ""
}

var (
	pyPB2RE        = regexp.MustCompile(`\b(\w+?)_pb2(?:_grpc)?\b`)
	pyImportRE     = regexp.MustCompile(`(?m)^\s*(?:from\s+[\w.]+\s+import\s+([\w, ]+)|import\s+([\w.]+)|from\s+([\w.]+)\s+import)`)
	jvmImportRE    = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+)`)
	csUsingRE      = regexp.MustCompile(`(?m)^\s*using\s+(?:static\s+)?([\w.]+)\s*;`)
	jsModuleRE     = regexp.MustCompile(`(?:\bfrom\s*|\brequire\s*\(\s*|\bimport\s*\(\s*|\bimport\s+)['"]([^'"]+)['"]`)
	generatedPBRE  = regexp.MustCompile(`^(.+?)_(?:grpc_pb|pb2_grpc|pb2|pb|connect|connectweb|grpc_web_pb)$`)
	jvmStdPrefixes = []string{"java.", "javax.", "jakarta.", "kotlin.", "kotlinx.", "scala.", "android.", "androidx.", "groovy."}
)

// protoImports emits ProtoUse endpoints for imports of generated protobuf
// code, once per reference and file.
func (s *sourceScanner) protoImports() {
	seen := map[string]bool{}
	use := func(off int, ref string, conf Confidence) {
		if !seen[ref] {
			seen[ref] = true
			s.emit(off, Endpoint{Kind: ProtoUse, Ref: ref, Confidence: conf, Detail: "import"})
		}
	}
	bare, code := s.f.bare, s.f.code
	switch s.lang {
	case langPython:
		for _, m := range pyImportRE.FindAllStringSubmatchIndex(bare, -1) {
			for _, pm := range pyPB2RE.FindAllStringSubmatch(bare[m[0]:m[1]], -1) {
				use(m[0], "file:"+pm[1], Resolved)
			}
		}
	case langJVM:
		for _, m := range jvmImportRE.FindAllStringSubmatchIndex(bare, -1) {
			imp := bare[m[2]:m[3]]
			if hasAnyPrefix(imp, jvmStdPrefixes) {
				continue
			}
			use(m[0], "java:"+javaPackageOf(imp), Resolved)
		}
	case langCSharp:
		for _, m := range csUsingRE.FindAllStringSubmatchIndex(bare, -1) {
			ns := bare[m[2]:m[3]]
			if !strings.HasPrefix(ns, "System") && !strings.HasPrefix(ns, "Microsoft") {
				use(m[0], "cs:"+ns, Resolved)
			}
		}
	case langJS: // module specifiers are strings: match the code view
		for _, m := range jsModuleRE.FindAllStringSubmatchIndex(code, -1) {
			spec := code[m[2]:m[3]]
			base := path.Base(spec)
			for _, ext := range []string{".js", ".mjs", ".cjs", ".ts"} {
				base = strings.TrimSuffix(base, ext)
			}
			if g := generatedPBRE.FindStringSubmatch(base); g != nil {
				use(m[0], "file:"+g[1], Partial)
			}
		}
	}
}

func hasAnyPrefix(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// javaPackageOf drops the class segments of an import (the first segment
// that starts upper case and everything after it).
func javaPackageOf(imp string) string {
	parts := strings.Split(imp, ".")
	for i, p := range parts {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' || p == "*" {
			return strings.Join(parts[:i], ".")
		}
	}
	return imp
}

// ormIdiom maps a table-naming pattern (group 1: table) to an endpoint.
type ormIdiom struct {
	re     *regexp.Regexp
	kind   Kind
	detail string
	guard  string
}

const tableNameRE = `([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)`

var ormIdioms = map[srcLang][]ormIdiom{
	langPython: {
		{re: regexp.MustCompile(`\b__tablename__\s*=\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "SQLAlchemy model"},
		{re: regexp.MustCompile(`\bdb_table\s*=\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "Django model"},
		{re: regexp.MustCompile(`\bTable\s*\(\s*['"]` + tableNameRE + `['"]\s*,\s*\w*meta`), kind: SQLAccess, detail: "SQLAlchemy Table"},
		{re: regexp.MustCompile(`\bop\.(?:create_table|drop_table|rename_table|add_column|drop_column|alter_column|batch_alter_table)\s*\(\s*['"]` + tableNameRE + `['"]`), kind: SQLSchema, detail: "alembic migration"},
		{re: regexp.MustCompile(`\bop\.create_index\s*\(\s*['"][^'"]*['"]\s*,\s*['"]` + tableNameRE + `['"]`), kind: SQLSchema, detail: "alembic migration"},
	},
	langRuby: {
		{re: regexp.MustCompile(`\b(?:create_table|drop_table|add_column|remove_column|rename_column|change_column|change_column_default|change_column_null|add_index|remove_index|add_reference|remove_reference|add_timestamps|change_table|rename_table|add_foreign_key|remove_foreign_key)\s*\(?\s*[:'"]` + tableNameRE), kind: SQLSchema, detail: "Rails migration", guard: "ActiveRecord::Migration"},
		{re: regexp.MustCompile(`\bself\.table_name\s*=\s*[:'"]` + tableNameRE), kind: SQLAccess, detail: "ActiveRecord model"},
	},
	langJVM: {
		{re: regexp.MustCompile(`@Table\s*\(\s*(?:name\s*=\s*)?"` + tableNameRE + `"`), kind: SQLAccess, detail: "JPA entity"},
		{re: regexp.MustCompile(`@Table\s*\([^)]*?,\s*name\s*=\s*"` + tableNameRE + `"`), kind: SQLAccess, detail: "JPA entity"},
	},
	langCSharp: {
		{re: regexp.MustCompile(`\[Table\s*\(\s*"` + tableNameRE + `"`), kind: SQLAccess, detail: "EF entity"},
		{re: regexp.MustCompile(`\.ToTable\s*\(\s*"` + tableNameRE + `"`), kind: SQLAccess, detail: "EF mapping"},
		{re: regexp.MustCompile(`migrationBuilder\s*\.\s*(?:CreateTable|DropTable|RenameTable)\s*\(\s*name\s*:\s*"` + tableNameRE + `"`), kind: SQLSchema, detail: "EF migration"},
		{re: regexp.MustCompile(`migrationBuilder\s*\.\s*\w+\s*\([^;]*?\btable\s*:\s*"` + tableNameRE + `"`), kind: SQLSchema, detail: "EF migration"},
	},
	langRust: {
		{re: regexp.MustCompile(`\btable!\s*\{\s*(?:\w+\.)?([A-Za-z_]\w*)\s*\(`), kind: SQLAccess, detail: "diesel schema"},
	},
	langPHP: {
		{re: regexp.MustCompile(`\bSchema::(?:create|table|drop|dropIfExists|rename)\s*\(\s*['"]` + tableNameRE + `['"]`), kind: SQLSchema, detail: "Laravel migration"},
		{re: regexp.MustCompile(`\bprotected\s+\$table\s*=\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "Eloquent model"},
		{re: regexp.MustCompile(`\bDB::table\s*\(\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "Laravel query"},
	},
	langJS: {
		{re: regexp.MustCompile(`@Entity\s*\(\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "TypeORM entity"},
		{re: regexp.MustCompile(`@Entity\s*\(\s*\{[^}]*\bname\s*:\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "TypeORM entity"},
		{re: regexp.MustCompile(`\btableName\s*:\s*['"]` + tableNameRE + `['"]`), kind: SQLAccess, detail: "Sequelize model"},
		{re: regexp.MustCompile(`\bknex\s*\(\s*['"]` + tableNameRE + `['"]\s*\)`), kind: SQLAccess, detail: "knex"},
		{re: regexp.MustCompile(`\.(?:from|into|table)\s*\(\s*['"]` + tableNameRE + `['"]\s*\)`), kind: SQLAccess, detail: "knex", guard: "knex"},
		{re: regexp.MustCompile(`\.schema\s*\.\s*(?:createTable|createTableIfNotExists|alterTable|table|dropTable|dropTableIfExists|renameTable)\s*\(\s*['"]` + tableNameRE + `['"]`), kind: SQLSchema, detail: "knex migration"},
		{re: regexp.MustCompile(`\bprisma\s*\.\s*(\w+)\s*\.\s*(?:findMany|findUnique|findUniqueOrThrow|findFirst|findFirstOrThrow|create|createMany|update|updateMany|upsert|delete|deleteMany|count|aggregate|groupBy)\s*\(`), kind: SQLAccess, detail: "prisma client"},
	},
}

func (s *sourceScanner) orm() {
	code := s.f.code
	for _, id := range ormIdioms[s.lang] {
		if id.guard != "" && !strings.Contains(code, id.guard) {
			continue
		}
		for _, m := range id.re.FindAllStringSubmatchIndex(code, -1) {
			if s.insideString(m[0]) {
				continue // a docstring or message mentioning an ORM call
			}
			table := strings.ToLower(code[m[2]:m[3]])
			if id.detail == "prisma client" {
				table = prismaPrefix + table // resolved against the schema in Scan
			}
			s.emit(m[0], Endpoint{Kind: id.kind, Table: table, Confidence: Resolved, Detail: id.detail})
		}
	}
}

// insideString reports whether offset off is inside a string literal: the
// views differ there (bare has the contents blanked).
func (s *sourceScanner) insideString(off int) bool {
	return off < len(s.f.bare) && s.f.bare[off] == ' ' && s.f.code[off] != ' '
}

// prismaPrefix marks a Prisma client model accessor until it is resolved
// to its table (resolvePrisma).
const prismaPrefix = "prisma:"
