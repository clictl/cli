// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var outdatedCmd = &cobra.Command{
	Use:   "outdated",
	Short: "Show installed tools with newer versions available",
	Long: `Check installed tools against publisher registries for newer versions.
Compares the installed spec_version against the latest available version.

  clictl outdated`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		installed := loadInstalled()
		if len(installed) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No tools installed.")
			return nil
		}

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(cfg.APIURL, cache, true) // bypass cache

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		bucketsDir := config.ToolboxesDir()

		// Load lock file for installed versions
		lockFile, _ := LoadLockFile()

		type outdatedEntry struct {
			name             string
			installedVersion string
			latestVersion    string
			status           string // "outdated", "deprecated", "delisted"
		}

		var outdatedTools []outdatedEntry

		for _, toolName := range installed {
			// Determine installed version
			installedVersion := ""
			if lockFile != nil {
				if entry, inLock := lockFile.Tools[toolName]; inLock {
					installedVersion = entry.Version
				}
			}
			if installedVersion == "" {
				localSpec, resolveErr := registry.ResolveSpec(ctx, toolName, cfg, cache, false)
				if resolveErr == nil {
					installedVersion = localSpec.Version
				}
			}

			// Determine latest version from registry index
			latestVersion := ""
			for _, reg := range cfg.Toolboxes {
				regDir := filepath.Join(bucketsDir, reg.Name)
				li := registry.NewLocalIndex(regDir, reg.Name)
				entry, entryErr := li.GetEntry(toolName)
				if entryErr != nil {
					continue
				}
				latestVersion = entry.Version
				break
			}

			// Fall back to fetching from API
			if latestVersion == "" {
				spec, _, fetchErr := client.GetSpecYAML(ctx, toolName)
				if fetchErr != nil {
					// P6.12: Tool may be delisted or publisher deactivated
					outdatedTools = append(outdatedTools, outdatedEntry{
						name:             toolName,
						installedVersion: installedVersion,
						latestVersion:    "unavailable",
						status:           "delisted",
					})
					continue
				}
				latestVersion = spec.Version

				// P6.12: Check if tool is deprecated / publisher deactivated
				if spec.Deprecated {
					delistStatus := "deprecated"
					if spec.DeprecatedBy != "" {
						delistStatus = fmt.Sprintf("deprecated (use %s)", spec.DeprecatedBy)
					}
					outdatedTools = append(outdatedTools, outdatedEntry{
						name:             toolName,
						installedVersion: installedVersion,
						latestVersion:    latestVersion,
						status:           delistStatus,
					})
					continue
				}
			}

			if installedVersion == "" || latestVersion == "" {
				continue
			}
			if installedVersion != latestVersion {
				outdatedTools = append(outdatedTools, outdatedEntry{
					name:             toolName,
					installedVersion: installedVersion,
					latestVersion:    latestVersion,
					status:           "outdated",
				})
			}
		}

		if len(outdatedTools) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "All installed tools are up to date.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tINSTALLED\tLATEST\tSTATUS")
		for _, t := range outdatedTools {
			installedStr := ""
			if t.installedVersion != "" {
				installedStr = "v" + t.installedVersion
			}
			latestStr := t.latestVersion
			if latestStr != "unavailable" && latestStr != "" {
				latestStr = "v" + latestStr
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.name, installedStr, latestStr, t.status)
		}
		if err := w.Flush(); err != nil {
			return err
		}

		// P6.12: Print advice for delisted/deprecated tools
		hasDelisted := false
		for _, t := range outdatedTools {
			if t.status == "delisted" || strings.HasPrefix(t.status, "deprecated") {
				hasDelisted = true
				break
			}
		}
		if hasDelisted {
			fmt.Fprintln(cmd.OutOrStdout(), "\nSome tools are delisted or deprecated. Consider uninstalling them:")
			for _, t := range outdatedTools {
				if t.status == "delisted" {
					fmt.Fprintf(cmd.OutOrStdout(), "  clictl uninstall %s  # no longer available in registry\n", t.name)
				} else if strings.HasPrefix(t.status, "deprecated") {
					fmt.Fprintf(cmd.OutOrStdout(), "  clictl uninstall %s  # %s\n", t.name, t.status)
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(outdatedCmd)
}
