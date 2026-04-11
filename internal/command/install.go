// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/enterprise"
	"github.com/clictl/cli/internal/memory"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/telemetry"
	"github.com/clictl/cli/internal/vault"
)

var (
	installNoMCP         bool
	installNoSkill       bool
	installTarget        string
	installWorkspace     bool
	installYes           bool
	installTrust         bool
	installDryRun        bool
	installAllowUnsigned bool
	installSkillSet      string
	installNoShims       bool
	installGlobal        bool
	installAs            string
	installFrom          string

	// pinnedVersions tracks version pins from install args (tool@version).
	// Merged into the lock file after GenerateLockFile runs.
	pinnedVersions = make(map[string]string)
)

// skillTargets defines where skill files are written for each target.
var skillTargets = map[string]struct {
	dir      func(name string) string // directory for the skill file
	filename string                   // skill filename
	label    string                   // display name
}{
	"claude-code": {
		dir:      func(name string) string { return filepath.Join(".claude", "skills", name) },
		filename: "SKILL.md",
		label:    "Claude Code",
	},
	"gemini": {
		dir:      func(_ string) string { return ".gemini" },
		filename: "GEMINI.md",
		label:    "Gemini CLI",
	},
	"codex": {
		dir:      func(_ string) string { return "." },
		filename: "AGENTS.md",
		label:    "OpenAI Codex",
	},
	"cursor": {
		dir:      func(_ string) string { return "." },
		filename: ".cursorrules",
		label:    "Cursor",
	},
	"windsurf": {
		dir:      func(_ string) string { return "." },
		filename: ".windsurfrules",
		label:    "Windsurf",
	},
	"goose": {
		dir:      func(_ string) string { return "." },
		filename: ".goose-instructions.md",
		label:    "Goose",
	},
	"cline": {
		dir:      func(_ string) string { return "." },
		filename: ".clinerules",
		label:    "Cline",
	},
	"roo-code": {
		dir:      func(_ string) string { return "." },
		filename: ".roorules",
		label:    "Roo Code",
	},
	"amazon-q": {
		dir:      func(_ string) string { return "." },
		filename: ".amazonq-rules",
		label:    "Amazon Q",
	},
	"boltai": {
		dir:      func(_ string) string { return "." },
		filename: ".boltai-rules",
		label:    "BoltAI",
	},
}

