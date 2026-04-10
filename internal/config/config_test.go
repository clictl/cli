// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clictl/cli/internal/vault"
)

func TestResolveAPIURL_FlagTakesPrecedence(t *testing.T) {
	cfg := &Config{APIURL: "https://from-config.com"}
	t.Setenv("CLICTL_API_URL", "https://from-env.com")

	got := ResolveAPIURL("https://from-flag.com", cfg)
	if got != "https://from-flag.com" {
		t.Errorf("ResolveAPIURL with flag: got %q, want %q", got, "https://from-flag.com")
	}
}

func TestResolveAPIURL_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{APIURL: "https://from-config.com"}
	t.Setenv("CLICTL_API_URL", "https://from-env.com")

	got := ResolveAPIURL("", cfg)
	if got != "https://from-env.com" {
		t.Errorf("ResolveAPIURL with env: got %q, want %q", got, "https://from-env.com")
	}
}

func TestResolveAPIURL_ConfigUsedWhenNoOverride(t *testing.T) {
	cfg := &Config{APIURL: "https://from-config.com"}
	t.Setenv("CLICTL_API_URL", "")

	got := ResolveAPIURL("", cfg)
	if got != "https://from-config.com" {
		t.Errorf("ResolveAPIURL with config: got %q, want %q", got, "https://from-config.com")
	}
}

func TestResolveAPIURL_DefaultFallback(t *testing.T) {
	t.Setenv("CLICTL_API_URL", "")

	got := ResolveAPIURL("", nil)
	if got != DefaultAPIURL {
		t.Errorf("ResolveAPIURL default: got %q, want %q", got, DefaultAPIURL)
	}
}

func TestResolveAuthToken_FlagTakesPrecedence(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{APIKey: "config-key", AccessToken: "config-token"}}
	t.Setenv("CLICTL_API_KEY", "env-key")

	got := ResolveAuthToken("flag-key", cfg)
	if got != "flag-key" {
		t.Errorf("ResolveAuthToken with flag: got %q, want %q", got, "flag-key")
	}
}

func TestResolveAuthToken_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{APIKey: "config-key"}}
	t.Setenv("CLICTL_API_KEY", "env-key")

	got := ResolveAuthToken("", cfg)
	if got != "env-key" {
		t.Errorf("ResolveAuthToken with env: got %q, want %q", got, "env-key")
	}
}

func TestResolveAuthToken_APIKeyOverridesAccessToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{Auth: AuthConfig{APIKey: "api-key", AccessToken: "access-token"}}
	t.Setenv("CLICTL_API_KEY", "")

	got := ResolveAuthToken("", cfg)
	if got != "api-key" {
		t.Errorf("ResolveAuthToken api_key over access_token: got %q, want %q", got, "api-key")
	}
}

func TestResolveAuthToken_FallsBackToAccessToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{Auth: AuthConfig{AccessToken: "access-token"}}
	t.Setenv("CLICTL_API_KEY", "")

	got := ResolveAuthToken("", cfg)
	if got != "access-token" {
		t.Errorf("ResolveAuthToken access_token fallback: got %q, want %q", got, "access-token")
	}
}

func TestResolveAuthToken_ReturnsEmptyWhenNoAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{}
	t.Setenv("CLICTL_API_KEY", "")

	got := ResolveAuthToken("", cfg)
	if got != "" {
		t.Errorf("ResolveAuthToken empty: got %q, want empty", got)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".clictl", "config.yaml")

	// Override configPath for testing
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := &Config{
		APIURL: "https://test.example.com",
		Output: "json",
		Auth: AuthConfig{
			APIKey:      "CLAK-testkey",
			AccessToken: "test-access",
		},
		Toolboxes: []ToolboxConfig{
			{Name: "test-reg", Type: "api", URL: "https://test.example.com", Default: true},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists with correct permissions
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("Config permissions: got %o, want 600", info.Mode().Perm())
	}

	// Load and verify
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.APIURL != "https://test.example.com" {
		t.Errorf("Loaded APIURL: got %q, want %q", loaded.APIURL, "https://test.example.com")
	}
	if loaded.Output != "json" {
		t.Errorf("Loaded Output: got %q, want %q", loaded.Output, "json")
	}
	if loaded.Auth.APIKey != "CLAK-testkey" {
		t.Errorf("Loaded Auth.APIKey: got %q, want %q", loaded.Auth.APIKey, "CLAK-testkey")
	}
}

