// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestToolNameValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"simple lowercase", "my-tool", false},
		{"alphanumeric", "tool123", false},
		{"underscore separator", "a_b", false},
		{"single char", "x", false},
		{"starts with digit", "3tool", false},
		{"mixed hyphens and underscores", "my-tool_v2", false},
		{"long valid name", "a-very-long-tool-name-that-is-valid", false},

		// Invalid names
		{"uppercase letter", "My-Tool", true},
		{"starts with hyphen", "-tool", true},
		{"starts with underscore", "_tool", true},
		{"contains exclamation", "tool!", true},
		{"contains space", "my tool", true},
		{"empty string", "", true},
		{"contains dot", "my.tool", true},
		{"contains slash", "ns/tool", true},
		{"contains at sign", "@tool", true},
		{"contains uppercase mid", "myTool", true},
		{"exceeds max length", strings.Repeat("a", 65), true},
		{"exactly max length", strings.Repeat("a", 64), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateToolName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("validateToolName(%q) = nil, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateToolName(%q) = %v, want nil", tt.input, err)
			}
		})
	}
}

func TestGooseConfigFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	serverEntry := map[string]interface{}{
		"command": "/usr/local/bin/clictl",
		"args":    []string{"mcp-serve", "github"},
	}

	configPath, err := writeGooseMCP("github", serverEntry)
	if err != nil {
		t.Fatalf("writeGooseMCP failed: %v", err)
	}

	// Verify path is under ~/.config/goose/
	expectedDir := filepath.Join(home, ".config", "goose")
	if !strings.HasPrefix(configPath, expectedDir) {
		t.Errorf("config path %q should be under %q", configPath, expectedDir)
	}
	if filepath.Base(configPath) != "config.yaml" {
		t.Errorf("expected config.yaml, got %q", filepath.Base(configPath))
	}

	// Read and verify YAML structure
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parsing YAML: %v", err)
	}

	extensions, ok := config["extensions"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected extensions key in YAML, got %v", config)
	}

	entry, ok := extensions["clictl-github"]
	if !ok {
		t.Fatalf("expected clictl-github in extensions, got keys: %v", extensions)
	}

	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for clictl-github, got %T", entry)
	}

	if entryMap["cmd"] != "/usr/local/bin/clictl" {
		t.Errorf("expected cmd '/usr/local/bin/clictl', got %v", entryMap["cmd"])
	}
}

func TestGooseConfigFormat_WithEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	serverEntry := map[string]interface{}{
		"command": "/usr/local/bin/clictl",
		"args":    []string{"mcp-serve", "stripe"},
		"env":     map[string]string{"STRIPE_API_KEY": "sk-test"},
	}

	_, err := writeGooseMCP("stripe", serverEntry)
	if err != nil {
		t.Fatalf("writeGooseMCP with env failed: %v", err)
	}

	configPath := filepath.Join(home, ".config", "goose", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	yaml.Unmarshal(data, &config)

	extensions := config["extensions"].(map[string]interface{})
	entry := extensions["clictl-stripe"].(map[string]interface{})

	envs, ok := entry["envs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected envs key in extension entry, got %v", entry)
	}

	if envs["STRIPE_API_KEY"] != "sk-test" {
		t.Errorf("expected STRIPE_API_KEY 'sk-test', got %v", envs["STRIPE_API_KEY"])
	}
}

func TestClineConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	serverEntry := map[string]interface{}{
		"command": "/usr/local/bin/clictl",
		"args":    []string{"mcp-serve", "github"},
	}

	configPath, err := writeClineMCP("github", serverEntry)
	if err != nil {
		t.Fatalf("writeClineMCP failed: %v", err)
	}

	// Verify config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("config file %q does not exist", configPath)
	}

	// Verify JSON structure
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	servers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mcpServers key, got %v", config)
	}

	entry, ok := servers["clictl-github"]
	if !ok {
		t.Fatalf("expected clictl-github in mcpServers")
	}

	entryMap := entry.(map[string]interface{})
	if entryMap["command"] != "/usr/local/bin/clictl" {
		t.Errorf("expected command, got %v", entryMap["command"])
	}

	// Verify the path contains the Cline extension ID
	if !strings.Contains(configPath, "saoudrizwan.claude-dev") {
		t.Errorf("expected Cline extension path, got %q", configPath)
	}
}

func TestAmazonQConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	serverEntry := map[string]interface{}{
		"command": "/usr/local/bin/clictl",
		"args":    []string{"mcp-serve", "github"},
	}

	configPath, err := writeAmazonQMCP("github", serverEntry)
	if err != nil {
		t.Fatalf("writeAmazonQMCP failed: %v", err)
	}

	// Verify path is under ~/.aws/amazonq/
	expectedDir := filepath.Join(home, ".aws", "amazonq")
	if !strings.HasPrefix(configPath, expectedDir) {
		t.Errorf("config path %q should be under %q", configPath, expectedDir)
	}
	if filepath.Base(configPath) != "mcp.json" {
		t.Errorf("expected mcp.json, got %q", filepath.Base(configPath))
	}

	// Verify JSON structure
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	servers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mcpServers key")
	}

	if _, ok := servers["clictl-github"]; !ok {
		t.Error("expected clictl-github in mcpServers")
	}
}

func TestFileLockingOnConfigWrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Write initial content
	err := lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		existing["mcpServers"] = map[string]interface{}{
			"clictl-tool-a": map[string]interface{}{
				"command": "clictl",
				"args":    []string{"mcp-serve", "tool-a"},
			},
		}
		return existing, nil
	})
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Second write should merge, not overwrite
	err = lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-tool-b"] = map[string]interface{}{
			"command": "clictl",
			"args":    []string{"mcp-serve", "tool-b"},
		}
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	// Verify both tools exist
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	servers := config["mcpServers"].(map[string]interface{})
	if _, ok := servers["clictl-tool-a"]; !ok {
		t.Error("expected clictl-tool-a to persist after second write")
	}
	if _, ok := servers["clictl-tool-b"]; !ok {
		t.Error("expected clictl-tool-b after second write")
	}

	// Verify lock file was cleaned up
	lockPath := configPath + ".lock"
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file %q should have been cleaned up", lockPath)
	}
}

func TestLockAndWriteJSON_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "config.json")

	err := lockAndWriteJSON(deepPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		existing["key"] = "value"
		return existing, nil
	})
	if err != nil {
		t.Fatalf("lockAndWriteJSON failed: %v", err)
	}

	data, err := os.ReadFile(deepPath)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	var config map[string]interface{}
	json.Unmarshal(data, &config)

	if config["key"] != "value" {
		t.Errorf("expected key 'value', got %v", config["key"])
	}
}

func TestSafeWriteSkillFile(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, ".claude", "skills", "test-tool")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	err := safeWriteSkillFile(skillPath, "# Test Skill\n\nThis is a test.")
	if err != nil {
		t.Fatalf("safeWriteSkillFile failed: %v", err)
	}

	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("reading skill file: %v", err)
	}

	if !strings.Contains(string(content), "Test Skill") {
		t.Errorf("expected skill content, got: %s", content)
	}

	// Verify permissions (0600)
	info, err := os.Stat(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestGooseSkillWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := writeGooseSkill("test-tool", "# Test Instructions\n\nUse this tool carefully.")
	if err != nil {
		t.Fatalf("writeGooseSkill failed: %v", err)
	}

	expectedPath := filepath.Join(home, ".config", "goose", "instructions.md")
	if path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading instructions: %v", err)
	}

	if !strings.Contains(string(content), "<!-- clictl:test-tool -->") {
		t.Error("expected clictl marker in instructions")
	}
	if !strings.Contains(string(content), "Test Instructions") {
		t.Error("expected skill content in instructions")
	}

	// Writing again should be idempotent
	path2, err := writeGooseSkill("test-tool", "# Test Instructions\n\nUse this tool carefully.")
	if err != nil {
		t.Fatalf("second writeGooseSkill failed: %v", err)
	}
	if path2 != path {
		t.Errorf("expected same path on second write")
	}

	content2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Should not have duplicate content
	count := strings.Count(string(content2), "<!-- clictl:test-tool -->")
	if count != 1 {
		t.Errorf("expected 1 marker, got %d", count)
	}
}

func TestRooCodeConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	serverEntry := map[string]interface{}{
		"command": "/usr/local/bin/clictl",
		"args":    []string{"mcp-serve", "github"},
	}

	configPath, err := writeRooCodeMCP("github", serverEntry)
	if err != nil {
		t.Fatalf("writeRooCodeMCP failed: %v", err)
	}

	// Verify path contains the Roo Code extension ID
	if !strings.Contains(configPath, "rooveterinaryinc.roo-cline") {
		t.Errorf("expected Roo Code extension path, got %q", configPath)
	}

	// Verify JSON structure
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	var config map[string]interface{}
	json.Unmarshal(data, &config)

	servers := config["mcpServers"].(map[string]interface{})
	if _, ok := servers["clictl-github"]; !ok {
		t.Error("expected clictl-github in mcpServers")
	}
}

func TestSkillTargetsMap(t *testing.T) {
	expectedTargets := []struct {
		name     string
		filename string
	}{
		{"claude-code", "SKILL.md"},
		{"gemini", "GEMINI.md"},
		{"codex", "AGENTS.md"},
		{"cursor", ".cursorrules"},
		{"windsurf", ".windsurfrules"},
		{"goose", ".goose-instructions.md"},
		{"cline", ".clinerules"},
		{"roo-code", ".roorules"},
		{"amazon-q", ".amazonq-rules"},
		{"boltai", ".boltai-rules"},
	}

	for _, tt := range expectedTargets {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := skillTargets[tt.name]
			if !ok {
				t.Fatalf("target %q not found in skillTargets", tt.name)
			}
			if target.filename != tt.filename {
				t.Errorf("target %q: expected filename %q, got %q", tt.name, tt.filename, target.filename)
			}
			if target.label == "" {
				t.Errorf("target %q: label should not be empty", tt.name)
			}
			dir := target.dir("test-tool")
			if dir == "" {
				t.Errorf("target %q: dir function returned empty string", tt.name)
			}
		})
	}
}

func TestBuildMCPServerEntry(t *testing.T) {
	entry := buildMCPServerEntry("github", nil)

	if entry["command"] == nil || entry["command"] == "" {
		t.Error("expected command in server entry")
	}

	args, ok := entry["args"].([]string)
	if !ok {
		t.Fatalf("expected args to be []string, got %T", entry["args"])
	}
	if len(args) != 2 || args[0] != "mcp-serve" || args[1] != "github" {
		t.Errorf("expected args [mcp-serve github], got %v", args)
	}

	if _, hasEnv := entry["env"]; hasEnv {
		t.Error("should not include env key when specEnv is nil")
	}
}
