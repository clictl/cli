// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/clictl/cli/internal/config"
)

// CLIIndexResponse is the response from GET /api/v1/workspaces/<slug>/registries/cli-index/.
type CLIIndexResponse struct {
	Sources       []CLIIndexSource `json:"sources"`
	Favorites     []string         `json:"favorites"`
	DisabledTools []string         `json:"disabled_tools"`
}

// CLIIndexSource is a single registry source from the workspace CLI index.
type CLIIndexSource struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	Provider     string `json:"provider"`
	Type         string `json:"type"`
	Branch       string `json:"branch"`
	SyncMode     string `json:"sync_mode"`
	Visibility   string `json:"visibility"`
	IsPrivate    bool   `json:"is_private"`
	SpecCount    int    `json:"spec_count"`
	LastSyncedAt string `json:"last_synced_at"`
	Scope        string `json:"scope"`
}

// FetchCLIIndex fetches the workspace CLI index from the API.
func FetchCLIIndex(ctx context.Context, apiURL, workspace, token string) (*CLIIndexResponse, error) {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/registries/cli-index/", apiURL, workspace)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating cli-index request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "clictl/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error (check your connection): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("session expired, run 'clictl login' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cli-index returned %d: %s", resp.StatusCode, string(body))
	}

	var result CLIIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding cli-index: %w", err)
	}
	return &result, nil
}

// WorkspaceCacheDir returns the path to the workspace cache directory.
func WorkspaceCacheDir() string {
	return filepath.Join(config.BaseDir(), "workspace-cache")
}

// LoadCachedCLIIndex loads a cached CLI index for the given workspace slug.
func LoadCachedCLIIndex(slug string) (*CLIIndexResponse, error) {
	path := filepath.Join(WorkspaceCacheDir(), slug+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result CLIIndexResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SaveCachedCLIIndex saves a CLI index response to the workspace cache.
func SaveCachedCLIIndex(slug string, idx *CLIIndexResponse) error {
	dir := WorkspaceCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, slug+".json"), data, 0o644)
}

// MergedRegistries returns a merged list of toolboxes from workspace sources
// and local config. Workspace sources take priority. Deduplicates by URL.
func MergedRegistries(cfg *config.Config, wsIndex *CLIIndexResponse) []config.ToolboxConfig {
	seen := map[string]bool{}
	var merged []config.ToolboxConfig

	// Workspace sources first (highest priority)
	if wsIndex != nil {
		for _, src := range wsIndex.Sources {
			seen[src.URL] = true
			regType := "git"
			if src.Type != "" {
				regType = src.Type
			}
			merged = append(merged, config.ToolboxConfig{
				Name:          src.Name,
				Type:          regType,
				URL:           src.URL,
				Branch:        src.Branch,
				FromWorkspace: true,
				SyncMode:      src.SyncMode,
				IsPrivate:     src.IsPrivate,
				SourceID:      src.ID,
				SpecCount:     src.SpecCount,
				LastSyncedAt:  src.LastSyncedAt,
				Scope:         src.Scope,
			})
		}
	}

	// Local config toolboxes (lower priority, skip duplicates)
	for _, reg := range cfg.Toolboxes {
		if seen[reg.URL] {
			// Workspace source overrides local - warn once per session
			warnWorkspaceOverride(reg.Name)
			continue
		}
		seen[reg.URL] = true
		merged = append(merged, reg)
	}

	// Ensure default API registry is always present
	hasDefault := false
	for _, reg := range merged {
		if reg.Default || reg.URL == config.DefaultAPIURL {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		merged = append(merged, config.ToolboxConfig{
			Name:    "clictl-official",
			Type:    "api",
			URL:     config.DefaultAPIURL,
			Default: true,
		})
	}

	return merged
}

// overrideWarnings tracks which registries we've already warned about.
// Only warn once per session (per process lifetime).
var overrideWarnings = map[string]bool{}

// warnWorkspaceOverride prints a one-time warning when a local registry
// is overridden by a workspace source with the same URL.
func warnWorkspaceOverride(localName string) {
	if overrideWarnings[localName] {
		return
	}
	overrideWarnings[localName] = true
	fmt.Fprintf(os.Stderr, "warning: local toolbox %q overridden by workspace source\n", localName)
}

// IsFavorite checks if a tool name is in the favorites list.
func IsFavorite(favorites []string, name string) bool {
	for _, f := range favorites {
		if f == name {
			return true
		}
	}
	return false
}

// IsDisabledByWorkspace checks if a tool is disabled at the workspace level.
func IsDisabledByWorkspace(disabledTools []string, name string) bool {
	for _, d := range disabledTools {
		if d == name {
			return true
		}
	}
	return false
}
