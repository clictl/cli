// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clictl/cli/internal/config"
)

func TestMergedRegistries_WorkspaceFirst(t *testing.T) {
	cfg := &config.Config{
		Toolboxes: []config.ToolboxConfig{
			{Name: "local-dev", Type: "git", URL: "/home/user/tools"},
			{Name: "clictl-official", Type: "api", URL: config.DefaultAPIURL, Default: true},
		},
	}
	wsIndex := &CLIIndexResponse{
		Sources: []CLIIndexSource{
			{ID: "uuid-1", Name: "org/tools", URL: "https://github.com/org/tools.git", Provider: "github", Type: "git", Branch: "main"},
		},
	}

	merged := MergedRegistries(cfg, wsIndex)

	if len(merged) != 3 {
		t.Fatalf("expected 3 registries, got %d", len(merged))
	}
	// Workspace source should be first
	if merged[0].Name != "org/tools" {
		t.Errorf("expected first registry to be workspace source, got %s", merged[0].Name)
	}
	if !merged[0].FromWorkspace {
		t.Error("expected first registry to be marked as from workspace")
	}
	// Local dev should be second
	if merged[1].Name != "local-dev" {
		t.Errorf("expected second registry to be local-dev, got %s", merged[1].Name)
	}
}

func TestMergedRegistries_DeduplicateByURL(t *testing.T) {
	cfg := &config.Config{
		Toolboxes: []config.ToolboxConfig{
			{Name: "my-tools", Type: "git", URL: "https://github.com/org/tools.git"},
		},
	}
	wsIndex := &CLIIndexResponse{
		Sources: []CLIIndexSource{
			{ID: "uuid-1", Name: "org/tools", URL: "https://github.com/org/tools.git", Provider: "github", Type: "git"},
		},
	}

	merged := MergedRegistries(cfg, wsIndex)

	// Should have workspace version + default, local duplicate removed
	count := 0
	for _, r := range merged {
		if r.URL == "https://github.com/org/tools.git" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry for tools URL, got %d", count)
	}
}

func TestMergedRegistries_NilWorkspace(t *testing.T) {
	cfg := &config.Config{
		Toolboxes: []config.ToolboxConfig{
			{Name: "clictl-official", Type: "api", URL: config.DefaultAPIURL, Default: true},
		},
	}

	merged := MergedRegistries(cfg, nil)

	if len(merged) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(merged))
	}
}

func TestMergedRegistries_DefaultAlwaysPresent(t *testing.T) {
	cfg := &config.Config{
		Toolboxes: []config.ToolboxConfig{
			{Name: "my-only", Type: "git", URL: "https://example.com/repo.git"},
		},
	}

	merged := MergedRegistries(cfg, nil)

	hasDefault := false
	for _, r := range merged {
		if r.Default || r.URL == config.DefaultAPIURL {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Error("expected default API registry to be present")
	}
}

func TestIsFavorite(t *testing.T) {
	favs := []string{"slack-webhook", "github-issues"}
	if !IsFavorite(favs, "slack-webhook") {
		t.Error("expected slack-webhook to be a favorite")
	}
	if IsFavorite(favs, "not-a-favorite") {
		t.Error("expected not-a-favorite to not be a favorite")
	}
}

func TestIsDisabledByWorkspace(t *testing.T) {
	disabled := []string{"dangerous-tool"}
	if !IsDisabledByWorkspace(disabled, "dangerous-tool") {
		t.Error("expected dangerous-tool to be disabled")
	}
	if IsDisabledByWorkspace(disabled, "safe-tool") {
		t.Error("expected safe-tool to not be disabled")
	}
}

func TestCacheCLIIndex(t *testing.T) {
	tmpDir := t.TempDir()
	// Override cache dir for test
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { _ = os.Setenv("HOME", origHome) }()

	idx := &CLIIndexResponse{
		Sources: []CLIIndexSource{
			{ID: "uuid-1", Name: "test/repo", URL: "https://github.com/test/repo.git"},
		},
		Favorites:     []string{"tool-a", "tool-b"},
		DisabledTools: []string{"bad-tool"},
	}

	err := SaveCachedCLIIndex("test-ws", idx)
	if err != nil {
		t.Fatalf("SaveCachedCLIIndex failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, ".clictl", "workspace-cache", "test-ws.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cached file not found: %v", err)
	}

	var loaded CLIIndexResponse
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal cached data: %v", err)
	}
	if len(loaded.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(loaded.Sources))
	}
	if len(loaded.Favorites) != 2 {
		t.Errorf("expected 2 favorites, got %d", len(loaded.Favorites))
	}

	// Test LoadCachedCLIIndex
	loaded2, err := LoadCachedCLIIndex("test-ws")
	if err != nil {
		t.Fatalf("LoadCachedCLIIndex failed: %v", err)
	}
	if loaded2.Sources[0].Name != "test/repo" {
		t.Errorf("expected source name test/repo, got %s", loaded2.Sources[0].Name)
	}
}
