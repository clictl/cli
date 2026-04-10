// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/telemetry"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall [tool...]",
	Short: "Remove skill files and MCP registrations",
	Long: `Remove previously installed skill files and clean up MCP server registrations.

  # Remove a specific tool
  clictl uninstall openweathermap

  # Remove multiple tools
  clictl uninstall github stripe slack

  # Remove the global clictl skill (all targets)
  clictl uninstall`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// Remove global clictl skill from all targets
			removed := false
			for target, t := range skillTargets {
				dir := t.dir("clictl")
				path := filepath.Join(dir, t.filename)
				if _, err := os.Stat(path); err == nil {
					os.RemoveAll(path)
					// For claude-code, remove the directory too
					if target == "claude-code" {
						os.RemoveAll(dir)
					}
					fmt.Printf("Removed %s skill: %s\n", t.label, path)
					removed = true
				}
			}
			// Remove global MCP entries
			removeMCPEntry(".mcp.json", "clictl")
			removeMCPEntry(filepath.Join(".cursor", "mcp.json"), "clictl")

			if !removed {
				fmt.Println("No clictl skill files found to remove.")
			}
			return nil
		}

		for _, toolName := range args {
			// Remove skill files from all targets
			for target, t := range skillTargets {
				dir := t.dir(toolName)
				path := filepath.Join(dir, t.filename)

				if target == "claude-code" || target == "gemini" {
					// These have per-tool directories/files
					if _, err := os.Stat(path); err == nil {
						os.RemoveAll(dir)
						fmt.Printf("Removed %s skill: %s\n", t.label, path)
					}
				} else {
					// Single-file targets (codex, cursor, windsurf) - remove tool section from file
					removeToolFromFile(path, toolName)
				}
			}

			// Clean up isolation settings (best-effort)
			removeClaudeSettingsSkill(toolName)
			removeCursorSettings(toolName)
			removeWindsurfSettings(toolName)

			// Remove from installed list
			if err := removeFromInstalled(toolName); err != nil {
				return fmt.Errorf("updating installed list: %w", err)
			}

			// Clean up MCP configs (best-effort)
			removeMCPEntry(".mcp.json", toolName)
			removeMCPEntry(filepath.Join(".cursor", "mcp.json"), toolName)

			// Remove bash filter hook (Phase 3, best-effort)
			hookPath := filepath.Join(".claude", "hooks", fmt.Sprintf("clictl-bash-filter-%s.sh", toolName))
			os.Remove(hookPath)

			// Workspace audit event (Phase 3)
			cfg, cfgErr := config.Load()
			if cfgErr == nil && cfg.Auth.ActiveWorkspace != "" {
				if auditErr := postSkillAuditEvent(cfg.Auth.ActiveWorkspace, "skill.uninstalled", toolName, map[string]interface{}{}); auditErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not post audit event: %v\n", auditErr)
				}
			}

			telemetry.TrackUninstall(toolName)

			fmt.Printf("Uninstalled: %s\n", toolName)
		}

		// Auto-regenerate lock file
		cfg, cfgErr := config.Load()
		if cfgErr == nil {
			cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)
			if lockErr := GenerateLockFile(cmd.Context(), cfg); lockErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update lock file: %v\n", lockErr)
			}
		}

		return nil
	},
}

func removeMCPEntry(path, toolName string) {
	key := "clictl-" + toolName

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	servers, ok := cfg["mcpServers"].(map[string]interface{})
	if !ok {
		return
	}

	if _, exists := servers[key]; !exists {
		return
	}
	delete(servers, key)
	cfg["mcpServers"] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(path, append(out, '\n'), 0o644)
}

// removeToolFromFile removes a tool section from a single-file target (.cursorrules, AGENTS.md, etc.)
func removeToolFromFile(path, toolName string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	content := string(data)
	marker := "# " + toolName
	if !strings.Contains(content, marker) {
		return
	}

	// Split by --- separator and remove the section containing this tool
	sections := strings.Split(content, "\n---\n")
	var kept []string
	for _, section := range sections {
		if !strings.Contains(section, marker) {
			kept = append(kept, section)
		}
	}

	if len(kept) == 0 {
		os.Remove(path)
		return
	}

	os.WriteFile(path, []byte(strings.Join(kept, "\n---\n")), 0o644)
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
