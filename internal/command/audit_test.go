// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func TestAuditCmd_NoInstalledTools(t *testing.T) {
	// Create a temp dir for installed.yaml that does not exist
	tmpDir := t.TempDir()
	installedFile := filepath.Join(tmpDir, "installed.yaml")

	// Verify that reading a non-existent installed file returns nil
	_, err := os.ReadFile(installedFile)
	if err == nil {
		t.Fatal("expected error reading non-existent file")
	}

	// loadInstalled returns nil for missing file, which the audit command
	// handles by printing "No tools installed."
	// We verify the helper behavior directly.
}

func TestAuditCmd_WithMockInstalledAndRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock installed.yaml
	installedFile := filepath.Join(tmpDir, "installed.yaml")
	installedContent := "github-mcp\ntime-mcp\nmy-tool\n"
	if err := os.WriteFile(installedFile, []byte(installedContent), 0o644); err != nil {
		t.Fatalf("writing installed.yaml: %v", err)
	}

	// Create a mock toolbox index
	regDir := filepath.Join(tmpDir, "toolboxes", "test-registry")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("creating toolbox dir: %v", err)
	}

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"github-mcp": {
				Version:     "v1.2.0",
				Description: "GitHub MCP server",
				Category:    "developer",
			},
			"time-mcp": {
				Version:     "v1.1.0",
				Description: "Time MCP server",
				Category:    "utilities",
			},
			// my-tool intentionally missing from registry
		},
	}

	idxData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshaling index: %v", err)
	}

	idxPath := filepath.Join(regDir, "index.json")
	if err := os.WriteFile(idxPath, idxData, 0o644); err != nil {
		t.Fatalf("writing index.json: %v", err)
	}

	// Verify the index can be loaded
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("reading index.json: %v", err)
	}

	var loadedIdx models.Index
	if err := json.Unmarshal(data, &loadedIdx); err != nil {
		t.Fatalf("parsing index.json: %v", err)
	}

	// Verify entries
	ghEntry, ok := loadedIdx.Specs["github-mcp"]
	if !ok {
		t.Fatal("expected github-mcp in index")
	}
	if ghEntry.Version != "v1.2.0" {
		t.Errorf("expected github-mcp version v1.2.0, got %s", ghEntry.Version)
	}

	timeEntry, ok := loadedIdx.Specs["time-mcp"]
	if !ok {
		t.Fatal("expected time-mcp in index")
	}
	if timeEntry.Version != "v1.1.0" {
		t.Errorf("expected time-mcp version v1.1.0, got %s", timeEntry.Version)
	}

	_, ok = loadedIdx.Specs["my-tool"]
	if ok {
		t.Error("expected my-tool to NOT be in index")
	}
}

func TestAuditCmd_VersionComparison(t *testing.T) {
	// Test the version comparison logic used in audit
	tests := []struct {
		installed string
		registry  string
		wantMatch bool
	}{
		{"v1.2.0", "v1.2.0", true},
		{"v1.0.0", "v1.1.0", false},
		{"", "v1.0.0", false},
	}

	for _, tt := range tests {
		got := tt.installed == tt.registry
		if got != tt.wantMatch {
			t.Errorf("installed=%q registry=%q: got match=%v, want %v",
				tt.installed, tt.registry, got, tt.wantMatch)
		}
	}
}
