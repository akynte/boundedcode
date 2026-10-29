package verify

import (
	"sort"
	"strings"
)

// lexMutants finds conservative mutation points in a file of C-like syntax
// (JavaScript, TypeScript, Java, Kotlin, Rust, C, C++, C#, Swift, Dart,
// Scala, Groovy, PHP) with a lexer that skips strings, characters,
// template literals and comments. It changes only:
//
//   - relational, equality and logical operators written between spaces
//     (" < ", " && "): generics and templates (List<String>) are not
//     written so, and an operator of the same class keeps the types;
//   - true and false;
//   - an integer right after a relational operator (a bound).
//
// When the lexer cannot follow the file (an unterminated string or
// comment), it finds nothing rather than risk mutating inside one.
func lexMutants(rel string, src []byte, lines map[int]bool, hashComments, rust bool) []Mutant {
	code := codeMask(src, hashComments, rust)
	if code == nil {
		return nil
	}
	ops := []struct{ from, to, kind string }{
		{" === ", " !== ", "relational"}, {" !== ", " === ", "relational"},
		{" <= ", " > ", "relational"}, {" >= ", " < ", "relational"}, {" == ", " != ", "relational"}, {" != ", " == ", "relational"},
		{" < ", " >= ", "relational"}, {" > ", " <= ", "relational"}, {" && ", " || ", "logical"}, {" || ", " && ", "logical"},
	}
	shift := map[string]string{"<": "<=", "<=": "<", ">": ">=", ">=": ">"}
	var out []Mutant
	line := 1
	taken := map[int]bool{}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			line++
			continue
		}
		if !lines[line] || !code[i] {
			continue
		}
		for _, o := range ops {
			if i+len(o.from) <= len(src) && string(src[i:i+len(o.from)]) == o.from && allCode(code, i, i+len(o.from)) && !taken[i+1] {
				// " = <" or "=> " are not this operator: the characters
				// around it must not extend it.
				orig := strings.TrimSpace(o.from)
				out = append(out, Mutant{File: rel, Line: line, Start: i + 1, End: i + len(o.from) - 1, Orig: orig, Repl: strings.TrimSpace(o.to), Operator: o.kind})
				if to, ok := shift[orig]; ok {
					out = append(out, Mutant{File: rel, Line: line, Start: i + 1, End: i + len(o.from) - 1, Orig: orig, Repl: to, Operator: "boundary"})
				}
				taken[i+1] = true
				if o.kind == "relational" {
					if n, end := intAfter(src, i+len(o.from)); end > 0 && allCode(code, i+len(o.from), end) {
						if v, ok := intLit(n); ok {
							out = append(out, Mutant{File: rel, Line: line, Start: i + len(o.from), End: end, Orig: n, Repl: itoa(v + 1), Operator: "boundary"})
						}
					}
				}
				break
			}
		}
		// return N: the value returned changes.
		if i+7 <= len(src) && string(src[i:i+7]) == "return " && allCode(code, i, i+7) && (i == 0 || !identByte(src[i-1])) {
			if n, end := intAfter(src, i+7); end > 0 && allCode(code, i+7, end) {
				if v, ok := intLit(n); ok {
					out = append(out, Mutant{File: rel, Line: line, Start: i + 7, End: end, Orig: n, Repl: itoa(v + 1), Operator: "return"})
				}
			}
		}
		for _, w := range []struct{ from, to string }{{"true", "false"}, {"false", "true"}} {
			if i+len(w.from) <= len(src) && string(src[i:i+len(w.from)]) == w.from && allCode(code, i, i+len(w.from)) &&
				(i == 0 || !identByte(src[i-1])) && (i+len(w.from) == len(src) || !identByte(src[i+len(w.from)])) {
				out = append(out, Mutant{File: rel, Line: line, Start: i, End: i + len(w.from), Orig: w.from, Repl: w.to, Operator: "constant"})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

func identByte(b byte) bool {
	return b == '_' || b == '$' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func allCode(code []bool, from, to int) bool {
	for i := from; i < to && i < len(code); i++ {
		if !code[i] {
			return false
		}
	}
	return true
}

// intAfter returns a decimal integer starting at i and where it ends (0
// when there is none, or it is part of a larger token: 1.5, 10px, x1).
func intAfter(src []byte, i int) (string, int) {
	j := i
	for j < len(src) && src[j] >= '0' && src[j] <= '9' {
		j++
	}
	if j == i || j < len(src) && (identByte(src[j]) || src[j] == '.') {
		return "", 0
	}
	return string(src[i:j]), j
}

// codeMask marks the bytes of src that are code (not in a string,
// character, template literal or comment); nil when the lexer loses track.
func codeMask(src []byte, hashComments, rust bool) []bool {
	code := make([]bool, len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/', hashComments && c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(string(src[i+2:]), "*/")
			if end < 0 {
				return nil
			}
			i += end + 4
		case c == '"' && i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"':
			end := strings.Index(string(src[i+3:]), `"""`)
			if end < 0 {
				return nil
			}
			i += end + 6
		case c == '"' || c == '`' || c == '\'' && (!rust || !lifetime(src, i)):
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '\n' && c != '`' {
					return nil // an unterminated string on this line: not followed
				}
				j++
			}
			if j >= len(src) {
				return nil
			}
			i = j + 1
		default:
			code[i] = true
			i++
		}
	}
	return code
}

// lifetime reports a Rust lifetime ('a, 'static) rather than a character
// literal ('a').
func lifetime(src []byte, i int) bool {
	j := i + 1
	for j < len(src) && identByte(src[j]) {
		j++
	}
	return j > i+1 && (j >= len(src) || src[j] != '\'')
}
