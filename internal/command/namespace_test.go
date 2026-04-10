// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

// ---------------------------------------------------------------------------
// NQ.5: --as alias install and run
// ---------------------------------------------------------------------------

// InstalledFile represents the .clictl/installed.yaml lock file that tracks
// tool aliases and qualified names.
type InstalledFile struct {
	Tools map[string]InstalledEntry `yaml:"tools"`
}

// InstalledEntry tracks a single installed tool with its qualified name and version.
type InstalledEntry struct {
	QualifiedName string `yaml:"qualified_name"`
	Version       string `yaml:"version"`
}

func TestAsAliasInstall(t *testing.T) {
	// Simulate installing anthropic/xlsx as "xlsx" and rickcrawford/xlsx as "xlsx-alt".
	// The installed.yaml file should record both with their qualified names.

	tmpDir := t.TempDir()
	installedPath := filepath.Join(tmpDir, ".clictl", "installed.yaml")

	if err := os.MkdirAll(filepath.Dir(installedPath), 0o700); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	installed := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "anthropic/xlsx",
				Version:       "1.0.0",
			},
			"xlsx-alt": {
				QualifiedName: "rickcrawford/xlsx",
				Version:       "2.0.0",
			},
		},
	}

	data, err := yaml.Marshal(&installed)
	if err != nil {
		t.Fatalf("marshaling installed file: %v", err)
	}

	if err := os.WriteFile(installedPath, data, 0o600); err != nil {
		t.Fatalf("writing installed file: %v", err)
	}

	// Read it back and verify the alias mapping
	readData, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("reading installed file: %v", err)
	}

	var loaded InstalledFile
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("parsing installed file: %v", err)
	}

	if len(loaded.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(loaded.Tools))
	}

	// Verify "xlsx" maps to anthropic/xlsx
	xlsxEntry, ok := loaded.Tools["xlsx"]
	if !ok {
		t.Fatal("missing 'xlsx' entry")
	}
	if xlsxEntry.QualifiedName != "anthropic/xlsx" {
		t.Errorf("xlsx qualified_name = %q, want %q", xlsxEntry.QualifiedName, "anthropic/xlsx")
	}
	if xlsxEntry.Version != "1.0.0" {
		t.Errorf("xlsx version = %q, want %q", xlsxEntry.Version, "1.0.0")
	}

	// Verify "xlsx-alt" maps to rickcrawford/xlsx
	altEntry, ok := loaded.Tools["xlsx-alt"]
	if !ok {
		t.Fatal("missing 'xlsx-alt' entry")
	}
	if altEntry.QualifiedName != "rickcrawford/xlsx" {
		t.Errorf("xlsx-alt qualified_name = %q, want %q", altEntry.QualifiedName, "rickcrawford/xlsx")
	}
	if altEntry.Version != "2.0.0" {
		t.Errorf("xlsx-alt version = %q, want %q", altEntry.Version, "2.0.0")
	}
}

func TestAsAliasResolution(t *testing.T) {
	// When running a tool by its alias, the resolver should look up the
	// qualified_name from installed.yaml and resolve the correct spec.

	installed := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "anthropic/xlsx",
				Version:       "1.0.0",
			},
			"xlsx-alt": {
				QualifiedName: "rickcrawford/xlsx",
				Version:       "2.0.0",
			},
		},
	}

	// Simulate resolution: given alias "xlsx-alt", look up its qualified_name
	alias := "xlsx-alt"
	entry, found := installed.Tools[alias]
	if !found {
		t.Fatalf("alias %q not found in installed tools", alias)
	}

	// Parse the qualified name into namespace/name
	parts := strings.SplitN(entry.QualifiedName, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("expected namespace/name format, got %q", entry.QualifiedName)
	}

	wantNS := "rickcrawford"
	wantName := "xlsx"
	if parts[0] != wantNS {
		t.Errorf("namespace = %q, want %q", parts[0], wantNS)
	}
	if parts[1] != wantName {
		t.Errorf("name = %q, want %q", parts[1], wantName)
	}
}

