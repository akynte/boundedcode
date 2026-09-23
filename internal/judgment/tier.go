package judgment

import "sort"

// Authority tiers. See docs/explanation/judgments.md.
//
// A judgment site is a named question this package answers on behalf of some
// caller — "does this diff hunk weaken a test", "is this waiver reason
// plausible". Two sites already exist in effect (retrieval reranking,
// localization ranking) and predate this file; they are unaffected by it,
// because they were promoted to their current behaviour by hand rather than
// through this mechanism. Every site added after this file exists starts
// here, at Logged, and earns more only by configuration a person wrote.
//
// The tiers are ordered by how much control flow a site's answer may change:
//
//   - Logged: the site runs, its answers are journalled by the recorder
//     internal/supervisor already wires up, and nothing else reads them. This
//     is where every new site begins. It exists so that a site can accumulate
//     the paired (prediction, outcome) evidence design.md §7 and §8 describe
//     before it is trusted with anything.
//   - Ordering: in addition, the site's answers may reorder, select, or add a
//     visible concern to what a later reader — a reviewer, a gate, a plan's
//     own risks list — is shown (design.md's ORDER, SELECT and FLAG
//     effects). They may never remove evidence a deterministic check already
//     produced, and they may never change a pass/fail verdict or any phase's
//     control flow: at this tier a site can only change what gets read, not
//     what gets decided or done next.
//   - Routing: in addition, the site's answers may taint a verification
//     result so the gate must show it, end an attempt before its budget, or
//     route an attempt back to an earlier phase (design.md's TAINT, STOP-
//     EARLIER and ROUTE effects) — the effects that change control flow
//     rather than only what is shown. Never acceptance, never a widened
//     write, never a shortened verification run — see design.md R1.
//
// Promotion is a line in judgment.yaml, reviewed by a person like any other
// configuration change. Nothing in this package promotes a site on its own.
type Tier string

const (
	// TierLogged is the default for a site nothing has been told about.
	TierLogged Tier = "logged"
	// TierOrdering permits ORDER and SELECT effects.
	TierOrdering Tier = "ordering"
	// TierRouting permits FLAG, TAINT, ROUTE and STOP-EARLIER effects, on top
	// of ordering and logging.
	TierRouting Tier = "routing"
)

// rank orders the tiers so Permits can compare them. An unknown tier ranks
// below Logged rather than panicking, because the place this matters most —
// a call site deciding whether it may taint a result — is exactly the place
// that must fail toward less authority on a value it does not recognise.
func (t Tier) rank() int {
	switch t {
	case TierLogged:
		return 1
	case TierOrdering:
		return 2
	case TierRouting:
		return 3
	default:
		return 0
	}
}

// Valid reports whether t is one of the three defined tiers.
func (t Tier) Valid() bool {
	return t == TierLogged || t == TierOrdering || t == TierRouting
}

// Permits reports whether a site configured at tier t may use an effect that
// requires at least min. TierLogged.Permits(TierOrdering) is false: a site
// nobody has promoted may not reorder anything, only log that it would have.
func (t Tier) Permits(min Tier) bool {
	return t.rank() >= min.rank()
}

// Tier reports the configured tier for a named site, or the site's default
// when judgment.yaml says nothing about it.
func (c Config) Tier(site string) Tier {
	if t, ok := c.Sites[site]; ok && t.Valid() {
		return t
	}
	return DefaultTierUnder(site, c.Redact)
}

// DefaultTierUnder is what a site runs at when nothing in judgment.yaml names
// it, given the configured redaction mode.
//
// A site that can change control flow runs at TierRouting by default. That is
// what makes the decision plane mandatory in effect and not merely in
// principle: with those sites at TierLogged the system would be asked, would
// record the answer, and would behave identically to one with no Jev at all —
// the silent degradation this architecture exists to remove.
//
// The redaction mode is part of the question because six of the eleven sites
// read repository source or verification output, and under a mode that forbids
// their subject they cannot answer at all. Defaulting such a site to routing
// would ship a configuration that contradicts itself: every task would reach
// it, be refused locally, and stop. So a site is promoted by default only as
// far as the operator's own privacy setting lets it function — under the
// shipped `strict` that is the two sites whose questions are metadata-only,
// and raising `redact` brings the rest in.
//
// The alternative was to raise the default redaction mode so that all six
// could route, and that is the wrong trade to make silently: it would send
// source excerpts, diffs and tool output to a third party because of a
// decision about decision-plane authority, which is not a privacy decision an
// operator made.
//
// Everything else defaults to TierLogged. An ordering-capable site changes
// what a reader is shown, and there is a real deterministic answer to keep
// when it cannot be asked, so nothing is lost by leaving it observational
// until its findings have been read.
//
// An operator still turns any of this down per site in judgment.yaml. What
// changed is which way the default points, not who is allowed to decide.
func DefaultTierUnder(site string, mode RedactMode) Tier {
	info, ok := Site(site)
	if !ok || info.MaxEffect != TierRouting {
		return TierLogged
	}
	if !mode.Valid() || !mode.AtLeast(info.Redaction) {
		return TierLogged
	}
	return TierRouting
}

// SiteTier reads the tier a Judge was configured with for a named site.
//
// It is a package-level function rather than a method on Judge because Judge
// is the narrow interface every call site programs against — Name, Available,
// Ask — and a tier is a property of how a *particular backend* was
// configured, not something every judge (Off, Fake, a future backend) must
// answer the same way. A Judge that does not declare one gets TierLogged,
// which is what Off effectively is anyway: unavailable judges never reach a
// call site that checks a tier, because AskAll returns before any effect
// would apply.
func SiteTier(j Judge, site string) Tier {
	if t, ok := j.(interface{ Tier(site string) Tier }); ok {
		if tier := t.Tier(site); tier.Valid() {
			return tier
		}
	}
	return TierLogged
}

