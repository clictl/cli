// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package config handles CLI configuration loading, saving, and resolution.
// Configuration is stored at ~/.clictl/config.yaml and includes API URLs,
// authentication credentials, registry sources, and output preferences.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/clictl/cli/internal/vault"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultAPIURL is the default registry API endpoint.
	DefaultAPIURL = "https://api.clictl.dev"
	// DefaultToolboxRepo is the public GitHub repository for the official toolbox.
	DefaultToolboxRepo = "https://github.com/clictl/toolbox"
	// DefaultOutput is the default output format.
	DefaultOutput = "text"
	// DefaultSyncInterval is how often to auto-sync the registry index.
	DefaultSyncInterval = 7 * 24 * time.Hour // 1 week
	// DefaultVersionCheckInterval is how often to check for CLI updates.
	DefaultVersionCheckInterval = 7 * 24 * time.Hour // 1 week
)

// flagHome holds the --home flag value, set via SetHome.
var flagHome string

// SetHome sets the base directory override from the --home flag.
func SetHome(path string) {
	flagHome = path
}

// BaseDir returns the clictl configuration directory.
// Precedence: --home flag > CLICTL_HOME env > ~/.clictl
func BaseDir() string {
	if flagHome != "" {
		return flagHome
	}
	if envHome := os.Getenv("CLICTL_HOME"); envHome != "" {
		return envHome
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".clictl"
	}
	return filepath.Join(home, ".clictl")
}

// AuthConfig holds authentication credentials.
type AuthConfig struct {
	APIKey          string `yaml:"api_key,omitempty"`
	AccessToken     string `yaml:"access_token,omitempty"`
	RefreshToken    string `yaml:"refresh_token,omitempty"`
	ExpiresAt       string `yaml:"expires_at,omitempty"`
	ActiveWorkspace string `yaml:"active_workspace,omitempty"`
}

// UpdateConfig holds auto-update and sync preferences.
type UpdateConfig struct {
	// AutoUpdate enables automatic CLI binary updates when a new version is available.
	AutoUpdate bool `yaml:"auto_update"`
	// SyncInterval is the duration between automatic registry index syncs (e.g. "168h" for weekly).
	SyncInterval string `yaml:"sync_interval,omitempty"`
	// VersionCheckInterval is the duration between automatic version checks (e.g. "168h" for weekly).
	VersionCheckInterval string `yaml:"version_check_interval,omitempty"`
	// LastSyncAt is the timestamp of the last successful registry sync.
	LastSyncAt string `yaml:"last_sync_at,omitempty"`
	// LastVersionCheckAt is the timestamp of the last version check.
	LastVersionCheckAt string `yaml:"last_version_check_at,omitempty"`
	// LatestVersion is the most recently known latest version from GitHub.
	LatestVersion string `yaml:"latest_version,omitempty"`
}

// SyncIntervalDuration returns the sync interval as a time.Duration.
// Falls back to DefaultSyncInterval if unset or invalid.
func (u *UpdateConfig) SyncIntervalDuration() time.Duration {
	if u.SyncInterval == "" {
		return DefaultSyncInterval
	}
	d, err := time.ParseDuration(u.SyncInterval)
	if err != nil {
		return DefaultSyncInterval
	}
	return d
}

// VersionCheckIntervalDuration returns the version check interval as a time.Duration.
// Falls back to DefaultVersionCheckInterval if unset or invalid.
func (u *UpdateConfig) VersionCheckIntervalDuration() time.Duration {
	if u.VersionCheckInterval == "" {
		return DefaultVersionCheckInterval
	}
	d, err := time.ParseDuration(u.VersionCheckInterval)
	if err != nil {
		return DefaultVersionCheckInterval
	}
	return d
}

// ToolboxConfig describes a configured toolbox source (git-based tool repository or API).
type ToolboxConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"` // "api" or "git"
	URL     string `yaml:"url"`
	Branch  string `yaml:"branch"`
	Default bool   `yaml:"default"`
	// Workspace-sourced fields (populated from CLI index, not config file)
	FromWorkspace bool   `yaml:"-"` // true if this came from workspace API
	SyncMode      string `yaml:"-"` // "full" or "metadata_only"
	IsPrivate     bool   `yaml:"-"` // private repo, spec content not on server
	SourceID      string `yaml:"-"` // UUID of the RegistrySource on the backend
	SpecCount     int    `yaml:"-"` // number of specs in source (for cache invalidation)
	LastSyncedAt  string `yaml:"-"` // last sync timestamp (for cache invalidation)
	Scope         string `yaml:"-"` // "personal", "workspace", or "project" (from API)
}

