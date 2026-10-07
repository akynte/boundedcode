// Package secrets stores provider API keys outside the configuration file:
// in the operating system's credential store (Secret Service on Linux, the
// macOS Keychain, Windows Credential Manager) when one is available, and
// otherwise in an owner-only file next to the configuration.
//
// Keys never appear in config.yaml, on command lines, in logs or audit
// events (every key read here is registered with telemetry.AddSecret), and
// never enter the agent sandbox: only the host-side model gateway uses them.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// Service is the credential-store service name.
const Service = "boundedcode"

// Sources a key can come from.
const (
	SourceEnv     = "environment"
	SourceKeyring = "keyring"
	SourceFile    = "file"
)

// ErrNotFound means no key is stored under the name.
var ErrNotFound = errors.New("no key stored")

// keyringTimeout bounds one credential-store call: on a headless Linux
// machine a D-Bus Secret Service request can hang.
const keyringTimeout = 5 * time.Second

// Store reads and writes named keys ("anthropic", "openai", ...).
type Store struct {
	// File is the fallback file (config dir/credentials.json).
	File string
	// NoKeyring disables the OS credential store (BOUNDEDCODE_SECRETS=file,
	// tests).
	NoKeyring bool

	mu sync.Mutex
}

// New returns the store for a configuration directory.
func New(configDir string) *Store {
	return &Store{File: filepath.Join(configDir, "credentials.json"), NoKeyring: os.Getenv("BOUNDEDCODE_SECRETS") == "file"}
}

// EnvVar is the environment variable that overrides a stored key, for
// servers and CI (for example BOUNDEDCODE_ANTHROPIC_API_KEY). Vendor
// variables such as ANTHROPIC_API_KEY are deliberately not read: a key is
// used only when it was given to BoundedCode explicitly.
func EnvVar(name string) string {
	up := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name))
	return "BOUNDEDCODE_" + up + "_API_KEY"
}

func account(name string) string { return "api-key:" + name }

// Get returns the key stored under name and where it came from.
func (s *Store) Get(name string) (string, string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvVar(name))); v != "" {
		telemetry.AddSecret(v)
		return v, SourceEnv, nil
	}
	if !s.NoKeyring {
		v, err := withTimeout(func() (string, error) { return keyring.Get(Service, account(name)) })
		if err == nil && v != "" {
			telemetry.AddSecret(v)
			return v, SourceKeyring, nil
		}
	}
	m, err := s.readFile()
	if err != nil {
		return "", "", err
	}
	if v := m[name]; v != "" {
		telemetry.AddSecret(v)
		return v, SourceFile, nil
	}
	return "", "", fmt.Errorf("%s: %w", name, ErrNotFound)
}

// Set stores a key, in the credential store when it works and otherwise in
// the owner-only file. It returns where the key was stored.
func (s *Store) Set(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("empty key")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("a key is a single line")
	}
	telemetry.AddSecret(value)
	if !s.NoKeyring {
		if _, err := withTimeout(func() (string, error) { return "", keyring.Set(Service, account(name), value) }); err == nil {
			// Remove any older copy from the file so only one source holds it.
			_ = s.deleteFromFile(name)
			return SourceKeyring, nil
		}
	}
	m, err := s.readFile()
	if err != nil {
		return "", err
	}
	m[name] = value
	if err := s.writeFile(m); err != nil {
		return "", err
	}
	return SourceFile, nil
}

// Delete removes a key from the credential store and the file.
func (s *Store) Delete(name string) error {
	if !s.NoKeyring {
		_, err := withTimeout(func() (string, error) { return "", keyring.Delete(Service, account(name)) })
		if err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, errKeyringTimeout) {
			return fmt.Errorf("remove %s from the credential store: %w", name, err)
		}
	}
	return s.deleteFromFile(name)
}

// Status reports whether a key is available and its source, without
// returning it.
func (s *Store) Status(name string) (bool, string) {
	_, src, err := s.Get(name)
	return err == nil, src
}

// Names lists the names stored in the file (the credential store cannot be
// enumerated portably).
func (s *Store) Names() []string {
	m, _ := s.readFile()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) readFile() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]string{}
	b, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.File, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", s.File, err)
	}
	return m, nil
}

func (s *Store) writeFile(m map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.File), 0o700); err != nil {
		return err
	}
	if len(m) == 0 {
		err := os.Remove(s.File)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.File, append(b, '\n'), 0o600)
}

func (s *Store) deleteFromFile(name string) error {
	m, err := s.readFile()
	if err != nil {
		return err
	}
	if _, ok := m[name]; !ok {
		return nil
	}
	delete(m, name)
	return s.writeFile(m)
}

var errKeyringTimeout = errors.New("credential store did not answer")

func withTimeout(f func() (string, error)) (string, error) {
	type result struct {
		v   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := f()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(keyringTimeout):
		return "", errKeyringTimeout
	}
}
