// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var (
	upgradeAll      bool
	upgradeYes      bool
	upgradeDryRun   bool
	upgradeSkillSet string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [tool...]",
	Short: "Upgrade installed tools to their latest versions",
	Long: `Upgrade tools to the latest spec from the registry.

  clictl upgrade github stripe     # upgrade specific tools
  clictl upgrade --all             # upgrade all installed tools
  clictl upgrade --all --yes       # skip confirmation prompts`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

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

		// Skill set mode: upgrade all skills in a set
		if upgradeSkillSet != "" {
			if cfg.Auth.ActiveWorkspace == "" {
				return fmt.Errorf("--skill-set requires an active workspace. Run 'clictl workspace switch' first")
			}
			skillSet, setErr := loadSkillSet(cfg.Auth.ActiveWorkspace, upgradeSkillSet)
			if setErr != nil {
				return fmt.Errorf("loading skill set %q: %w", upgradeSkillSet, setErr)
			}
			args = skillSet.Skills
			fmt.Printf("Upgrading skill set: %s (%d skills)\n", skillSet.Name, len(args))
		}

		// Determine which tools to upgrade
		tools := args
		if upgradeAll || len(tools) == 0 {
			tools = loadInstalled()
			if len(tools) == 0 {
				fmt.Println("No tools installed.")
				return nil
			}
		}

		// Load lock file to get installed versions
		lockFile, _ := LoadLockFile()

		target := installTarget
		if target == "" {
			target = detectTarget()
		}

		upgraded := 0
		skipped := 0
		failed := 0

		for _, toolName := range tools {
			// Fetch latest spec from registry
			latestSpec, latestYAML, err := client.GetSpecYAML(ctx, toolName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: failed to fetch - %v\n", toolName, err)
				failed++
				continue
			}

			// Determine installed version from lock file
			oldVersion := ""
			if lockFile != nil {
				if entry, ok := lockFile.Tools[toolName]; ok {
					oldVersion = entry.Version
				}
			}

			newVersion := latestSpec.Version

			// Check if upgrade is needed
			if oldVersion != "" && oldVersion == newVersion {
				skipped++
				continue
			}

			// Check for breaking changes via version diff
			hasBreaking := false
			if oldVersion != "" && isMajorBump(oldVersion, newVersion) {
				hasBreaking = true
				fmt.Fprintf(os.Stderr, "Warning: %s has a major version bump (%s -> %s). This may include breaking changes.\n", toolName, oldVersion, newVersion)
			}

			// Show upgrade info
			if oldVersion != "" {
				fmt.Printf("Upgrading %s from v%s to v%s\n", toolName, oldVersion, newVersion)
			} else {
				fmt.Printf("Upgrading %s to v%s\n", toolName, newVersion)
			}

			// Fetch and display version diff with breaking change details
			if oldVersion != "" && oldVersion != newVersion {
				diff, diffErr := client.GetVersionDiff(ctx, toolName, oldVersion, newVersion)
				if diffErr != nil {
					fmt.Fprintf(os.Stderr, "  (could not fetch version diff: %v)\n", diffErr)
				} else if diff != nil {
					if diff.Summary != "" {
						fmt.Printf("  Changes: %s\n", diff.Summary)
					}
					for _, ch := range diff.Changes {
						fmt.Printf("    %s: %s -> %s\n", ch.Field, ch.Old, ch.New)
					}
					if diff.IsBreaking {
						hasBreaking = true
						fmt.Fprintf(os.Stderr, "\n  ** BREAKING CHANGES DETECTED **\n")
						for _, reason := range diff.BreakingReasons {
							fmt.Fprintf(os.Stderr, "    - %s\n", reason)
						}
						fmt.Fprintln(os.Stderr)
					}
				}
			}

			// Dry-run mode: show what would change, do not install
			if upgradeDryRun {
				label := "update"
				if hasBreaking {
					label = "BREAKING update"
				}
				fmt.Printf("  [dry-run] Would %s %s: %s -> %s\n", label, toolName, oldVersion, newVersion)
				upgraded++
				continue
			}

			// Ask for confirmation unless --yes (extra warning for breaking changes)
			if !upgradeYes {
				if hasBreaking {
					fmt.Printf("This upgrade contains BREAKING changes. Proceed? [y/N] ")
				} else {
					fmt.Printf("Proceed with upgrade? [Y/n] ")
				}
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(strings.ToLower(input))
				if hasBreaking {
					// Breaking changes default to NO
					if input != "y" && input != "yes" {
						fmt.Printf("  %s: skipped (breaking)\n", toolName)
						skipped++
						continue
					}
				} else {
					if input != "" && input != "y" && input != "yes" {
						fmt.Printf("  %s: skipped\n", toolName)
						skipped++
						continue
					}
				}
			}

			// Re-run install flow: generate skill file for the target
			if latestSpec.IsSkill() {
				path, installErr := installSkillFromSource(ctx, latestSpec, target)
				if installErr != nil {
					fmt.Fprintf(os.Stderr, "  %s: failed to install skill - %v\n", toolName, installErr)
					failed++
					continue
				}
				fmt.Printf("  %s: upgraded to %s (%s)\n", toolName, newVersion, path)
			} else {
				path, installErr := generateSkillForTarget(latestSpec, target)
				if installErr != nil {
					fmt.Fprintf(os.Stderr, "  %s: failed to generate skill - %v\n", toolName, installErr)
					failed++
					continue
				}
				fmt.Printf("  %s: upgraded to %s (%s)\n", toolName, newVersion, path)
			}

			// Ensure tracked in installed list
			if err := addToInstalled(latestSpec.Name); err != nil {
				fmt.Fprintf(os.Stderr, "  %s: failed to track install - %v\n", toolName, err)
			}

			// Update lock file entry if lock file exists
			if lockFile != nil {
				lockFile.Tools[toolName] = LockEntry{
					Version:       newVersion,
					ETag:          computeETag(latestYAML),
					ContentSHA256: computeContentSHA256(latestYAML),
				}
			}

			// Workspace audit event (Phase 3)
			if cfg.Auth.ActiveWorkspace != "" {
				if auditErr := postSkillAuditEvent(cfg.Auth.ActiveWorkspace, "skill.upgraded", toolName, map[string]interface{}{
					"old_version": oldVersion, "new_version": newVersion, "target": target,
				}); auditErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not post audit event: %v\n", auditErr)
				}
			}

			upgraded++
		}

		// Write updated lock file if it was modified (skip in dry-run mode)
		if lockFile != nil && upgraded > 0 && !upgradeDryRun {
			lockFile.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			if err := writeLockFile(lockFile); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not update lock file: %v\n", err)
			} else {
				fmt.Printf("Lock file updated: %s\n", lockFilePath())
			}
		}

		if upgraded == 0 && failed == 0 {
			fmt.Println("All tools up to date")
			return nil
		}

		if upgradeDryRun {
			fmt.Printf("\n[dry-run] %d tool(s) would be upgraded", upgraded)
		} else {
			fmt.Printf("\n%d tool(s) upgraded", upgraded)
		}
		if skipped > 0 {
			fmt.Printf(", %d already up to date", skipped)
		}
		if failed > 0 {
			fmt.Printf(", %d failed", failed)
		}
		fmt.Println(".")

		if failed > 0 {
			return fmt.Errorf("%d tool(s) failed to upgrade", failed)
		}
		return nil
	},
}

