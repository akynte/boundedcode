package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/setup"
)

func TestSetupCommandIsTheCanonicalNonInteractiveConfigurationPath(t *testing.T) {
	data := t.TempDir()
	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--external-url", "http://127.0.0.1:9090",
		"--install-runtime", "--profile", "external-inference", "--skip-judgment", "--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v\n%s", err, output.String())
	}
	var report setup.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("setup JSON is invalid: %v\n%s", err, output.String())
	}
	if report.MarkerPath == "" || !setup.Completed(data) {
		t.Fatalf("setup did not write a completed marker: %+v", report)
	}
	if _, err := os.Stat(data + "/config/providers.yaml"); err != nil {
		t.Fatalf("provider configuration was not written: %v", err)
	}
}

func TestExternalYesSetupDoesNotDownloadOrInstallLocalModel(t *testing.T) {
	var modelRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelRequests.Add(1)
		_, _ = w.Write([]byte("unexpected model request"))
	}))
	defer server.Close()

	data := t.TempDir()
	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--external-url", "http://127.0.0.1:9090",
		"--model-url", server.URL, "--model-sha256", "", "--yes", "--skip-judgment", "--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("external setup failed: %v\n%s", err, output.String())
	}
	if modelRequests.Load() != 0 {
		t.Fatalf("external setup made %d model download requests", modelRequests.Load())
	}
	var report setup.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != string(config.ModeExternal) {
		t.Fatalf("external setup mode = %q", report.Mode)
	}
}

func TestDownloadModelWithoutRuntimeAuthorizationFailsBeforeTheNetwork(t *testing.T) {
	t.Setenv("BC_LLAMA_SERVER", "")
	t.Setenv("BC_PRISM_DIR", "")
	t.Setenv("PATH", t.TempDir())
	var modelRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelRequests.Add(1)
		_, _ = w.Write([]byte("unexpected model request"))
	}))
	defer server.Close()

	data := t.TempDir()
	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--model-url", server.URL, "--model-sha256", "",
		"--download-model", "--skip-judgment", "--json",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("download without runtime authorization returned %v", err)
	}
	if modelRequests.Load() != 0 {
		t.Fatalf("unauthorized setup made %d model requests", modelRequests.Load())
	}
}

func TestSwitchingExternalToEmbeddedClearsTheOldProviderEndpoint(t *testing.T) {
	data := t.TempDir()
	first := newSetupCmd()
	first.SetContext(context.Background())
	var firstOutput bytes.Buffer
	first.SetOut(&firstOutput)
	first.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--external-url", "http://127.0.0.1:9090", "--skip-judgment", "--json",
	})
	if err := first.Execute(); err != nil {
		t.Fatalf("initial external setup failed: %v\n%s", err, firstOutput.String())
	}

	model := filepath.Join(data, "models", "bonsai.gguf")
	if err := os.MkdirAll(filepath.Dir(model), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(data, "runtime", "build", "bin", "llama-server")
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldInstaller := installPrismRuntime
	oldCheck := checkPrismRuntimeBuild
	t.Cleanup(func() {
		installPrismRuntime = oldInstaller
		checkPrismRuntimeBuild = oldCheck
	})
	checkPrismRuntimeBuild = func() error { return nil }
	installPrismRuntime = func(context.Context, string, func(string, ...any)) (string, error) {
		return runtimePath, nil
	}

	second := newSetupCmd()
	second.SetContext(context.Background())
	var secondOutput bytes.Buffer
	second.SetOut(&secondOutput)
	second.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--install-runtime", "--model", model, "--skip-judgment", "--json",
	})
	if err := second.Execute(); err != nil {
		t.Fatalf("embedded reconfiguration failed: %v\n%s", err, secondOutput.String())
	}
	body, err := os.ReadFile(filepath.Join(data, "config", "providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "127.0.0.1:9090") || !strings.Contains(string(body), "127.0.0.1:8080") {
		t.Fatalf("providers retained the external endpoint after switching modes:\n%s", body)
	}
}