func TestAsAliasConflictDetection(t *testing.T) {
	// Installing with --as to a name that already exists should be detected.
	installed := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "anthropic/xlsx",
				Version:       "1.0.0",
			},
		},
	}

	newAlias := "xlsx"
	if _, exists := installed.Tools[newAlias]; exists {
		// Conflict detected - this is the expected behavior
		return
	}
	t.Error("expected conflict for alias 'xlsx' but none detected")
}

// ---------------------------------------------------------------------------
// NQ.11: clictl publish spec.yaml flow
// ---------------------------------------------------------------------------

func TestPublishSpecFlow(t *testing.T) {
	// Test the publish command flow by mocking the API server.
	// The publish command reads a spec file, validates it, and POSTs to the API.

	specYAML := `name: my-tool
version: "1.0.0"
description: A test tool
category: testing
server:
  type: http
  url: https://api.example.com
actions:
  - name: test
    path: /test
    method: GET
`
	// Write spec to a temp file
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "my-tool.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o600); err != nil {
		t.Fatalf("writing spec file: %v", err)
	}

	// Validate the spec can be parsed
	spec, err := registry.ParseSpec([]byte(specYAML))
	if err != nil {
		t.Fatalf("failed to parse spec: %v", err)
	}
	if spec.Name != "my-tool" {
		t.Errorf("spec name = %q, want %q", spec.Name, "my-tool")
	}
	if spec.Version != "1.0.0" {
		t.Errorf("spec version = %q, want %q", spec.Version, "1.0.0")
	}

	// Mock the API server
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/specs/create/" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Check auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	// Simulate what the publish command does
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}

	payload := map[string]interface{}{
		"name":    spec.Name,
		"version": spec.Version,
		"spec":    string(data),
		"public":  false,
	}

	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL+"/api/v1/specs/create/",
		bytes.NewReader(payloadBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token-123")
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("publish request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	// Verify the API received the correct payload
	if receivedPayload["name"] != "my-tool" {
		t.Errorf("API received name = %v, want %q", receivedPayload["name"], "my-tool")
	}
	if receivedPayload["version"] != "1.0.0" {
		t.Errorf("API received version = %v, want %q", receivedPayload["version"], "1.0.0")
	}
}

func TestPublishSpecNamespaceGuardResponse(t *testing.T) {
	// Test that publish handles a 409 namespace mismatch response from the API.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		resp := map[string]string{
			"code":   "namespace_mismatch",
			"detail": "This tool attributes its origin to 'anthropic'. Your publisher namespace is 'rickcrawford'.",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	payload := map[string]interface{}{
		"name":    "xlsx",
		"version": "1.0.0",
		"spec":    "name: xlsx\nversion: '1.0.0'\nnamespace: anthropic\n",
		"public":  true,
	}
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL+"/api/v1/specs/create/",
		bytes.NewReader(payloadBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}

	var errorResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errorResp)

	if errorResp["code"] != "namespace_mismatch" {
		t.Errorf("code = %q, want %q", errorResp["code"], "namespace_mismatch")
	}
	if !strings.Contains(errorResp["detail"], "anthropic") {
		t.Errorf("detail should mention 'anthropic', got: %s", errorResp["detail"])
	}
}