// ShelfConfig is a deprecated alias for ToolboxConfig.
type ShelfConfig = ToolboxConfig

// BucketConfig is a deprecated alias for ToolboxConfig.
type BucketConfig = ToolboxConfig

// RegistryConfig is a deprecated alias for ToolboxConfig.
type RegistryConfig = ToolboxConfig

// WorkspaceSyncConfig holds workspace registry sync preferences.
type WorkspaceSyncConfig struct {
	// Enabled turns on workspace registry inheritance. Default: true when logged in.
	Enabled bool `yaml:"enabled"`
	// CacheTTL is how long to cache the workspace CLI index. Default: 5m.
	CacheTTL string `yaml:"cache_ttl,omitempty"`
	// LastFetchedAt is the timestamp of the last successful CLI index fetch.
	LastFetchedAt string `yaml:"last_fetched_at,omitempty"`
}

// CacheTTLDuration returns the cache TTL as a time.Duration.
// Falls back to 5 minutes if unset or invalid.
func (w *WorkspaceSyncConfig) CacheTTLDuration() time.Duration {
	if w.CacheTTL == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(w.CacheTTL)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// ResponseCacheConfig controls the HTTP response cache.
type ResponseCacheConfig struct {
	// Enabled turns on response caching. Default is false.
	Enabled bool `yaml:"enabled"`
	// MaxSizeMB is the maximum cache size in megabytes. 0 means unlimited.
	MaxSizeMB int `yaml:"max_size_mb,omitempty"`
}

// ExecutionConfig controls how tools are executed (standard OS sandbox or container).
type ExecutionConfig struct {
	// Mode is the execution mode: "standard" (OS-level sandbox) or "container" (Docker).
	Mode string `yaml:"mode"`
	// Image is the Docker image used for container execution.
	Image string `yaml:"image"`
	// Timeout is the maximum execution time in seconds.
	Timeout int `yaml:"timeout"`
	// Memory is the container memory limit (e.g. "512m").
	Memory string `yaml:"memory"`
	// CPUs is the container CPU limit (e.g. "1").
	CPUs string `yaml:"cpus"`
}

// LogConfig controls CLI logging behavior.
type LogConfig struct {
	// Enabled turns on logging. Default is false (no logs).
	Enabled bool `yaml:"enabled"`
	// Level is the minimum log level: debug, info, warn, error. Default: info.
	Level string `yaml:"level,omitempty"`
	// Format is the log format: text or json. Default: text.
	Format string `yaml:"format,omitempty"`
	// File is the path to a log file. Default: stderr. Use "~/.clictl/clictl.log" for persistent logs.
	File string `yaml:"file,omitempty"`
}

// Config holds the CLI configuration loaded from ~/.clictl/config.yaml.
type Config struct {
	APIURL        string              `yaml:"api_url"`
	Output        string              `yaml:"output"`
	CacheDir      string              `yaml:"cache_dir"`
	Auth          AuthConfig          `yaml:"auth,omitempty"`
	Update        UpdateConfig        `yaml:"update,omitempty"`
	WorkspaceSync WorkspaceSyncConfig `yaml:"workspace_sync,omitempty"`
	ResponseCache ResponseCacheConfig `yaml:"response_cache,omitempty"`
	Log           LogConfig           `yaml:"log,omitempty"`
	Execution     ExecutionConfig     `yaml:"execution,omitempty"`
	Toolboxes     []ToolboxConfig      `yaml:"toolboxes"`
	DisabledTools []string            `yaml:"disabled_tools,omitempty"`
	PinnedTools   []string            `yaml:"pinned_tools,omitempty"`
	AutoInstall   bool                `yaml:"auto_install"`
	Sandbox       bool                `yaml:"sandbox"`
	StrictSandbox *bool               `yaml:"strict_sandbox,omitempty"`
	Telemetry     bool                `yaml:"telemetry"`
	Logging       *bool               `yaml:"logging,omitempty"`
	FirstRunDone  bool                `yaml:"first_run_done,omitempty"`
}

// StrictSandboxEnabled returns whether sandbox must fail-closed.
// Defaults to true (fail-closed) when not explicitly set.
func (c *Config) StrictSandboxEnabled() bool {
	if c.StrictSandbox == nil {
		return true
	}
	return *c.StrictSandbox
}

// LoggingEnabled returns whether enterprise log submission is enabled.
// Defaults to true when Logging is nil (not explicitly set).
func (c *Config) LoggingEnabled() bool {
	if c.Logging == nil {
		return true
	}
	return *c.Logging
}

// IsToolDisabled checks if a tool is in the disabled list.
func (c *Config) IsToolDisabled(toolName string) bool {
	for _, name := range c.DisabledTools {
		if name == toolName {
			return true
		}
	}
	return false
}

// DisableTool adds a tool to the disabled list if not already present.
func (c *Config) DisableTool(toolName string) {
	if !c.IsToolDisabled(toolName) {
		c.DisabledTools = append(c.DisabledTools, toolName)
	}
}

// EnableTool removes a tool from the disabled list.
func (c *Config) EnableTool(toolName string) {
	filtered := make([]string, 0, len(c.DisabledTools))
	for _, name := range c.DisabledTools {
		if name != toolName {
			filtered = append(filtered, name)
		}
	}
	c.DisabledTools = filtered
}

// IsToolPinned checks if a tool is in the pinned list.
func (c *Config) IsToolPinned(toolName string) bool {
	for _, name := range c.PinnedTools {
		if name == toolName {
			return true
		}
	}
	return false
}

// PinTool adds a tool to the pinned list if not already present.
func (c *Config) PinTool(toolName string) {
	if !c.IsToolPinned(toolName) {
		c.PinnedTools = append(c.PinnedTools, toolName)
	}
}

// UnpinTool removes a tool from the pinned list.
func (c *Config) UnpinTool(toolName string) {
	filtered := make([]string, 0, len(c.PinnedTools))
	for _, name := range c.PinnedTools {
		if name != toolName {
			filtered = append(filtered, name)
		}
	}
	c.PinnedTools = filtered
}

// DefaultCacheDir returns the default cache directory path.
func DefaultCacheDir() string {
	return filepath.Join(BaseDir(), "cache")
}

// configPath returns the path to the config file.
func configPath() string {
	return filepath.Join(BaseDir(), "config.yaml")
}

// Load reads the config file from ~/.clictl/config.yaml.
// Missing file is not an error; defaults are returned instead.
func Load() (*Config, error) {
	cfg := &Config{
		APIURL:    DefaultAPIURL,
		Output:    DefaultOutput,
		CacheDir:  DefaultCacheDir(),
		Sandbox:   true,
		Telemetry: true,
		Execution: ExecutionConfig{
			Mode: "standard",
		},
	}

	path := configPath()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return cfg, err
	}

	if fileCfg.APIURL != "" {
		cfg.APIURL = fileCfg.APIURL
	}
	if fileCfg.Output != "" {
		cfg.Output = fileCfg.Output
	}
	if fileCfg.CacheDir != "" {
		if len(fileCfg.CacheDir) >= 2 && fileCfg.CacheDir[:2] == "~/" {
			home, err := os.UserHomeDir()
			if err == nil {
				fileCfg.CacheDir = filepath.Join(home, fileCfg.CacheDir[2:])
			}
		}
		cfg.CacheDir = fileCfg.CacheDir
	}

	cfg.Auth = fileCfg.Auth
	cfg.Update = fileCfg.Update
	cfg.WorkspaceSync = fileCfg.WorkspaceSync
	cfg.ResponseCache = fileCfg.ResponseCache

	// Merge execution config, preserving defaults for unset fields.
	if fileCfg.Execution.Mode != "" {
		cfg.Execution.Mode = fileCfg.Execution.Mode
	}
	if fileCfg.Execution.Image != "" {
		cfg.Execution.Image = fileCfg.Execution.Image
	}
	if fileCfg.Execution.Timeout > 0 {
		cfg.Execution.Timeout = fileCfg.Execution.Timeout
	}
	if fileCfg.Execution.Memory != "" {
		cfg.Execution.Memory = fileCfg.Execution.Memory
	}
	if fileCfg.Execution.CPUs != "" {
		cfg.Execution.CPUs = fileCfg.Execution.CPUs
	}
	cfg.DisabledTools = fileCfg.DisabledTools
	cfg.PinnedTools = fileCfg.PinnedTools
	cfg.AutoInstall = fileCfg.AutoInstall
	cfg.FirstRunDone = fileCfg.FirstRunDone

	// Telemetry defaults to true. Only override if the config file explicitly
	// contains the "telemetry" key (since the YAML zero value for bool is false,
	// we cannot distinguish "absent" from "set to false" on the struct alone).
	var rawMap map[string]interface{}
	if yaml.Unmarshal(data, &rawMap) == nil {
		if _, exists := rawMap["telemetry"]; exists {
			cfg.Telemetry = fileCfg.Telemetry
		}
	}

	if len(fileCfg.Toolboxes) > 0 {
		cfg.Toolboxes = fileCfg.Toolboxes
	}

	if len(cfg.Toolboxes) == 0 {
		cfg.Toolboxes = []ToolboxConfig{
			{Name: "clictl-toolbox", Type: "git", URL: DefaultToolboxRepo, Default: true},
		}
	}

	return cfg, nil
}

