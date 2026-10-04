package xservice

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// jsTokKind is a JavaScript/TypeScript token class.
type jsTokKind int

const (
	jsIdent jsTokKind = iota
	jsString
	jsTemplate
	jsNumber
	jsPunct
)

// jsTok is one token. Templates keep their literal chunks and the token
// lists of each ${...} substitution.
type jsTok struct {
	kind   jsTokKind
	text   string // identifier/punct text, or decoded string value
	line   int
	chunks []string  // template: literal parts (len = len(subs)+1)
	subs   [][]jsTok // template: substitution token lists
}

// jsLex tokenizes JS/TS source. It is tolerant: on malformed input it skips
// bytes rather than failing, because analysis is best effort.
func jsLex(src string) []jsTok {
	l := &jsLexer{src: src, line: 1}
	return l.run(false)
}

type jsLexer struct {
	src  string
	pos  int
	line int
}

// regexAllowedAfter: a '/' starts a regex literal after these tokens.
func regexAllowedAfter(prev *jsTok) bool {
	if prev == nil {
		return true
	}
	switch prev.kind {
	case jsIdent:
		switch prev.text {
		case "return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await":
			return true
		}
		return false
	case jsString, jsTemplate, jsNumber:
		return false
	}
	return prev.text != ")" && prev.text != "]" && prev.text != "}"
}

// run lexes until EOF, or until an unmatched '}' when inTemplate is true
// (end of a ${...} substitution).
func (l *jsLexer) run(inTemplate bool) []jsTok {
	var toks []jsTok
	depth := 0
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case strings.HasPrefix(l.src[l.pos:], "//"):
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case strings.HasPrefix(l.src[l.pos:], "/*"):
			end := strings.Index(l.src[l.pos+2:], "*/")
			if end < 0 {
				end = len(l.src) - l.pos - 2
			}
			l.line += strings.Count(l.src[l.pos:l.pos+2+end], "\n")
			l.pos += end + 4
		case c == '"' || c == '\'':
			line := l.line
			toks = append(toks, jsTok{kind: jsString, text: l.quoted(c), line: line})
		case c == '`':
			toks = append(toks, l.template())
		case c == '/' && regexAllowedAfter(last(toks)):
			l.regex()
		case isIdentStart(c):
			start := l.pos
			for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
				l.pos++
			}
			toks = append(toks, jsTok{kind: jsIdent, text: l.src[start:l.pos], line: l.line})
		case c >= '0' && c <= '9':
			start := l.pos
			for l.pos < len(l.src) && (isIdentPart(l.src[l.pos]) || l.src[l.pos] == '.') {
				l.pos++
			}
			toks = append(toks, jsTok{kind: jsNumber, text: l.src[start:l.pos], line: l.line})
		default:
			if inTemplate {
				switch c {
				case '{':
					depth++
				case '}':
					if depth == 0 {
						l.pos++
						return toks
					}
					depth--
				}
			}
			p := l.punct()
			toks = append(toks, jsTok{kind: jsPunct, text: p, line: l.line})
		}
	}
	return toks
}

func last(t []jsTok) *jsTok {
	if len(t) == 0 {
		return nil
	}
	return &t[len(t)-1]
}

var puncts = []string{"?.", "??", "=>", "...", "===", "!==", "==", "!=", "<=", ">=", "&&", "||", "++", "--", "+=", "-="}

func (l *jsLexer) punct() string {
	for _, p := range puncts {
		if strings.HasPrefix(l.src[l.pos:], p) {
			l.pos += len(p)
			return p
		}
	}
	_, size := utf8.DecodeRuneInString(l.src[l.pos:])
	p := l.src[l.pos : l.pos+size]
	l.pos += size
	return p
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || c == '@' || unicode.IsLetter(rune(c)) || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

// quoted reads a '...' or "..." string and returns its decoded value.
func (l *jsLexer) quoted(q byte) string {
	l.pos++
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == q:
			l.pos++
			return b.String()
		case c == '\\' && l.pos+1 < len(l.src):
			b.WriteString(unescape(l.src[l.pos+1]))
			l.pos += 2
		case c == '\n': // unterminated
			return b.String()
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
	return b.String()
}

func unescape(c byte) string {
	switch c {
	case 'n':
		return "\n"
	case 't':
		return "\t"
	default:
		return string(c)
	}
}

func (l *jsLexer) template() jsTok {
	tok := jsTok{kind: jsTemplate, line: l.line}
	l.pos++
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '`':
			l.pos++
			tok.chunks = append(tok.chunks, b.String())
			return tok
		case c == '\\' && l.pos+1 < len(l.src):
			b.WriteString(unescape(l.src[l.pos+1]))
			l.pos += 2
		case c == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{':
			tok.chunks = append(tok.chunks, b.String())
			b.Reset()
			l.pos += 2
			tok.subs = append(tok.subs, l.run(true))
		default:
			if c == '\n' {
				l.line++
			}
			b.WriteByte(c)
			l.pos++
		}
	}
	tok.chunks = append(tok.chunks, b.String())
	return tok
}

func (l *jsLexer) regex() {
	l.pos++
	inClass := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\\':
			l.pos += 2
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			l.pos++
			for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) { // flags
				l.pos++
			}
			return
		case c == '\n':
			return
		}
		l.pos++
	}
}
