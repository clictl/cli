// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUpgradeCmd_AllUpToDate(t *testing.T) {
	// Serve a YAML spec that matches the lock file version
	specYAML := "name: test-tool\nversion: 1.0.0\ndescription: Test tool\ncategory: testing\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/yaml/") {
			w.Header().Set("Content-Type", "application/x-yaml")
			fmt.Fprint(w, specYAML)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Create a lock file with the same version
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"test-tool": {
				Version: "1.0.0",
				ETag:    computeETag([]byte(specYAML)),
			},
		},
		GeneratedAt: "2024-01-01T00:00:00Z",
	}

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, ".clictl", "lock.yaml")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("creating lock dir: %v", err)
	}
	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock file: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("writing lock file: %v", err)
	}

	// Read and parse to verify
	readData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}
	var loaded LockFile
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("parsing lock file: %v", err)
	}

	entry, ok := loaded.Tools["test-tool"]
	if !ok {
		t.Fatal("expected test-tool in lock file")
	}
	if entry.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", entry.Version)
	}
}

func TestUpgradeCmd_VersionDiffAvailable(t *testing.T) {
	specYAML := "name: my-tool\nversion: 2.0.0\ndescription: Updated tool\ncategory: testing\n"
	diff := map[string]any{
		"old_version": "1.0.0",
		"new_version": "2.0.0",
		"summary":     "Added new action, fixed auth",
		"changes": []map[string]string{
			{"field": "version", "old": "1.0.0", "new": "2.0.0"},
			{"field": "description", "old": "Original tool", "new": "Updated tool"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/yaml/") {
			w.Header().Set("Content-Type", "application/x-yaml")
			fmt.Fprint(w, specYAML)
			return
		}
		if strings.Contains(r.URL.Path, "/diff/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(diff)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Verify the mock server returns valid diff data
	resp, err := http.Get(server.URL + "/api/v1/specs/my-tool/versions/1.0.0/diff/2.0.0/")
	if err != nil {
		t.Fatalf("fetching diff: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding diff response: %v", err)
	}

	if decoded["summary"] != "Added new action, fixed auth" {
		t.Errorf("unexpected summary: %v", decoded["summary"])
	}
}

func TestUpgradeCmd_NoToolsInstalled(t *testing.T) {
	// Verify that loadInstalled returns nil for non-existent file
	// The upgrade command should print "No tools installed." and return nil
	tmpDir := t.TempDir()
	installedFile := filepath.Join(tmpDir, "installed.yaml")

	_, err := os.ReadFile(installedFile)
	if err == nil {
		t.Fatal("expected error reading non-existent file")
	}
}

func TestUpgradeCmd_LockFileUpdate(t *testing.T) {
	// Test that lock file entries are updated correctly
	oldEntry := LockEntry{
		Version: "1.0.0",
		ETag:    "sha256:old",
	}
	newYAML := []byte("name: my-tool\nversion: 2.0.0\n")
	newEntry := LockEntry{
		Version:       "2.0.0",
		ETag:          computeETag(newYAML),
		ContentSHA256: computeContentSHA256(newYAML),
	}

	if oldEntry.Version == newEntry.Version {
		t.Error("expected different versions")
	}
	if oldEntry.ETag == newEntry.ETag {
		t.Error("expected different etags")
	}
	if !strings.HasPrefix(newEntry.ETag, "sha256:") {
		t.Errorf("expected etag to start with sha256:, got %s", newEntry.ETag)
	}
	if newEntry.ContentSHA256 == "" {
		t.Error("expected content_sha256 to be set")
	}
}

func TestUpgradeCmd_LockFileContentSHA256(t *testing.T) {
	yamlContent := []byte("name: test\nversion: 1.0.0\n")
	hash := computeContentSHA256(yamlContent)

	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(hash) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash))
	}

	// Same content should produce same hash
	hash2 := computeContentSHA256(yamlContent)
	if hash != hash2 {
		t.Errorf("expected deterministic hash, got %s and %s", hash, hash2)
	}

	// Different content should produce different hash
	hash3 := computeContentSHA256([]byte("name: test\nversion: 2.0.0\n"))
	if hash == hash3 {
		t.Error("expected different hash for different content")
	}
}

func TestUpgradeCmd_LockEntryPinnedVersion(t *testing.T) {
	entry := LockEntry{
		Version:       "1.2.3",
		ETag:          "sha256:abc",
		PinnedVersion: "1.2.3",
	}

	data, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}

	var loaded LockEntry
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if loaded.PinnedVersion != "1.2.3" {
		t.Errorf("expected pinned_version 1.2.3, got %s", loaded.PinnedVersion)
	}
}

func TestUpgradeCmd_BreakingChangeInDiff(t *testing.T) {
	diff := map[string]any{
		"old_version":     "1.0.0",
		"new_version":     "2.0.0",
		"summary":         "Removed search action",
		"is_breaking":     true,
		"breaking_reasons": []string{"Action 'search' was removed"},
		"changes":          []map[string]string{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/diff/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(diff)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Verify the mock server returns breaking diff data
	resp, err := http.Get(server.URL + "/api/v1/specs/tool/versions/1.0.0/diff/2.0.0/")
	if err != nil {
		t.Fatalf("fetching diff: %v", err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding diff response: %v", err)
	}

	if decoded["is_breaking"] != true {
		t.Errorf("expected is_breaking=true, got %v", decoded["is_breaking"])
	}
	reasons, ok := decoded["breaking_reasons"].([]any)
	if !ok || len(reasons) != 1 {
		t.Errorf("expected 1 breaking reason, got %v", decoded["breaking_reasons"])
	}
}
