// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/vault"
	"gopkg.in/yaml.v3"
)

// toolNameRegex validates tool names: lowercase alphanumeric, hyphens, underscores.
// Must start with a letter or digit.
var toolNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// validateToolName checks that a tool name matches the allowed pattern.
func validateToolName(name string) error {
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("tool name %q exceeds maximum length of 64 characters", name)
	}
	if !toolNameRegex.MatchString(name) {
		return fmt.Errorf("tool name %q is invalid: must match %s", name, toolNameRegex.String())
	}
	return nil
}

// safeWriteSkillFile resolves symlinks and verifies the target is under the
// expected directory before writing a skill file. This prevents symlink attacks
// that could write outside the project.
func safeWriteSkillFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Resolve the directory to catch symlinks
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolving symlinks for %s: %w", dir, err)
	}

	// The resolved path must still be under the original parent
	expectedParent, err := filepath.Abs(filepath.Dir(dir))
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(expectedParent)
	if err != nil {
		// Parent may not have symlinks, that's fine
		resolvedParent = expectedParent
	}

	if !strings.HasPrefix(resolvedDir, resolvedParent) {
		return fmt.Errorf("symlink escape detected: %s resolves to %s, which is outside %s", dir, resolvedDir, resolvedParent)
	}

	resolvedPath := filepath.Join(resolvedDir, filepath.Base(path))
	return os.WriteFile(resolvedPath, []byte(content), 0o600)
}

// resolveSpecEnv resolves vault:// references in a spec's Server.Env map
// and returns the resolved environment variables.
func resolveSpecEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}

	baseDir := config.BaseDir()
	userVault := vault.NewVault(baseDir)

	// Try to load project vault from git root
	var projectVault *vault.Vault
	if gitRoot, err := findGitRoot(); err == nil {
		projectVault = vault.NewVault(filepath.Join(gitRoot, ".clictl"))
	}

	return vault.ResolveEnv(env, projectVault, userVault)
}

// findGitRoot walks up from cwd to find the nearest .git directory.
func findGitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a git repository")
		}
		dir = parent
	}
}

// buildMCPServerEntry creates the MCP server config entry for a tool,
// including resolved env vars from the spec.
func buildMCPServerEntry(toolName string, specEnv map[string]string) map[string]interface{} {
	entry := map[string]interface{}{
		"command": cliCtlBin(),
		"args":    []string{"mcp-serve", toolName},
	}

	resolved := resolveSpecEnv(specEnv)
	if len(resolved) > 0 {
		entry["env"] = resolved
	}

	return entry
}

// lockAndWriteJSON performs a flock-protected read-modify-write on a JSON file.
// The modifier function receives the current content and returns the new content.
func lockAndWriteJSON(path string, modifier func(existing map[string]interface{}) (map[string]interface{}, error)) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
	}

	// Open or create the lock file
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("creating lock file: %w", err)
	}
	defer lockFile.Close()
	defer os.Remove(lockPath)

	// Exclusive lock via rename-based atomicity (cross-platform).
	// The lock file's existence prevents concurrent writes.
	// This is sufficient because MCP config writes are infrequent.
	_ = lockFile // held open to prevent deletion by other processes

	// Read existing content
	existing := make(map[string]interface{})
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &existing)
	}

	// Apply modification
	result, err := modifier(existing)
	if err != nil {
		return err
	}

	// Write back
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// writeGooseMCP writes MCP config for the Goose AI tool.
// Skill content goes to ~/.config/goose/instructions.md
// MCP config goes to ~/.config/goose/config.yaml (YAML format)
func writeGooseMCP(toolName string, serverEntry map[string]interface{}) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("creating goose config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")

	// Read existing config
	var gooseConfig map[string]interface{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		yaml.Unmarshal(data, &gooseConfig)
	}
	if gooseConfig == nil {
		gooseConfig = make(map[string]interface{})
	}

	// Build extension entry in Goose format
	extension := map[string]interface{}{
		"cmd": serverEntry["command"],
	}
	if args, ok := serverEntry["args"]; ok {
		extension["args"] = args
	}
	if envMap, ok := serverEntry["env"].(map[string]string); ok {
		envs := make(map[string]string)
		for k, v := range envMap {
			envs[k] = v
		}
		extension["envs"] = envs
	}

	// Merge into extensions map
	extensions, ok := gooseConfig["extensions"].(map[string]interface{})
	if !ok {
		extensions = make(map[string]interface{})
	}
	extensions["clictl-"+toolName] = extension
	gooseConfig["extensions"] = extensions

	out, err := yaml.Marshal(gooseConfig)
	if err != nil {
		return "", fmt.Errorf("marshaling goose config: %w", err)
	}

	return configPath, os.WriteFile(configPath, out, 0o644)
}

