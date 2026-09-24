// Package attest holds the verifier's signing key and the signature format of
// the evidence chain.
//
// A verification record is signed so that its history can be checked by
// someone who does not trust the database it came from: a changed record no
// longer verifies against the key, and the key's public half can be handed to
// an auditor, a CI job or a reviewer without handing over anything that can
// sign.
//
// What the signature does not do is prove the verifier was honest. The key
// lives under the data directory, readable by the user the supervisor runs as,
// which no model tool and no verification sandbox is granted. Someone with that
// user's access can sign anything. The signature narrows who could have
// written a record to "the supervisor or someone with its credentials"; it
// says nothing about a supervisor that was itself compromised.
package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Domain separates evidence-chain signatures from any other use of the key.
const Domain = "boundedcode-evidence-chain-v1\n"

const (
	privateFile = "verifier.ed25519"
	publicFile  = "verifier.ed25519.pub"
	privateType = "BOUNDEDCODE VERIFIER PRIVATE KEY"
	publicType  = "BOUNDEDCODE VERIFIER PUBLIC KEY"
)

// Key is the verifier's signing key.
type Key struct {
	priv ed25519.PrivateKey
	id   string
}

// KeyID names a public key: the first 16 hex characters of its SHA-256.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// ID is the key's identifier, recorded beside each signature.
func (k *Key) ID() string { return k.id }

// Public is the key's verifying half.
func (k *Key) Public() ed25519.PublicKey { return publicKey(k.priv) }

func publicKey(priv ed25519.PrivateKey) ed25519.PublicKey {
	if len(priv) != ed25519.PrivateKeySize {
		return nil
	}
	return ed25519.PublicKey(priv[ed25519.SeedSize:])
}

// Sign signs a chain hash.
func (k *Key) Sign(hash string) string {
	return hex.EncodeToString(ed25519.Sign(k.priv, []byte(Domain+hash)))
}

// Verify checks a signature over a chain hash.
func Verify(pub ed25519.PublicKey, hash, signature string) bool {
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, []byte(Domain+hash), sig)
}

// LoadOrCreate reads the key in dir, creating dir and a new key when there is
// none. The private key is written 0600 in a 0700 directory.
func LoadOrCreate(dir string) (*Key, error) {
	k, err := Load(dir)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("attest: %w", err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("attest: generate key: %w", err)
	}
	// O_EXCL: two supervisors starting at once must not each write a key and
	// sign half the chain with one that is then overwritten.
	f, err := os.OpenFile(filepath.Join(dir, privateFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return Load(dir)
	}
	if err != nil {
		return nil, fmt.Errorf("attest: %w", err)
	}
	if err := pem.Encode(f, &pem.Block{Type: privateType, Bytes: priv.Seed()}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("attest: write key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("attest: write key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: publicType, Bytes: pub})
	if err := os.WriteFile(filepath.Join(dir, publicFile), pubPEM, 0o644); err != nil { //nolint:gosec // a public key
		return nil, fmt.Errorf("attest: write public key: %w", err)
	}
	return &Key{priv: priv, id: KeyID(pub)}, nil
}

// Load reads the private key in dir.
func Load(dir string) (*Key, error) {
	body, err := os.ReadFile(filepath.Join(dir, privateFile)) //nolint:gosec // the data directory's own key
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != privateType || len(block.Bytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("attest: %s is not a verifier key", filepath.Join(dir, privateFile))
	}
	priv := ed25519.NewKeyFromSeed(block.Bytes)
	return &Key{priv: priv, id: KeyID(publicKey(priv))}, nil
}

// LoadPublic reads a public key file written by LoadOrCreate.
func LoadPublic(path string) (ed25519.PublicKey, error) {
	body, err := os.ReadFile(path) //nolint:gosec // an operator-named public key
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != publicType || len(block.Bytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("attest: %s is not a verifier public key", path)
	}
	return ed25519.PublicKey(block.Bytes), nil
}

// PublicPath is where LoadOrCreate writes the public key in dir.
func PublicPath(dir string) string { return filepath.Join(dir, publicFile) }
