package attest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/attest"
)

func TestKeyIsCreatedOnceAndReloaded(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	k1, err := attest.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := attest.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if k1.ID() != k2.ID() {
		t.Fatal("a second load must return the same key, not a new one")
	}
	info, err := os.Stat(filepath.Join(dir, "verifier.ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v, want 0600", info.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("key directory mode = %v, want 0700", d.Mode().Perm())
	}
	pub, err := attest.LoadPublic(attest.PublicPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if attest.KeyID(pub) != k1.ID() {
		t.Error("the exported public key must be the signing key's")
	}
}

func TestSignatureVerifiesOnlyTheSignedHashWithTheRightKey(t *testing.T) {
	k, err := attest.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := attest.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sig := k.Sign("abc")
	if !attest.Verify(k.Public(), "abc", sig) {
		t.Fatal("a signature must verify against its own key and hash")
	}
	if attest.Verify(k.Public(), "abd", sig) {
		t.Error("a signature must not verify a different hash")
	}
	if attest.Verify(other.Public(), "abc", sig) {
		t.Error("a signature must not verify against another key")
	}
	if attest.Verify(k.Public(), "abc", "not-hex") {
		t.Error("garbage must not verify")
	}
}

func TestLoadRefusesSomethingThatIsNotAKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "verifier.ed25519"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := attest.LoadOrCreate(dir); err == nil {
		t.Fatal("a corrupt key file must be an error, not silently replaced")
	}
}
