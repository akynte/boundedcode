package telemetry

import (
	"regexp"
	"strings"
	"sync"
)

// secretPatterns match common credential shapes. Redaction is best effort and
// defence in depth: the primary control is never giving secrets to the agent.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\b(sk-(?:proj-|ant-)?[A-Za-z0-9_\-]{20,})`),
	regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{30,})`),
	regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{30,})`),
	regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`),
	regexp.MustCompile(`\b(xox[abprs]-[A-Za-z0-9\-]{10,})`),
	regexp.MustCompile(`\b(AIza[0-9A-Za-z_\-]{35})\b`),
	regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|secret|password|passwd|token)\s*[:=]\s*["']?)[^\s"']{8,}`),
}

var (
	knownMu sync.RWMutex
	known   []string
)

// AddSecret registers a credential the process holds (a provider API key),
// so Redact removes it wherever it appears, whatever its shape. Values
// shorter than 8 characters are ignored (too likely to match plain text).
func AddSecret(v string) {
	v = strings.TrimSpace(v)
	if len(v) < 8 {
		return
	}
	knownMu.Lock()
	defer knownMu.Unlock()
	for _, k := range known {
		if k == v {
			return
		}
	}
	known = append(known, v)
}

// Redact replaces likely secrets in s with a placeholder.
func Redact(s string) string {
	knownMu.RLock()
	for _, k := range known {
		s = strings.ReplaceAll(s, k, "[REDACTED]")
	}
	knownMu.RUnlock()
	for _, re := range secretPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			sub := re.FindStringSubmatch(m)
			// Keep a non-secret prefix group (e.g. "Bearer ", "api_key=") when present.
			if len(sub) > 1 && sub[1] != "" && sub[1] != m && len(sub[1]) < len(m) && !re.MatchString(sub[1]) {
				return sub[1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return s
}