var installCmd = &cobra.Command{
	Use:   "install [tool...]",
	Short: "Install tools as skills and MCP servers",
	Long: `Install the clictl skill and MCP server, or specific tool specs for your AI provider.
Both skill files and MCP server registration are installed by default.

  # Install clictl (skill + MCP)
  clictl install

  # Install specific tools
  clictl install github stripe slack

  # Install a tool group (all tools in the group)
  clictl install group hacker-news

  # Skill only (no MCP registration)
  clictl install --no-mcp

  # MCP only (no skill file)
  clictl install --no-skill

  # Install for a specific target
  clictl install github --target gemini

Target is auto-detected from your project. Override with --target.
Supported targets: claude-code, gemini, codex, cursor, windsurf.`,
	Args: cobra.ArbitraryArgs,
	// Install downloads a spec, generates a SKILL.md for agent discovery,
	// and registers an MCP server entry in the agent's config. The --no-mcp
	// and --no-skill flags control which outputs are generated.
	RunE: func(cmd *cobra.Command, args []string) error {
		// Global mode: clictl install (no tool names)
		// Prompts user to pick targets, then installs clictl skill for each.
		if len(args) == 0 {
			var targets []string
			if installTarget != "" {
				targets = []string{installTarget}
			} else {
				targets = detectAndPromptTargets()
			}

			for _, target := range targets {
				t, ok := skillTargets[target]
				if !ok {
					fmt.Fprintf(os.Stderr, "Unknown target: %s, skipping\n", target)
					continue
				}

				if !installNoSkill {
					path, err := generateGlobalSkillForTarget(target)
					if err != nil {
						return fmt.Errorf("generating skill for %s: %w", t.label, err)
					}
					fmt.Printf("Installed %s skill: %s\n", t.label, path)

					// Prompt to add clictl commands to allowed permissions
					if addPermFn := permissionSetterForTarget(target); addPermFn != nil {
						approved := installYes
						if !approved {
							fmt.Println("\nAllow your agent to run clictl commands without prompting?")
							fmt.Println("  This adds clictl run, search, info, install, etc. to your agent's settings.")
							fmt.Print("  Approve? [Y/n] ")
							var input string
							fmt.Scanln(&input)
							input = strings.TrimSpace(strings.ToLower(input))
							approved = input == "" || input == "y" || input == "yes"
						}
						if approved {
							if err := addPermFn(); err != nil {
								fmt.Fprintf(os.Stderr, "Warning: could not set permissions: %v\n", err)
							} else {
								fmt.Println("  Added clictl to allowed commands")
							}
						} else {
							fmt.Println("  Skipped. You can add permissions later by re-running: clictl install")
						}
					}
				}

				if !installNoMCP {
					mcpPath, err := generateGlobalMCPConfig(target)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  MCP not supported for %s, skipping\n", t.label)
					} else {
						fmt.Printf("Registered MCP server: %s\n", mcpPath)
					}
				}
			}

			fmt.Println("\nTo add tool discovery rules to your project, run:")
			fmt.Println("  clictl instructions")

			// Initialize vault key if not already present
			v := vault.NewVault(config.BaseDir())
			if err := v.InitKey(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not initialize vault: %v\n", err)
			} else {
				fmt.Printf("Vault initialized at %s\n", v.KeyPath())
			}

			fmt.Println("Your Agent can now search, install, and use any tool from clictl.")
			return nil
		}

		target := installTarget
		if target == "" {
			target = detectTarget()
		}

		// Skill set mode: install all skills in a set
		if installSkillSet != "" {
			return installSkillSetFlow(cmd, installSkillSet, target)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(cfg.APIURL, cache, flagNoCache)

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		// Group install mode: clictl install group <name> [<name>...]
		if len(args) >= 2 && args[0] == "group" {
			for _, groupName := range args[1:] {
				if err := installGroup(cmd.Context(), cfg, groupName, target); err != nil {
					return fmt.Errorf("installing group %q: %w", groupName, err)
				}
			}
			return nil
		}

		// Track visited dependencies to prevent circular installs
		visited := make(map[string]bool)

		// Install each tool
		for _, toolArg := range args {
			// Parse version pin: @ns/tool@2.1.0 or tool@2.1.0
			toolName, pinnedVersion := parseToolVersion(toolArg)
			if pinnedVersion != "" {
				pinnedVersions[toolName] = pinnedVersion
			}

			var spec *models.ToolSpec
			var specYAML []byte

			// N3.8: If name contains a /, treat as qualified name (namespace/name)
			namespace, baseName := parseNamespacedTool(toolName)

			if pinnedVersion != "" {
				if namespace != "" {
					// Try qualified name with version
					var fetchErr error
					spec, specYAML, fetchErr = client.GetPackByQualifiedName(cmd.Context(), namespace, baseName)
					if fetchErr != nil {
						// Fall back to regular version lookup
						spec, specYAML, fetchErr = client.GetSpecVersionYAML(cmd.Context(), baseName, pinnedVersion)
					}
					if fetchErr != nil {
						return fmt.Errorf("tool %q version %q not found: %w\n  The version may have been yanked or the tool delisted. Try: clictl info %s", toolName, pinnedVersion, fetchErr, toolName)
					}
				} else {
					var fetchErr error
					spec, specYAML, fetchErr = client.GetSpecVersionYAML(cmd.Context(), toolName, pinnedVersion)
					if fetchErr != nil {
						return fmt.Errorf("tool %q version %q not found: %w\n  The version may have been yanked or the tool delisted. Try: clictl info %s", toolName, pinnedVersion, fetchErr, toolName)
					}
				}
			} else if namespace != "" {
				// N3.8: Direct qualified name resolution
				var fetchErr error
				spec, specYAML, fetchErr = client.GetPackByQualifiedName(cmd.Context(), namespace, baseName)
				if fetchErr != nil {
					// Fall back to regular spec lookup
					spec, specYAML, fetchErr = client.GetSpecYAML(cmd.Context(), baseName)
					if fetchErr != nil {
						return fmt.Errorf("tool %q not found: %w", toolName, fetchErr)
					}
					// Verify namespace matches
					if spec.Namespace != "" && spec.Namespace != namespace {
						return fmt.Errorf("tool %q resolved to namespace %q, not %q", baseName, spec.Namespace, namespace)
					}
				}
			} else {
				// N4.5: --from flag to specify source
				if installFrom != "" {
					// Treat --from value as namespace
					var fetchErr error
					spec, specYAML, fetchErr = client.GetPackByQualifiedName(cmd.Context(), installFrom, toolName)
					if fetchErr != nil {
						return fmt.Errorf("tool %q not found from %q: %w", toolName, installFrom, fetchErr)
					}
				} else {
					// N3.9: Try disambiguation for unscoped names
					var fetchErr error
					spec, specYAML, fetchErr = client.GetSpecYAML(cmd.Context(), toolName)
					if fetchErr != nil {
						// Try resolve endpoint for disambiguation
						matches, resolveErr := client.ResolveToolByName(cmd.Context(), toolName)
						if resolveErr == nil && len(matches) > 1 {
							selected, disambigErr := disambiguateTools(matches)
							if disambigErr != nil {
								return fmt.Errorf("tool %q: %w", toolName, disambigErr)
							}
							selectedNs, selectedName := parseNamespacedTool(selected)
							if selectedNs != "" {
								spec, specYAML, fetchErr = client.GetPackByQualifiedName(cmd.Context(), selectedNs, selectedName)
							} else {
								spec, specYAML, fetchErr = client.GetSpecYAML(cmd.Context(), selectedName)
							}
							if fetchErr != nil {
								return fmt.Errorf("tool %q not found: %w", selected, fetchErr)
							}
						} else {
							// P10.15: Check for redirects before giving up
							redirNs, redirBn := parseNamespacedTool(toolName)
							if redirNs != "" {
								redirect, _ := client.CheckRedirect(cmd.Context(), redirNs, redirBn)
								if redirect != nil && redirect.NewName != "" {
									fmt.Fprintf(os.Stderr, "Note: %s has been renamed to %s\n", toolName, redirect.NewName)
									fmt.Fprintf(os.Stderr, "  Redirect expires: %s\n", redirect.ExpiresAt)
									newNs, newBn := parseNamespacedTool(redirect.NewName)
									if newNs != "" {
										spec, specYAML, fetchErr = client.GetPackByQualifiedName(cmd.Context(), newNs, newBn)
									} else {
										spec, specYAML, fetchErr = client.GetSpecYAML(cmd.Context(), redirect.NewName)
									}
									if fetchErr == nil {
										toolName = redirect.NewName
									}
								}
							}

							if spec == nil {
								msg := fmt.Sprintf("tool %q not found in any configured registry", toolName)
								if dym := toolSuggestion(toolName, cfg); dym != "" {
									msg += dym
								}
								msg += "\n\nPossible causes:"
								msg += "\n  - The tool may have been delisted or removed by its publisher"
								msg += "\n  - The tool name may be misspelled"
								msg += "\n  - Your CLI version may be too old to resolve this tool"
								msg += fmt.Sprintf("\n\nTry: clictl search %s", toolName)
								return fmt.Errorf("%s", msg)
							}
						}
					}
				}
			}
			_ = specYAML

			// M3.16: Validate tool name at install time
			if err := validateToolName(spec.Name); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}

			// N5.9: Warning on install of deprecated tool
			if spec.Deprecated {
				msg := fmt.Sprintf("Warning: %s is deprecated.", spec.Name)
				if spec.DeprecatedBy != "" {
					msg += fmt.Sprintf(" Use %s instead.", spec.DeprecatedBy)
				}
				if spec.DeprecatedMsg != "" {
					msg += " " + spec.DeprecatedMsg
				}
				fmt.Fprintln(os.Stderr, msg)
				if !installYes {
					fmt.Fprintf(os.Stderr, "Continue anyway? [y/N] ")
					var input string
					fmt.Scanln(&input)
					input = strings.TrimSpace(strings.ToLower(input))
					if input != "y" && input != "yes" {
						fmt.Fprintf(os.Stderr, "Skipping %s\n", spec.Name)
						continue
					}
				}
			}

			// N4.6: Conflict warning when skill exists with same name
			localName := spec.Name
			if installAs != "" {
				localName = installAs
			}
			skillDir := filepath.Join(".claude", "skills", localName)
			if info, statErr := os.Stat(skillDir); statErr == nil && info.IsDir() {
				skillMD := filepath.Join(skillDir, "SKILL.md")
				if _, mdErr := os.Stat(skillMD); mdErr == nil {
					fmt.Fprintf(os.Stderr, "Warning: a skill already exists at %s. Installing will overwrite it.\n", skillDir)
				}
			}

			// Alias resolution: display note if the spec was resolved from a different name
			if spec.ResolvedFrom != "" {
				displayName := spec.Name
				if spec.Namespace != "" {
					displayName = fmt.Sprintf("%s/%s", spec.Namespace, spec.Name)
				}
				fmt.Fprintf(os.Stderr, "Note: %s resolved from %s -> %s\n", toolName, spec.ResolvedFrom, displayName)
			}

			// Mark as verified if the tool exists in the curated toolbox
			if !spec.IsVerified {
				spec.IsVerified = isInCuratedToolbox(spec.Name)
			}

			// Trust check: warn and skip unverified tools unless --trust is set
			if !spec.IsVerified && spec.Namespace == "" {
				if !installTrust {
					fmt.Fprintf(os.Stderr, "Warning: %s is from an unverified publisher.\nUse --trust to install unverified tools.\n", spec.Name)
					continue
				}
				fmt.Fprintf(os.Stderr, "Warning: %s is from an unverified publisher. Proceeding with --trust.\n", spec.Name)
			}

			// Enterprise policy: require signed specs
			if ent := enterprise.GetProvider(); ent.RequireSignedSpecs() {
				// Verify spec integrity via RawYAML hash
				if spec.RawYAML == "" {
					fmt.Fprintf(os.Stderr, "Error: %s has no raw YAML for verification. Enterprise policy requires signed specs.\n", spec.Name)
					continue
				}
			}

			// P11.13: Security advisory check
			if spec.SecurityAdvisory != "" {
				if ent := enterprise.GetProvider(); ent.BlockVulnerableTools() {
					fmt.Fprintf(os.Stderr, "Error: %s has a known security advisory: %s\n", spec.Name, spec.SecurityAdvisory)
					fmt.Fprintf(os.Stderr, "Workspace policy blocks tools with known vulnerabilities.\n")
					continue
				}
				fmt.Fprintf(os.Stderr, "Warning: %s has a known security advisory: %s\n", spec.Name, spec.SecurityAdvisory)
				if !installYes {
					fmt.Fprintf(os.Stderr, "Install anyway? [y/N] ")
					var input string
					fmt.Scanln(&input)
					input = strings.TrimSpace(strings.ToLower(input))
					if input != "y" && input != "yes" {
						fmt.Fprintf(os.Stderr, "Skipping %s\n", spec.Name)
						continue
					}
				}
			}

			// Skill source file integrity verification (via SHA256 hashes)
			if spec.Source != nil {
				if err := verifySkillSignature(spec, []byte(spec.RawYAML), client); err != nil {
					fmt.Fprintf(os.Stderr, "Error: skill integrity verification failed for %s: %v\n", spec.Name, err)
					continue
				}
			}

			// Workspace policy check (Phase 2)
			if err := checkWorkspacePolicy(cfg, spec); err != nil {
				fmt.Fprintf(os.Stderr, "Error: workspace policy violation for %s: %v\n", spec.Name, err)
				continue
			}

			// Workspace skill override check (Phase 3)
			if cfg.Auth.ActiveWorkspace != "" {
				overrides, overrideErr := loadSkillOverrides(cfg.Auth.ActiveWorkspace)
				if overrideErr == nil {
					override := findSkillOverride(overrides, spec.Name)
					if override != nil {
						if override.Blocked {
							fmt.Fprintf(os.Stderr, "Error: skill blocked by workspace policy: %s\n", spec.Name)
							continue
						}
						applySkillOverride(spec, override)
					}
				}
			}

			// Runtime detection for MCP packages distributed via npm/pypi
			if spec.Discover && spec.Package != nil && spec.Package.Registry != "" {
				rt, rtErr := DetectRuntime(spec.Package.Registry)
				if rtErr != nil {
					fmt.Fprintf(os.Stderr, "Skipping %s: %v\n", spec.Name, rtErr)
					continue
				}
				fmt.Fprintf(os.Stderr, "Using %s %s for %s\n", rt.Name, rt.Version, spec.Name)
			}

			// N1.11: Always show tool_name (not source path) in install output
			displayName := spec.Name
			if spec.DisplayName != "" {
				displayName = spec.DisplayName
			} else if spec.Namespace != "" {
				displayName = fmt.Sprintf("%s/%s", spec.Namespace, spec.Name)
			}
			fmt.Printf("Installing %s\n", displayName)

			// Permission and isolation display
			hasNetwork := spec.Sandbox != nil && spec.Sandbox.Network != nil && len(spec.Sandbox.Network.Allow) > 0
			hasEnv := spec.Sandbox != nil && spec.Sandbox.Env != nil && len(spec.Sandbox.Env.Allow) > 0
			hasPerms := hasNetwork || hasEnv
			hasFilesystem := spec.Sandbox != nil && spec.Sandbox.Filesystem != nil &&
				(len(spec.Sandbox.Filesystem.Read) > 0 || len(spec.Sandbox.Filesystem.Write) > 0)
			hasToolRestrictions := spec.Sandbox != nil && len(spec.Sandbox.Commands) > 0

			if hasPerms || hasFilesystem || hasToolRestrictions {
				fmt.Printf("\n%s requires these permissions:\n", spec.Name)
				if hasPerms {
					if hasNetwork {
						fmt.Printf("  Network: %s\n", strings.Join(spec.Sandbox.Network.Allow, ", "))
					}
					if hasEnv {
						fmt.Printf("  Env: %s\n", strings.Join(spec.Sandbox.Env.Allow, ", "))
					}
				}
				if hasToolRestrictions {
					fmt.Printf("  Tools: %s\n", formatToolRestrictionSummary(spec))
				}
				if hasFilesystem {
					fmt.Printf("  Filesystem: %s\n", formatFilesystemScopeSummary(spec))
				}

				if !installYes {
					fmt.Printf("\nInstall and grant these permissions? [Y/n] ")
					var input string
					fmt.Scanln(&input)
					input = strings.TrimSpace(strings.ToLower(input))
					if input != "" && input != "y" && input != "yes" {
						fmt.Printf("Skipping %s\n", spec.Name)
						continue
					}
				}
				fmt.Println()
			} else if !installDryRun {
				fmt.Printf("  No restrictions declared\n")
			}

			// Dry-run mode: show what would be generated
			if installDryRun {
				content := buildSkillContent(spec, target)
				fmt.Printf("\n--- Dry run: %s for %s ---\n", spec.Name, target)
				fmt.Printf("Skill content:\n%s\n", content)
				fmt.Printf("Tool restrictions: %s\n", formatToolRestrictionSummary(spec))
				fmt.Printf("Filesystem scope: %s\n", formatFilesystemScopeSummary(spec))
				if spec.Sandbox != nil && spec.Sandbox.Filesystem != nil {
					if target == "claude-code" {
						fmt.Printf("Would merge filesystem rules into .claude/settings.json\n")
					}
				}
				if spec.Sandbox != nil && len(spec.Sandbox.Commands) > 0 {
					switch target {
					case "cursor":
						fmt.Printf("Would generate .cursor/settings/%s.json\n", spec.Name)
					case "windsurf":
						fmt.Printf("Would generate .windsurf/settings/%s.json\n", spec.Name)
					}
				}
				fmt.Printf("--- End dry run ---\n")
				continue
			}

			// Skill protocol: fetch SKILL.md from source instead of generating
			if spec.IsSkill() {
				// Runtime detection for skill scripts
				if spec.Runtime != nil {
					runtimeRegistry := ""
					if spec.Runtime.Manager == "uvx" {
						runtimeRegistry = "pypi"
					}
					if runtimeRegistry != "" {
						rt, rtErr := DetectRuntime(runtimeRegistry)
						if rtErr != nil {
							fmt.Fprintf(os.Stderr, "Warning: %s runtime not available: %v\n", spec.Runtime.Manager, rtErr)
						} else {
							fmt.Fprintf(os.Stderr, "Using %s %s for %s\n", rt.Name, rt.Version, spec.Name)
						}
					}
					if len(spec.Runtime.Dependencies) > 0 {
						fmt.Fprintf(os.Stderr, "Python dependencies: %s\n", strings.Join(spec.Runtime.Dependencies, ", "))
					}
				}

				path, err := installSkillFromSource(cmd.Context(), spec, target)
				if err != nil {
					return fmt.Errorf("installing skill %s: %w", toolName, err)
				}
				fmt.Printf("Installed %s skill: %s\n", skillTargets[target].label, path)

				if spec.Runtime != nil && spec.Runtime.Manager == "uvx" && len(spec.Runtime.Dependencies) > 0 {
					fmt.Println("Tip: skill scripts will run in isolated uvx environments")
				}

				// Apply isolation rules
				applyIsolationRules(spec, target)

				// Bash filter hook (Phase 3)
				if len(sandboxCommands(spec)) > 0 && target == "claude-code" {
					if hookErr := generateBashFilterHook(spec.Name, sandboxCommands(spec), "."); hookErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not generate bash filter hook for %s: %v\n", spec.Name, hookErr)
					} else {
						fmt.Fprintf(os.Stderr, "  Bash filter hook installed for %s\n", spec.Name)
					}
				} else if len(sandboxCommands(spec)) > 0 {
					// For non-claude targets, embed bash allowlist as advisory directive
					fmt.Fprintf(os.Stderr, "  Bash allowlist (advisory for %s): %s\n", target, strings.Join(sandboxCommands(spec), ", "))
				}

				// Network restriction (Phase 2)
				applyNetworkRestriction(spec, target)

				// N4.2: Use alias name if --as flag provided
				trackName := spec.Name
				if installAs != "" {
					trackName = installAs
				}
				if err := addToInstalled(trackName); err != nil {
					return fmt.Errorf("tracking install: %w", err)
				}

				telemetry.TrackInstall(spec.Name, spec.Version, spec.ServerType(), spec.Category)

				// Enterprise: audit log
				if ent := enterprise.GetProvider(); ent.AuditLogEnabled() {
					ent.AuditLog("install", map[string]string{
						"tool": spec.Name, "version": spec.Version, "target": target, "method": "skill-source",
					})
				}

				// Workspace audit event (Phase 3)
				if cfg.Auth.ActiveWorkspace != "" {
					if auditErr := postSkillAuditEvent(cfg.Auth.ActiveWorkspace, "skill.installed", spec.Name, map[string]interface{}{
						"version": spec.Version, "target": target, "method": "skill-source",
					}); auditErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not post audit event: %v\n", auditErr)
					}
				}

				// Install transitive MCP dependencies
				visited[spec.Name] = true
				installDependencies(cmd.Context(), spec, target, client, visited)

				continue
			}

			// Generate skill file for the target
			path, err := generateSkillForTarget(spec, target)
			if err != nil {
				return fmt.Errorf("generating skill for %s: %w", toolName, err)
			}
			fmt.Printf("Installed %s skill: %s\n", skillTargets[target].label, path)

			// Apply isolation rules
			applyIsolationRules(spec, target)

			// Bash filter hook (Phase 3)
			if len(sandboxCommands(spec)) > 0 && target == "claude-code" {
				if hookErr := generateBashFilterHook(spec.Name, sandboxCommands(spec), "."); hookErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not generate bash filter hook for %s: %v\n", spec.Name, hookErr)
				} else {
					fmt.Fprintf(os.Stderr, "  Bash filter hook installed for %s\n", spec.Name)
				}
			} else if len(sandboxCommands(spec)) > 0 {
				fmt.Fprintf(os.Stderr, "  Bash allowlist (advisory for %s): %s\n", target, strings.Join(sandboxCommands(spec), ", "))
			}

			// N4.2: Track installed tool with alias name if provided
			trackName2 := spec.Name
			if installAs != "" {
				trackName2 = installAs
			}
			if err := addToInstalled(trackName2); err != nil {
				return fmt.Errorf("tracking install: %w", err)
			}

			// Generate shim (default: on, skip with --no-shims)
			if !installNoShims {
				if shimErr := generateShim(spec.Name); shimErr != nil {
					fmt.Fprintf(os.Stderr, "  Warning: could not generate shim: %v\n", shimErr)
				} else {
					fmt.Fprintf(os.Stderr, "  Shim: %s\n", filepath.Join(shimBinDir(), spec.Name))
				}
			}

			telemetry.TrackInstall(spec.Name, spec.Version, spec.ServerType(), spec.Category)

			// Enterprise: audit log
			if ent := enterprise.GetProvider(); ent.AuditLogEnabled() {
				ent.AuditLog("install", map[string]string{
					"tool": spec.Name, "version": spec.Version, "target": target,
				})
			}

			// Workspace audit event (Phase 3)
			if cfg.Auth.ActiveWorkspace != "" {
				if auditErr := postSkillAuditEvent(cfg.Auth.ActiveWorkspace, "skill.installed", spec.Name, map[string]interface{}{
					"version": spec.Version, "target": target,
				}); auditErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not post audit event: %v\n", auditErr)
				}
			}

			// Generate MCP config (default: on, skip with --no-mcp)
			if !installNoMCP {
				mcpPath, err := generateMCPConfig(spec.Name, target)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  MCP not supported for %s, skipping\n", toolName)
				} else {
					fmt.Printf("Registered MCP server in: %s\n", mcpPath)
				}
			}

			// Sync to workspace if requested
			if installWorkspace {
				if token == "" {
					fmt.Fprintf(os.Stderr, "Warning: --workspace requires login. Run 'clictl login' first.\n")
				} else if cfg.Auth.ActiveWorkspace == "" {
					fmt.Fprintf(os.Stderr, "Warning: no active workspace set. Run 'clictl workspace switch' first.\n")
				} else {
					if err := syncToolToWorkspace(cmd.Context(), cfg, token, spec.Name); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: workspace sync failed: %v\n", err)
					} else {
						fmt.Printf("Synced to workspace: %s\n", cfg.Auth.ActiveWorkspace)
					}
				}
			}

			// Install transitive MCP dependencies
			visited[spec.Name] = true
			installDependencies(cmd.Context(), spec, target, client, visited)
		}

		// Post-install: auto-generate lock file
		if err := GenerateLockFile(cmd.Context(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update lock file: %v\n", err)
		}

		// Post-install: check for outdated tools
		showOutdatedHint(cmd.Context(), cfg)

		// Post-install: suggest cleanup if not run recently
		if ShouldAutoCleanup() {
			fmt.Fprintf(os.Stderr, "\n`clictl cleanup` has not been run in the last 30 days, running now...\n")
			fmt.Fprintf(os.Stderr, "Disable this with CLICTL_NO_INSTALL_CLEANUP=1.\n\n")
			if os.Getenv("CLICTL_NO_INSTALL_CLEANUP") == "" {
				cleanupAll = false
				cleanupDryRun = false
				cleanupCmd.RunE(cmd, nil)
			}
		}

		return nil
	},
}

