package judgeval

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/akynte/boundedcode/internal/judgment"
)

// Promotion policy — design instructions §8, §16, §28.
//
// The rule this file exists to make structurally true, not merely stated:
// "no Site may receive authority because its design sounds useful." A
// Threshold this package has not been given a real value for reports itself
// as unconfigured, and Eligible (eligibility.go) treats an unconfigured
// threshold as a blocking reason, never as "pass" and never as some
// plausible-looking default this package invented. Every numeric floor
// below therefore starts at RequiresSelection: true and Value: 0 — not
// because 0 is a sensible floor, but because no evidence-based number has
// been chosen yet, and shipping one that merely looks reasonable would be
// exactly the "authority because the design sounds useful" failure this
// framework exists to prevent.

// EffectClass groups a site by the highest-risk kind of effect its own code
// implements, because a routing-capable site's mistakes change control flow
// and an ordering-capable site's mistakes only change what a person reads
// first — design instructions §16 and §28 ask for materially different
// promotion criteria between the two, not one shared bar.
type EffectClass string

const (
	EffectOrdering EffectClass = "ordering"
	EffectRouting  EffectClass = "routing"
)

// classOf derives a site's EffectClass from its registered MaxEffect. A site
// whose MaxEffect is only TierLogged (none currently registered, but the
// registry allows it) is treated as EffectOrdering, since a Threshold that
// exists only to be more permissive than "ordering" would have nothing to
// gate.
func classOf(info judgment.SiteInfo) EffectClass {
	if info.MaxEffect == judgment.TierRouting {
		return EffectRouting
	}
	return EffectOrdering
}

// Threshold is one numeric promotion requirement.
type Threshold struct {
	// Value is meaningless while RequiresSelection is true; a caller must
	// check RequiresSelection before reading Value, not the other way
	// around — see Threshold.Configured.
	Value float64 `yaml:"value" json:"value"`
	// RequiresSelection is true until an operator has written a real value
	// for this threshold into policy.yaml, chosen from actual held-out
	// evidence on this domain rather than guessed. This package never
	// flips it to false on its own.
	RequiresSelection bool   `yaml:"requires_selection" json:"requires_selection"`
	Note              string `yaml:"note,omitempty" json:"note,omitempty"`
}

// Configured reports whether this threshold has an operator-selected value.
func (t Threshold) Configured() bool { return !t.RequiresSelection }

func unselected(note string) Threshold {
	return Threshold{RequiresSelection: true, Note: note}
}

// SitePolicy is one site's promotion requirements — design instruction §16's
// "encode promotion policy per site or per effect class, keep it reviewable
// in code/config."
type SitePolicy struct {
	Site        string      `yaml:"site" json:"site"`
	EffectClass EffectClass `yaml:"effect_class" json:"effect_class"`
	// MinReportableSamples is ledger.MinCalibrationSample restated here for
	// visibility in a policy file; it is a reporting floor, never sufficient
	// evidence for promotion on its own — design instruction §8's explicit
	// distinction between minimum_reportable_samples and
	// minimum_promotion_samples.
	MinReportableSamples int `yaml:"min_reportable_samples" json:"min_reportable_samples"`
	// MinPromotionSamples must be operator-set and, per §8, larger than
	// MinReportableSamples and site-specific.
	MinPromotionSamples Threshold `yaml:"min_promotion_samples" json:"min_promotion_samples"`
	// MinSkillLowerBound is the floor the *lower* bound of the bootstrap CI
	// on Brier skill (BrierResult.SkillCILow) must clear — never the point
	// estimate (§9's conservative criterion).
	MinSkillLowerBound Threshold `yaml:"min_skill_lower_bound" json:"min_skill_lower_bound"`
	// MaxFlipRate bounds StabilityResult.FlipRate under repeat evaluation
	// (§17). Only meaningful for sites this package can run repeat trials
	// against.
	MaxFlipRate Threshold `yaml:"max_flip_rate" json:"max_flip_rate"`
	// MaxDistractorDelta bounds how much a probability may move under an
	// irrelevant-distractor or permutation perturbation (§17, called out
	// specifically for M7 in §28: "do not allow a site to become eligible
	// for ordering if ranking materially changes under irrelevant
	// permutation/distractors").
	MaxDistractorDelta Threshold `yaml:"max_distractor_delta" json:"max_distractor_delta"`
	// MaxLatencyP95Seconds bounds p95 latency (§18); highest-stakes for M2,
	// which sits in the execution path.
	MaxLatencyP95Seconds Threshold `yaml:"max_latency_p95_seconds" json:"max_latency_p95_seconds"`
	// RequireBaselineBeaten requires the Jev arm to beat the deterministic
	// baseline arm where one exists (§5, §11's M6 note: "this comparison is
	// especially important because deterministic baselines already exist").
	RequireBaselineBeaten bool `yaml:"require_baseline_beaten" json:"require_baseline_beaten"`
}

