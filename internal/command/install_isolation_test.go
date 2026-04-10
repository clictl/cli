// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

// ---------------------------------------------------------------------------
// Skill content building and allowed tools
// ---------------------------------------------------------------------------

func TestBuildSkillContent_AllowedToolsFrontmatter(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
		Sandbox:     &models.Sandbox{Commands: []string{"bash", "read"}},
	}

	content := buildSkillContent(spec, "claude-code")

	// Check frontmatter contains correct allowed-tools
	if !strings.Contains(content, "allowed-tools: [Bash, Read]") {
		t.Errorf("expected frontmatter with allowed-tools: [Bash, Read], got:\n%s", content)
	}
}

func TestBuildSkillContent_DefaultAllowedTools(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
		// RequiresTools is nil - should default to Bash, Read, Write
	}

	content := buildSkillContent(spec, "claude-code")

	if !strings.Contains(content, "allowed-tools: [Bash, Read, Write]") {
		t.Errorf("expected default allowed-tools: [Bash, Read, Write], got:\n%s", content)
	}
}

func TestBuildSkillContent_ToolRestrictionsForCodex(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
		Sandbox:     &models.Sandbox{Commands: []string{"read", "write"}},
	}

	content := buildSkillContent(spec, "codex")

	if !strings.Contains(content, "## Tool Restrictions") {
		t.Error("expected Tool Restrictions section for codex target")
	}
	if !strings.Contains(content, "Read, Write") {
		t.Errorf("expected tool list in restrictions section, got:\n%s", content)
	}
	if !strings.Contains(content, "IMPORTANT: Only use the tools listed above") {
		t.Error("expected codex-specific directive")
	}
}

func TestBuildSkillContent_NoToolRestrictionsForClaudeCode(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
		Sandbox:     &models.Sandbox{Commands: []string{"bash", "read"}},
	}

	content := buildSkillContent(spec, "claude-code")

	// Claude Code should NOT have a Tool Restrictions section (uses frontmatter instead)
	if strings.Contains(content, "## Tool Restrictions") {
		t.Error("claude-code should not have a Tool Restrictions markdown section")
	}
}

func TestBuildSkillContent_FilesystemScope(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
		Sandbox: &models.Sandbox{
			Commands: []string{"read", "write"},
			Filesystem: &models.FilesystemPermissions{
				Read:  []string{"./src", "./tests"},
				Write: []string{"./src"},
			},
		},
	}

	content := buildSkillContent(spec, "cursor")

	if !strings.Contains(content, "## Filesystem Scope") {
		t.Error("expected Filesystem Scope section")
	}
	if !strings.Contains(content, "Read access: ./src, ./tests") {
		t.Errorf("expected read access paths in content, got:\n%s", content)
	}
	if !strings.Contains(content, "Write access: ./src") {
		t.Errorf("expected write access paths in content, got:\n%s", content)
	}
}

func TestBuildSkillContent_NoFilesystemScopeWhenEmpty(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Description: "A test tool",
		Version:     "1.0.0",
		Category:    "testing",
	}

	content := buildSkillContent(spec, "cursor")

	if strings.Contains(content, "## Filesystem Scope") {
		t.Error("expected no Filesystem Scope section when permissions are nil")
	}
}

func TestResolveAllowedTools_Empty(t *testing.T) {
	spec := &models.ToolSpec{}
	tools := resolveAllowedTools(spec)

	if len(tools) != 3 {
		t.Fatalf("expected 3 default tools, got %d", len(tools))
	}
	if tools[0] != "Bash" || tools[1] != "Read" || tools[2] != "Write" {
		t.Errorf("unexpected defaults: %v", tools)
	}
}

func TestResolveAllowedTools_Capitalization(t *testing.T) {
	spec := &models.ToolSpec{
		Sandbox: &models.Sandbox{Commands: []string{"bash", "read"}},
	}
	tools := resolveAllowedTools(spec)

	if tools[0] != "Bash" || tools[1] != "Read" {
		t.Errorf("expected capitalized tools, got: %v", tools)
	}
}

