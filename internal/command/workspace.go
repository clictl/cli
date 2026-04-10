// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspace settings",
}

type workspaceEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch [slug]",
	Short: "Set the active workspace",
	Long: `Switch the active workspace. When a workspace is active, tool execution
is gated by the workspace's permission policy.

If no slug is provided, fetches your workspaces from the API and presents
an interactive picker. Use an empty string to clear the active workspace.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		var slug string

		if len(args) == 1 {
			slug = args[0]
		} else {
			// Interactive: fetch workspaces and let user pick
			picked, err := pickWorkspace(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			slug = picked
		}

		cfg.Auth.ActiveWorkspace = slug
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		if slug == "" {
			fmt.Println("Active workspace cleared.")
		} else {
			fmt.Printf("Active workspace set to %q.\n", slug)
		}
		return nil
	},
}

func pickWorkspace(ctx context.Context, cfg *config.Config) (string, error) {
	token := config.ResolveAuthToken("", cfg)
	if token == "" {
		return "", fmt.Errorf("not logged in. Run: clictl login")
	}

	apiURL := config.ResolveAPIURL("", cfg)
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL+"/api/v1/workspaces/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching workspaces: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var workspaces []workspaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return "", fmt.Errorf("parsing workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		return "", fmt.Errorf("no workspaces found")
	}

	// Display list
	current := cfg.Auth.ActiveWorkspace
	fmt.Println("Your workspaces:")
	fmt.Println()
	for i, ws := range workspaces {
		marker := "  "
		if ws.Slug == current {
			marker = "> "
		}
		fmt.Printf("  %s%d. %s (%s)\n", marker, i+1, ws.Name, ws.Slug)
	}
	fmt.Println()

	// Prompt for selection
	fmt.Printf("Select workspace (1-%d): ", len(workspaces))
	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}

	input = strings.TrimSpace(input)
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err != nil || idx < 1 || idx > len(workspaces) {
		// Try matching by slug
		for _, ws := range workspaces {
			if ws.Slug == input || ws.Name == input {
				return ws.Slug, nil
			}
		}
		return "", fmt.Errorf("invalid selection: %s", input)
	}

	return workspaces[idx-1].Slug, nil
}

var workspaceShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current active workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		ws := cfg.Auth.ActiveWorkspace
		if ws == "" {
			fmt.Println("No active workspace set. Run: clictl workspace switch")
		} else {
			fmt.Printf("Active workspace: %s\n", ws)
		}
		return nil
	},
}

func init() {
	workspaceCmd.AddCommand(workspaceSwitchCmd)
	workspaceCmd.AddCommand(workspaceShowCmd)
	rootCmd.AddCommand(workspaceCmd)
}