// ToolboxesDir returns the path to the toolboxes directory.
func ToolboxesDir() string {
	return filepath.Join(BaseDir(), "toolboxes")
}

// ShelvesDir is a deprecated alias for ToolboxesDir.
func ShelvesDir() string {
	return ToolboxesDir()
}

// BucketsDir is a deprecated alias for ToolboxesDir.
func BucketsDir() string {
	return ToolboxesDir()
}

// RegistriesDir is a deprecated alias for ToolboxesDir.
func RegistriesDir() string {
	return ToolboxesDir()
}

// Save writes the config back to ~/.clictl/config.yaml with 0600 permissions.
func Save(cfg *Config) error {
	path := configPath()
	if path == "" {
		return fmt.Errorf("could not determine config path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// ResolveAPIURL returns the API URL, checking env var and flag override in order of precedence:
// flag > env var > config file > default.
func ResolveAPIURL(flagValue string, cfg *Config) string {
	if flagValue != "" {
		return flagValue
	}
	if envURL := os.Getenv("CLICTL_API_URL"); envURL != "" {
		return envURL
	}
	if cfg != nil && cfg.APIURL != "" {
		return cfg.APIURL
	}
	return DefaultAPIURL
}

// ResolveAuthToken returns the auth token with precedence:
// --api-key flag > CLICTL_API_KEY env > vault CLICTL_API_KEY >
// vault CLICTL_ACCESS_TOKEN > config api_key > config access_token.
func ResolveAuthToken(flagAPIKey string, cfg *Config) string {
	if flagAPIKey != "" {
		return flagAPIKey
	}
	if envKey := os.Getenv("CLICTL_API_KEY"); envKey != "" {
		return envKey
	}

	// Check vault for API key and access token
	{
		v := vault.NewVault(BaseDir())
		if v.HasKey() {
			if val, err := v.Get("CLICTL_API_KEY"); err == nil && val != "" {
				return val
			}
			if val, err := v.Get("CLICTL_ACCESS_TOKEN"); err == nil && val != "" {
				return val
			}
		}
	}

	if cfg != nil && cfg.Auth.APIKey != "" {
		return cfg.Auth.APIKey
	}
	if cfg != nil && cfg.Auth.AccessToken != "" {
		return cfg.Auth.AccessToken
	}
	return ""
}

// RefreshAuth checks if the access token is expired or near-expiry and refreshes
// it using the stored refresh token. Returns true if the token was refreshed.
func RefreshAuth(ctx context.Context, cfg *Config) bool {
	if cfg == nil || cfg.Auth.RefreshToken == "" || cfg.Auth.ExpiresAt == "" {
		return false
	}

	// If using API key auth, no refresh needed
	if cfg.Auth.APIKey != "" {
		return false
	}

	expiresAt, err := time.Parse(time.RFC3339, cfg.Auth.ExpiresAt)
	if err != nil {
		return false
	}

	// Refresh if token expires within 5 minutes
	if time.Until(expiresAt) > 5*time.Minute {
		return false
	}

	apiURL := ResolveAPIURL("", cfg)
	refreshURL := fmt.Sprintf("%s/api/v1/auth/refresh/", apiURL)

	body, _ := json.Marshal(map[string]string{"refresh": cfg.Auth.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	var tokenResp struct {
		Access  string `json:"access"`
		Refresh string `json:"refresh,omitempty"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil || tokenResp.Access == "" {
		return false
	}

	// Update config with new tokens
	cfg.Auth.AccessToken = tokenResp.Access
	cfg.Auth.ExpiresAt = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	if tokenResp.Refresh != "" {
		cfg.Auth.RefreshToken = tokenResp.Refresh
	}

	// Persist to config file
	_ = Save(cfg)

	// Update vault if available
	v := vault.NewVault(BaseDir())
	if v.HasKey() {
		_ = v.Set("CLICTL_ACCESS_TOKEN", tokenResp.Access)
		if tokenResp.Refresh != "" {
			_ = v.Set("CLICTL_REFRESH_TOKEN", tokenResp.Refresh)
		}
	}

	return true
}
