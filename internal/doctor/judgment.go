package doctor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/judgment"
)

// checkJudgment reports whether questions are being answered by a service
// outside this machine.
//
// It reads judgment.yaml from disk rather than inspecting a constructed judge,
// because the state most worth reporting is the one that prevents startup: a
// configuration that errors out has no judge to inspect.
//
// The level when it is live is Warn, not OK, for the same reason a wildcard
// allowlist rule is: it is not a fault, and it is the thing an operator should
// look at twice. Everything else on this report says what does not leave the
// machine, and one line saying what does belongs among them.
func checkJudgment(ctx context.Context, cfg *config.Config, configDir string) Check {
	const name = "judgment (external)"

	if configDir == "" {
		return Check{Name: name, Level: Skipped, Detail: "no data directory open"}
	}
	jcfg, err := judgment.Load(configDir)
	if err != nil {
		return Check{Name: name, Level: Fail, Detail: err.Error(),
			Fix: fmt.Sprintf("Fix %s, or delete it: an absent file means judgments are off.",
				judgment.ConfigFile)}
	}
	// Validate before the disabled short-circuit. A malformed tier such as
	// sites.review_rubric: routng is a broken configuration whether or not
	// the integration is currently on — an operator staging a promotion
	// with enabled left false must still see the typo, not an OK that only
	// reflects the disabled state. Validate itself only checks endpoint,
	// model, redact and credential shape when Enabled is true, so this
	// still reports the "off, and nothing wrong with it" case correctly.
	if err := jcfg.Validate(); err != nil {
		return Check{Name: name, Level: Fail, Detail: err.Error(),
			Fix: fmt.Sprintf("Fix %s, or set enabled to false in it.", judgment.ConfigFile)}
	}

	// Which decisions are load-bearing under this configuration. Not a
	// pass/fail input any more — the plane is required either way — but the
	// operator should be told which sites can stop a task, because that set
	// is derived from `redact` rather than written down anywhere.
	routing := requiredRoutingSites(jcfg)

	if !jcfg.Enabled {
		return Check{Name: name, Level: Fail,
			Detail: "disabled: the decision plane is a required runtime component, so " +
				"`bcode task run` will refuse to start. No question about your code leaves " +
				"this machine in this state, and no task runs either",
			Fix: fmt.Sprintf("Run scripts/configure-judgment.sh, or set enabled: true in %s and "+
				"export the credential. Every other command works without it, including this one.",
				judgment.ConfigFile)}
	}

	detail := fmt.Sprintf("enabled: %s at %s, redact %s, min_confidence %.2f",
		jcfg.Model, hostOf(jcfg.Endpoint), jcfg.Redact, jcfg.MinConfidence)

	// A missing key is a fault, not a warning. The plane is required, so a
	// judge that cannot authenticate is a system that cannot run a task.
	if !jcfg.HasCredential() {
		return Check{Name: name, Level: Fail,
			Detail: detail + fmt.Sprintf("; %s is unset, so no judgment can be requested and "+
				"`bcode task run` will refuse to start", jcfg.APIKeyEnv),
			Fix: fmt.Sprintf("Run scripts/configure-judgment.sh to store the key in an owner-only "+
				"shell environment file, or export %s in this shell. Never put it in judgment.yaml.",
				jcfg.APIKeyEnv)}
	}

	// A site promoted to routing whose subject the configured redact mode
	// forbids can never answer. Reported here because nothing at runtime can
	// resolve it — every task would reach the site, be refused locally, and
	// stop. judgment.Preflight makes the same check against a live judge.
	if contradictions := redactContradictions(jcfg); len(contradictions) > 0 {
		return Check{Name: name, Level: Fail,
			Detail: detail + "; " + strings.Join(contradictions, "; "),
			Fix: fmt.Sprintf("Raise redact in %s to what those sites need, or return them to "+
				"the logged tier. As configured they are promised authority over decisions "+
				"they may never make.", judgment.ConfigFile)}
	}

	if !jcfg.PinnedModel() {
		return Check{Name: name, Level: Warn,
			Detail: detail + "; the model is an alias, not a pinned version",
			Fix: "The model id is part of the judgment cache key, so an alias that moves " +
				"underneath makes cached answers and `bcode eval` runs incomparable while the " +
				"configuration looks unchanged. Pin a dated snapshot."}
	}
	if jcfg.Redact == judgment.RedactOutput {
		return Check{Name: name, Level: Warn,
			Detail: detail + "; signatures, excerpts and diffs from your repository are sent " +
				"to that host, and so is normalized output from your own tools and tests, " +
				"on top of the metadata strict mode already sends",
			Fix: "Set redact: strict to send only paths, symbol names and statuses. " +
				"Judgments still work; they see less."}
	}
	if jcfg.Redact.AtLeast(judgment.RedactRepoText) {
		return Check{Name: name, Level: Warn,
			Detail: detail + "; signatures and excerpts from your repository are sent to " +
				"that host, on top of the metadata strict mode already sends",
			Fix: "Set redact: strict to send only paths, symbol names and statuses. " +
				"Judgments still work; they see less."}
	}
	if err := reachable(ctx, jcfg.Endpoint); err != nil {
		fix := "Check the network. Every routing-capable site is at the logged tier, so no " +
			"task blocks on this; it is paying for a feature that is not running."
		if len(routing) > 0 {
			fix = fmt.Sprintf("Check the network. %s are configured at routing, so a task "+
				"reaching one of them will block rather than assume an answer.",
				strings.Join(routing, ", "))
		}
		return Check{Name: name, Level: Fail,
			Detail: detail + "; the endpoint is not reachable", Fix: fix}
	}
	routingSuffix := "; no site can currently stop a task"
	if len(routing) > 0 {
		routingSuffix = fmt.Sprintf("; %d site(s) can stop a task when they cannot get an "+
			"answer: %s", len(routing), strings.Join(routing, ", "))
	}
	return Check{Name: name, Level: Warn,
		Detail: detail + "; the objective, paths, symbol names, kinds and line ranges are " +
			"sent to that host — no source" + promotedSitesSuffix(jcfg) + routingSuffix,
		Fix: "Nothing to fix. Listed so that what leaves this machine is on the same report " +
			"as what does not."}
}

