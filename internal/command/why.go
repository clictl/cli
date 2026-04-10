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

var whyCmd = &cobra.Command{
	Use:   "why <tool>",
	Short: "Show how a tool was installed (direct, via group, as dependency)",
	Long: `Show installation provenance for a tool in your workspace.

Reports whether the tool was installed directly, as part of a group/pack,
or as a dependency of another tool.

  clictl why openweathermap`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("login required: run 'clictl login' first")
		}

		result, err := fetchProvenance(ctx, cfg.APIURL, token, toolName)
		if err != nil {
			return err
		}

		// Display results
		fmt.Fprintf(cmd.OutOrStdout(), "Tool:       %s\n", toolName)
		fmt.Fprintf(cmd.OutOrStdout(), "Source:     %s\n", result.InstallSource)
		if result.InstallSourceName != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Via:        %s\n", result.InstallSourceName)
		}
		if result.InstalledBy != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Installed by: %s\n", result.InstalledBy)
		}
		if result.InstalledAt != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Installed:  %s\n", result.InstalledAt)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Type:       %s\n", result.SourceType)

		return nil
	},
}

type provenanceResult struct {
	ToolName          string `json:"tool_name"`
	InstallSource     string `json:"install_source"`
	InstallSourceName string `json:"install_source_name"`
	InstalledBy       string `json:"installed_by"`
	InstalledAt       string `json:"installed_at"`
	SourceType        string `json:"source_type"`
}

func fetchProvenance(ctx context.Context, apiURL string, token string, toolName string) (*provenanceResult, error) {
	url := fmt.Sprintf("%s/api/v1/specs/%s/why/", apiURL, toolName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching provenance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tool '%s' not found in workspace", toolName)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result provenanceResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

func init() {
	rootCmd.AddCommand(whyCmd)
}
