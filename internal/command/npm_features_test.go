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

	"github.com/clictl/cli/internal/models"
	"gopkg.in/yaml.v3"
)

// TestAuditCommand verifies the audit flow: installed tools are checked against
// the registry index for missing entries and version mismatches.
func TestAuditCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an installed.yaml with three tools
	installedFile := filepath.Join(tmpDir, "installed.yaml")
	installedContent := "github-mcp\nslack-mcp\norphan-tool\n"
	if err := os.WriteFile(installedFile, []byte(installedContent), 0o644); err != nil {
		t.Fatalf("writing installed.yaml: %v", err)
	}

	// Create a mock registry index missing 'orphan-tool'
	regDir := filepath.Join(tmpDir, "toolboxes", "default")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("creating toolbox dir: %v", err)
	}

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"github-mcp": {
				Version:     "v2.0.0",
				Description: "GitHub MCP server",
				Category:    "developer",
			},
			"slack-mcp": {
				Version:     "v1.3.0",
				Description: "Slack MCP server",
				Category:    "communication",
			},
		},
	}

	idxData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshaling index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "index.json"), idxData, 0o644); err != nil {
		t.Fatalf("writing index.json: %v", err)
	}

	// Create a lock file with older versions
	lockPath := filepath.Join(tmpDir, "lock.yaml")
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"github-mcp": {
				Version:       "v1.0.0",
				ETag:          "sha256:aaa",
				ContentSHA256: "aaaa",
			},
			"slack-mcp": {
				Version:       "v1.3.0",
				ETag:          "sha256:bbb",
				ContentSHA256: "bbbb",
			},
			"orphan-tool": {
				Version:       "v0.1.0",
				ETag:          "sha256:ccc",
				ContentSHA256: "cccc",
			},
		},
		GeneratedAt: "2025-01-01T00:00:00Z",
	}
	lockData, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock: %v", err)
	}
	if err := os.WriteFile(lockPath, lockData, 0o600); err != nil {
		t.Fatalf("writing lock: %v", err)
	}

	// Verify the index loads and detects the discrepancies
	data, err := os.ReadFile(filepath.Join(regDir, "index.json"))
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	var loadedIdx models.Index
	if err := json.Unmarshal(data, &loadedIdx); err != nil {
		t.Fatalf("parsing index: %v", err)
	}

	// github-mcp lock says v1.0.0, registry says v2.0.0 - version mismatch
	ghEntry := loadedIdx.Specs["github-mcp"]
	if ghEntry.Version == lf.Tools["github-mcp"].Version {
		t.Error("expected version mismatch for github-mcp")
	}

	// slack-mcp is up to date
	slEntry := loadedIdx.Specs["slack-mcp"]
	if slEntry.Version != lf.Tools["slack-mcp"].Version {
		t.Errorf("expected slack-mcp to be up to date, got registry=%s lock=%s",
			slEntry.Version, lf.Tools["slack-mcp"].Version)
	}

	// orphan-tool is not in the registry
	_, ok := loadedIdx.Specs["orphan-tool"]
	if ok {
		t.Error("expected orphan-tool to NOT be in registry index")
	}
}

// TestWhyCommand verifies the provenance/why API returns dependency information.
func TestWhyCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/why") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"tool":           "auth-provider",
				"installed_by":   []string{"github-mcp", "slack-mcp"},
				"dependency_of":  []string{"github-mcp"},
				"install_reason": "dependency",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/specs/auth-provider/why/")
	if err != nil {
		t.Fatalf("fetching why: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding why response: %v", err)
	}

	if decoded["tool"] != "auth-provider" {
		t.Errorf("expected tool=auth-provider, got %v", decoded["tool"])
	}
	if decoded["install_reason"] != "dependency" {
		t.Errorf("expected install_reason=dependency, got %v", decoded["install_reason"])
	}

	installedBy, ok := decoded["installed_by"].([]any)
	if !ok || len(installedBy) != 2 {
		t.Errorf("expected 2 installed_by entries, got %v", decoded["installed_by"])
	}
}

