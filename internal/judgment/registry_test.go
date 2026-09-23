package judgment

import "testing"

func TestRegisterSiteRejectsADuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("registering the same site name twice must panic")
		}
	}()
	info := SiteInfo{Name: "test_duplicate_site_xyz", Description: "first", Mechanism: "M0",
		Version: "1", MaxEffect: TierLogged, Redaction: RedactStrict}
	RegisterSite(info)
	RegisterSite(info)
}

func TestKnownSitesIsSorted(t *testing.T) {
	// internal/judgment's own test binary does not import internal/workflow,
	// internal/task or internal/retrieval — importing them back would be the
	// cycle RegisterSite exists to avoid — so KnownSites here only contains
	// whatever this file's own tests registered. Run `go test ./internal/...`
	// (or `bcode judgment sites`, which links the real binary) to see every
	// site this repository actually defines.
	sites := KnownSites()
	for i := 1; i < len(sites); i++ {
		if sites[i-1].Name >= sites[i].Name {
			t.Fatalf("KnownSites is not sorted at index %d: %q >= %q", i, sites[i-1].Name, sites[i].Name)
		}
	}
}
