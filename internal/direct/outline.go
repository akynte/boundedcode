package direct

import (
	"fmt"
	"github.com/akynte/boundedcode/internal/lang"
	"path/filepath"
	"regexp"
	"strings"
)

// outlineLines is the length from which read shows an outline instead of
// the whole file when no range was asked for.
const outlineLines = 400

// maxOutlineEntries bounds an outline.
const maxOutlineEntries = 150

// outline returns a line-numbered list of a long file's definitions, so
// the model can pick the region to read instead of loading the whole
// file into its context. A language's own outliner or definition patterns
// (internal/lang) are used, and for any other text a generic definition
// pattern. ok is false when nothing useful was found.
func outline(rel string, lines []string) (string, bool) {
	var entries []int
	l := lang.ForPath(rel, "")
	if l != nil && l.Outline != nil {
		entries = l.Outline(strings.Join(lines, "\n"))
	}
	if len(entries) == 0 && l != nil && len(l.Definitions) > 0 {
		for i, line := range lines {
			if _, ok := l.Definition(line); ok {
				entries = append(entries, i+1)
			}
		}
	}
	if len(entries) == 0 {
		entries = patternOutline(rel, lines)
	}
	if len(entries) < 3 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d lines; this is its outline (definitions with their line numbers). Read a region with offset and limit.\n", rel, len(lines))
	for i, ln := range entries {
		if i == maxOutlineEntries {
			fmt.Fprintf(&b, "… and %d more\n", len(entries)-i)
			break
		}
		text := strings.TrimSpace(lines[ln-1])
		if len(text) > 160 {
			text = text[:160] + "…"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", ln, text)
	}
	return b.String(), true
}

var (
	// definitionLine matches a definition at the start of a line, with
	// the usual modifiers before the keyword.
	definitionLine = regexp.MustCompile(`^(?:(?:export|default|async|static|public|private|protected|abstract|final|declare|override|unsafe|extern|inline|virtual|const|pub(?:\([^)]*\))?)\s+)*(?:function\*?|class|def|fn|impl|struct|enum|interface|type|trait|module|func|object|record|namespace|package|sub|proc|macro_rules!|defn|defun|defmodule|defmacro|defp|protocol|extension|actor|union|service|rpc|message|contract|library|describe|CREATE\s+(?:OR\s+REPLACE\s+)?(?:TABLE|VIEW|FUNCTION|PROCEDURE|INDEX))\b`)
	// memberLine matches an indented method or function (one level).
	memberLine = regexp.MustCompile(`^(?: {1,4}|\t)(?:(?:async|static|public|private|protected|abstract|override|pub(?:\([^)]*\))?)\s+)*(?:def|fn|function\*?|func)\b`)
	// headingLine matches a Markdown heading or a top-level YAML/TOML key.
	headingLine = regexp.MustCompile(`^(?:#{1,3} \S|\[[^\]]+\]\s*$|[A-Za-z_][\w.-]*:(?:\s|$))`)
)

func patternOutline(rel string, lines []string) []int {
	ext := strings.ToLower(filepath.Ext(rel))
	prose := ext == ".md" || ext == ".markdown" || ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".ini" || ext == ".cfg"
	var out []int
	for i, l := range lines {
		switch {
		case prose:
			if headingLine.MatchString(l) {
				out = append(out, i+1)
			}
		case definitionLine.MatchString(l), memberLine.MatchString(l):
			out = append(out, i+1)
		}
	}
	return out
}