func TestSetupCommandInstallsThePinnedRuntimeWithoutReplacingFreshDefaults(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "models", "bonsai.gguf")
	if err := os.MkdirAll(filepath.Dir(model), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(data, "runtime", "build", "bin", "llama-server")
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldInstaller := installPrismRuntime
	oldCheck := checkPrismRuntimeBuild
	t.Cleanup(func() {
		installPrismRuntime = oldInstaller
		checkPrismRuntimeBuild = oldCheck
	})
	checkPrismRuntimeBuild = func() error { return nil }
	installPrismRuntime = func(context.Context, string, func(string, ...any)) (string, error) {
		return runtimePath, nil
	}

	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{
		"--non-interactive", "--data-dir", data, "--install-runtime", "--model", model, "--skip-judgment", "--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v\n%s", err, output.String())
	}
	var report setup.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "bonsai-2-27b-8gb-cuda" {
		t.Fatalf("fresh setup profile = %q, want Bonsai reference", report.Profile)
	}
	cfg, err := config.Load(filepath.Join(data, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inference.Mode != config.ModeEmbedded || cfg.Inference.Binary != runtimePath {
		t.Fatalf("generated embedded config = %+v", cfg.Inference)
	}
}

func TestMergeExistingChoiceDoesNotKeepTheDefaultRuntimePlaceholder(t *testing.T) {
	data := t.TempDir()
	choice := setup.Choice{Mode: config.ModeNone}
	cfg := config.Default()
	mergeExistingChoice(&choice, cfg, data)
	if choice.RuntimeBinary != "" {
		t.Fatalf("default config placeholder became a runtime: %q", choice.RuntimeBinary)
	}
}

func TestEnsureEmbeddedRuntimeRequiresExplicitBuildAuthorization(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "model.gguf")
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldInstaller := installPrismRuntime
	t.Cleanup(func() { installPrismRuntime = oldInstaller })
	called := false
	installPrismRuntime = func(context.Context, string, func(string, ...any)) (string, error) {
		called = true
		return "", nil
	}

	choice := setup.Choice{Mode: config.ModeEmbedded, ModelPath: model}
	if err := ensureEmbeddedRuntime(context.Background(), &bytes.Buffer{}, data, &choice, setup.Options{}); err == nil {
		t.Fatal("expected setup to refuse an implicit runtime build")
	}
	if called {
		t.Fatal("runtime builder was called without explicit authorization")
	}
}

func TestSetupConversationRequiresAConfirmationBeforeBuildingTheRuntime(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "model.gguf")
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldInstaller := installPrismRuntime
	t.Cleanup(func() { installPrismRuntime = oldInstaller })
	called := false
	installPrismRuntime = func(context.Context, string, func(string, ...any)) (string, error) {
		called = true
		return filepath.Join(data, "runtime", "build", "bin", "llama-server"), nil
	}

	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	choice := setup.Choice{DataDir: data, Mode: config.ModeEmbedded, ModelPath: model}
	if err := setupConversation(cmd, bufio.NewReader(strings.NewReader("n\n\n")), &choice, setup.Options{}); err == nil {
		t.Fatal("expected a declined runtime build to cancel setup")
	}
	if called {
		t.Fatal("declining the runtime confirmation still invoked the builder")
	}
	if !strings.Contains(output.String(), "Build the pinned Prism runtime now?") {
		t.Fatalf("runtime confirmation was not shown:\n%s", output.String())
	}
}

func TestSetupConversationBuildsAfterAnExplicitYes(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "model.gguf")
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldInstaller := installPrismRuntime
	oldCheck := checkPrismRuntimeBuild
	t.Cleanup(func() {
		installPrismRuntime = oldInstaller
		checkPrismRuntimeBuild = oldCheck
	})
	checkPrismRuntimeBuild = func() error { return nil }
	installPrismRuntime = func(context.Context, string, func(string, ...any)) (string, error) {
		return filepath.Join(data, "runtime", "build", "bin", "llama-server"), nil
	}

	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetOut(&output)
	choice := setup.Choice{DataDir: data, Mode: config.ModeEmbedded, ModelPath: model}
	if err := setupConversation(cmd, bufio.NewReader(strings.NewReader("y\n")), &choice, setup.Options{}); err != nil {
		t.Fatal(err)
	}
	if choice.RuntimeBinary == "" {
		t.Fatal("explicit runtime confirmation did not record a binary")
	}
}

func TestEnsureEmbeddedRuntimeUsesThePinnedInstallerAfterAuthorization(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "model.gguf")
	if err := os.WriteFile(model, []byte("model"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldInstaller := installPrismRuntime
	t.Cleanup(func() { installPrismRuntime = oldInstaller })
	installPrismRuntime = func(_ context.Context, gotDataDir string, logf func(string, ...any)) (string, error) {
		if gotDataDir != data {
			t.Fatalf("installer data directory = %q, want %q", gotDataDir, data)
		}
		logf("building")
		return filepath.Join(data, "runtime", "build", "bin", "llama-server"), nil
	}

	choice := setup.Choice{Mode: config.ModeEmbedded, ModelPath: model}
	if err := ensureEmbeddedRuntime(context.Background(), &bytes.Buffer{}, data, &choice, setup.Options{InstallRuntime: true}); err != nil {
		t.Fatal(err)
	}
	if choice.RuntimeBinary != filepath.Join(data, "runtime", "build", "bin", "llama-server") {
		t.Fatalf("runtime binary = %q", choice.RuntimeBinary)
	}
}