// DefaultPolicy builds an unconfigured policy for a registered site: correct
// EffectClass and reporting floor, every promotion threshold marked
// RequiresSelection. It is a starting document for `bcode judgment policy
// init` to write, never a policy this package would act on as-is.
func DefaultPolicy(info judgment.SiteInfo) SitePolicy {
	class := classOf(info)
	p := SitePolicy{
		Site:                  info.Name,
		EffectClass:           class,
		MinReportableSamples:  20, // ledger.MinCalibrationSample; a reporting floor only.
		MinPromotionSamples:   unselected("must be > min_reportable_samples and chosen from real held-out evidence, not guessed"),
		MinSkillLowerBound:    unselected("the lower 95% bootstrap CI bound on Brier skill this site must clear; 0 is the theoretical floor (beats guessing the base rate) but the operating threshold is a domain choice"),
		MaxFlipRate:           unselected("repeat-evaluation label flip rate this site must stay under"),
		MaxDistractorDelta:    unselected("max probability movement under an irrelevant distractor or permutation this site must stay under"),
		MaxLatencyP95Seconds:  unselected("p95 request latency this site must stay under; tightest for a site that sits in the execution path"),
		RequireBaselineBeaten: class == EffectRouting,
	}
	return p
}

// PolicyFile is the on-disk shape of evals/judgment/policy.yaml: every
// registered site's policy, keyed by name, plus a schema version so a
// breaking change to SitePolicy's shape can be detected on load rather than
// silently misread.
type PolicyFile struct {
	SchemaVersion int                   `yaml:"schema_version"`
	Sites         map[string]SitePolicy `yaml:"sites"`
}

const PolicySchemaVersion = 1

// LoadPolicy reads evals/judgment/policy.yaml. A missing file is not an
// error: DefaultPolicy(site) is returned for every site, all unconfigured,
// which is the honest state of "no promotion policy has been written yet."
func LoadPolicy(path string) (PolicyFile, error) {
	pf := PolicyFile{SchemaVersion: PolicySchemaVersion, Sites: map[string]SitePolicy{}}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return pf, nil
	}
	if err != nil {
		return pf, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return pf, fmt.Errorf("judgeval: parsing policy file: %w", err)
	}
	if pf.SchemaVersion != PolicySchemaVersion {
		return pf, fmt.Errorf("judgeval: policy file schema_version %d, this build reads %d",
			pf.SchemaVersion, PolicySchemaVersion)
	}
	return pf, nil
}

// For returns the policy for site: whatever the file configured, or a fresh
// DefaultPolicy (unconfigured) if the file says nothing about it.
func (pf PolicyFile) For(info judgment.SiteInfo) SitePolicy {
	if p, ok := pf.Sites[info.Name]; ok {
		return p
	}
	return DefaultPolicy(info)
}

// WriteDefaultPolicy writes an unconfigured policy file covering every
// currently registered site — the seed an operator edits in, never a
// document this package expects to be trusted unedited.
func WriteDefaultPolicy(path string) error {
	pf := PolicyFile{SchemaVersion: PolicySchemaVersion, Sites: map[string]SitePolicy{}}
	for _, s := range judgment.KnownSites() {
		pf.Sites[s.Name] = DefaultPolicy(s)
	}
	body, err := yaml.Marshal(pf)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