// installDependencies installs transitive dependencies listed in spec.Depends,
// and checks server requirements.
// It uses a visited map to prevent circular dependency loops.
func installDependencies(ctx context.Context, spec *models.ToolSpec, target string, client *registry.Client, visited map[string]bool) {
	allDeps := make([]string, 0, len(spec.Depends))
	allDeps = append(allDeps, spec.Depends...)

	for _, dep := range allDeps {
		if visited[dep] {
			continue
		}
		visited[dep] = true

		// Check if already installed
		if isInstalled(dep) {
			fmt.Fprintf(os.Stderr, "  Dependency %s already installed\n", dep)
			continue
		}

		depSpec, _, err := client.GetSpecYAML(ctx, dep)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not fetch dependency %s: %v\n", dep, err)
			continue
		}

		depDisplay := dep
		if depSpec.Namespace != "" {
			depDisplay = fmt.Sprintf("%s/%s", depSpec.Namespace, depSpec.Name)
		}
		fmt.Fprintf(os.Stderr, "  Installing dependency: %s v%s\n", depDisplay, depSpec.Version)

		// Skill protocol: fetch from source
		if depSpec.IsSkill() {
			path, err := installSkillFromSource(ctx, depSpec, target)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not install dependency %s: %v\n", dep, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  Installed dependency skill: %s -> %s\n", dep, path)
		} else {
			// Generate skill file for the target
			path, err := generateSkillForTarget(depSpec, target)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not install dependency %s: %v\n", dep, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  Installed dependency skill: %s -> %s\n", dep, path)
		}

		if err := addToInstalled(dep); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not track dependency %s: %v\n", dep, err)
		}

		// Recurse into this dependency's own dependencies
		installDependencies(ctx, depSpec, target, client, visited)
	}

	// Check server requirements (warnings only, not blockers)
	if spec.Server != nil {
		for _, req := range spec.Server.Requires {
			path, err := exec.LookPath(req.Name)
			if err == nil {
				fmt.Fprintf(os.Stderr, "  System: %s found at %s\n", req.Name, path)
			} else {
				msg := fmt.Sprintf("  Warning: %s not found.", req.Name)
				if req.URL != "" {
					msg += fmt.Sprintf(" Install from: %s", req.URL)
				}
				fmt.Fprintf(os.Stderr, "%s\n", msg)
			}
		}
	}
}

func generateGlobalSkillForTarget(target string) (string, error) {
	t, ok := skillTargets[target]
	if !ok {
		return "", fmt.Errorf("unknown target: %s (use claude-code, gemini, codex, cursor, or windsurf)", target)
	}

	dir := t.dir("clictl")

	// Global install uses home directory so skills are available across all projects
	if target == "claude-code" || target == "gemini" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		dir = filepath.Join(home, dir)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	content := globalSkillContent(target)
	path := filepath.Join(dir, t.filename)
	return path, os.WriteFile(path, []byte(content), 0o600)
}

