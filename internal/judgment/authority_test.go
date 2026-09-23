package judgment

import "testing"

// The authority rules, asserted rather than described.
//
// Every one of these is a property the design document states in prose and
// the tier system is supposed to enforce. A prose statement nobody can run
// is how a safety property quietly stops holding.

func TestConfigCannotGrantASiteMoreThanItDeclares(t *testing.T) {
	RegisterSite(SiteInfo{
		Name: "test_ordering_only_site", Description: "d", Mechanism: "M0", Version: "1",
		MaxEffect: TierOrdering, Redaction: RedactStrict,
	})

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Model = "jev-1.13.0"
	cfg.Sites = map[string]Tier{"test_ordering_only_site": TierRouting}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("a site that implements no effect above ordering must not be grantable routing")
	}

	cfg.Sites = map[string]Tier{"test_ordering_only_site": TierOrdering}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("granting exactly what the site declares must be accepted: %v", err)
	}
}

func TestAnUnregisteredSiteIsLeftAloneRatherThanRefused(t *testing.T) {
	// A judgment.yaml written for a newer build may name a site this binary
	// does not have. Refusing to start on it would make a configuration file
	// unusable across builds; the site simply never runs here.
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Model = "jev-1.13.0"
	cfg.Sites = map[string]Tier{"a_site_from_a_future_build": TierRouting}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an unregistered site must not stop startup: %v", err)
	}
}

func TestDefaultsGrantRoutingOnlyToSitesThatCanChangeControlFlow(t *testing.T) {
	// The default used to be absence — a configuration that said nothing
	// about a site granted it nothing. It is now the site's own MaxEffect,
	// bounded by the redaction mode, because a decision plane that is
	// required to exist and permitted to matter to nothing is not a decision
	// plane. What did not change: an ordering-capable site still starts at
	// logged, and nothing is ever granted an authority its own code does not
	// implement.
	cfg := DefaultConfig()
	for _, s := range KnownSites() {
		got := cfg.Tier(s.Name)
		want := TierLogged
		if s.MaxEffect == TierRouting && cfg.Redact.AtLeast(s.Redaction) {
			want = TierRouting
		}
		if got != want {
			t.Errorf("site %s defaults to %q, want %q (max effect %q, needs redact %q, configured %q)",
				s.Name, got, want, s.MaxEffect, s.Redaction, cfg.Redact)
		}
		if got.rank() > s.MaxEffect.rank() {
			t.Errorf("site %s defaults to %q, above its MaxEffect %q", s.Name, got, s.MaxEffect)
		}
	}
}

// The default must never be a configuration Validate would reject or that
// Preflight would refuse: a shipped default that contradicts itself would
// refuse every task before anyone had configured anything.
func TestDefaultsAreAValidStartingConfiguration(t *testing.T) {
	for _, mode := range []RedactMode{RedactStrict, RedactRepoText, RedactOutput} {
		cfg := DefaultConfig()
		cfg.Redact = mode
		cfg.Enabled = true
		cfg.Model = "jev-1.13.0"
		if err := cfg.Validate(); err != nil {
			t.Errorf("redact %s: the shipped defaults must validate: %v", mode, err)
		}
		for _, s := range KnownSites() {
			if cfg.Tier(s.Name) == TierRouting && !mode.AtLeast(s.Redaction) {
				t.Errorf("redact %s: %s routes by default but cannot send its subject",
					mode, s.Name)
			}
		}
	}
}

func TestAnInvalidTierInConfigurationFailsClosed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Model = "jev-1.13.0"
	cfg.Sites = map[string]Tier{"whatever": "aggressive"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("an unknown tier name must be refused, not silently ignored")
	}
	// And if one reached a call site anyway, it grants nothing.
	var bogus Tier = "aggressive"
	if bogus.Permits(TierLogged) || bogus.Permits(TierOrdering) || bogus.Permits(TierRouting) {
		t.Fatalf("an unrecognised tier must permit nothing")
	}
}

func TestAnInvalidTierIsRefusedEvenWhileDisabled(t *testing.T) {
	// The gap this closes: Validate used to return nil immediately for a
	// disabled or absent config, so `sites: {review_rubric: routng}` written
	// while staging a promotion — enabled still false — passed validation
	// and `bcode doctor` reported OK, with the typo silently reading as logged
	// at every call site. A malformed tier is wrong regardless of whether a
	// live judge is currently consulted.
	cfg := DefaultConfig()
	cfg.Enabled = false
	cfg.Sites = map[string]Tier{"review_rubric": "routng"}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("an unknown tier must be refused even while the integration is disabled")
	}

	// An over-grant must be caught the same way while disabled.
	RegisterSite(SiteInfo{
		Name: "test_disabled_overgrant_site", Description: "d", Mechanism: "M0", Version: "1",
		MaxEffect: TierOrdering, Redaction: RedactStrict,
	})
	cfg.Sites = map[string]Tier{"test_disabled_overgrant_site": TierRouting}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("an over-grant must be refused even while the integration is disabled")
	}

	// A well-formed config with no enabled/model/endpoint set must still
	// pass: absence is not an error, only a mistyped or over-granted value
	// is.
	cfg2 := DefaultConfig()
	cfg2.Enabled = false
	cfg2.Sites = map[string]Tier{"test_disabled_overgrant_site": TierOrdering}
	if err := cfg2.Validate(); err != nil {
		t.Fatalf("a valid grant on a disabled config must not be refused: %v", err)
	}
}

func TestConfigTierIgnoresAnInvalidValueRatherThanHonouringIt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sites = map[string]Tier{"some_site": "routing-ish"}
	if got := cfg.Tier("some_site"); got != TierLogged {
		t.Fatalf("Tier(%q) = %q; an invalid value must read as logged, never as more", "some_site", got)
	}
}

func TestRegisterSiteRequiresACompleteDeclaration(t *testing.T) {
	for name, info := range map[string]SiteInfo{
		"no mechanism":  {Name: "t_incomplete_1", Version: "1", MaxEffect: TierLogged, Redaction: RedactStrict},
		"no version":    {Name: "t_incomplete_2", Mechanism: "M0", MaxEffect: TierLogged, Redaction: RedactStrict},
		"no max effect": {Name: "t_incomplete_3", Mechanism: "M0", Version: "1", Redaction: RedactStrict},
		"no redaction":  {Name: "t_incomplete_4", Mechanism: "M0", Version: "1", MaxEffect: TierLogged},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("an incomplete registration must panic at start rather than " +
						"read as an unconstrained site later")
				}
			}()
			RegisterSite(info)
		})
	}
}

func TestSiteVersionIsEmptyForAnUnregisteredSite(t *testing.T) {
	if got := SiteVersion("nothing_registered_this"); got != "" {
		t.Fatalf("SiteVersion = %q, want empty for an unregistered site", got)
	}
}
