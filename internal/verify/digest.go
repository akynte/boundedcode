package verify

import (
	"regexp"
	"strings"
)

// Test runners often print the failure before a flood of logs, and stage
// output is kept as a tail, so the failing assertion could be lost: on caddy
// the retry pack showed only "FAIL caddytest/integration" and a 55-name skip
// list, and the agent spent a whole attempt without knowing what failed. The
// digest keeps the lines that say what failed, from the whole output.

// failureLineRE matches lines that identify a failure in common runners:
// Go (--- FAIL, file.go:N:, panic, build errors), TypeScript (error TSnnnn),
// vitest/jest/mocha (FAIL, ✗/×, AssertionError, Expected/Received) and
// generic "error:" lines.
var failureLineRE = regexp.MustCompile(`(?i)(^\s*--- FAIL|^FAIL\b|^\s*(FAIL|✗|×|✘)\s|panic:|^\s*[\w./-]+\.(go|ts|tsx|js|mjs|cjs|jsx|vue|py|rs):\d+(:\d+)?:|error TS\d+|assertionerror|^\s*(expected|received|actual)\b|^\s*\d+\) |\berror:|^\s*\[build failed\]|undefined: |cannot use |^\s*Error: )`)

// digestContext is how many lines after a failure line are kept.
const digestContext = 2

// FailureDigest returns the failure-identifying lines of a stage's whole
// output (with a little context), bounded to max bytes; "" if none.
func FailureDigest(out string, max int) string {
	lines := strings.Split(out, "\n")
	keep := make([]bool, len(lines))
	any := false
	for i, l := range lines {
		if len(l) > 600 || !failureLineRE.MatchString(l) {
			continue
		}
		any = true
		for j := i; j < len(lines) && j <= i+digestContext; j++ {
			keep[j] = true
		}
	}
	if !any {
		return ""
	}
	var b strings.Builder
	prev := -2
	for i, l := range lines {
		if !keep[i] || len(l) > 600 {
			continue
		}
		if prev >= 0 && i > prev+1 {
			b.WriteString("…\n")
		}
		if b.Len()+len(l)+1 > max {
			b.WriteString("[failure digest truncated]\n")
			break
		}
		b.WriteString(l)
		b.WriteByte('\n')
		prev = i
	}
	return strings.TrimRight(b.String(), "\n")
}