// showOutdatedHint checks for outdated tools and prints a hint after install.
func showOutdatedHint(ctx context.Context, cfg *config.Config) {
	installed := loadInstalled()
	if len(installed) == 0 {
		return
	}

	bucketsDir := config.ToolboxesDir()
	cache := registry.NewCache(cfg.CacheDir)

	var count int
	for _, name := range installed {
		spec, err := registry.ResolveSpec(ctx, name, cfg, cache, false)
		if err != nil {
			continue
		}
		installedVersion := spec.Version

		for _, reg := range cfg.Toolboxes {
			regDir := filepath.Join(bucketsDir, reg.Name)
			li := registry.NewLocalIndex(regDir, reg.Name)
			entry, err := li.GetEntry(name)
			if err != nil {
				continue
			}
			if entry.Version != "" && entry.Version != installedVersion {
				count++
			}
			break
		}
	}

	if count > 0 {
		fmt.Fprintf(os.Stderr, "\nYou have %d outdated %s installed.\n", count, pluralize(count, "tool", "tools"))
		fmt.Fprintf(os.Stderr, "Run `clictl upgrade --all` to update.\n")
	}
}

// isMajorBump returns true if the new version has a higher major version than old.
func isMajorBump(oldVersion, newVersion string) bool {
	oldMajor := majorVersion(oldVersion)
	newMajor := majorVersion(newVersion)
	return oldMajor >= 0 && newMajor >= 0 && newMajor > oldMajor
}

// majorVersion extracts the major version number from a semver string.
// Returns -1 if the version cannot be parsed.
func majorVersion(v string) int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 {
		return -1
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1
	}
	return n
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeAll, "all", false, "Upgrade all installed tools")
	upgradeCmd.Flags().BoolVar(&upgradeYes, "yes", false, "Skip confirmation prompts")
	upgradeCmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Show what would change without actually upgrading")
	upgradeCmd.Flags().StringVar(&installTarget, "target", "", "Target AI tool: claude-code, gemini, codex, cursor, windsurf (auto-detected if omitted)")
	upgradeCmd.Flags().StringVar(&upgradeSkillSet, "skill-set", "", "Upgrade all skills in a workspace skill set")
	rootCmd.AddCommand(upgradeCmd)
}
