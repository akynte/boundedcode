package judgeval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileIsStableForIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := os.WriteFile(p, []byte("same content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h1, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash of unchanged file differed: %s vs %s", h1, h2)
	}
}

func TestHashFileChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	os.WriteFile(p, []byte("a\n"), 0o600)
	h1, _ := HashFile(p)
	os.WriteFile(p, []byte("b\n"), 0o600)
	h2, _ := HashFile(p)
	if h1 == h2 {
		t.Fatal("hash did not change when file content changed")
	}
}

func TestHashPolicyChangesWithAThresholdEdit(t *testing.T) {
	p1 := SitePolicy{Site: "x", MinPromotionSamples: Threshold{RequiresSelection: true}}
	p2 := SitePolicy{Site: "x", MinPromotionSamples: Threshold{Value: 200, RequiresSelection: false}}
	h1, err := HashPolicy(p1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPolicy(p2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("policy hash must change when a threshold is configured")
	}
}

func TestManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	m := Manifest{
		SchemaVersion: 1, Site: "verification_integrity", SiteVersion: "1",
		DatasetPath: "evals/judgment/datasets/verification_integrity.jsonl",
		DatasetHash: "abc123", Split: SplitDev, Model: "jev-1.13.0",
		PolicyVersion: PolicySchemaVersion, Arm: string(ArmJev), Seed: 42, Live: false,
	}
	if err := SaveManifest(p, m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != m {
		t.Fatalf("round trip changed the manifest:\nwant %+v\ngot  %+v", m, got)
	}
}

func TestGitRevisionOnThisRepository(t *testing.T) {
	rev, _ := GitRevision(context.Background(), ".")
	if rev == "" {
		t.Skip("not running inside a git checkout with a resolvable HEAD; not a package failure")
	}
	if len(rev) != 40 {
		t.Errorf("git revision %q does not look like a full SHA", rev)
	}
}

func TestGitRevisionOutsideAGitCheckoutReturnsEmptyRatherThanError(t *testing.T) {
	dir := t.TempDir() // no .git here
	rev, dirty := GitRevision(context.Background(), dir)
	if rev != "" || dirty {
		t.Fatalf("expected empty/false outside a git checkout, got rev=%q dirty=%v", rev, dirty)
	}
}
