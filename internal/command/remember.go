// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/memory"
	"github.com/spf13/cobra"
)

var rememberCmd = &cobra.Command{
	Use:   "remember <tool> <note>",
	Short: "Attach a memory to a tool",
	Long: `Save a note about a tool. Memories appear on inspect and in skill files.

Types: note (default), gotcha, tip, context, error

Examples:
  clictl remember openweathermap "use --units metric for EU"
  clictl remember github --type gotcha "rate limit is 5000/hr with token"
  clictl remember slack --type tip "batch messages to avoid rate limits"
  clictl remember my-api --type error "returns 500 if query has special chars"
  clictl remember my-api --type context "we use this for the daily digest"
  clictl remember my-api --share "team-wide note visible to all workspace members"`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tool := args[0]
		note := strings.Join(args[1:], " ")

		memType, _ := cmd.Flags().GetString("type")
		share, _ := cmd.Flags().GetBool("share")

		t := memory.ParseType(memType)

		if err := memory.Add(tool, note, t); err != nil {
			return fmt.Errorf("saving memory: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Remembered for %s [%s]: %s\n", tool, t, note)

		// Auto-sync to workspace if logged in, or explicit --share
		cfg, _ := config.Load()
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		shouldSync := share || (token != "" && cfg != nil && cfg.Auth.ActiveWorkspace != "")

		if shouldSync && token != "" && cfg != nil && cfg.Auth.ActiveWorkspace != "" {
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			syncURL := fmt.Sprintf("%s/api/v1/workspaces/%s/memories/", apiURL, cfg.Auth.ActiveWorkspace)
			body := fmt.Sprintf(`{"tool_name":"%s","note":"%s","memory_type":"%s"}`, tool, note, t)

			req, err := http.NewRequest("POST", syncURL, strings.NewReader(body))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode < 300 {
						fmt.Fprintf(cmd.OutOrStdout(), "Synced to workspace.\n")
					}
				}
			}
		} else if share && token == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Could not share: not logged in. Run 'clictl login' first.\n")
		}

		return nil
	},
}

func init() {
	rememberCmd.Flags().String("type", "note", "Memory type: note, gotcha, tip, context, error")
	rememberCmd.Flags().Bool("share", false, "Share this memory with your workspace")
	rootCmd.AddCommand(rememberCmd)
}
