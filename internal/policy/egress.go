package policy

import (
	"path/filepath"
	"strings"
)

// EgressSensitive reports paths whose *existence* must not be disclosed to a
// service outside this machine.
//
// It is deliberately a superset of Sensitive, and deliberately a separate
// predicate, because the two guard different boundaries and the right answer
// differs between them.
//
// Sensitive answers "may the local model read this file's contents". It is
// narrow on purpose: it costs the operator retrieval coverage every time it
// says yes, and the model it withholds from is running on their own hardware.
//
// This answers "may a third party learn that this file exists, and under what
// name". That is a weaker disclosure per item and a worse one in aggregate: a
// path list is a map of somebody's infrastructure, and it leaves the machine.
// So the bar is lower, the families are wider, and a false positive costs
// nothing but a slightly less well-ordered packet.
//
// It is a deterministic list, and it stays one. Nothing here may be decided by
// a model: a judgment about whether a path is safe to send is a judgment that
// has to be sent somewhere first.
func EgressSensitive(rel string) bool {
	clean := strings.ToLower(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	if clean == "" {
		return true
	}
	// Everything Sensitive refuses, refused here too. The relationship is
	// asserted by a test rather than left to whoever edits one of them next.
	if Sensitive(rel) {
		return true
	}
	// Private directories match as a path segment, not only as a prefix: a
	// checkout that contains somebody's home directory, a fixture, or a
	// vendored dotfile tree puts .ssh several levels down, and "it was not at
	// the root" is not a reason to send it.
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if egressPrivateDirs[part] {
			return true
		}
		if egressSensitiveSegment(part) {
			return true
		}
	}
	return false
}

// egressPrivateDirs are directories whose contents are never repository
// source: this system's own state, git's internals, and the operator's keys.
// Matched as a whole path segment, at any depth.
var egressPrivateDirs = map[string]bool{
	".git": true, ".bc": true, ".agent": true,
	".ssh": true, ".gnupg": true, ".secrets": true, ".aws": true, ".kube": true,
}

// egressSensitiveExts are file extensions that carry key material.
var egressSensitiveExts = []string{".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".crt", ".cer", ".der", ".asc", ".gpg", ".ppk"}

// egressSensitiveNames are files whose whole name is the signal.
var egressSensitiveNames = []string{
	".npmrc", ".pypirc", ".netrc", "_netrc", ".dockercfg", ".git-credentials",
	".htpasswd", ".pgpass", ".my.cnf", ".rclone.conf", ".boto",
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "id_ed25519_sk", "id_ecdsa_sk",
	"authorized_keys", "known_hosts", "shadow", "master.key",
}

// egressSensitivePrefixes match a name that begins this way, so that
// secrets.yaml, secret_key.go and secretsmanager.tf are all covered by one
// entry rather than by three that somebody has to remember to keep in step.
var egressSensitivePrefixes = []string{".env", "secret", "credential", "id_rsa", "id_ed25519"}

// egressSensitiveSubstrings are the families that appear in the middle of a
// name as often as at the start.
var egressSensitiveSubstrings = []string{"credentials", "secrets", "_secret", "-secret", ".secret", "passwd", "password", "apikey", "api_key", "token.json"}

func egressSensitiveSegment(part string) bool {
	if part == "" {
		return false
	}
	for _, name := range egressSensitiveNames {
		if part == name {
			return true
		}
	}
	for _, ext := range egressSensitiveExts {
		if strings.HasSuffix(part, ext) {
			return true
		}
	}
	for _, prefix := range egressSensitivePrefixes {
		if strings.HasPrefix(part, prefix) {
			return true
		}
	}
	for _, sub := range egressSensitiveSubstrings {
		if strings.Contains(part, sub) {
			return true
		}
	}
	// id_rsa.pub and the like: a public key is not a secret, but publishing
	// the fact that this machine holds the matching private one is still a
	// disclosure nobody asked for.
	return strings.HasSuffix(part, ".pub") && strings.HasPrefix(part, "id_")
}
