package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/policy"
)

// Modes a message can be handled in. The router picks one automatically;
// the user never has to choose.
//
//   - ModeChat: a question or discussion. The agent reads and searches;
//     the first attempt to change a file switches the turn to ModeCode.
//   - ModeCode: a change made directly in the checkout.
//   - ModeVerify: a change that deserves isolation and evidence: a bounded
//     task (worktree, sandbox, deterministic checks, behavioural evidence).
const (
	ModeChat   = "chat"
	ModeCode   = "code"
	ModeVerify = "verify"
)

// Decision is the router's choice for one message.
type Decision struct {
	Mode       string `json:"mode"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"` // high | low (low: the model was consulted or the rules were unsure)
	// Fallback is set when the chosen mode was downgraded because the
	// environment cannot run the one the message deserved.
	Fallback string `json:"fallback,omitempty"`
}

// Capabilities describe what the environment can run.
type Capabilities struct {
	// BoundedAvailable: a bounded task can run here (git repository,
	// set-up complete, sandbox present). Reason says why not.
	BoundedAvailable bool
	BoundedReason    string
}

// RiskRules name what makes a change deserve verification. Keywords are
// matched in the request; path patterns in the files a change touches.
type RiskRules struct {
	// Keywords from the configuration's escalation lists (architectural
	// risk, high-risk review), lower-case.
	Keywords []string
	// MaxDirectFiles: a request that names more files than this, or an
	// edit set larger than this, deserves verification (0 = 8).
	MaxDirectFiles int
}

var (
	questionLead  = regexp.MustCompile(`^(what|why|how|where|which|who|when|is|are|does|do|can|could|should|explain|describe|show|tell|summari[sz]e|list|find|walk me|help me understand|compare)\b`)
	changeVerb    = regexp.MustCompile(`\b(fix|add|implement|create|write|change|update|refactor|rename|remove|delete|move|extract|replace|convert|migrate|upgrade|introduce|make|wire|hook up|clean up|cleanup|optimi[sz]e|improve|rewrite|generate|bump|support|handle|apply|revert|split|merge|inline|port)\b`)
	verifyAsk     = regexp.MustCompile(`\b(verif(y|ied|ication)|with (a )?tests?|test-backed|evidence|prove|in a sandbox|isolated|bounded mode|safely|carefully|production|prod\b|release)\b`)
	runVerb       = regexp.MustCompile(`\b(run|execute|build|compile|lint|check|test)\b`)
	codeFence     = regexp.MustCompile("```")
	filePathLike  = regexp.MustCompile(`[\w./-]+\.(go|ts|tsx|js|jsx|py|rs|java|kt|rb|php|c|cc|cpp|h|hpp|cs|swift|sql|proto|ya?ml|json|toml|md|sh)\b`)
	riskyPathRe   = regexp.MustCompile(`(?i)(^|/)(auth|authn|authz|oauth|sso|login|session|password|secret|crypto|payment|billing|checkout|ledger|account|wallet|iam|rbac|permission|migrations?|schema|proto|openapi|swagger|contracts?|api/v\d|terraform|helm|k8s|kubernetes|deploy|infra)(/|[._-]|$)`)
	riskyFileRe   = regexp.MustCompile(`(?i)\.(proto|sql|tf|tfvars)$|(^|/)(openapi|swagger)[^/]*\.(ya?ml|json)$|(^|/)(migrations?|db/migrate)/`)
	politeLead    = regexp.MustCompile(`^(can|could|would|will|please)\b`)
	multiRepoHint = regexp.MustCompile(`\b(across|both|all|every|each)\s+(services?|repos?|repositories|packages|modules|microservices)\b|\b(service|repo|repository)s?\s+and\s+(service|repo|repository)`)
)

func (r RiskRules) maxFiles() int {
	if r.MaxDirectFiles > 0 {
		return r.MaxDirectFiles
	}
	return 8
}

// RiskyPath reports whether a path a change touches deserves verification
// on its own: protected repository configuration, contracts, migrations,
// infrastructure, or security- and money-related code.
func RiskyPath(rel string) (string, bool) {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	switch {
	case policy.IsProtectedPath(rel):
		return "protected path " + rel, true
	case riskyFileRe.MatchString(rel):
		return "contract, migration or infrastructure file " + rel, true
	case riskyPathRe.MatchString(rel):
		return "security- or money-related code " + rel, true
	}
	return "", false
}

// Route classifies a request with deterministic rules. It returns a low
// confidence when the rules cannot tell; RouteWithModel then asks the
// model. Capabilities downgrade ModeVerify to ModeCode with a Fallback.
func Route(request string, rules RiskRules, caps Capabilities) Decision {
	d := route(request, rules)
	return applyCaps(d, caps)
}