func globalSkillContent(target string) string {
	body := `# clictl - Package Manager for Your Agent

clictl is a package manager for your Agent. It gives you access to a curated registry of APIs, CLIs, and data sources.

## When to Use clictl (IMPORTANT)

ALWAYS check clictl BEFORE writing code to call an external service. Use clictl when:
- Calling any third-party API (GitHub, Stripe, Slack, weather, payments, etc.)
- Fetching data from an external source
- Sending messages or notifications to a service
- Any task involving a service the user might have credentials for
- Interacting with cloud infrastructure or SaaS platforms

Do NOT use clictl for:
- Reading or writing local files
- Git operations
- Pure computation (math, string manipulation)
- Tasks you can complete with built-in knowledge

## Decision Flow

1. Need an external service? Run ` + "`clictl search \"<what you need>\"`" + `
2. Found a tool? Run ` + "`clictl info <tool>`" + ` to check auth requirements
3. Auth key missing? Tell the user: "Run ` + "`clictl vault set <KEY> <value>`" + `"
4. Ready? Run ` + "`clictl run <tool> <action> [--params]`" + `
5. Use the output to continue your task. Chain multiple tool calls if needed.

## Core Workflow

` + "```bash" + `
# Search for what you need
clictl search "weather"

# Check details and auth requirements
clictl info open-meteo

# Execute
clictl run open-meteo current --latitude 51.5 --longitude -0.12
` + "```" + `

## Discovering Tools

` + "```bash" + `
clictl search <query> --category ai --type cli --tag docker
clictl search <query> --auth none   # only tools with no auth required
clictl search <query> --ready       # only tools ready to use now
clictl categories    # list all categories
clictl tags          # list popular tags
clictl info <tool>   # check requirements before running
` + "```" + `

## Key Commands

| Command | Description |
|---------|-------------|
| clictl search <query> | Find tools by keyword |
| clictl info <tool> | View actions, parameters, and auth requirements |
| clictl run <tool> <action> [--params] | Execute a tool action |
| clictl explain <tool> <action> | Get structured JSON help for an action |
| clictl install <tool> | Install as a dedicated skill with full docs |
| clictl list [--category <cat>] | Browse all available tools |
| clictl vault set <name> <value> | Store a secret in the encrypted vault |
| clictl remember <tool> <note> | Save a note about a tool |
| clictl feedback <tool> up/down | Rate a tool |

## Authentication

Tools may require API keys. The vault stores them securely:

` + "```bash" + `
# Check what a tool needs
clictl info <tool>

# Store a key in the vault
clictl vault set STRIPE_API_KEY sk_live_xxx

# Keys are resolved automatically when running tools
clictl run stripe charge --amount 100
` + "```" + `

If a key is missing, tell the user what to set and where to get it.
Do NOT ask the user to paste secrets directly. Always use the vault.

## Memory

Save what you learn about tools for future sessions:

` + "```bash" + `
clictl remember <tool> <note>
` + "```" + `

## Safety

- Read actions execute without confirmation
- Safe write actions (search, AI inference) execute without confirmation
- Other write actions prompt for confirmation the first time
- Destructive actions are not available in the official registry
- Never modify or delete user data without explicit confirmation
- Never send credentials or secrets to any tool

## Chaining Tools

Run multiple tools to compare, combine, or compose results:

` + "```bash" + `
# Compare Python packages
clictl run pypi package-info --name requests
clictl run pypi package-info --name httpx

# Cross-reference: find a repo, then check its issues
clictl run github search-repos --query "fastapi"
clictl run github repo-issues --owner tiangolo --repo fastapi --state open
` + "```" + `

Use output from one tool as input to the next. Summarize, compare, or recommend based on results.

## Code Mode

For complex workflows that stitch multiple tools together in a single turn, use code mode via the MCP server:

` + "```javascript" + `
// Compare two packages in one shot
var req = pypi.packageInfo({name: "requests"})
var httpx = pypi.packageInfo({name: "httpx"})
console.log(req.info.summary + " vs " + httpx.info.summary)
` + "```" + `

Code mode executes all calls in one turn instead of multiple round trips.
Generate typed SDKs with ` + "`clictl codegen <tool> --lang typescript`" + `.

## Tips

- Use ` + "`clictl explain <tool> <action>`" + ` for structured JSON help
- Use ` + "`clictl run <tool> <action> --json`" + ` to get raw JSON (no transforms)
- Use ` + "`clictl feedback <tool> up/down`" + ` if something works or breaks
- Install frequently used tools as dedicated skills: clictl install <tool>
- Chain tool calls: use output from one tool as input to the next
`

	// Claude Code uses YAML frontmatter
	if target == "claude-code" {
		return "---\nname: clictl\ndescription: Package manager for Agent tools. Use this skill to search, install, and call APIs, CLIs, and data sources.\nallowed-tools: [Bash]\n---\n\n" + body
	}

	// Other targets use the body without frontmatter
	return body
}

func generateGlobalMCPConfig(target string) (string, error) {
	serverEntry := map[string]interface{}{
		"command": cliCtlBin(),
		"args":    []string{"mcp-serve"},
	}
	return writeGlobalMCPForTarget("clictl", target, serverEntry)
}

func writeGlobalMCPForTarget(name, target string, serverEntry map[string]interface{}) (string, error) {
	switch target {
	case "claude-code":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return writeMCPJSON(filepath.Join(home, ".claude", "mcp.json"), name, serverEntry)
	case "claude-desktop":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configPath := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		return writeClaudeDesktopMCP(configPath, name, serverEntry)
	case "cursor":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return writeMCPJSON(filepath.Join(home, ".cursor", "mcp.json"), name, serverEntry)
	case "goose":
		return writeGooseMCP(name, serverEntry)
	case "cline":
		return writeClineMCP(name, serverEntry)
	case "roo-code":
		return writeRooCodeMCP(name, serverEntry)
	case "amazon-q":
		return writeAmazonQMCP(name, serverEntry)
	case "boltai":
		return writeBoltAIMCP(name, serverEntry)
	case "gemini", "codex", "windsurf":
		return "", fmt.Errorf("target %q uses skill files, not MCP. Remove --mcp or use --target claude-code/cursor", target)
	default:
		return "", fmt.Errorf("unknown MCP target: %s", target)
	}
}

func writeMCPForTarget(name, target string, serverEntry map[string]interface{}) (string, error) {
	switch target {
	case "claude-code":
		return writeMCPJSON(".mcp.json", name, serverEntry)
	case "claude-desktop":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configPath := filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		return writeClaudeDesktopMCP(configPath, name, serverEntry)
	case "cursor":
		return writeMCPJSON(filepath.Join(".cursor", "mcp.json"), name, serverEntry)
	case "goose":
		return writeGooseMCP(name, serverEntry)
	case "cline":
		return writeClineMCP(name, serverEntry)
	case "roo-code":
		return writeRooCodeMCP(name, serverEntry)
	case "amazon-q":
		return writeAmazonQMCP(name, serverEntry)
	case "boltai":
		return writeBoltAIMCP(name, serverEntry)
	case "gemini", "codex", "windsurf":
		// These targets use skill files, not MCP config
		return "", fmt.Errorf("target %q uses skill files, not MCP. Remove --mcp or use --target claude-code/cursor", target)
	default:
		return "", fmt.Errorf("unknown MCP target: %s", target)
	}
}

func generateSkillForTarget(spec *models.ToolSpec, target string) (string, error) {
	t, ok := skillTargets[target]
	if !ok {
		return "", fmt.Errorf("unknown target: %s (use claude-code, gemini, codex, cursor, or windsurf)", target)
	}

	dir := t.dir(spec.Name)

	// C1.15: --global installs to home directory instead of project
	if installGlobal {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	content := buildSkillContent(spec, target)
	path := filepath.Join(dir, t.filename)

	// For single-file targets (codex, cursor, windsurf), append to existing file
	// so multiple tools can coexist in one file
	if target == "codex" || target == "cursor" || target == "windsurf" {
		existing, _ := os.ReadFile(path)
		if len(existing) > 0 && !strings.Contains(string(existing), "# "+spec.Name) {
			content = string(existing) + "\n---\n\n" + content
		} else if len(existing) > 0 && strings.Contains(string(existing), "# "+spec.Name) {
			// Tool already in file, skip append
			return path, nil
		}
	}

	return path, os.WriteFile(path, []byte(content), 0o600)
}

// isInCuratedToolbox checks if a tool exists in the locally synced curated toolbox.
// Tools in the curated toolbox are trusted and skip the --trust requirement.
func isInCuratedToolbox(name string) bool {
	bucketsDir := config.ToolboxesDir()
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	for _, reg := range cfg.Toolboxes {
		if registry.IsCuratedToolbox(reg) {
			regDir := filepath.Join(bucketsDir, reg.Name)
			li := registry.NewLocalIndex(regDir, reg.Name)
			if entry, _ := li.GetEntry(name); entry != nil {
				return true
			}
			// Also check filesystem convention (toolbox/{letter}/{name}/{name}.yaml)
			if len(name) > 0 {
				specPath := filepath.Join(regDir, string(name[0]), name, name+".yaml")
				if _, err := os.Stat(specPath); err == nil {
					return true
				}
			}
		}
	}
	return false
}

// sandboxCommands returns the sandbox commands list from the spec, or nil if none.
func sandboxCommands(spec *models.ToolSpec) []string {
	if spec.Sandbox == nil {
		return nil
	}
	return spec.Sandbox.Commands
}

// resolveAllowedTools returns the effective tool list from Sandbox.Commands.
// If Sandbox.Commands is empty/nil, returns the default unrestricted set.
func resolveAllowedTools(spec *models.ToolSpec) []string {
	cmds := sandboxCommands(spec)
	if len(cmds) == 0 {
		return []string{"Bash", "Read", "Write"}
	}
	// Capitalize first letter for Claude Code frontmatter format
	tools := make([]string, len(cmds))
	for i, t := range cmds {
		if len(t) > 0 {
			tools[i] = strings.ToUpper(t[:1]) + t[1:]
		}
	}
	return tools
}

// formatToolRestrictionSummary generates a human-readable summary of tool restrictions.
func formatToolRestrictionSummary(spec *models.ToolSpec) string {
	allowed := resolveAllowedTools(spec)
	if len(sandboxCommands(spec)) == 0 {
		return "No tool restrictions (default: Bash, Read, Write)"
	}

	allowedSet := make(map[string]bool)
	for _, t := range allowed {
		allowedSet[strings.ToLower(t)] = true
	}

	var unavailable []string
	for _, tool := range []string{"bash", "read", "write"} {
		if !allowedSet[tool] {
			unavailable = append(unavailable, tool)
		}
	}

	summary := fmt.Sprintf("This skill can use: %s", strings.Join(allowed, ", "))
	if len(unavailable) > 0 {
		summary += fmt.Sprintf(" (%s not available)", strings.Join(unavailable, ", "))
	}
	return summary
}

// formatFilesystemScopeSummary generates a human-readable summary of filesystem scope.
func formatFilesystemScopeSummary(spec *models.ToolSpec) string {
	if spec.Sandbox == nil || spec.Sandbox.Filesystem == nil {
		return "No restrictions declared"
	}
	fs := spec.Sandbox.Filesystem
	if len(fs.Read) == 0 && len(fs.Write) == 0 {
		return "No restrictions declared"
	}

	var parts []string
	if len(fs.Read) > 0 {
		parts = append(parts, fmt.Sprintf("Read access: %s", strings.Join(fs.Read, ", ")))
	}
	if len(fs.Write) > 0 {
		parts = append(parts, fmt.Sprintf("Write access: %s", strings.Join(fs.Write, ", ")))
	}
	return strings.Join(parts, " | ")
}

// clictl commands to allow agents to run without prompting.
var cliCtlAllowedCommands = []string{
	"Clictl run *",
	"Clictl search *",
	"Clictl info *",
	"Clictl install *",
	"Clictl explain *",
	"Clictl list *",
	"Clictl categories",
	"Clictl tags",
	"Clictl remember *",
	"Clictl memory *",
	"Clictl vault set *",
}

// permissionSetterForTarget returns a function to add clictl permissions
// for the given target, or nil if the target has no permission system.
func permissionSetterForTarget(target string) func() error {
	switch target {
	case "claude-code":
		return addClaudeCodePermissions
	case "cursor":
		return addCursorPermissions
	case "windsurf":
		return addWindsurfPermissions
	default:
		return nil
	}
}

// addClaudeCodePermissions adds clictl commands to Claude Code's allowed
// permissions so the agent can run tools without prompting.
// Writes to ~/.claude/settings.json (user-level, applies to all projects).
func addClaudeCodePermissions() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	return mergeAllowedCommands(filepath.Join(home, ".claude", "settings.json"), cliCtlAllowedCommands)
}