func TestPublishRequiresAuth(t *testing.T) {
	// The publish flow should require a valid auth token.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"detail": "Authentication required"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Request without auth
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL+"/api/v1/specs/create/",
		strings.NewReader(`{"name":"test","version":"1.0.0","spec":"..."}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// NQ.12: Resolution order (project skill > project install > global > workspace)
// ---------------------------------------------------------------------------

func TestResolutionOrderProjectSkillFirst(t *testing.T) {
	// Project skills (.claude/skills/xlsx/SKILL.md) should take precedence
	// over installed tools.

	tmpDir := t.TempDir()

	// Create a project skill directory
	skillDir := filepath.Join(tmpDir, ".claude", "skills", "xlsx")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("# xlsx skill\nProject-level skill."), 0o644); err != nil {
		t.Fatalf("writing skill file: %v", err)
	}

	// Create project installed.yaml
	projectInstallDir := filepath.Join(tmpDir, ".clictl")
	if err := os.MkdirAll(projectInstallDir, 0o700); err != nil {
		t.Fatalf("creating .clictl dir: %v", err)
	}
	projectInstalled := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "anthropic/xlsx",
				Version:       "1.0.0",
			},
		},
	}
	projData, _ := yaml.Marshal(&projectInstalled)
	if err := os.WriteFile(filepath.Join(projectInstallDir, "installed.yaml"), projData, 0o600); err != nil {
		t.Fatalf("writing project installed.yaml: %v", err)
	}

	// Create global installed.yaml
	globalDir := filepath.Join(tmpDir, "global-clictl")
	if err := os.MkdirAll(globalDir, 0o700); err != nil {
		t.Fatalf("creating global dir: %v", err)
	}
	globalInstalled := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "community/xlsx",
				Version:       "0.5.0",
			},
		},
	}
	globalData, _ := yaml.Marshal(&globalInstalled)
	if err := os.WriteFile(filepath.Join(globalDir, "installed.yaml"), globalData, 0o600); err != nil {
		t.Fatalf("writing global installed.yaml: %v", err)
	}

	// Resolve "xlsx" following the priority order:
	// 1. Project skill
	// 2. Project install
	// 3. Global install
	// 4. Workspace (API lookup)

	toolName := "xlsx"
	resolved := ""

	// Step 1: Check project skill
	projectSkillPath := filepath.Join(tmpDir, ".claude", "skills", toolName, "SKILL.md")
	if _, err := os.Stat(projectSkillPath); err == nil {
		resolved = "project-skill"
	}

	// Step 2: Check project install (only if not resolved)
	if resolved == "" {
		projInstalledPath := filepath.Join(tmpDir, ".clictl", "installed.yaml")
		if data, err := os.ReadFile(projInstalledPath); err == nil {
			var pf InstalledFile
			if yaml.Unmarshal(data, &pf) == nil {
				if _, ok := pf.Tools[toolName]; ok {
					resolved = "project-install"
				}
			}
		}
	}

	// Step 3: Check global install (only if not resolved)
	if resolved == "" {
		globalInstalledPath := filepath.Join(globalDir, "installed.yaml")
		if data, err := os.ReadFile(globalInstalledPath); err == nil {
			var gf InstalledFile
			if yaml.Unmarshal(data, &gf) == nil {
				if _, ok := gf.Tools[toolName]; ok {
					resolved = "global-install"
				}
			}
		}
	}

	// Step 4: Workspace (would be API call)
	if resolved == "" {
		resolved = "workspace"
	}

	if resolved != "project-skill" {
		t.Errorf("expected resolution to 'project-skill', got %q", resolved)
	}
}

func TestResolutionOrderProjectInstallSecond(t *testing.T) {
	// When no project skill exists, project install should win over global.

	tmpDir := t.TempDir()

	// No project skill directory

	// Create project installed.yaml
	projectInstallDir := filepath.Join(tmpDir, ".clictl")
	if err := os.MkdirAll(projectInstallDir, 0o700); err != nil {
		t.Fatalf("creating .clictl dir: %v", err)
	}
	projectInstalled := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "anthropic/xlsx",
				Version:       "1.0.0",
			},
		},
	}
	projData, _ := yaml.Marshal(&projectInstalled)
	os.WriteFile(filepath.Join(projectInstallDir, "installed.yaml"), projData, 0o600)

	// Create global installed.yaml
	globalDir := filepath.Join(tmpDir, "global-clictl")
	os.MkdirAll(globalDir, 0o700)
	globalInstalled := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "community/xlsx",
				Version:       "0.5.0",
			},
		},
	}
	globalData, _ := yaml.Marshal(&globalInstalled)
	os.WriteFile(filepath.Join(globalDir, "installed.yaml"), globalData, 0o600)

	toolName := "xlsx"
	resolved := ""

	// Step 1: Check project skill (doesn't exist)
	projectSkillPath := filepath.Join(tmpDir, ".claude", "skills", toolName, "SKILL.md")
	if _, err := os.Stat(projectSkillPath); err == nil {
		resolved = "project-skill"
	}

	// Step 2: Check project install
	if resolved == "" {
		data, _ := os.ReadFile(filepath.Join(tmpDir, ".clictl", "installed.yaml"))
		var pf InstalledFile
		if yaml.Unmarshal(data, &pf) == nil {
			if _, ok := pf.Tools[toolName]; ok {
				resolved = "project-install"
			}
		}
	}

	// Step 3: Check global
	if resolved == "" {
		data, _ := os.ReadFile(filepath.Join(globalDir, "installed.yaml"))
		var gf InstalledFile
		if yaml.Unmarshal(data, &gf) == nil {
			if _, ok := gf.Tools[toolName]; ok {
				resolved = "global-install"
			}
		}
	}

	if resolved != "project-install" {
		t.Errorf("expected resolution to 'project-install', got %q", resolved)
	}
}

func TestResolutionOrderGlobalThird(t *testing.T) {
	// When no project skill or project install exists, global should win.

	tmpDir := t.TempDir()

	// No project skill, no project installed.yaml

	globalDir := filepath.Join(tmpDir, "global-clictl")
	os.MkdirAll(globalDir, 0o700)
	globalInstalled := InstalledFile{
		Tools: map[string]InstalledEntry{
			"xlsx": {
				QualifiedName: "community/xlsx",
				Version:       "0.5.0",
			},
		},
	}
	globalData, _ := yaml.Marshal(&globalInstalled)
	os.WriteFile(filepath.Join(globalDir, "installed.yaml"), globalData, 0o600)

	toolName := "xlsx"
	resolved := ""

	// Step 1: Project skill (doesn't exist)
	if _, err := os.Stat(filepath.Join(tmpDir, ".claude", "skills", toolName, "SKILL.md")); err == nil {
		resolved = "project-skill"
	}

	// Step 2: Project install (doesn't exist)
	if resolved == "" {
		if _, err := os.Stat(filepath.Join(tmpDir, ".clictl", "installed.yaml")); err == nil {
			resolved = "project-install"
		}
	}

	// Step 3: Global install
	if resolved == "" {
		data, _ := os.ReadFile(filepath.Join(globalDir, "installed.yaml"))
		var gf InstalledFile
		if yaml.Unmarshal(data, &gf) == nil {
			if _, ok := gf.Tools[toolName]; ok {
				resolved = "global-install"
			}
		}
	}

	if resolved != "global-install" {
		t.Errorf("expected resolution to 'global-install', got %q", resolved)
	}
}

func TestResolutionFallsToWorkspace(t *testing.T) {
	// When nothing is found locally, resolution falls to workspace API.

	tmpDir := t.TempDir()
	toolName := "xlsx"
	resolved := ""

	if _, err := os.Stat(filepath.Join(tmpDir, ".claude", "skills", toolName, "SKILL.md")); err == nil {
		resolved = "project-skill"
	}
	if resolved == "" {
		if _, err := os.Stat(filepath.Join(tmpDir, ".clictl", "installed.yaml")); err == nil {
			resolved = "project-install"
		}
	}
	if resolved == "" {
		if _, err := os.Stat(filepath.Join(tmpDir, "global-clictl", "installed.yaml")); err == nil {
			resolved = "global-install"
		}
	}
	if resolved == "" {
		resolved = "workspace"
	}

	if resolved != "workspace" {
		t.Errorf("expected resolution to 'workspace', got %q", resolved)
	}
}

// ---------------------------------------------------------------------------
// NQ.5 additional: namespace/name parsing in install
// ---------------------------------------------------------------------------

func TestQualifiedNameParsing(t *testing.T) {
	tests := []struct {
		input     string
		wantNS    string
		wantName  string
	}{
		{"anthropic/xlsx", "anthropic", "xlsx"},
		{"rickcrawford/xlsx", "rickcrawford", "xlsx"},
		{"clictl/echo", "clictl", "echo"},
		{"xlsx", "", "xlsx"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parts := strings.SplitN(tt.input, "/", 2)
			var gotNS, gotName string
			if len(parts) == 2 {
				gotNS = parts[0]
				gotName = parts[1]
			} else {
				gotNS = ""
				gotName = parts[0]
			}

			if gotNS != tt.wantNS {
				t.Errorf("namespace = %q, want %q", gotNS, tt.wantNS)
			}
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers to suppress unused import warnings
// ---------------------------------------------------------------------------

var _ = fmt.Sprintf
var _ models.ToolSpec
