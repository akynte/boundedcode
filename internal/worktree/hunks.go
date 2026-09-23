package worktree

import (
	"strconv"
	"strings"
)

// LineRange is an inclusive range of line numbers in the base version of a
// file.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Overlaps reports whether the range overlaps [start, end].
func (r LineRange) Overlaps(start, end int) bool { return r.Start <= end && start <= r.End }

// OldRanges reads a unified diff and returns, per base path, the base lines
// the diff touches. A pure insertion is recorded as the line it follows, since
// that is the declaration it lands in. Files the diff creates have no base
// lines and are omitted: nothing in the base describes them.
func OldRanges(patch string) map[string][]LineRange {
	before, _ := Ranges(patch)
	return before
}

// Ranges reads a unified diff and returns the lines it touches on each side:
// per base path in the base, and per result path in the result. A pure
// insertion on the base side, or a pure deletion on the result side, is
// recorded as the line it follows. A created file has no base side and a
// deleted file no result side.
func Ranges(patch string) (before, after map[string][]LineRange) {
	before, after = map[string][]LineRange{}, map[string][]LineRange{}
	var oldPath, newPath string
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, newPath = "", ""
		case strings.HasPrefix(line, "--- "):
			oldPath = sidePath(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			newPath = sidePath(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "@@ "):
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			if r, ok := parseSide(fields[1], "-"); ok && oldPath != "" {
				before[oldPath] = append(before[oldPath], r)
			}
			if r, ok := parseSide(fields[2], "+"); ok && newPath != "" {
				after[newPath] = append(after[newPath], r)
			}
		}
	}
	return before, after
}

func sidePath(name, prefix string) string {
	if name == "/dev/null" {
		return ""
	}
	return unquote(strings.TrimPrefix(name, prefix), prefix)
}

// parseSide reads one side of a hunk header, "-start,count" or "+start,count".
func parseSide(spec, sign string) (LineRange, bool) {
	if !strings.HasPrefix(spec, sign) {
		return LineRange{}, false
	}
	startText, countText, hasCount := strings.Cut(strings.TrimPrefix(spec, sign), ",")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return LineRange{}, false
	}
	count := 1
	if hasCount {
		if count, err = strconv.Atoi(countText); err != nil {
			return LineRange{}, false
		}
	}
	if count == 0 {
		// Nothing on this side: attribute the change to the line it follows,
		// or to the first line when it is at the top of the file.
		if start < 1 {
			start = 1
		}
		return LineRange{Start: start, End: start}, true
	}
	return LineRange{Start: start, End: start + count - 1}, true
}

// unquote handles git's C-style quoting of unusual path names. Only the
// quotes are removed; escapes inside are rare enough in source trees that a
// path containing them simply matches nothing.
func unquote(name, prefix string) string {
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		name = strings.TrimPrefix(name[1:len(name)-1], prefix)
	}
	return name
}