// addCursorPermissions adds clictl commands to Cursor's allowed commands.
// Writes to ~/.cursor/settings.json.
func addCursorPermissions() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}

	settingsPath := filepath.Join(home, ".cursor", "settings.json")
	return mergeAllowedCommands(settingsPath, cliCtlAllowedCommands)
}

// addWindsurfPermissions adds clictl commands to Windsurf's allowed commands.
// Writes to ~/.windsurf/settings.json.
func addWindsurfPermissions() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}

	settingsPath := filepath.Join(home, ".windsurf", "settings.json")
	return mergeAllowedCommands(settingsPath, cliCtlAllowedCommands)
}

// mergeAllowedCommands adds commands to the permissions.allow list in a JSON settings file.
func mergeAllowedCommands(settingsPath string, cmds []string) error {
	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	existing := make(map[string]interface{})
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		json.Unmarshal(data, &existing)
	}

	perms, _ := existing["permissions"].(map[string]interface{})
	if perms == nil {
		perms = make(map[string]interface{})
	}
	allowList, _ := perms["allow"].([]interface{})

	existingSet := make(map[string]bool)
	for _, v := range allowList {
		if s, ok := v.(string); ok {
			existingSet[s] = true
		}
	}
	for _, cmd := range cmds {
		if !existingSet[cmd] {
			allowList = append(allowList, cmd)
		}
	}

	perms["allow"] = allowList
	existing["permissions"] = perms

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// mergeClaudeSettings reads an existing .claude/settings.json, adds skill-scoped
// filesystem rules, and writes it back. If the file does not exist, it creates one.
func mergeClaudeSettings(skillName string, fs *models.FilesystemPermissions) error {
	settingsPath := filepath.Join(".claude", "settings.json")
	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating .claude directory: %w", err)
	}

	existing := make(map[string]interface{})
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		json.Unmarshal(data, &existing)
	}

	// Build the skill-scoped permissions key
	permKey := "skill_permissions"
	skillPerms, _ := existing[permKey].(map[string]interface{})
	if skillPerms == nil {
		skillPerms = make(map[string]interface{})
	}

	entry := make(map[string]interface{})
	if len(fs.Read) > 0 {
		entry["read"] = fs.Read
	}
	if len(fs.Write) > 0 {
		entry["write"] = fs.Write
	}
	skillPerms[skillName] = entry
	existing[permKey] = skillPerms

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// removeClaudeSettingsSkill removes skill-scoped filesystem rules from .claude/settings.json.
func removeClaudeSettingsSkill(skillName string) {
	settingsPath := filepath.Join(".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}

	var existing map[string]interface{}
	if err := json.Unmarshal(data, &existing); err != nil {
		return
	}

	skillPerms, ok := existing["skill_permissions"].(map[string]interface{})
	if !ok {
		return
	}

	if _, exists := skillPerms[skillName]; !exists {
		return
	}
	delete(skillPerms, skillName)

	if len(skillPerms) == 0 {
		delete(existing, "skill_permissions")
	} else {
		existing["skill_permissions"] = skillPerms
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// generateCursorSettings writes tool restriction rules for Cursor.
func generateCursorSettings(skillName string, allowedTools []string) error {
	dir := filepath.Join(".cursor", "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, skillName+".json")
	rules := map[string]interface{}{
		"skill":         skillName,
		"allowed_tools": allowedTools,
	}
	out, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// removeCursorSettings removes Cursor tool restriction rules for a skill.
func removeCursorSettings(skillName string) {
	path := filepath.Join(".cursor", "settings", skillName+".json")
	os.Remove(path)
}

// generateWindsurfSettings writes tool restriction rules for Windsurf.
func generateWindsurfSettings(skillName string, allowedTools []string) error {
	dir := filepath.Join(".windsurf", "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, skillName+".json")
	rules := map[string]interface{}{
		"skill":         skillName,
		"allowed_tools": allowedTools,
	}
	out, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// removeWindsurfSettings removes Windsurf tool restriction rules for a skill.
func removeWindsurfSettings(skillName string) {
	path := filepath.Join(".windsurf", "settings", skillName+".json")
	os.Remove(path)
}

func buildSkillContent(spec *models.ToolSpec, target string) string {
	var sb strings.Builder

	allowedTools := resolveAllowedTools(spec)

	// Claude Code uses YAML frontmatter
	if target == "claude-code" {
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("name: %s\n", spec.Name))
		desc := strings.TrimRight(spec.Description, ".")
		sb.WriteString(fmt.Sprintf("description: %s. Use this skill when the user needs %s-related functionality.\n",
			desc, spec.Category))
		sb.WriteString(fmt.Sprintf("allowed-tools: [%s]\n", strings.Join(allowedTools, ", ")))
		sb.WriteString("---\n\n")
	}

	// Title and description
	sb.WriteString(fmt.Sprintf("# %s\n\n", spec.Name))
	sb.WriteString(fmt.Sprintf("%s\n\n", spec.Description))
	sb.WriteString(fmt.Sprintf("- **Version:** %s\n", spec.Version))
	sb.WriteString(fmt.Sprintf("- **Category:** %s\n", spec.Category))
	if len(spec.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("- **Tags:** %s\n", strings.Join(spec.Tags, ", ")))
	}

	// Auth requirements
	if spec.Auth != nil && len(spec.Auth.Env) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Authentication\n\nRequires the `%s` environment variable to be set.\n\n",
			spec.Auth.Env[0]))
		sb.WriteString(fmt.Sprintf("```bash\nexport %s=your-key-here\n```\n", spec.Auth.Env[0]))
	}

	// Actions
	sb.WriteString("\n## Available Actions\n\n")
	for _, action := range spec.Actions {
		sb.WriteString(fmt.Sprintf("### %s\n\n", action.Name))
		sb.WriteString(fmt.Sprintf("%s\n\n", action.Description))
		if action.Output != "" {
			sb.WriteString(fmt.Sprintf("**Output:** %s\n\n", action.Output))
		}

		// Build example command using param examples when available
		sb.WriteString("**Usage:**\n\n```bash\nclictl run ")
		sb.WriteString(spec.Name)
		sb.WriteString(" ")
		sb.WriteString(action.Name)
		for _, p := range action.Params {
			if p.Required {
				if p.Example != "" {
					sb.WriteString(fmt.Sprintf(" --%s %q", p.Name, p.Example))
				} else {
					sb.WriteString(fmt.Sprintf(" --%s <value>", p.Name))
				}
			}
		}
		sb.WriteString("\n```\n\n")

		// Parameters table
		if len(action.Params) > 0 {
			sb.WriteString("| Parameter | Type | Required | Description |\n")
			sb.WriteString("|-----------|------|----------|-------------|\n")
			for _, p := range action.Params {
				req := "No"
				if p.Required {
					req = "Yes"
				}
				desc := p.Description
				if p.Default != "" {
					desc += fmt.Sprintf(" (default: %s)", p.Default)
				}
				sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
					p.Name, p.Type, req, desc))
			}
			sb.WriteString("\n")
		}
	}

	// Tool restrictions (for non-claude targets, embed as directives)
	if target != "claude-code" && len(sandboxCommands(spec)) > 0 {
		sb.WriteString("\n## Tool Restrictions\n\n")
		sb.WriteString(fmt.Sprintf("This skill is restricted to the following tools: %s\n\n", strings.Join(allowedTools, ", ")))
		if target == "codex" {
			sb.WriteString("IMPORTANT: Only use the tools listed above when executing actions for this skill.\n\n")
		}
	}

	// Filesystem scope (for non-claude targets, embed as advisory directives)
	if spec.Sandbox != nil && spec.Sandbox.Filesystem != nil {
		fs := spec.Sandbox.Filesystem
		if len(fs.Read) > 0 || len(fs.Write) > 0 {
			sb.WriteString("\n## Filesystem Scope\n\n")
			if len(fs.Read) > 0 {
				sb.WriteString(fmt.Sprintf("Read access: %s\n", strings.Join(fs.Read, ", ")))
			}
			if len(fs.Write) > 0 {
				sb.WriteString(fmt.Sprintf("Write access: %s\n", strings.Join(fs.Write, ", ")))
			}
			sb.WriteString("\nDo not access files outside the paths listed above.\n\n")
		}
	}

	// Include memories if any exist
	mem, _ := memory.Load(spec.Name)
	if len(mem) > 0 {
		sb.WriteString("## Memories\n\n")
		sb.WriteString("Notes from previous sessions:\n\n")
		for _, m := range mem {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", m.Note, m.CreatedAt.Format("2006-01-02")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// applyIsolationRules writes target-specific tool restriction and filesystem scope
// configuration files based on the spec's RequiresTools and Permissions.Filesystem.
func applyIsolationRules(spec *models.ToolSpec, target string) {
	allowedTools := resolveAllowedTools(spec)

	// Layer 1: Tool restriction enforcement
	if len(sandboxCommands(spec)) > 0 {
		switch target {
		case "cursor":
			if err := generateCursorSettings(spec.Name, allowedTools); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not write Cursor settings for %s: %v\n", spec.Name, err)
			}
		case "windsurf":
			if err := generateWindsurfSettings(spec.Name, allowedTools); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not write Windsurf settings for %s: %v\n", spec.Name, err)
			}
		}
		// Claude Code and Codex handle tool restrictions via skill content (frontmatter / directives)
	}

	// Layer 2: Filesystem scope enforcement
	if spec.Sandbox != nil && spec.Sandbox.Filesystem != nil {
		fs := spec.Sandbox.Filesystem
		if len(fs.Read) > 0 || len(fs.Write) > 0 {
			if target == "claude-code" {
				if err := mergeClaudeSettings(spec.Name, fs); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not merge Claude settings for %s: %v\n", spec.Name, err)
				}
			}
			// Other targets use advisory directives embedded in skill content
		}
	}
}

// verifySkillSignature verifies the integrity of skill source files using
// per-file SHA256 hashes declared in source.files[].sha256.
func verifySkillSignature(spec *models.ToolSpec, content []byte, apiClient *registry.Client) error {
	if spec.Source == nil {
		return nil
	}
	// Per-file SHA256 verification is handled during file fetch in fetchAndVerifySkillFiles.
	// This function is kept as a no-op for callers that still reference it.
	_ = content
	_ = apiClient
	return nil
}

