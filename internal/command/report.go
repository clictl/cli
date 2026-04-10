// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/spf13/cobra"
)

var validReportReasons = []string{
	"broken_source",
	"malicious",
	"abandoned",
	"incorrect",
	"other",
}

func isValidReason(reason string) bool {
	for _, r := range validReportReasons {
		if r == reason {
			return true
		}
	}
	return false
}

var reportCmd = &cobra.Command{
	Use:   "report <tool>",
	Short: "Report a tool for review",
	Long: `Report a tool to the registry maintainers for review.

Reasons: broken_source, malicious, abandoned, incorrect, other

Examples:
  clictl report bad-tool --reason malicious
  clictl report old-tool --reason abandoned --description "No updates in 2 years"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		if reason == "" {
			return fmt.Errorf("--reason is required (broken_source, malicious, abandoned, incorrect, other)")
		}
		if !isValidReason(reason) {
			return fmt.Errorf("invalid reason %q - must be one of: broken_source, malicious, abandoned, incorrect, other", reason)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in - run `clictl login` first")
		}

		description, _ := cmd.Flags().GetString("description")
		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

		return submitReport(cmd.Context(), cmd, apiURL, token, args[0], reason, description)
	},
}

func submitReport(ctx context.Context, cmd *cobra.Command, apiURL, token, toolName, reason, description string) error {
	u := fmt.Sprintf("%s/api/v1/specs/%s/report/", apiURL, url.PathEscape(toolName))

	payload := map[string]string{"reason": reason}
	if description != "" {
		payload["description"] = description
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("submitting report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		fmt.Fprintf(cmd.OutOrStdout(), "Reported %s (reason: %s). Thank you.\n", toolName, reason)
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to report %s: %d %s", toolName, resp.StatusCode, string(respBody))
}

func init() {
	reportCmd.Flags().String("reason", "", "Reason for reporting: broken_source, malicious, abandoned, incorrect, other")
	reportCmd.Flags().String("description", "", "Optional description with more detail")
	rootCmd.AddCommand(reportCmd)
}