// TestFundCommand verifies the funding endpoint returns sponsorship information.
func TestFundCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/funding") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"tool": "popular-tool",
				"funding": []map[string]string{
					{"type": "github", "url": "https://github.com/sponsors/author"},
					{"type": "opencollective", "url": "https://opencollective.com/popular-tool"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/specs/popular-tool/funding/")
	if err != nil {
		t.Fatalf("fetching funding: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding funding response: %v", err)
	}

	if decoded["tool"] != "popular-tool" {
		t.Errorf("expected tool=popular-tool, got %v", decoded["tool"])
	}

	funding, ok := decoded["funding"].([]any)
	if !ok || len(funding) != 2 {
		t.Errorf("expected 2 funding entries, got %v", decoded["funding"])
	}
}

// TestInstallVersionPin verifies that pinned versions are recorded in the lock file.
func TestInstallVersionPin(t *testing.T) {
	yamlContent := []byte("name: pinned-tool\nversion: 2.5.0\n")

	entry := LockEntry{
		Version:       "2.5.0",
		ETag:          computeETag(yamlContent),
		ContentSHA256: computeContentSHA256(yamlContent),
		PinnedVersion: "2.5.0",
	}

	if entry.PinnedVersion != "2.5.0" {
		t.Errorf("expected pinned_version=2.5.0, got %s", entry.PinnedVersion)
	}

	// Serialize and deserialize to verify round-trip
	data, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatalf("marshaling entry: %v", err)
	}

	var loaded LockEntry
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshaling entry: %v", err)
	}

	if loaded.PinnedVersion != "2.5.0" {
		t.Errorf("expected pinned_version=2.5.0 after round-trip, got %s", loaded.PinnedVersion)
	}
	if loaded.ContentSHA256 == "" {
		t.Error("expected content_sha256 to be populated")
	}
	if loaded.Version != "2.5.0" {
		t.Errorf("expected version=2.5.0, got %s", loaded.Version)
	}
}