// fetchPublisherPublicKey retrieves the Ed25519 public key for a publisher namespace.
func fetchPublisherPublicKey(apiClient *registry.Client, namespace string) ([]byte, error) {
	u := fmt.Sprintf("%s/api/v1/publishers/%s/public-key", apiClient.BaseURL, namespace)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if apiClient.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiClient.AuthToken)
	}

	resp, err := apiClient.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching public key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("publisher key API returned %d: %s", resp.StatusCode, string(body))
	}

	var keyResp struct {
		PublicKey string `json:"public_key"` // base64-encoded Ed25519 public key
	}
	if err := json.NewDecoder(resp.Body).Decode(&keyResp); err != nil {
		return nil, fmt.Errorf("decoding public key response: %w", err)
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(keyResp.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decoding public key: %w", err)
	}

	return pubKeyBytes, nil
}

// checkWorkspacePolicy checks installed skill permissions against the active workspace policy.
// It returns an error if the skill declares hosts or env vars not allowed by the workspace.
func checkWorkspacePolicy(cfg *config.Config, spec *models.ToolSpec) error {
	if cfg.Auth.ActiveWorkspace == "" {
		return nil
	}

	// Load cached workspace policy
	policy, err := loadWorkspacePolicy(cfg.Auth.ActiveWorkspace)
	if err != nil {
		// No cached policy available, skip check
		return nil
	}

	// Check require_signed_specs
	if policy.RequireSignedSpecs {
		// Signed specs are verified via per-file SHA256 hashes in source.files
		hasFileHashes := false
		if spec.Source != nil {
			for _, f := range spec.Source.Files {
				if f.SHA256 != "" {
					hasFileHashes = true
					break
				}
			}
		}
		if !hasFileHashes {
			return fmt.Errorf("workspace policy requires signed specs, but %s has no file hashes", spec.Name)
		}
	}

	// Check blocked trust tiers
	if len(policy.BlockedTrustTiers) > 0 {
		tier := strings.ToLower(spec.TrustTier)
		if tier == "" {
			tier = "community"
		}
		for _, blocked := range policy.BlockedTrustTiers {
			if strings.EqualFold(blocked, tier) {
				displayName := spec.Name
				if spec.Namespace != "" {
					displayName = fmt.Sprintf("%s/%s", spec.Namespace, spec.Name)
				}
				return fmt.Errorf("Tool %s is blocked by your workspace's tool policy (%s tools not allowed)", displayName, tier)
			}
		}
	}

	if spec.Sandbox == nil {
		return nil
	}

	// Check network permissions
	specNetwork := []string{}
	if spec.Sandbox.Network != nil {
		specNetwork = spec.Sandbox.Network.Allow
	}
	if len(policy.AllowedNetwork) > 0 && len(specNetwork) > 0 {
		allowedSet := make(map[string]bool, len(policy.AllowedNetwork))
		for _, h := range policy.AllowedNetwork {
			allowedSet[h] = true
		}
		for _, host := range specNetwork {
			if !allowedSet[host] {
				return fmt.Errorf("workspace policy does not allow network access to %q (declared by %s)", host, spec.Name)
			}
		}
	}

	// Check env permissions
	specEnv := []string{}
	if spec.Sandbox.Env != nil {
		specEnv = spec.Sandbox.Env.Allow
	}
	if len(policy.AllowedEnv) > 0 && len(specEnv) > 0 {
		allowedSet := make(map[string]bool, len(policy.AllowedEnv))
		for _, e := range policy.AllowedEnv {
			allowedSet[e] = true
		}
		for _, envVar := range specEnv {
			if !allowedSet[envVar] {
				return fmt.Errorf("workspace policy does not allow env var %q (declared by %s)", envVar, spec.Name)
			}
		}
	}

	return nil
}

// WorkspacePolicy represents the cached workspace tool policy.
type WorkspacePolicy struct {
	AllowedNetwork     []string `json:"allowed_permissions_network" yaml:"allowed_permissions_network"`
	AllowedEnv         []string `json:"allowed_permissions_env" yaml:"allowed_permissions_env"`
	RequireSignedSpecs bool     `json:"require_signed_specs" yaml:"require_signed_specs"`
	BlockedTrustTiers  []string `json:"blocked_trust_tiers" yaml:"blocked_trust_tiers"`
}

// loadWorkspacePolicy loads the cached workspace policy for the given workspace slug.
func loadWorkspacePolicy(slug string) (*WorkspacePolicy, error) {
	path := filepath.Join(config.BaseDir(), "workspace-cache", slug+"-policy.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var policy WorkspacePolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

// installSkillFromSource fetches skill files from the spec's source config
// and writes them to the appropriate platform directory.
func installSkillFromSource(ctx context.Context, spec *models.ToolSpec, target string) (string, error) {
	src := spec.Source
	if src == nil {
		return "", fmt.Errorf("skill spec %q has no source config", spec.Name)
	}

	var files map[string][]byte
	var err error

	// Source always fetches from GitHub repo
	if src.Repo != "" {
		files, err = fetchAndVerifySkillFiles(ctx, src)
	} else {
		return "", fmt.Errorf("skill spec %q has no source repo configured", spec.Name)
	}
	if err != nil {
		return "", err
	}

	// Determine the install path based on platform config or target defaults
	dir, _ := skillInstallPath(spec, target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Resolve the install directory to detect symlink escapes.
	// This prevents a malicious spec from writing files outside the expected location
	// via symlinked intermediate directories.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolving install directory: %w", err)
	}

	// Write all files to the install directory
	for name, content := range files {
		path := filepath.Join(resolvedDir, name)
		// Create parent directories for nested files (e.g., assets/icon.svg, scripts/helpers/util.py)
		if parentDir := filepath.Dir(path); parentDir != resolvedDir {
			if err := os.MkdirAll(parentDir, 0o755); err != nil {
				return "", fmt.Errorf("creating directory %s: %w", parentDir, err)
			}
		}
		// Verify the resolved write target is still under the expected directory
		resolvedPath, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return "", fmt.Errorf("resolving path for %s: %w", name, err)
		}
		if !strings.HasPrefix(resolvedPath, resolvedDir) {
			return "", fmt.Errorf("symlink escape detected: %s resolves outside %s", path, resolvedDir)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return "", fmt.Errorf("writing %s: %w", path, err)
		}
	}

	// Return the directory path as the install location
	return dir, nil
}

// fetchAndVerifySkillFiles downloads skill files from a GitHub repository.
// If source.Files is populated, each file is fetched and verified against its
// SHA256 hash. Otherwise, falls back to fetching source.Path as a single file
// for backward compatibility with old single-file skills.
func fetchAndVerifySkillFiles(ctx context.Context, src *models.SkillSource) (map[string][]byte, error) {
	ref := src.Ref
	if ref == "" {
		ref = "main"
	}

	client := &http.Client{Timeout: 15 * time.Second}

	// Backward compat: no Files list means single-file fetch of source.Path
	if len(src.Files) == 0 {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", src.Repo, ref, src.Path)
		body, err := fetchRawGitHubFile(ctx, client, rawURL)
		if err != nil {
			return nil, err
		}
		// Derive filename from the path (e.g., "skills/foo/SKILL.md" -> "SKILL.md")
		name := filepath.Base(src.Path)
		return map[string][]byte{name: body}, nil
	}

	// Multi-file fetch with SHA256 verification
	// source.Path is the directory containing the files
	dir := src.Path
	files := make(map[string][]byte, len(src.Files))

	for _, f := range src.Files {
		fpath := f.FilePath()
		if fpath == "" {
			continue
		}
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", src.Repo, ref, dir, fpath)
		body, err := fetchRawGitHubFile(ctx, client, rawURL)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", fpath, err)
		}

		// Verify SHA256 if provided
		if f.SHA256 != "" {
			hash := sha256.Sum256(body)
			actual := hex.EncodeToString(hash[:])
			if actual != f.SHA256 {
				return nil, fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", fpath, f.SHA256, actual)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Warning: no SHA256 hash for %s, skipping integrity check\n", fpath)
		}

		files[fpath] = body
	}

	return files, nil
}

// fetchRawGitHubFile downloads a single file from GitHub raw content.
func fetchRawGitHubFile(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %d for %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return body, nil
}

// skillInstallPath determines where to write the skill file based on
// the spec's platforms config, the selected target, and the --global flag.
// By default, skills install to the project directory (e.g. .claude/skills/<name>).
// With --global, skills install under the user's home directory.
func skillInstallPath(spec *models.ToolSpec, target string) (dir string, filename string) {
	t, ok := skillTargets[target]
	if !ok {
		t = skillTargets["claude-code"]
	}

	dir = t.dir(spec.Name)
	filename = t.filename

	if installGlobal {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir)
		}
	}

	return dir, filename
}

// installedPath returns the path to the installed tools manifest.
func installedPath() string {
	return filepath.Join(config.BaseDir(), "installed.yaml")
}

func addToInstalled(name string) error {
	path := installedPath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	installed := loadInstalled()
	for _, n := range installed {
		if n == name {
			return nil // Already installed
		}
	}
	installed = append(installed, name)

	data := strings.Join(installed, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0o600)
}

func removeFromInstalled(name string) error {
	path := installedPath()
	if path == "" {
		return nil
	}

	installed := loadInstalled()
	var filtered []string
	for _, n := range installed {
		if n != name {
			filtered = append(filtered, n)
		}
	}

	if len(filtered) == 0 {
		return os.Remove(path)
	}

	data := strings.Join(filtered, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0o600)
}