func TestLoad_DefaultsWhenNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.APIURL != DefaultAPIURL {
		t.Errorf("Default APIURL: got %q, want %q", cfg.APIURL, DefaultAPIURL)
	}
	if cfg.Output != DefaultOutput {
		t.Errorf("Default Output: got %q, want %q", cfg.Output, DefaultOutput)
	}
}

func TestLoad_DefaultToolboxesFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Write a config file with no toolboxes to trigger default toolboxes
	cfgDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("api_url: https://api.clictl.dev\n"), 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Toolboxes) != 1 {
		t.Fatalf("Default Toolboxes: got %d, want 1", len(cfg.Toolboxes))
	}
	if cfg.Toolboxes[0].Name != "clictl-toolbox" {
		t.Errorf("Default Toolbox[0] name: got %q, want %q", cfg.Toolboxes[0].Name, "clictl-toolbox")
	}
}

func TestUpdateConfig_SyncIntervalDuration_Default(t *testing.T) {
	u := UpdateConfig{}
	got := u.SyncIntervalDuration()
	if got != DefaultSyncInterval {
		t.Errorf("Default SyncInterval: got %v, want %v", got, DefaultSyncInterval)
	}
}

func TestUpdateConfig_SyncIntervalDuration_Custom(t *testing.T) {
	u := UpdateConfig{SyncInterval: "24h"}
	got := u.SyncIntervalDuration()
	if got != 24*time.Hour {
		t.Errorf("Custom SyncInterval: got %v, want 24h", got)
	}
}

func TestUpdateConfig_SyncIntervalDuration_Invalid(t *testing.T) {
	u := UpdateConfig{SyncInterval: "not-valid"}
	got := u.SyncIntervalDuration()
	if got != DefaultSyncInterval {
		t.Errorf("Invalid SyncInterval: got %v, want default %v", got, DefaultSyncInterval)
	}
}

func TestUpdateConfig_VersionCheckIntervalDuration_Default(t *testing.T) {
	u := UpdateConfig{}
	got := u.VersionCheckIntervalDuration()
	if got != DefaultVersionCheckInterval {
		t.Errorf("Default VersionCheckInterval: got %v, want %v", got, DefaultVersionCheckInterval)
	}
}

func TestUpdateConfig_VersionCheckIntervalDuration_Custom(t *testing.T) {
	u := UpdateConfig{VersionCheckInterval: "1h"}
	got := u.VersionCheckIntervalDuration()
	if got != time.Hour {
		t.Errorf("Custom VersionCheckInterval: got %v, want 1h", got)
	}
}

func TestIsToolDisabled(t *testing.T) {
	cfg := &Config{DisabledTools: []string{"weather", "stocks"}}

	if !cfg.IsToolDisabled("weather") {
		t.Error("expected weather to be disabled")
	}
	if !cfg.IsToolDisabled("stocks") {
		t.Error("expected stocks to be disabled")
	}
	if cfg.IsToolDisabled("news") {
		t.Error("expected news to not be disabled")
	}
}

func TestIsToolDisabled_Empty(t *testing.T) {
	cfg := &Config{}
	if cfg.IsToolDisabled("anything") {
		t.Error("expected no tools disabled on empty config")
	}
}

