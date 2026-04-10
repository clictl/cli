// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/permissions"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var flagPermTool string

var permissionsCmd = &cobra.Command{
	Use:   "permissions",
	Short: "Check tool permissions for the active workspace",
	Long: `Check which tools and actions you have access to in the active workspace.
Requires login and an active workspace (see "clictl workspace switch").`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run \"clictl login\" first")
		}

		ws := cfg.Auth.ActiveWorkspace
		if ws == "" {
			return fmt.Errorf("no active workspace. Run \"clictl workspace switch <slug>\" first")
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		checker := permissions.NewChecker(apiURL, token)

		tool := flagPermTool
		if tool == "" {
			tool = "*"
		}

		allowed, canRequest, reason, err := checker.Check(ctx, ws, tool, "*")
		if err != nil {
			return fmt.Errorf("permission check failed: %w", err)
		}

		type permResult struct {
			Workspace  string `json:"workspace" yaml:"workspace"`
			Tool       string `json:"tool" yaml:"tool"`
			Allowed    bool   `json:"allowed" yaml:"allowed"`
			CanRequest bool   `json:"can_request" yaml:"can_request"`
			Reason     string `json:"reason,omitempty" yaml:"reason,omitempty"`
		}

		result := permResult{
			Workspace:  ws,
			Tool:       tool,
			Allowed:    allowed,
			CanRequest: canRequest,
			Reason:     reason,
		}

		switch flagOutput {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		case "yaml":
			return yaml.NewEncoder(os.Stdout).Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "WORKSPACE\tTOOL\tALLOWED\tCAN REQUEST\tREASON")
			canReqStr := "no"
			if result.CanRequest {
				canReqStr = "yes"
			}
			allowedStr := "denied"
			if result.Allowed {
				allowedStr = "allowed"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", result.Workspace, result.Tool, allowedStr, canReqStr, result.Reason)
			return w.Flush()
		}
	},
}

func init() {
	permissionsCmd.Flags().StringVar(&flagPermTool, "tool", "", "Check permissions for a specific tool (default: all)")
	rootCmd.AddCommand(permissionsCmd)
}