// requiredRoutingSites names the sites an operator has configured at a tier
// where their answer actually changes what a task does.
//
// judgment.Required is the same test the call sites use, so this report and
// the runtime cannot disagree about which decisions are load-bearing.
func requiredRoutingSites(jcfg judgment.Config) []string {
	var out []string
	for _, site := range judgment.KnownSites() {
		if judgment.Required(site.Name, jcfg.Tier(site.Name)) {
			out = append(out, site.Name)
		}
	}
	sort.Strings(out)
	return out
}

// redactContradictions names sites configured to route on a decision the
// configured redaction mode forbids them from ever making.
func redactContradictions(jcfg judgment.Config) []string {
	var out []string
	for _, site := range judgment.KnownSites() {
		if !judgment.Required(site.Name, jcfg.Tier(site.Name)) {
			continue
		}
		if !jcfg.Redact.AtLeast(site.Redaction) {
			out = append(out, fmt.Sprintf(
				"%s is at routing but needs redact %s, and the configured mode is %s",
				site.Name, site.Redaction, jcfg.Redact))
		}
	}
	sort.Strings(out)
	return out
}

// promotedSitesSuffix names every judgment site an operator has promoted
// past the default logged tier, so a live judge's report says not just that
// it is live but what it has actually been given authority to change. `bcode
// judgment sites` lists every site this build defines, promoted or not.
func promotedSitesSuffix(jcfg judgment.Config) string {
	var promoted []string
	for _, site := range judgment.KnownSites() {
		if tier := jcfg.Tier(site.Name); tier != judgment.TierLogged {
			promoted = append(promoted, site.Name+":"+string(tier))
		}
	}
	if len(promoted) == 0 {
		return "; every judgment site is at the default logged tier (see `bcode judgment sites`)"
	}
	sort.Strings(promoted)
	return "; promoted sites: " + strings.Join(promoted, ", ")
}

// reachable opens a TCP connection and closes it. It deliberately does not
// make a request: a reachability check that spent a judgment would bill the
// operator for running `bcode doctor`.
func reachable(ctx context.Context, endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	host := u.Host
	if u.Port() == "" {
		port := "443"
		if u.Scheme == "http" {
			port = "80"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	return conn.Close()
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}

// configDirOf reports where judgment.yaml would live, or "" when no data
// directory is open.
func configDirOf(opts Options) string {
	if opts.Root == nil {
		return ""
	}
	dir := opts.Root.Layout().ConfigDir()
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}