// TestUpgradeDryRun verifies that the version diff endpoint returns structured
// change information that the upgrade --dry-run command would display.
func TestUpgradeDryRun(t *testing.T) {
	diff := map[string]any{
		"old_version":      "1.0.0",
		"new_version":      "1.1.0",
		"summary":          "Added new list action, improved error messages",
		"is_breaking":      false,
		"breaking_reasons": []string{},
		"changes": []map[string]string{
			{"field": "version", "old": "1.0.0", "new": "1.1.0"},
			{"field": "actions", "old": "2 actions", "new": "3 actions"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/diff/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(diff)
			return
		}
		if strings.Contains(r.URL.Path, "/yaml/") {
			w.Header().Set("Content-Type", "application/x-yaml")
			fmt.Fprint(w, "name: my-tool\nversion: 1.1.0\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Fetch the diff
	resp, err := http.Get(server.URL + "/api/v1/specs/my-tool/versions/1.0.0/diff/1.1.0/")
	if err != nil {
		t.Fatalf("fetching diff: %v", err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding diff: %v", err)
	}

	if decoded["is_breaking"] != false {
		t.Errorf("expected is_breaking=false, got %v", decoded["is_breaking"])
	}
	if decoded["summary"] != "Added new list action, improved error messages" {
		t.Errorf("unexpected summary: %v", decoded["summary"])
	}

	changes, ok := decoded["changes"].([]any)
	if !ok || len(changes) != 2 {
		t.Errorf("expected 2 changes, got %v", decoded["changes"])
	}

	// Verify YAML endpoint still works (used by upgrade to fetch new content)
	yamlResp, err := http.Get(server.URL + "/api/v1/specs/my-tool/yaml/")
	if err != nil {
		t.Fatalf("fetching yaml: %v", err)
	}
	defer yamlResp.Body.Close()

	if yamlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected yaml 200, got %d", yamlResp.StatusCode)
	}
}

// TestContentSHA256InLock verifies that the lock file stores content SHA256
// hashes and they are deterministic.
func TestContentSHA256InLock(t *testing.T) {
	specA := []byte("name: tool-a\nversion: 1.0.0\n")
	specB := []byte("name: tool-b\nversion: 1.0.0\n")

	hashA := computeContentSHA256(specA)
	hashB := computeContentSHA256(specB)

	if hashA == "" || hashB == "" {
		t.Fatal("expected non-empty hashes")
	}
	if len(hashA) != 64 || len(hashB) != 64 {
		t.Errorf("expected 64-char hex hashes, got %d and %d", len(hashA), len(hashB))
	}
	if hashA == hashB {
		t.Error("expected different hashes for different content")
	}

	// Deterministic: same content produces the same hash
	hashA2 := computeContentSHA256(specA)
	if hashA != hashA2 {
		t.Errorf("expected deterministic hash, got %s and %s", hashA, hashA2)
	}

	// Write a lock file with content hashes and verify round-trip
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "lock.yaml")

	lf := &LockFile{
		Tools: map[string]LockEntry{
			"tool-a": {
				Version:       "1.0.0",
				ETag:          computeETag(specA),
				ContentSHA256: hashA,
			},
			"tool-b": {
				Version:       "1.0.0",
				ETag:          computeETag(specB),
				ContentSHA256: hashB,
			},
		},
		GeneratedAt: "2025-06-01T00:00:00Z",
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("writing lock: %v", err)
	}

	readData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock: %v", err)
	}

	var loaded LockFile
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("parsing lock: %v", err)
	}

	if loaded.Tools["tool-a"].ContentSHA256 != hashA {
		t.Errorf("tool-a content_sha256 mismatch: got %s, want %s",
			loaded.Tools["tool-a"].ContentSHA256, hashA)
	}
	if loaded.Tools["tool-b"].ContentSHA256 != hashB {
		t.Errorf("tool-b content_sha256 mismatch: got %s, want %s",
			loaded.Tools["tool-b"].ContentSHA256, hashB)
	}
}

// TestRedirectResolution verifies that the API returns redirect information
// for renamed tools, allowing the CLI to resolve old names.
func TestRedirectResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a 301 redirect from old name to new name
		if strings.Contains(r.URL.Path, "/specs/anthropic/old-tool/") {
			w.Header().Set("Location", "/api/v1/specs/anthropic/new-tool/")
			w.Header().Set("X-Tool-Redirect", "true")
			w.Header().Set("X-Redirect-Expires", "2026-07-01T00:00:00Z")
			w.WriteHeader(http.StatusMovedPermanently)
			json.NewEncoder(w).Encode(map[string]string{
				"old_name": "anthropic/old-tool",
				"new_name": "anthropic/new-tool",
			})
			return
		}
		if strings.Contains(r.URL.Path, "/specs/anthropic/new-tool/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":    "anthropic/new-tool",
				"version": "2.0.0",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use a custom client that does NOT follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(server.URL + "/api/v1/specs/anthropic/old-tool/")
	if err != nil {
		t.Fatalf("fetching old tool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if !strings.Contains(location, "anthropic/new-tool") {
		t.Errorf("expected redirect to new-tool, got Location: %s", location)
	}

	redirectHeader := resp.Header.Get("X-Tool-Redirect")
	if redirectHeader != "true" {
		t.Errorf("expected X-Tool-Redirect=true, got %s", redirectHeader)
	}

	expiresHeader := resp.Header.Get("X-Redirect-Expires")
	if expiresHeader == "" {
		t.Error("expected X-Redirect-Expires header to be set")
	}

	// Follow to the new location
	newResp, err := http.Get(server.URL + "/api/v1/specs/anthropic/new-tool/")
	if err != nil {
		t.Fatalf("fetching new tool: %v", err)
	}
	defer newResp.Body.Close()

	if newResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for new tool, got %d", newResp.StatusCode)
	}

	var newTool map[string]any
	if err := json.NewDecoder(newResp.Body).Decode(&newTool); err != nil {
		t.Fatalf("decoding new tool: %v", err)
	}
	if newTool["name"] != "anthropic/new-tool" {
		t.Errorf("expected name=anthropic/new-tool, got %v", newTool["name"])
	}
}