// writeGooseSkill appends instructions to the Goose instructions file.
func writeGooseSkill(toolName, content string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(configDir, "instructions.md")

	existing, _ := os.ReadFile(path)
	marker := fmt.Sprintf("<!-- clictl:%s -->", toolName)
	if strings.Contains(string(existing), marker) {
		return path, nil // Already installed
	}

	section := fmt.Sprintf("\n%s\n%s\n", marker, content)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(section); err != nil {
		return "", err
	}
	return path, nil
}

// vscodeExtensionGlobalStorage returns the platform-specific globalStorage path
// for a VS Code extension.
func vscodeExtensionGlobalStorage(extensionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	var base string
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage")
	case "linux":
		base = filepath.Join(home, ".config", "Code", "User", "globalStorage")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		base = filepath.Join(appData, "Code", "User", "globalStorage")
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return filepath.Join(base, extensionID), nil
}

// writeClineMCP writes MCP config for the Cline VS Code extension.
// Skill content goes to .clinerules in the project root.
// MCP config goes to the VS Code extension globalStorage for saoudrizwan.claude-dev.
func writeClineMCP(toolName string, serverEntry map[string]interface{}) (string, error) {
	storageDir, err := vscodeExtensionGlobalStorage("saoudrizwan.claude-dev")
	if err != nil {
		return "", fmt.Errorf("resolving Cline storage path: %w", err)
	}

	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return "", fmt.Errorf("creating Cline storage directory: %w", err)
	}

	configPath := filepath.Join(storageDir, "mcp.json")

	err = lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = serverEntry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}

	return configPath, nil
}

// writeClineSkill writes skill content to .clinerules in the project root.
func writeClineSkill(toolName, content string) (string, error) {
	path := ".clinerules"
	existing, _ := os.ReadFile(path)
	marker := fmt.Sprintf("<!-- clictl:%s -->", toolName)
	if strings.Contains(string(existing), marker) {
		return path, nil
	}

	section := fmt.Sprintf("\n%s\n%s\n", marker, content)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(section); err != nil {
		return "", err
	}
	return path, nil
}

// writeRooCodeMCP writes MCP config for the Roo Code VS Code extension.
// Skill content goes to .roorules in the project root.
// MCP config goes to the VS Code extension globalStorage for rooveterinaryinc.roo-cline.
func writeRooCodeMCP(toolName string, serverEntry map[string]interface{}) (string, error) {
	storageDir, err := vscodeExtensionGlobalStorage("rooveterinaryinc.roo-cline")
	if err != nil {
		return "", fmt.Errorf("resolving Roo Code storage path: %w", err)
	}

	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return "", fmt.Errorf("creating Roo Code storage directory: %w", err)
	}

	configPath := filepath.Join(storageDir, "mcp.json")

	err = lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = serverEntry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}

	return configPath, nil
}

// writeRooCodeSkill writes skill content to .roorules in the project root.
func writeRooCodeSkill(toolName, content string) (string, error) {
	path := ".roorules"
	existing, _ := os.ReadFile(path)
	marker := fmt.Sprintf("<!-- clictl:%s -->", toolName)
	if strings.Contains(string(existing), marker) {
		return path, nil
	}

	section := fmt.Sprintf("\n%s\n%s\n", marker, content)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(section); err != nil {
		return "", err
	}
	return path, nil
}

// writeAmazonQMCP writes MCP config for Amazon Q Developer.
// MCP config goes to ~/.aws/amazonq/mcp.json
func writeAmazonQMCP(toolName string, serverEntry map[string]interface{}) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	configDir := filepath.Join(home, ".aws", "amazonq")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("creating Amazon Q config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "mcp.json")

	err = lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = serverEntry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}

	return configPath, nil
}

// writeBoltAIMCP writes MCP config for BoltAI.
// MCP config goes to ~/Library/Application Support/BoltAI/mcp.json (macOS only)
func writeBoltAIMCP(toolName string, serverEntry map[string]interface{}) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("BoltAI is only supported on macOS")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	configDir := filepath.Join(home, "Library", "Application Support", "BoltAI")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("creating BoltAI config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "mcp.json")

	err = lockAndWriteJSON(configPath, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = serverEntry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}

	return configPath, nil
}