func applyCaps(d Decision, caps Capabilities) Decision {
	if d.Mode == ModeVerify && !caps.BoundedAvailable {
		d.Fallback = ModeVerify
		d.Mode = ModeCode
		why := caps.BoundedReason
		if why == "" {
			why = "bounded mode is not available here"
		}
		d.Reason += "; " + why + ", so the change is made directly"
	}
	return d
}

func route(request string, rules RiskRules) Decision {
	t := strings.ToLower(strings.TrimSpace(request))
	if t == "" {
		return Decision{Mode: ModeChat, Reason: "empty message", Confidence: "high"}
	}
	first := firstSentence(t)
	hasChange := changeVerb.MatchString(t)
	isQuestion := questionLead.MatchString(first) || strings.HasSuffix(first, "?")
	polite := politeLead.MatchString(first) // "can you add…" is a request, not a question
	// A genuine question is answered, whatever it is about: nothing changes
	// until the answer needs an edit, and risky paths are checked then.
	if isQuestion && !polite {
		if hasChange {
			return Decision{Mode: ModeChat, Reason: "a question about how to change something", Confidence: "low"}
		}
		return Decision{Mode: ModeChat, Reason: "a question", Confidence: "high"}
	}
	// Risk keywords and explicit verification requests.
	for _, k := range rules.Keywords {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" && strings.Contains(t, k) && hasChange {
			return Decision{Mode: ModeVerify, Reason: "the change concerns " + k, Confidence: "high"}
		}
	}
	if hasChange && verifyAsk.MatchString(t) {
		return Decision{Mode: ModeVerify, Reason: "the request asks for a verified or test-backed change", Confidence: "high"}
	}
	if hasChange && multiRepoHint.MatchString(t) {
		return Decision{Mode: ModeVerify, Reason: "the change spans several services or repositories", Confidence: "high"}
	}
	files := filePathLike.FindAllString(t, -1)
	for _, f := range files {
		if why, ok := RiskyPath(f); ok && hasChange {
			return Decision{Mode: ModeVerify, Reason: "it touches a " + why, Confidence: "high"}
		}
	}
	if hasChange && len(uniq(files)) > rules.maxFiles() {
		return Decision{Mode: ModeVerify, Reason: fmt.Sprintf("the change names %d files", len(uniq(files))), Confidence: "high"}
	}
	switch {
	case hasChange:
		return Decision{Mode: ModeCode, Reason: "a request for a change", Confidence: "high"}
	case runVerb.MatchString(first) && !codeFence.MatchString(t):
		return Decision{Mode: ModeChat, Reason: "a request to run or check something", Confidence: "high"}
	}
	return Decision{Mode: ModeChat, Reason: "no change requested", Confidence: "low"}
}

func firstSentence(t string) string {
	if i := strings.IndexAny(t, ".?!\n"); i > 0 {
		return strings.TrimSpace(t[:i+1])
	}
	return t
}

func uniq(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Completer is what RouteWithModel needs from a gateway.
type Completer interface {
	Forward(ctx context.Context, path string, body map[string]any) (int, any, error)
}

// RouteWithModel applies the rules and, when they are unsure, asks the
// model for a one-word classification. A failed or odd answer keeps the
// rules' choice.
func RouteWithModel(ctx context.Context, c Completer, request string, rules RiskRules, caps Capabilities) Decision {
	d := route(request, rules)
	if d.Confidence == "high" || c == nil {
		return applyCaps(d, caps)
	}
	body := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You route requests to a coding agent. Answer with one JSON object and nothing else: {\"mode\":\"chat\"|\"code\"|\"verify\",\"reason\":\"...\"}. chat: the user wants an answer, explanation, review or a command run, with no file changed. code: the user wants files changed and the change is small and low-risk. verify: the user wants a change that touches authentication, payments, data migrations, shared API contracts, infrastructure, or several components, or asks for tests or proof."},
			{"role": "user", "content": request},
		},
		"max_tokens":  80,
		"temperature": 0,
	}
	status, resp, err := c.Forward(ctx, "/v1/chat/completions", body)
	if err != nil || status != http.StatusOK {
		return applyCaps(d, caps)
	}
	raw, _ := json.Marshal(resp)
	m, err := parseCompletion(raw)
	if err != nil {
		return applyCaps(d, caps)
	}
	var out struct {
		Mode, Reason string
	}
	txt := m.Content
	if i := strings.Index(txt, "{"); i >= 0 {
		if j := strings.LastIndex(txt, "}"); j > i {
			txt = txt[i : j+1]
		}
	}
	if json.Unmarshal([]byte(txt), &out) != nil {
		return applyCaps(d, caps)
	}
	switch strings.ToLower(out.Mode) {
	case ModeChat, ModeCode, ModeVerify:
		return applyCaps(Decision{Mode: strings.ToLower(out.Mode), Reason: strings.TrimSpace(out.Reason), Confidence: "low"}, caps)
	}
	return applyCaps(d, caps)
}