func loadInstalled() []string {
	path := installedPath()
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// detectableTools defines which AI tools we can detect and their display info.
var detectableTools = []struct {
	target string
	label  string
	paths  []string // if any of these exist, the tool is detected
}{
	{"claude-code", "Claude Code", []string{".claude", ".claude/skills"}},
	{"cursor", "Cursor", []string{".cursor", ".cursor/mcp.json"}},
	{"gemini", "Gemini CLI", []string{".gemini"}},
	{"codex", "OpenAI Codex", []string{"AGENTS.md"}},
	{"windsurf", "Windsurf", []string{".windsurf", ".windsurfrules"}},
	{"goose", "Goose", []string{".goose-instructions.md"}},
	{"cline", "Cline", []string{".clinerules"}},
	{"roo-code", "Roo Code", []string{".roorules"}},
	{"amazon-q", "Amazon Q", []string{".amazonq-rules"}},
	{"boltai", "BoltAI", []string{".boltai-rules"}},
}

// detectTarget checks which AI tools are present and prompts the user to
// choose which ones to install for. Returns the selected targets.
func detectTarget() string {
	targets := detectTargets()
	if len(targets) == 1 {
		return targets[0]
	}
	return targets[0] // primary target (first selected)
}

// detectAndPromptTargets finds installed AI tools and lets the user pick.
// Returns one or more selected targets.
func detectAndPromptTargets() []string {
	var detected []int // indices into detectableTools
	for i, tool := range detectableTools {
		for _, p := range tool.paths {
			if _, err := os.Stat(p); err == nil {
				detected = append(detected, i)
				break
			}
		}
	}

	// Build the checklist
	fmt.Fprintf(os.Stderr, "\nDetected AI tools in this project:\n\n")

	type choice struct {
		index    int
		selected bool
	}
	choices := make([]choice, len(detectableTools))
	for i := range detectableTools {
		choices[i] = choice{index: i, selected: false}
	}

	// Pre-select detected tools
	for _, idx := range detected {
		choices[idx].selected = true
	}

	// If nothing detected, pre-select claude-code
	if len(detected) == 0 {
		choices[0].selected = true
	}

	// Show the checklist
	for i, tool := range detectableTools {
		marker := "  "
		if choices[i].selected {
			marker = "* "
		}
		status := ""
		for _, idx := range detected {
			if idx == i {
				status = " (detected)"
				break
			}
		}
		fmt.Fprintf(os.Stderr, "  %s[%d] %s%s\n", marker, i+1, tool.label, status)
	}

	fmt.Fprintf(os.Stderr, "\nEnter numbers to toggle (e.g., 1 3), or press Enter to accept [*]:\n> ")

	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(input)

	if input != "" {
		// Parse user selections
		for i := range choices {
			choices[i].selected = false
		}
		for _, s := range strings.Fields(input) {
			var num int
			if _, err := fmt.Sscanf(s, "%d", &num); err == nil && num >= 1 && num <= len(detectableTools) {
				choices[num-1].selected = true
			}
		}
	}

	var selected []string
	for i, c := range choices {
		if c.selected {
			selected = append(selected, detectableTools[i].target)
		}
	}

	if len(selected) == 0 {
		selected = []string{"claude-code"}
	}

	return selected
}

// detectTargets returns detected targets without prompting.
func detectTargets() []string {
	var detected []string
	for _, tool := range detectableTools {
		for _, p := range tool.paths {
			if _, err := os.Stat(p); err == nil {
				detected = append(detected, tool.target)
				break
			}
		}
	}
	if len(detected) == 0 {
		return []string{"claude-code"}
	}
	return detected
}

// clictl binary path for MCP server config
func cliCtlBin() string {
	exe, err := os.Executable()
	if err != nil {
		return "clictl"
	}
	return exe
}

func generateMCPConfig(toolName, target string) (string, error) {
	serverEntry := map[string]interface{}{
		"command": cliCtlBin(),
		"args":    []string{"mcp-serve", toolName},
	}
	return writeMCPForTarget(toolName, target, serverEntry)
}

// writeMCPJSON writes or merges a server entry into a flat MCP JSON file.
// Format: { "mcpServers": { "clictl-<tool>": { ... } } }
// Uses file locking to prevent TOCTOU races from concurrent installs.
func writeMCPJSON(path, toolName string, entry map[string]interface{}) (string, error) {
	err := lockAndWriteJSON(path, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = entry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// writeClaudeDesktopMCP merges into the Claude Desktop config file.
// Uses file locking to prevent TOCTOU races from concurrent installs.
func writeClaudeDesktopMCP(path, toolName string, entry map[string]interface{}) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	err := lockAndWriteJSON(path, func(existing map[string]interface{}) (map[string]interface{}, error) {
		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			servers = make(map[string]interface{})
		}
		servers["clictl-"+toolName] = entry
		existing["mcpServers"] = servers
		return existing, nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// syncToolToWorkspace posts the tool name to the workspace tools endpoint.
func syncToolToWorkspace(ctx context.Context, cfg *config.Config, token, toolName string) error {
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/tools/", apiURL, cfg.Auth.ActiveWorkspace)

	body, err := json.Marshal(map[string]string{"name": toolName})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// applyNetworkRestriction generates network restriction hooks or directives
// for skills that declare network permissions.
func applyNetworkRestriction(spec *models.ToolSpec, target string) {
	if spec.Sandbox == nil || spec.Sandbox.Network == nil || len(spec.Sandbox.Network.Allow) == 0 {
		return
	}

	networkHosts := spec.Sandbox.Network.Allow

	switch target {
	case "claude-code":
		// Generate a pre-tool hook that warns if bash commands reference undeclared hosts
		hookDir := filepath.Join(".claude", "hooks")
		if err := os.MkdirAll(hookDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create hooks directory: %v\n", err)
			return
		}

		hookPath := filepath.Join(hookDir, "clictl-network-guard.sh")
		hookContent := generateNetworkGuardHook(spec.Name, networkHosts)
		if err := os.WriteFile(hookPath, []byte(hookContent), 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write network guard hook: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "  Network guard hook: %s\n", hookPath)

	default:
		// Embed network restriction as a SKILL.md directive (advisory)
		fmt.Fprintf(os.Stderr, "  Network hosts declared: %s (advisory for %s target)\n",
			strings.Join(networkHosts, ", "), target)
	}
}

// generateNetworkGuardHook creates a bash hook script that checks if bash
// commands reference hosts not in the skill's declared network permissions.
func generateNetworkGuardHook(skillName string, allowedHosts []string) string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("# clictl network guard hook - auto-generated\n")
	sb.WriteString(fmt.Sprintf("# Skill: %s\n", skillName))
	sb.WriteString("# This hook warns if a bash command references undeclared hosts.\n")
	sb.WriteString("set -euo pipefail\n\n")
	sb.WriteString("COMMAND=\"$1\"\n")
	sb.WriteString(fmt.Sprintf("ALLOWED_HOSTS=\"%s\"\n\n", strings.Join(allowedHosts, " ")))
	sb.WriteString("# Extract potential hostnames from the command\n")
	sb.WriteString("for word in $COMMAND; do\n")
	sb.WriteString("  # Check if it looks like a hostname or URL\n")
	sb.WriteString("  host=$(echo \"$word\" | grep -oP '(?:https?://)?([a-zA-Z0-9.-]+\\.[a-zA-Z]{2,})' | head -1 || true)\n")
	sb.WriteString("  if [ -n \"$host\" ]; then\n")
	sb.WriteString("    # Strip protocol prefix\n")
	sb.WriteString("    host=$(echo \"$host\" | sed 's|https\\?://||')\n")
	sb.WriteString("    found=false\n")
	sb.WriteString("    for allowed in $ALLOWED_HOSTS; do\n")
	sb.WriteString("      if [ \"$host\" = \"$allowed\" ] || [[ \"$host\" == *\".$allowed\" ]]; then\n")
	sb.WriteString("        found=true\n")
	sb.WriteString("        break\n")
	sb.WriteString("      fi\n")
	sb.WriteString("    done\n")
	sb.WriteString("    if [ \"$found\" = \"false\" ]; then\n")
	sb.WriteString(fmt.Sprintf("      echo \"clictl: WARNING: skill '%s' is accessing undeclared host: $host\" >&2\n", skillName))
	sb.WriteString(fmt.Sprintf("      echo \"clictl: Declared hosts: %s\" >&2\n", strings.Join(allowedHosts, ", ")))
	sb.WriteString("    fi\n")
	sb.WriteString("  fi\n")
	sb.WriteString("done\n")
	return sb.String()
}

// installSkillSetFlow installs all skills defined in a workspace skill set.
func installSkillSetFlow(cmd *cobra.Command, setName, target string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

	if cfg.Auth.ActiveWorkspace == "" {
		return fmt.Errorf("--skill-set requires an active workspace. Run 'clictl workspace switch' first")
	}

	skillSet, err := loadSkillSet(cfg.Auth.ActiveWorkspace, setName)
	if err != nil {
		return fmt.Errorf("loading skill set %q: %w", setName, err)
	}

	cache := registry.NewCache(cfg.CacheDir)
	client := registry.NewClient(cfg.APIURL, cache, flagNoCache)
	token := config.ResolveAuthToken(flagAPIKey, cfg)
	if token != "" {
		client.AuthToken = token
	}

	fmt.Printf("Installing skill set: %s (%d skills)\n", skillSet.Name, len(skillSet.Skills))
	if skillSet.Locked {
		fmt.Fprintf(os.Stderr, "  This skill set is locked. Installed skills cannot be modified.\n")
	}

	visited := make(map[string]bool)

	for _, toolName := range skillSet.Skills {
		spec, _, err := client.GetSpecYAML(cmd.Context(), toolName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: failed to fetch - %v\n", toolName, err)
			continue
		}

		// Apply skill set overrides
		override := findSkillOverride(skillSet.Overrides, toolName)
		if override != nil {
			if override.Blocked {
				fmt.Fprintf(os.Stderr, "  %s: blocked by skill set policy\n", toolName)
				continue
			}
			applySkillOverride(spec, override)
		}

		// Workspace policy check
		if err := checkWorkspacePolicy(cfg, spec); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: workspace policy violation - %v\n", toolName, err)
			continue
		}

		// Install the skill
		if spec.IsSkill() {
			path, installErr := installSkillFromSource(cmd.Context(), spec, target)
			if installErr != nil {
				fmt.Fprintf(os.Stderr, "  %s: install failed - %v\n", toolName, installErr)
				continue
			}
			fmt.Printf("  Installed %s: %s\n", toolName, path)
		} else {
			path, installErr := generateSkillForTarget(spec, target)
			if installErr != nil {
				fmt.Fprintf(os.Stderr, "  %s: install failed - %v\n", toolName, installErr)
				continue
			}
			fmt.Printf("  Installed %s: %s\n", toolName, path)
		}

		applyIsolationRules(spec, target)

		// Bash filter hook
		if len(sandboxCommands(spec)) > 0 && target == "claude-code" {
			if hookErr := generateBashFilterHook(spec.Name, sandboxCommands(spec), "."); hookErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: bash filter hook failed for %s: %v\n", spec.Name, hookErr)
			}
		}

		if err := addToInstalled(spec.Name); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not track %s: %v\n", spec.Name, err)
		}

		// Audit event
		if cfg.Auth.ActiveWorkspace != "" {
			if auditErr := postSkillAuditEvent(cfg.Auth.ActiveWorkspace, "skill.installed", spec.Name, map[string]interface{}{
				"version": spec.Version, "target": target, "skill_set": setName,
			}); auditErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not post audit event: %v\n", auditErr)
			}
		}

		visited[spec.Name] = true
		installDependencies(cmd.Context(), spec, target, client, visited)
	}

	fmt.Printf("Skill set %q installed.\n", setName)
	return nil
}

// SkillOverride represents workspace-level permission overrides for a specific tool.
type SkillOverride struct {
	ToolName       string   `json:"tool_name"`
	FilesystemRead []string `json:"filesystem_read"`
	FilesystemWrite []string `json:"filesystem_write"`
	BashAllow      []string `json:"bash_allow"`
	Network        []string `json:"network"`
	Blocked        bool     `json:"blocked"`
}

// SkillSet represents a collection of skills with per-skill overrides.
type SkillSet struct {
	Name       string          `json:"name"`
	Skills     []string        `json:"skills"`
	Locked     bool            `json:"locked"`
	Overrides  []SkillOverride `json:"overrides"`
}

