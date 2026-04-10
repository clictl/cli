// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var homeDocs bool

var homeCmd = &cobra.Command{
	Use:   "home <tool>",
	Short: "Open a tool's website in the browser",
	Long: `Open the tool's website or documentation in the default browser.

  clictl home openweathermap           # opens website
  clictl home openweathermap --docs    # opens API reference docs`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
		if err != nil {
			msg := fmt.Sprintf("tool %q not found", toolName)
			if dym := toolSuggestion(toolName, cfg); dym != "" {
				msg += dym
			}
			return fmt.Errorf("%s", msg)
		}

		var url string
		if spec.Canonical != "" {
			url = spec.Canonical
		} else if spec.Pricing != nil && spec.Pricing.URL != "" {
			url = spec.Pricing.URL
		} else if spec.Server != nil && spec.Server.URL != "" {
			// Fall back to the API base URL
			url = spec.Server.URL
		}
		if url == "" {
			return fmt.Errorf("no URL found for %q", toolName)
		}

		fmt.Printf("Opening %s...\n", url)
		return openBrowser(url)
	},
}

func init() {
	homeCmd.Flags().BoolVar(&homeDocs, "docs", false, "Open the API documentation instead of the website")
	rootCmd.AddCommand(homeCmd)
}
