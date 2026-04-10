// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
)

var fundCmd = &cobra.Command{
	Use:   "fund <tool>",
	Short: "Open the funding URL for a tool",
	Long: `Open the funding/sponsorship URL for a tool in your browser.

If the tool has a funding URL configured, it will be opened automatically.

  clictl fund openweathermap`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		fundingURL, err := fetchFundingURL(ctx, cfg.APIURL, toolName)
		if err != nil {
			return err
		}

		if fundingURL == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "No funding URL configured for %s.\n", toolName)
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Opening funding page for %s: %s\n", toolName, fundingURL)
		return openBrowser(fundingURL)
	},
}

type fundingResponse struct {
	Name    string `json:"name"`
	Funding string `json:"funding"`
}

func fetchFundingURL(ctx context.Context, apiURL string, toolName string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/specs/%s/funding/", apiURL, toolName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching funding info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("tool '%s' not found", toolName)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result fundingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return result.Funding, nil
}

func init() {
	rootCmd.AddCommand(fundCmd)
}
