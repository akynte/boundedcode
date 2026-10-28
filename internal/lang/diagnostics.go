package lang

import (
	"regexp"
	"strconv"
	"strings"
)

// Diagnostic is one located problem in tool output.
type Diagnostic struct {
	File    string
	Line    int
	Col     int
	Message string
}

func (d Diagnostic) String() string {
	s := d.File + ":" + strconv.Itoa(d.Line)
	if d.Col > 0 {
		s += ":" + strconv.Itoa(d.Col)
	}
	if d.Message != "" {
		s += ": " + d.Message
	}
	return s
}

// genericDiagnostics are the formats most compilers, interpreters, test
// runners and linters share: file:line[:col]: message (gcc, clang, Go,
// Swift, Ruby, shellcheck -f gcc, ESLint unix, mypy, pytest), and
// file(line,col): message (tsc, MSBuild).
var genericDiagnostics = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(?P<file>[^\s:()"'<>]+\.[A-Za-z0-9+]{1,8}):(?P<line>\d+)(?::(?P<col>\d+))?:?\s*(?P<msg>.*)$`),
	regexp.MustCompile(`^\s*(?P<file>[^\s()"'<>]+\.[A-Za-z0-9+]{1,8})\((?P<line>\d+),(?P<col>\d+)\):\s*(?P<msg>.*)$`),
}

// ParseDiagnostics finds the located problems in a tool's output: the
// generic formats and every registered language's own. keep filters the
// files (nil keeps all); duplicates are dropped; at most max are returned.
func ParseDiagnostics(out string, keep func(file string) bool, max int) []Diagnostic {
	pats := append([]*regexp.Regexp{}, genericDiagnostics...)
	for _, l := range All() {
		pats = append(pats, l.Diagnostics...)
	}
	seen := map[string]bool{}
	var res []Diagnostic
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		for _, re := range pats {
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			d := Diagnostic{File: group(re, m, "file"), Message: strings.TrimSpace(group(re, m, "msg"))}
			d.Line, _ = strconv.Atoi(group(re, m, "line"))
			d.Col, _ = strconv.Atoi(group(re, m, "col"))
			if d.File == "" || d.Line == 0 || strings.Contains(d.File, "://") {
				continue
			}
			// rustc and Python put the message on another line.
			if d.Message == "" && i > 0 {
				prev := strings.TrimSpace(lines[i-1])
				if strings.HasPrefix(prev, "error") || strings.HasPrefix(prev, "warning") {
					d.Message = prev
				}
			}
			if keep != nil && !keep(d.File) {
				break
			}
			key := d.File + ":" + strconv.Itoa(d.Line)
			if !seen[key] {
				seen[key] = true
				res = append(res, d)
			}
			break
		}
		if max > 0 && len(res) >= max {
			break
		}
	}
	return res
}

func group(re *regexp.Regexp, m []string, name string) string {
	if i := re.SubexpIndex(name); i > 0 && i < len(m) {
		return m[i]
	}
	return ""
}
