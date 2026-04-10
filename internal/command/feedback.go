// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/memory"
	"github.com/spf13/cobra"
)

var feedbackCmd = &cobra.Command{
	Use:   "feedback <tool> <up|down>",
	Short: "Rate a tool for its maintainer",
	Long: `Submit feedback on a tool. Saved locally and synced to the platform if logged in.

Examples:
  clictl feedback openweathermap up
  clictl feedback openweathermap down --reason "API returns 500 for city names with spaces"
  clictl feedback slack up --label accurate --comment "Great API coverage"
  clictl feedback github down --label outdated --reason "Missing the new code search endpoint"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tool := args[0]
		direction := args[1]

		if direction != "up" && direction != "down" {
			return fmt.Errorf("direction must be 'up' or 'down', got %q", direction)
		}

		label, _ := cmd.Flags().GetString("label")
		comment, _ := cmd.Flags().GetString("comment")
		reason, _ := cmd.Flags().GetString("reason")

		score := 5
		if direction == "down" {
			score = 1
		}

		review := ""
		if label != "" {
			review = "[" + label + "]"
		}
		if reason != "" {
			if review != "" {
				review += " "
			}
			review += reason
		}
		if comment != "" {
			if review != "" {
				review += " "
			}
			review += comment
		}

		// Save feedback locally as a memory with type "feedback"
		feedbackNote := fmt.Sprintf("feedback:%s", direction)
		if review != "" {
			feedbackNote += " " + review
		}
		_ = memory.Add(tool, feedbackNote, memory.ParseType("feedback"))

		fmt.Fprintf(cmd.OutOrStdout(), "Feedback for %s: %s", tool, direction)
		if label != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " [%s]", label)
		}
		fmt.Fprintln(cmd.OutOrStdout())

		// Sync to platform if logged in
		cfg, _ := config.Load()
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			url := fmt.Sprintf("%s/api/v1/specs/%s/rate/", apiURL, tool)

			body, _ := json.Marshal(map[string]any{"score": score, "review": review})
			req, err := http.NewRequest("POST", url, bytes.NewReader(body))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode < 300 {
						fmt.Fprintln(cmd.OutOrStdout(), "Synced to platform. The maintainer will see your feedback.")
					}
				}
			}
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Saved locally. Log in to sync feedback to the maintainer.")
		}

		return nil
	},
}

func init() {
	feedbackCmd.Flags().String("label", "", "Label: accurate, helpful, outdated, inaccurate, incomplete")
	feedbackCmd.Flags().String("reason", "", "Why: what worked, what broke, what's missing")
	feedbackCmd.Flags().String("comment", "", "Optional additional comment for the maintainer")
	rootCmd.AddCommand(feedbackCmd)
}