func TestFormatToolRestrictionSummary_WithRestrictions(t *testing.T) {
	spec := &models.ToolSpec{
		Sandbox: &models.Sandbox{Commands: []string{"read", "write"}},
	}

	summary := formatToolRestrictionSummary(spec)

	if !strings.Contains(summary, "Read, Write") {
		t.Errorf("expected allowed tools in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "bash not available") {
		t.Errorf("expected bash not available in summary, got: %s", summary)
	}
}

func TestFormatToolRestrictionSummary_NoRestrictions(t *testing.T) {
	spec := &models.ToolSpec{}
	summary := formatToolRestrictionSummary(spec)

	if !strings.Contains(summary, "No tool restrictions") {
		t.Errorf("expected 'No tool restrictions', got: %s", summary)
	}
}

func TestFormatFilesystemScopeSummary(t *testing.T) {
	spec := &models.ToolSpec{
		Sandbox: &models.Sandbox{
			Filesystem: &models.FilesystemPermissions{
				Read:  []string{"./src", "./tests"},
				Write: []string{"./src"},
			},
		},
	}

	summary := formatFilesystemScopeSummary(spec)

	if !strings.Contains(summary, "Read access: ./src, ./tests") {
		t.Errorf("expected read access in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "Write access: ./src") {
		t.Errorf("expected write access in summary, got: %s", summary)
	}
}

func TestFormatFilesystemScopeSummary_NilPermissions(t *testing.T) {
	spec := &models.ToolSpec{}
	summary := formatFilesystemScopeSummary(spec)

	if summary != "No restrictions declared" {
		t.Errorf("expected 'No restrictions declared', got: %s", summary)
	}
}

// ---------------------------------------------------------------------------
// Claude settings merge and removal
// ---------------------------------------------------------------------------

func TestMergeClaudeSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	fs := &models.FilesystemPermissions{
		Read:  []string{"./src", "./tests"},
		Write: []string{"./src"},
	}

	if err := mergeClaudeSettings("test-skill", fs); err != nil {
		t.Fatalf("mergeClaudeSettings failed: %v", err)
	}

	// Read the settings file
	data, err := os.ReadFile(filepath.Join(".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}

	skillPerms, ok := settings["skill_permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("expected skill_permissions key in settings")
	}

	entry, ok := skillPerms["test-skill"].(map[string]interface{})
	if !ok {
		t.Fatal("expected test-skill entry in skill_permissions")
	}

	readPaths, ok := entry["read"].([]interface{})
	if !ok || len(readPaths) != 2 {
		t.Errorf("expected 2 read paths, got: %v", entry["read"])
	}

	writePaths, ok := entry["write"].([]interface{})
	if !ok || len(writePaths) != 1 {
		t.Errorf("expected 1 write path, got: %v", entry["write"])
	}
}

func TestMergeClaudeSettings_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create existing settings
	os.MkdirAll(".claude", 0o755)
	existing := map[string]interface{}{
		"other_key": "other_value",
		"skill_permissions": map[string]interface{}{
			"existing-skill": map[string]interface{}{
				"read": []string{"./lib"},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(".claude", "settings.json"), data, 0o644)

	// Merge new skill
	fs := &models.FilesystemPermissions{
		Read: []string{"./src"},
	}
	if err := mergeClaudeSettings("new-skill", fs); err != nil {
		t.Fatalf("mergeClaudeSettings failed: %v", err)
	}

	// Read back
	data, _ = os.ReadFile(filepath.Join(".claude", "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(data, &settings)

	// Verify existing data preserved
	if settings["other_key"] != "other_value" {
		t.Error("existing key was not preserved")
	}

	skillPerms := settings["skill_permissions"].(map[string]interface{})
	if _, ok := skillPerms["existing-skill"]; !ok {
		t.Error("existing skill was not preserved")
	}
	if _, ok := skillPerms["new-skill"]; !ok {
		t.Error("new skill was not added")
	}
}

func TestRemoveClaudeSettingsSkill(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create settings with two skills
	os.MkdirAll(".claude", 0o755)
	existing := map[string]interface{}{
		"skill_permissions": map[string]interface{}{
			"skill-a": map[string]interface{}{"read": []string{"./a"}},
			"skill-b": map[string]interface{}{"read": []string{"./b"}},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(".claude", "settings.json"), data, 0o644)

	// Remove skill-a
	removeClaudeSettingsSkill("skill-a")

	// Read back
	data, _ = os.ReadFile(filepath.Join(".claude", "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(data, &settings)

	skillPerms := settings["skill_permissions"].(map[string]interface{})
	if _, ok := skillPerms["skill-a"]; ok {
		t.Error("skill-a should have been removed")
	}
	if _, ok := skillPerms["skill-b"]; !ok {
		t.Error("skill-b should have been preserved")
	}
}

// ---------------------------------------------------------------------------
// Cursor and Windsurf settings
// ---------------------------------------------------------------------------

func TestGenerateCursorSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := generateCursorSettings("test-skill", []string{"Bash", "Read"}); err != nil {
		t.Fatalf("generateCursorSettings failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(".cursor", "settings", "test-skill.json"))
	if err != nil {
		t.Fatalf("reading cursor settings: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing cursor settings: %v", err)
	}

	if settings["skill"] != "test-skill" {
		t.Errorf("expected skill name 'test-skill', got: %v", settings["skill"])
	}

	tools, ok := settings["allowed_tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Errorf("expected 2 allowed tools, got: %v", settings["allowed_tools"])
	}
}

func TestRemoveCursorSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create settings file
	generateCursorSettings("test-skill", []string{"Bash"})

	path := filepath.Join(".cursor", "settings", "test-skill.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file should exist before removal")
	}

	removeCursorSettings("test-skill")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("settings file should have been removed")
	}
}

func TestGenerateWindsurfSettings(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if err := generateWindsurfSettings("test-skill", []string{"Read", "Write"}); err != nil {
		t.Fatalf("generateWindsurfSettings failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(".windsurf", "settings", "test-skill.json"))
	if err != nil {
		t.Fatalf("reading windsurf settings: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing windsurf settings: %v", err)
	}

	if settings["skill"] != "test-skill" {
		t.Errorf("expected skill name 'test-skill', got: %v", settings["skill"])
	}
}

// ---------------------------------------------------------------------------
// Skill overrides and workspace isolation
// ---------------------------------------------------------------------------

func TestLoadSkillOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheDir := filepath.Join(home, ".clictl", "workspace-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	overrides := []SkillOverride{
		{
			ToolName:       "github",
			FilesystemRead: []string{"."},
			BashAllow:      []string{"git *"},
			Blocked:        false,
		},
		{
			ToolName: "blocked-tool",
			Blocked:  true,
		},
	}

	data, err := json.Marshal(overrides)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cacheDir, "test-ws-overrides.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadSkillOverrides("test-ws")
	if err != nil {
		t.Fatalf("loadSkillOverrides failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 overrides, got %d", len(loaded))
	}

	if loaded[0].ToolName != "github" {
		t.Errorf("expected first override for github, got %s", loaded[0].ToolName)
	}

	if loaded[1].Blocked != true {
		t.Error("expected second override to be blocked")
	}
}

func TestFindSkillOverride(t *testing.T) {
	overrides := []SkillOverride{
		{ToolName: "github", BashAllow: []string{"git *"}},
		{ToolName: "stripe", Blocked: true},
	}

	found := findSkillOverride(overrides, "github")
	if found == nil {
		t.Fatal("expected to find override for github")
	}
	if len(found.BashAllow) != 1 {
		t.Errorf("expected 1 bash allow pattern, got %d", len(found.BashAllow))
	}

	notFound := findSkillOverride(overrides, "nonexistent")
	if notFound != nil {
		t.Error("expected nil for nonexistent tool")
	}
}

func TestApplySkillOverride(t *testing.T) {
	tests := []struct {
		name          string
		spec          *models.ToolSpec
		override      *SkillOverride
		wantReadLen   int
		wantWriteLen  int
		wantCmdsLen   int
		wantNetLen    int
		wantUnchanged bool
	}{
		{
			name: "tightens filesystem",
			spec: &models.ToolSpec{
				Name: "test-tool",
				Sandbox: &models.Sandbox{
					Filesystem: &models.FilesystemPermissions{
						Read:  []string{".", "/tmp", "/home"},
						Write: []string{".", "/tmp"},
					},
					Network: &models.NetworkPermissions{
						Allow: []string{"api.github.com", "cdn.github.com", "example.com"},
					},
				},
			},
			override: &SkillOverride{
				ToolName:        "test-tool",
				FilesystemRead:  []string{".", "/tmp"},
				FilesystemWrite: []string{"."},
				Network:         []string{"api.github.com", "cdn.github.com"},
			},
			wantReadLen:  2,
			wantWriteLen: 1,
			wantNetLen:   2,
		},
		{
			name: "tightens bash allow",
			spec: &models.ToolSpec{
				Name: "test-tool",
				Sandbox: &models.Sandbox{
					Commands: []string{"git *", "npm run *", "make *"},
				},
			},
			override: &SkillOverride{
				ToolName:  "test-tool",
				BashAllow: []string{"git *", "npm run *"},
			},
			wantCmdsLen: 2,
		},
		{
			name: "nil override leaves spec unchanged",
			spec: &models.ToolSpec{
				Name: "test-tool",
				Sandbox: &models.Sandbox{
					Commands: []string{"git *"},
				},
			},
			override:      nil,
			wantUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applySkillOverride(tt.spec, tt.override)

			if tt.wantUnchanged {
				if tt.spec.Sandbox == nil || len(tt.spec.Sandbox.Commands) != 1 {
					t.Error("spec should be unchanged")
				}
				return
			}

			if tt.wantReadLen > 0 {
				if len(tt.spec.Sandbox.Filesystem.Read) != tt.wantReadLen {
					t.Errorf("expected %d read paths, got %d: %v",
						tt.wantReadLen, len(tt.spec.Sandbox.Filesystem.Read), tt.spec.Sandbox.Filesystem.Read)
				}
			}
			if tt.wantWriteLen > 0 {
				if len(tt.spec.Sandbox.Filesystem.Write) != tt.wantWriteLen {
					t.Errorf("expected %d write paths, got %d: %v",
						tt.wantWriteLen, len(tt.spec.Sandbox.Filesystem.Write), tt.spec.Sandbox.Filesystem.Write)
				}
			}
			if tt.wantCmdsLen > 0 {
				cmds := []string{}
				if tt.spec.Sandbox != nil {
					cmds = tt.spec.Sandbox.Commands
				}
				if len(cmds) != tt.wantCmdsLen {
					t.Errorf("expected %d bash allow patterns, got %d: %v",
						tt.wantCmdsLen, len(cmds), cmds)
				}
			}
			if tt.wantNetLen > 0 {
				if len(tt.spec.Sandbox.Network.Allow) != tt.wantNetLen {
					t.Errorf("expected %d network allows, got %d", tt.wantNetLen, len(tt.spec.Sandbox.Network.Allow))
				}
			}
		})
	}
}

func TestBlockedSkillOverride(t *testing.T) {
	overrides := []SkillOverride{
		{ToolName: "blocked-tool", Blocked: true},
	}

	override := findSkillOverride(overrides, "blocked-tool")
	if override == nil {
		t.Fatal("expected to find override")
	}
	if !override.Blocked {
		t.Error("expected override to be blocked")
	}
}

// ---------------------------------------------------------------------------
// Skill sets
// ---------------------------------------------------------------------------

func TestLoadSkillSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheDir := filepath.Join(home, ".clictl", "workspace-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sets := []SkillSet{
		{
			Name:   "dev-tools",
			Skills: []string{"github", "stripe"},
			Locked: true,
			Overrides: []SkillOverride{
				{ToolName: "github", BashAllow: []string{"git *"}},
			},
		},
		{
			Name:   "analytics",
			Skills: []string{"mixpanel"},
			Locked: false,
		},
	}

	data, err := json.Marshal(sets)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cacheDir, "test-ws-skillsets.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadSkillSet("test-ws", "dev-tools")
	if err != nil {
		t.Fatalf("loadSkillSet failed: %v", err)
	}

	if loaded.Name != "dev-tools" {
		t.Errorf("expected name dev-tools, got %s", loaded.Name)
	}

	if len(loaded.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(loaded.Skills))
	}

	if !loaded.Locked {
		t.Error("expected skill set to be locked")
	}

	if len(loaded.Overrides) != 1 {
		t.Errorf("expected 1 override, got %d", len(loaded.Overrides))
	}

	// Test not found
	_, err = loadSkillSet("test-ws", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent skill set")
	}
}

// ---------------------------------------------------------------------------
// Bash filter hook generation
// ---------------------------------------------------------------------------

func TestGenerateBashFilterHook(t *testing.T) {
	dir := t.TempDir()

	patterns := []string{"npm run *", "git status"}
	err := generateBashFilterHook("test-skill", patterns, dir)
	if err != nil {
		t.Fatalf("generateBashFilterHook failed: %v", err)
	}

	hookPath := filepath.Join(dir, ".claude", "hooks", "clictl-bash-filter-test-skill.sh")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("could not read hook file: %v", err)
	}

	content := string(data)

	// Verify key content
	if len(content) == 0 {
		t.Error("hook file is empty")
	}

	// Check it's executable
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Error("hook file should be executable")
	}

	// Check it contains the skill name and patterns
	for _, want := range []string{"test-skill", "npm run *", "git status"} {
		if !strings.Contains(content, want) {
			t.Errorf("hook should contain %q", want)
		}
	}
}

func TestGenerateBashFilterHookContent(t *testing.T) {
	content := generateBashFilterHookContent("my-skill", []string{"go test *", "go build *"})

	for _, want := range []string{
		"#!/usr/bin/env bash",
		"my-skill",
		"exit 1",
		"exit 0",
		"not in skill allowlist",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("hook content should contain %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Audit events
// ---------------------------------------------------------------------------

func TestPostSkillAuditEvent(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		decoder := json.NewDecoder(r.Body)
		decoder.Decode(&receivedBody)

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	// Create temp config with the test server URL
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".clictl")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	configContent := "api_url: " + server.URL + "\nauth:\n  access_token: test-token-123\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	err := postSkillAuditEvent("my-workspace", "skill.installed", "github", map[string]interface{}{
		"version": "1.0.0",
	})
	if err != nil {
		t.Fatalf("postSkillAuditEvent failed: %v", err)
	}

	if receivedBody == nil {
		t.Fatal("server did not receive a request body")
	}

	if receivedBody["action"] != "skill.installed" {
		t.Errorf("expected action skill.installed, got %v", receivedBody["action"])
	}

	if receivedBody["target_type"] != "skill" {
		t.Errorf("expected target_type skill, got %v", receivedBody["target_type"])
	}

	if receivedBody["target_id"] != "github" {
		t.Errorf("expected target_id github, got %v", receivedBody["target_id"])
	}

	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("expected Bearer auth, got %q", receivedAuth)
	}
}

func TestPostSkillAuditEvent_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".clictl")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	configContent := "api_url: " + server.URL + "\nauth:\n  access_token: test-token\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	err := postSkillAuditEvent("my-workspace", "skill.installed", "github", map[string]interface{}{})
	if err == nil {
		t.Error("expected error for server 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// Path intersection
// ---------------------------------------------------------------------------

func TestIntersectPaths(t *testing.T) {
	tests := []struct {
		name     string
		spec     []string
		override []string
		want     int
	}{
		{"both populated", []string{"a", "b", "c"}, []string{"a", "c"}, 2},
		{"no overlap", []string{"a", "b"}, []string{"c", "d"}, 0},
		{"empty spec", nil, []string{"a"}, 0},
		{"full overlap", []string{"a", "b"}, []string{"a", "b"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersectPaths(tt.spec, tt.override)
			if len(got) != tt.want {
				t.Errorf("intersectPaths(%v, %v) returned %d items, want %d", tt.spec, tt.override, len(got), tt.want)
			}
		})
	}
}
