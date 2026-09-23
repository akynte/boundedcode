package policy_test

import (
	"testing"

	"github.com/akynte/boundedcode/internal/policy"
)

// Every family the egress policy is required to cover, by family, so that a
// gap is a named failing subtest rather than a line missing from a list.
func TestEgressSensitiveFamilies(t *testing.T) {
	families := map[string][]string{
		"dotenv": {
			".env", ".env.local", ".env.production", ".env.test.local",
			"app/.env", "deploy/config/.env.staging",
		},
		"pem and key material": {
			"tls.pem", "certs/server.pem", "server.key", "deploy/tls/app.key",
			"keys/app.p12", "keys/app.pfx", "store.jks", "app.keystore",
			"ca.crt", "ca.cer", "ca.der", "release.asc", "release.gpg", "deploy.ppk",
		},
		"ssh key material": {
			"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
			".ssh/id_rsa", "home/.ssh/config", "deploy/id_ed25519",
			"id_rsa.pub", "id_ed25519.pub", ".ssh/authorized_keys", ".ssh/known_hosts",
		},
		"package and tool credentials": {
			".npmrc", ".pypirc", ".netrc", "_netrc", "sub/.npmrc",
			".dockercfg", ".git-credentials", ".htpasswd", ".pgpass",
			".my.cnf", ".rclone.conf", ".boto",
		},
		"credentials by name": {
			"credentials", "credentials.json", "aws_credentials",
			"config/credentials.yaml", "svc/gcp-credentials.json",
		},
		"secrets by name": {
			"secret.yaml", "secrets.yaml", "secrets.tf", "secret_key.go",
			"app_secrets.json", "config/app-secret.env", ".secrets/token",
			"infra/secretsmanager.tf",
		},
		"passwords and tokens": {
			"passwd", "etc/shadow", "password.txt", "apikey.txt",
			"config/api_key.json", "token.json", "svc/token.json",
		},
		"private directories": {
			".git/config", ".git/HEAD", ".bc/workspace.yaml",
			".agent/memory.json", ".agent/secrets/token",
			".gnupg/secring.gpg", ".ssh/", ".git/",
		},
	}
	for family, paths := range families {
		t.Run(family, func(t *testing.T) {
			for _, p := range paths {
				if !policy.EgressSensitive(p) {
					t.Errorf("%q is not treated as egress-sensitive", p)
				}
			}
		})
	}
}

// The predicate is useless if it swallows ordinary source: a packet whose
// candidates are all ineligible is a rerank that never runs.
func TestEgressSensitiveLeavesOrdinarySourceAlone(t *testing.T) {
	for _, p := range []string{
		"internal/task/runner.go", "cmd/bcode/main.go", "README.md",
		"internal/retrieval/rerank.go", "web/src/App.tsx", "go.mod",
		"migrations/001_init.sql", "deploy/Dockerfile", "internal/policy/scope.go",
		"internal/auth/session.go", "pkg/keyring/keyring.go",
	} {
		if policy.EgressSensitive(p) {
			t.Errorf("%q was refused; ordinary source must remain eligible", p)
		}
	}
}

// The two predicates guard different boundaries, and the wider one must never
// be narrower. Stated as a test because they live in different files and will
// be edited by different changes.
func TestEgressSensitiveIsASupersetOfSensitive(t *testing.T) {
	for _, p := range []string{
		".env", ".env.local", "app/.env", "tls.pem", "certs/server.key",
		"credentials.json", "aws_credentials", ".agent/secrets/token",
	} {
		if policy.Sensitive(p) && !policy.EgressSensitive(p) {
			t.Errorf("%q is Sensitive but not EgressSensitive; the egress bar must be lower", p)
		}
	}
}

// An empty path is a path nothing is known about, and "nothing is known about
// it" is not a reason to send it.
func TestEgressSensitiveRefusesTheEmptyPath(t *testing.T) {
	if !policy.EgressSensitive("") {
		t.Error("the empty path was treated as eligible")
	}
}
