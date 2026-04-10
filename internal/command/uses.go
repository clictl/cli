// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var usesCmd = &cobra.Command{
	Use:   "uses <tool>",
	Short: "Show reverse dependencies (what composite tools use this tool)",
	Long: `Show which composite tools reference the given tool in their steps.

  clictl uses github
  clictl uses openweathermap`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetTool := args[0]
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		bucketsDir := config.ToolboxesDir()
		cache := registry.NewCache(cfg.CacheDir)

		type usageEntry struct {
			specName   string
			actionName string
		}
		var usages []usageEntry

		// Scan all specs in local indexes
		for _, reg := range cfg.Toolboxes {
			regDir := filepath.Join(bucketsDir, reg.Name)
			li := registry.NewLocalIndex(regDir, reg.Name)
			idx, err := li.Load()
			if err != nil {
				continue
			}

			for specName := range idx.Specs {
				if specName == targetTool {
					continue
				}
				spec, resolveErr := registry.ResolveSpec(ctx, specName, cfg, cache, true)
				if resolveErr != nil {
					continue
				}
				for _, action := range spec.Actions {
					if !action.IsComposite() {
						continue
					}
					for _, step := range action.Steps {
						if step.Tool == targetTool {
							usages = append(usages, usageEntry{
								specName:   specName,
								actionName: action.Name,
							})
							break
						}
					}
				}
			}
		}

		if len(usages) == 0 {
			fmt.Printf("No tools reference %q in their composite actions.\n", targetTool)
			return nil
		}

		fmt.Printf("Tools that use %q:\n\n", targetTool)
		for _, u := range usages {
			fmt.Printf("  %s (action: %s)\n", u.specName, u.actionName)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(usesCmd)
}
