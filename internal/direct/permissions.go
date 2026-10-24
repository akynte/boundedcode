package direct

import (
	"path"
	"strings"
)

// Rules are permission rules in the form TOOL(PATTERN): "bash(go test *)",
// "edit(src/**)", "web_fetch(docs.example.com)", "read(.env*)". A bare
// tool name matches every call of that tool. Deny rules win; allow rules
// skip the approval prompt (the deterministic policy still applies).
type Rules struct {
	Allow []string
	Deny  []string
}

// Allowed reports whether spec (TOOL(ARG)) matches an allow rule.
func (r Rules) Allowed(spec string) bool { return matchRule(r.Allow, spec) }

// Denied reports whether spec matches a deny rule, with the rule.
func (r Rules) Denied(spec string) (string, bool) {
	for _, rule := range r.Deny {
		if matchRule([]string{rule}, spec) {
			return "denied by the permission rule " + rule, true
		}
	}
	return "", false
}

func matchRule(rules []string, spec string) bool {
	tool, arg := splitSpec(spec)
	for _, rule := range rules {
		rt, rp := splitSpec(strings.TrimSpace(rule))
		if !strings.EqualFold(rt, tool) {
			continue
		}
		if rp == "" || rp == "*" {
			return true
		}
		if matchPattern(rp, arg) {
			return true
		}
	}
	return false
}

func splitSpec(spec string) (tool, arg string) {
	i := strings.IndexByte(spec, '(')
	if i < 0 {
		return strings.ToLower(spec), ""
	}
	return strings.ToLower(spec[:i]), strings.TrimSuffix(spec[i+1:], ")")
}

// matchPattern matches a rule pattern against an argument: a prefix
// pattern ending in " *" or ":*" matches by prefix (commands), otherwise a
// path glob ("**" means any depth) or an exact string.
func matchPattern(p, arg string) bool {
	if p == arg {
		return true
	}
	if strings.HasSuffix(p, " *") {
		pre := strings.TrimSuffix(p, " *")
		return arg == pre || strings.HasPrefix(arg, pre+" ")
	}
	if strings.HasSuffix(p, "*") && !strings.ContainsAny(strings.TrimSuffix(p, "*"), "*?[") {
		return strings.HasPrefix(arg, strings.TrimSuffix(p, "*"))
	}
	if strings.Contains(p, "/") || strings.ContainsAny(p, "*?[") {
		if matchGlob(p, strings.ReplaceAll(arg, "\\", "/")) {
			return true
		}
		ok, _ := path.Match(p, arg)
		return ok
	}
	return false
}