func TestDisableTool(t *testing.T) {
	cfg := &Config{}

	cfg.DisableTool("weather")
	if !cfg.IsToolDisabled("weather") {
		t.Error("expected weather to be disabled after DisableTool")
	}
	if len(cfg.DisabledTools) != 1 {
		t.Errorf("expected 1 disabled tool, got %d", len(cfg.DisabledTools))
	}

	// Disable again - should not duplicate
	cfg.DisableTool("weather")
	if len(cfg.DisabledTools) != 1 {
		t.Errorf("expected 1 disabled tool after duplicate disable, got %d", len(cfg.DisabledTools))
	}
}

func TestEnableTool(t *testing.T) {
	cfg := &Config{DisabledTools: []string{"weather", "stocks", "news"}}

	cfg.EnableTool("stocks")
	if cfg.IsToolDisabled("stocks") {
		t.Error("expected stocks to be enabled after EnableTool")
	}
	if len(cfg.DisabledTools) != 2 {
		t.Errorf("expected 2 disabled tools after enable, got %d", len(cfg.DisabledTools))
	}

	// Enable something not in the list - should be no-op
	cfg.EnableTool("nonexistent")
	if len(cfg.DisabledTools) != 2 {
		t.Errorf("expected 2 disabled tools after enabling nonexistent, got %d", len(cfg.DisabledTools))
	}
}

func TestDisabledTools_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &Config{
		APIURL:        DefaultAPIURL,
		Output:        DefaultOutput,
		DisabledTools: []string{"weather", "stocks"},
		Toolboxes: []ToolboxConfig{
			{Name: "test", Type: "api", URL: "https://test.example.com", Default: true},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.DisabledTools) != 2 {
		t.Fatalf("expected 2 disabled tools after load, got %d", len(loaded.DisabledTools))
	}
	if !loaded.IsToolDisabled("weather") {
		t.Error("expected weather to be disabled after load")
	}
	if !loaded.IsToolDisabled("stocks") {
		t.Error("expected stocks to be disabled after load")
	}
}

func TestResolveAuthToken_VaultAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("CLICTL_API_KEY", "")

	// Set up vault with CLICTL_API_KEY
	cliDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cliDir, 0o700)
	v := vault.NewVault(cliDir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("CLICTL_API_KEY", "vault-api-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cfg := &Config{}
	got := ResolveAuthToken("", cfg)
	if got != "vault-api-key" {
		t.Errorf("ResolveAuthToken vault API key: got %q, want %q", got, "vault-api-key")
	}
}

func TestResolveAuthToken_VaultAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("CLICTL_API_KEY", "")

	// Set up vault with CLICTL_ACCESS_TOKEN only
	cliDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cliDir, 0o700)
	v := vault.NewVault(cliDir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("CLICTL_ACCESS_TOKEN", "vault-access-token"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cfg := &Config{}
	got := ResolveAuthToken("", cfg)
	if got != "vault-access-token" {
		t.Errorf("ResolveAuthToken vault access token: got %q, want %q", got, "vault-access-token")
	}
}

func TestResolveAuthToken_EnvOverridesVault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("CLICTL_API_KEY", "env-key")

	// Set up vault with a different value
	cliDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cliDir, 0o700)
	v := vault.NewVault(cliDir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("CLICTL_API_KEY", "vault-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cfg := &Config{}
	got := ResolveAuthToken("", cfg)
	if got != "env-key" {
		t.Errorf("ResolveAuthToken env over vault: got %q, want %q", got, "env-key")
	}
}

func TestResolveAuthToken_VaultOverridesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("CLICTL_API_KEY", "")

	// Set up vault
	cliDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cliDir, 0o700)
	v := vault.NewVault(cliDir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("CLICTL_API_KEY", "vault-key"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	cfg := &Config{Auth: AuthConfig{APIKey: "config-key"}}
	got := ResolveAuthToken("", cfg)
	if got != "vault-key" {
		t.Errorf("ResolveAuthToken vault over config: got %q, want %q", got, "vault-key")
	}
}
