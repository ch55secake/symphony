package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileReadsYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("kurrentdb_url: kurrentdb://localhost:2113?tls=false\nprovider: opencode\ntransport: chat-completions\nmodel: kimi-test\nworkspace: /workspace\nopenai_api_key: openai-key\nanthropic_api_key: anthropic-key\nopencode_api_key: opencode-key\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	settings, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if settings.KurrentDBURL != "kurrentdb://localhost:2113?tls=false" || settings.Provider != "opencode" || settings.Transport != "chat-completions" || settings.Model != "kimi-test" || settings.Workspace != "/workspace" || settings.OpenAIAPIKey != "openai-key" || settings.AnthropicAPIKey != "anthropic-key" || settings.OpenCodeAPIKey != "opencode-key" {
		t.Fatalf("LoadFile() = %#v", settings)
	}
}

func TestLoadFileEnvironmentOverridesYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider: openai\nopenai_api_key: file-key\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("PROVIDER", "anthropic")
	t.Setenv("OPENAI_API_KEY", "environment-key")

	settings, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if settings.Provider != "anthropic" || settings.OpenAIAPIKey != "environment-key" {
		t.Fatalf("LoadFile() = %#v", settings)
	}
}

func TestLoadFileAllowsMissingFile(t *testing.T) {
	settings, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if settings.Transport != "responses" {
		t.Fatalf("transport = %q, want responses", settings.Transport)
	}
}