// loadSkillOverrides loads skill overrides from cached workspace data.
func loadSkillOverrides(slug string) ([]SkillOverride, error) {
	path := filepath.Join(config.BaseDir(), "workspace-cache", slug+"-overrides.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var overrides []SkillOverride
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, err
	}
	return overrides, nil
}

// findSkillOverride finds the override for a specific tool name, if any.
func findSkillOverride(overrides []SkillOverride, toolName string) *SkillOverride {
	for i, o := range overrides {
		if o.ToolName == toolName {
			return &overrides[i]
		}
	}
	return nil
}

// applySkillOverride merges a workspace override into a spec, tightening permissions.
// Override paths must be a subset of spec-declared paths; any override path not in
// the spec is silently ignored.
func applySkillOverride(spec *models.ToolSpec, override *SkillOverride) {
	if override == nil {
		return
	}

	// Tighten filesystem read
	if len(override.FilesystemRead) > 0 {
		if spec.Sandbox == nil {
			spec.Sandbox = &models.Sandbox{}
		}
		if spec.Sandbox.Filesystem == nil {
			spec.Sandbox.Filesystem = &models.FilesystemPermissions{}
		}
		spec.Sandbox.Filesystem.Read = intersectPaths(spec.Sandbox.Filesystem.Read, override.FilesystemRead)
	}

	// Tighten filesystem write
	if len(override.FilesystemWrite) > 0 {
		if spec.Sandbox == nil {
			spec.Sandbox = &models.Sandbox{}
		}
		if spec.Sandbox.Filesystem == nil {
			spec.Sandbox.Filesystem = &models.FilesystemPermissions{}
		}
		spec.Sandbox.Filesystem.Write = intersectPaths(spec.Sandbox.Filesystem.Write, override.FilesystemWrite)
	}

	// Tighten bash allow
	if len(override.BashAllow) > 0 {
		if spec.Sandbox == nil {
			spec.Sandbox = &models.Sandbox{}
		}
		spec.Sandbox.Commands = intersectPatterns(spec.Sandbox.Commands, override.BashAllow)
	}

	// Tighten network
	if len(override.Network) > 0 {
		if spec.Sandbox == nil {
			spec.Sandbox = &models.Sandbox{}
		}
		if spec.Sandbox.Network == nil {
			spec.Sandbox.Network = &models.NetworkPermissions{}
		}
		spec.Sandbox.Network.Allow = intersectPaths(spec.Sandbox.Network.Allow, override.Network)
	}
}

// intersectPaths returns only those entries from specPaths that appear in overridePaths.
func intersectPaths(specPaths, overridePaths []string) []string {
	if len(specPaths) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(overridePaths))
	for _, p := range overridePaths {
		allowed[p] = true
	}
	var result []string
	for _, p := range specPaths {
		if allowed[p] {
			result = append(result, p)
		}
	}
	return result
}

// intersectPatterns returns only those entries from specPatterns that appear in overridePatterns.
func intersectPatterns(specPatterns, overridePatterns []string) []string {
	if len(specPatterns) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(overridePatterns))
	for _, p := range overridePatterns {
		allowed[p] = true
	}
	var result []string
	for _, p := range specPatterns {
		if allowed[p] {
			result = append(result, p)
		}
	}
	return result
}

// loadSkillSet loads a skill set definition from cached workspace data.
func loadSkillSet(slug, setName string) (*SkillSet, error) {
	path := filepath.Join(config.BaseDir(), "workspace-cache", slug+"-skillsets.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sets []SkillSet
	if err := json.Unmarshal(data, &sets); err != nil {
		return nil, err
	}
	for i, s := range sets {
		if s.Name == setName {
			return &sets[i], nil
		}
	}
	return nil, fmt.Errorf("skill set %q not found in workspace", setName)
}

// generateBashFilterHook generates a hook script at .claude/hooks/clictl-bash-filter-{skillName}.sh
// that validates bash commands against an allowlist before execution.
func generateBashFilterHook(skillName string, patterns []string, installDir string) error {
	hookDir := filepath.Join(installDir, ".claude", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}

	hookPath := filepath.Join(hookDir, fmt.Sprintf("clictl-bash-filter-%s.sh", skillName))
	content := generateBashFilterHookContent(skillName, patterns)

	if err := os.WriteFile(hookPath, []byte(content), 0o700); err != nil {
		return fmt.Errorf("writing bash filter hook: %w", err)
	}

	return nil
}

// generateBashFilterHookContent creates the content for a bash filter hook script.
func generateBashFilterHookContent(skillName string, patterns []string) string {
	var sb strings.Builder

	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("# clictl bash filter hook - auto-generated\n")
	sb.WriteString(fmt.Sprintf("# Skill: %s\n", skillName))
	sb.WriteString("# This hook validates bash commands against the skill's allowlist.\n")
	sb.WriteString("set -euo pipefail\n\n")

	sb.WriteString("# Read command from first argument or stdin\n")
	sb.WriteString("if [ $# -gt 0 ]; then\n")
	sb.WriteString("  COMMAND=\"$1\"\n")
	sb.WriteString("else\n")
	sb.WriteString("  COMMAND=$(cat)\n")
	sb.WriteString("fi\n\n")

	sb.WriteString("# Allowlist patterns\n")
	sb.WriteString("ALLOWED_PATTERNS=(\n")
	for _, p := range patterns {
		// Escape single quotes in patterns
		escaped := strings.ReplaceAll(p, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("  '%s'\n", escaped))
	}
	sb.WriteString(")\n\n")

	sb.WriteString("# Check for shell operators\n")
	sb.WriteString("has_operator=false\n")
	sb.WriteString("for op in '|' ';' '&&' '||' '`' '$('; do\n")
	sb.WriteString("  if [[ \"$COMMAND\" == *\"$op\"* ]]; then\n")
	sb.WriteString("    has_operator=true\n")
	sb.WriteString("    break\n")
	sb.WriteString("  fi\n")
	sb.WriteString("done\n\n")

	sb.WriteString("# Match against patterns\n")
	sb.WriteString("matched=false\n")
	sb.WriteString("for pattern in \"${ALLOWED_PATTERNS[@]}\"; do\n")
	sb.WriteString("  if [ \"$has_operator\" = \"true\" ]; then\n")
	sb.WriteString("    # For commands with operators, require exact match\n")
	sb.WriteString("    if [ \"$COMMAND\" = \"$pattern\" ]; then\n")
	sb.WriteString("      matched=true\n")
	sb.WriteString("      break\n")
	sb.WriteString("    fi\n")
	sb.WriteString("  else\n")
	sb.WriteString("    # For simple commands, support glob matching\n")
	sb.WriteString("    if [ \"$COMMAND\" = \"$pattern\" ]; then\n")
	sb.WriteString("      matched=true\n")
	sb.WriteString("      break\n")
	sb.WriteString("    fi\n")
	sb.WriteString("    # Check glob pattern\n")
	sb.WriteString("    if [[ \"$COMMAND\" == $pattern ]]; then\n")
	sb.WriteString("      matched=true\n")
	sb.WriteString("      break\n")
	sb.WriteString("    fi\n")
	sb.WriteString("  fi\n")
	sb.WriteString("done\n\n")

	sb.WriteString("if [ \"$matched\" = \"false\" ]; then\n")
	patternsStr := strings.Join(patterns, ", ")
	sb.WriteString(fmt.Sprintf("  echo \"Command '$COMMAND' not in skill allowlist for %s. Allowed patterns: %s\" >&2\n", skillName, patternsStr))
	sb.WriteString("  exit 1\n")
	sb.WriteString("fi\n\n")

	sb.WriteString("exit 0\n")

	return sb.String()
}

// postSkillAuditEvent posts an audit event to the workspace API.
// If the POST fails, it logs a warning but does not block the install.
func postSkillAuditEvent(slug string, eventType string, toolName string, details map[string]interface{}) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	token := config.ResolveAuthToken(flagAPIKey, cfg)
	if token == "" {
		return nil // No auth, skip audit
	}

	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/audit-log/", apiURL, slug)

	payload := map[string]interface{}{
		"action":      eventType,
		"target_type": "skill",
		"target_id":   toolName,
		"details":     details,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling audit event: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("creating audit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("posting audit event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("audit event failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// parseToolVersion splits a tool argument into name and optional version.
// Supports formats: tool@1.0.0, @ns/tool@1.0.0, @ns/tool
func parseToolVersion(arg string) (name, version string) {
	// Handle @ns/tool@version
	if strings.HasPrefix(arg, "@") {
		// Find the second @ (version separator)
		rest := arg[1:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx == -1 {
			return arg, ""
		}
		afterSlash := rest[slashIdx+1:]
		atIdx := strings.Index(afterSlash, "@")
		if atIdx == -1 {
			return arg, ""
		}
		name = arg[:1+slashIdx+1+atIdx]
		version = afterSlash[atIdx+1:]
		return name, version
	}

	// Handle tool@version
	atIdx := strings.LastIndex(arg, "@")
	if atIdx == -1 || atIdx == 0 {
		return arg, ""
	}
	return arg[:atIdx], arg[atIdx+1:]
}

func init() {
	installCmd.Flags().BoolVar(&installNoMCP, "no-mcp", false, "Skip MCP server registration (install skill only)")
	installCmd.Flags().BoolVar(&installNoSkill, "no-skill", false, "Skip skill file generation (register MCP only)")
	installCmd.Flags().StringVar(&installTarget, "target", "", "Target AI tool: claude-code, gemini, codex, cursor, windsurf, goose, cline, roo-code, amazon-q, boltai (auto-detected if omitted)")
	installCmd.Flags().BoolVar(&installWorkspace, "workspace", false, "Sync installed tools to the active workspace")
	installCmd.Flags().BoolVar(&installYes, "yes", false, "Skip permission confirmation prompts")
	installCmd.Flags().BoolVar(&installTrust, "trust", false, "Install tools from unverified publishers")
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Show what config would be generated without writing files")
	installCmd.Flags().BoolVar(&installAllowUnsigned, "allow-unsigned", false, "Suppress warnings for unsigned skills")
	installCmd.Flags().StringVar(&installSkillSet, "skill-set", "", "Install all skills in a workspace skill set")
	installCmd.Flags().BoolVar(&installNoShims, "no-shims", false, "Skip shim generation in ~/.clictl/bin/")
	installCmd.Flags().BoolVar(&installGlobal, "global", false, "Install to ~/.clictl/ instead of project-scoped .claude/skills/")
	installCmd.Flags().StringVar(&installAs, "as", "", "Alias the tool to a different local name")
	installCmd.Flags().StringVar(&installFrom, "from", "", "Specify source namespace when ambiguous")
	rootCmd.AddCommand(installCmd)
}
