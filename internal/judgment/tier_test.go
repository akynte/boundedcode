package judgment

import "testing"

func TestTierPermits(t *testing.T) {
	cases := []struct {
		have, want Tier
		permits    bool
	}{
		{TierLogged, TierLogged, true},
		{TierLogged, TierOrdering, false},
		{TierLogged, TierRouting, false},
		{TierOrdering, TierLogged, true},
		{TierOrdering, TierOrdering, true},
		{TierOrdering, TierRouting, false},
		{TierRouting, TierLogged, true},
		{TierRouting, TierOrdering, true},
		{TierRouting, TierRouting, true},
	}
	for _, c := range cases {
		if got := c.have.Permits(c.want); got != c.permits {
			t.Errorf("Tier(%q).Permits(%q) = %v, want %v", c.have, c.want, got, c.permits)
		}
	}
}

func TestTierUnknownValueRanksBelowLogged(t *testing.T) {
	var bogus Tier = "aggressive"
	if bogus.Valid() {
		t.Fatalf("an invented tier must not be Valid")
	}
	if bogus.Permits(TierLogged) {
		t.Fatalf("an unrecognised tier must not even permit Logged-level effects")
	}
}

func TestConfigTierDefaultsToLogged(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Tier("verification_integrity"); got != TierLogged {
		t.Fatalf("an unconfigured site must read as TierLogged, got %q", got)
	}
	cfg.Sites = map[string]Tier{"verification_integrity": TierRouting}
	if got := cfg.Tier("verification_integrity"); got != TierRouting {
		t.Fatalf("a configured site must report the tier it was given, got %q", got)
	}
	if got := cfg.Tier("some_other_site"); got != TierLogged {
		t.Fatalf("a site with no entry must still default to TierLogged, got %q", got)
	}
}

func TestConfigValidateRejectsAnUnknownTier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Model = "jev-1.13.0"
	cfg.Sites = map[string]Tier{"verification_integrity": "aggressive"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate accepted an unknown tier")
	}
}

func TestConfigValidateAcceptsTheThreeDefinedTiers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Model = "jev-1.13.0"
	cfg.Sites = map[string]Tier{
		"a": TierLogged, "b": TierOrdering, "c": TierRouting,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected a config with only defined tiers: %v", err)
	}
}

func TestSiteTierReadsAFakesConfiguredTier(t *testing.T) {
	f := &Fake{Tiers: map[string]Tier{"verification_integrity": TierOrdering}}
	if got := SiteTier(f, "verification_integrity"); got != TierOrdering {
		t.Fatalf("SiteTier = %q, want ordering", got)
	}
	if got := SiteTier(f, "unconfigured_site"); got != TierLogged {
		t.Fatalf("SiteTier for an unconfigured site = %q, want logged", got)
	}
}

func TestSiteTierOnAJudgeWithNoTierMethodIsLogged(t *testing.T) {
	// Off() declares no Tier method; SiteTier must not panic and must return
	// the conservative default.
	if got := SiteTier(Off(), "verification_integrity"); got != TierLogged {
		t.Fatalf("SiteTier on Off() = %q, want logged", got)
	}
}

func TestRedactModeOfDefaultsToStrict(t *testing.T) {
	if got := RedactModeOf(Off()); got != RedactStrict {
		t.Fatalf("RedactModeOf(Off()) = %q, want strict", got)
	}
	f := &Fake{RedactMode: RedactRepoText}
	if got := RedactModeOf(f); got != RedactRepoText {
		t.Fatalf("RedactModeOf(fake) = %q, want repo_text", got)
	}
}

// --- default tiers: routing-capable sites are promoted by default ----------

func init() {
	RegisterSite(SiteInfo{
		Name: "default_tier_strict_routing", Description: "routing site, metadata only",
		Mechanism: "TEST", Version: "1", MaxEffect: TierRouting, Redaction: RedactStrict,
	})
	RegisterSite(SiteInfo{
		Name: "default_tier_output_routing", Description: "routing site needing output",
		Mechanism: "TEST", Version: "1", MaxEffect: TierRouting, Redaction: RedactOutput,
	})
	RegisterSite(SiteInfo{
		Name: "default_tier_ordering", Description: "ordering site",
		Mechanism: "TEST", Version: "1", MaxEffect: TierOrdering, Redaction: RedactStrict,
	})
}

// The point of the default: a site that can change control flow does so
// without anyone writing a line of configuration. Otherwise the decision plane
// is required to exist and then permitted to matter to nothing.
func TestARoutingCapableSiteRoutesByDefault(t *testing.T) {
	cfg := Config{Redact: RedactStrict}
	if got := cfg.Tier("default_tier_strict_routing"); got != TierRouting {
		t.Errorf("tier = %q, want %q", got, TierRouting)
	}
}

// An ordering-capable site still defaults to logged: there is a real
// deterministic answer to keep when it cannot be asked.
func TestAnOrderingCapableSiteStaysLoggedByDefault(t *testing.T) {
	cfg := Config{Redact: RedactOutput}
	if got := cfg.Tier("default_tier_ordering"); got != TierLogged {
		t.Errorf("tier = %q, want %q", got, TierLogged)
	}
}

// A site whose subject the configured mode forbids must not be promoted by
// default, or the shipped configuration would contradict itself and refuse
// every task that reached it.
func TestADefaultPromotionNeverExceedsTheRedactionMode(t *testing.T) {
	strict := Config{Redact: RedactStrict}
	if got := strict.Tier("default_tier_output_routing"); got != TierLogged {
		t.Errorf("under strict, tier = %q, want %q", got, TierLogged)
	}
	loose := Config{Redact: RedactOutput}
	if got := loose.Tier("default_tier_output_routing"); got != TierRouting {
		t.Errorf("under output, tier = %q, want %q", got, TierRouting)
	}
}

// An operator's explicit entry still wins, in both directions.
func TestAnExplicitTierOverridesTheDefault(t *testing.T) {
	down := Config{Redact: RedactStrict,
		Sites: map[string]Tier{"default_tier_strict_routing": TierLogged}}
	if got := down.Tier("default_tier_strict_routing"); got != TierLogged {
		t.Errorf("an operator turning a site down must win: got %q", got)
	}
	up := Config{Redact: RedactStrict,
		Sites: map[string]Tier{"default_tier_ordering": TierOrdering}}
	if got := up.Tier("default_tier_ordering"); got != TierOrdering {
		t.Errorf("an operator promoting a site must win: got %q", got)
	}
}

// An unknown site is not promoted by a default it was never registered for.
func TestAnUnknownSiteStaysLogged(t *testing.T) {
	cfg := Config{Redact: RedactOutput}
	if got := cfg.Tier("no_such_site_at_all"); got != TierLogged {
		t.Errorf("tier = %q, want %q", got, TierLogged)
	}
}
