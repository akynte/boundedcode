package opencode_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/opencode"
	"github.com/akynte/boundedcode/internal/sandbox"
)

func TestManagedSessionSeparatesPersistentStateFromEphemeralControl(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	home := filepath.Join(dir, "session", "home")
	control := filepath.Join(dir, "session", "control")
	data := filepath.Join(dir, "workspace", "opencode", "data")
	state := filepath.Join(dir, "workspace", "opencode", "state")
	plugin := filepath.Join(dir, "workspace", "opencode", "opencode-plugin")
	tmp := filepath.Join(dir, "session", "tmp")
	for _, path := range []string{repo, home, control, data, state, plugin, tmp} {
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(dir, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := opencode.Session{
		Binary: binary, Repo: repo, StateDir: home, HomeDir: home,
		DataHome: data, StateHome: state, ControlDir: control, PluginDir: plugin, TmpDir: tmp,
	}
	spec, err := s.Confine(sandbox.Spec{ReadWrite: []string{filepath.Join(dir, "all-worktrees")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{repo, home, data, state, tmp} {
		if !slices.Contains(spec.ReadWrite, path) {
			t.Errorf("persistent/session path %s is not writable: %v", path, spec.ReadWrite)
		}
	}
	if slices.Contains(spec.ReadWrite, filepath.Join(dir, "all-worktrees")) {
		t.Errorf("editor received supervisor task-worktree authority: %v", spec.ReadWrite)
	}
	for _, path := range []string{control, plugin} {
		if !slices.Contains(spec.ReadOnly, path) {
			t.Errorf("control/plugin path %s is not readable: %v", path, spec.ReadOnly)
		}
		if slices.Contains(spec.ReadWrite, path) {
			t.Errorf("control/plugin path %s is writable: %v", path, spec.ReadWrite)
		}
	}
	values := map[string]string{}
	for _, entry := range s.Env() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["XDG_DATA_HOME"] != data || values["XDG_STATE_HOME"] != state || values["HOME"] != home {
		t.Fatalf("session environment did not split persistent and ephemeral state: %v", values)
	}
}