// RedactModeOf reads the redaction mode a Judge was configured with. Several
// call sites need to build state before they know whether repository source
// may go in it at all; this is the shared form of the type assertion that
// internal/retrieval's reranker already used privately.
func RedactModeOf(j Judge) RedactMode {
	if r, ok := j.(interface{ Redact() RedactMode }); ok {
		return r.Redact()
	}
	return RedactStrict
}

// Tier implements the optional interface SiteTier reads.
func (c *Client) Tier(site string) Tier { return c.cfg.Tier(site) }

// SiteInfo describes one judgment site for `bcode judgment sites`, for
// `bcode doctor`, for the calibration report, and — through MaxEffect — for the
// configuration check that stops judgment.yaml granting a site an authority
// its own code never implements.
//
// Sites are defined in the packages that use them — internal/workflow,
// internal/task, internal/retrieval — which already import this package;
// this package cannot import them back without a cycle. Each site's own file
// registers itself here from an init(), the same pattern database/sql and
// image.RegisterFormat use, so this package can enumerate every site that
// exists without needing to know about any of them by name.
type SiteInfo struct {
	// Name is the key judgment.yaml's `sites` map uses.
	Name string
	// Description is one line for a person reading `bcode judgment sites`.
	Description string
	// Mechanism names the design.md mechanism this site implements — "M4",
	// "M8" — so the conformance document and the CLI agree on which
	// mechanism a site belongs to without a second table to keep in sync.
	// Several sites may share one mechanism: M8 has three.
	Mechanism string
	// Version is the site's question version. It changes when the
	// proposition, criteria, or composition thresholds change materially
	// enough that answers before and after are not the same measurement.
	// Calibration partitions on it: pooling two versions would average two
	// different questions into a number describing neither.
	Version string
	// MaxEffect is the highest tier this site's own code can actually act
	// at. A site whose call site only ever checks Permits(TierOrdering)
	// declares TierOrdering here, and Config.Validate refuses a
	// judgment.yaml that grants it TierRouting — otherwise configuration
	// would appear to hand out an authority nothing implements, and an
	// operator would believe they had turned something on.
	MaxEffect Tier
	// Redaction is the least permissive redact mode under which this site
	// can run at all. A site that sends diff hunks declares RedactRepoText;
	// under a stricter mode it never attempts a call. Reported so an
	// operator can see why a site is silent without reading its source.
	Redaction RedactMode
	// Outcome describes what a prediction from this site is later paired
	// with, in the words the calibration report prints. Empty means this
	// site records no predictions, or records them with no outcome
	// available — `bcode judgment sites` says so rather than leaving a column
	// blank and letting a reader guess.
	Outcome string
	// EffectThreshold is the probability at or above which this site's
	// *caller* acts on a finding, over and above whatever threshold the site
	// applied when producing it. Zero means the caller acts on any finding
	// the site reported.
	//
	// It is declared here so `bcode judgment replay` can say what a recorded
	// prediction would do under a different configured tier without
	// re-implementing each call site's arithmetic — and so that the number a
	// reader sees in a replay report is the same constant the call site
	// uses, rather than a second copy that could drift.
	EffectThreshold float64
}

// Paired reports whether this site's predictions are resolved against a
// later observed outcome.
func (s SiteInfo) Paired() bool { return s.Outcome != "" }

var registeredSites = map[string]SiteInfo{}

// RegisterSite records one judgment site. Called from the defining package's
// init(). A name registered twice panics at program start, the same way a
// duplicate sql.Register does — the earliest possible point to catch a
// copy-pasted site constant. An incomplete registration panics for the same
// reason: a site with no declared MaxEffect would silently be treated as
// unknown by the configuration check that exists to catch over-granting.
func RegisterSite(info SiteInfo) {
	if _, dup := registeredSites[info.Name]; dup {
		panic("judgment: site " + info.Name + " registered twice")
	}
	switch {
	case info.Name == "":
		panic("judgment: a site must have a name")
	case info.Mechanism == "":
		panic("judgment: site " + info.Name + " must name its design mechanism")
	case info.Version == "":
		panic("judgment: site " + info.Name + " must declare a question version")
	case !info.MaxEffect.Valid():
		panic("judgment: site " + info.Name + " must declare a valid MaxEffect tier")
	case !info.Redaction.Valid():
		panic("judgment: site " + info.Name + " must declare the redaction mode it needs")
	}
	registeredSites[info.Name] = info
}

// KnownSites lists every registered site, sorted by name.
func KnownSites() []SiteInfo {
	out := make([]SiteInfo, 0, len(registeredSites))
	for _, info := range registeredSites {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Site returns one registered site by name.
//
// The second return is false for a name nothing registered, which is not
// necessarily an error: a test binary that does not link internal/workflow
// sees none of its sites, and a judgment.yaml written for a newer build may
// name one this binary does not have. Callers decide what that means; the
// configuration check treats an unknown site as unconstrained rather than
// refusing to start.
func Site(name string) (SiteInfo, bool) {
	info, ok := registeredSites[name]
	return info, ok
}

// SiteVersion reports a registered site's question version, or "" when the
// site is not registered in this binary. Recorded beside every prediction so
// calibration never pools answers to two different questions.
func SiteVersion(name string) string {
	if info, ok := registeredSites[name]; ok {
		return info.Version
	}
	return ""
}
