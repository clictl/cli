// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
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

var (
	flagActivityWorkspace bool
	flagActivityTool      string
	flagActivityLimit     int
)

var activityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Show recent tool invocations",
	Long:  "Show your recent tool invocations. Use --workspace for workspace-wide view (admin only).",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in - run `clictl login` first")
		}
		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

		if flagActivityWorkspace {
			ws := cfg.Auth.ActiveWorkspace
			if ws == "" {
				return fmt.Errorf("no active workspace - run `clictl login` first")
			}
			return showWorkspaceActivity(cmd.Context(), apiURL, ws, token)
		}
		return showMyActivity(cmd.Context(), apiURL, token)
	},
}

type activityEntry struct {
	ID            string `json:"id"`
	ToolName      string `json:"tool_name"`
	Action        string `json:"action"`
	Status        string `json:"status"`
	LatencyMs     *int   `json:"latency_ms"`
	Client        string `json:"client"`
	Error         string `json:"error"`
	Timestamp     string `json:"timestamp"`
	WorkspaceSlug string `json:"workspace_slug,omitempty"`
	User          string `json:"user,omitempty"`
	UserEmail     string `json:"user_email,omitempty"`
}

type activityResponse struct {
	Results    []activityEntry `json:"results"`
	NextCursor *string         `json:"next_cursor"`
}

func showMyActivity(ctx context.Context, apiURL, token string) error {
	u := fmt.Sprintf("%s/api/v1/me/logs/?page_size=%d", apiURL, flagActivityLimit)
	if flagActivityTool != "" {
		u += "&tool=" + url.QueryEscape(flagActivityTool)
	}

	data, err := fetchActivityJSON(ctx, u, token)
	if err != nil {
		return err
	}

	var resp activityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if len(resp.Results) == 0 {
		fmt.Println("No activity found.")
		return nil
	}

	fmt.Printf("%-35s %-20s %-10s %8s  %s\n", "TOOL", "ACTION", "STATUS", "LATENCY", "TIME")
	for _, e := range resp.Results {
		latency := "-"
		if e.LatencyMs != nil {
			latency = fmt.Sprintf("%dms", *e.LatencyMs)
		}
		ts := formatActivityTime(e.Timestamp)
		workspace := ""
		if e.WorkspaceSlug != "" {
			workspace = " [" + e.WorkspaceSlug + "]"
		}
		fmt.Printf("%-35s %-20s %-10s %8s  %s%s\n",
			truncate(e.ToolName, 35), truncate(e.Action, 20),
			e.Status, latency, ts, workspace)
	}
	return nil
}

func showWorkspaceActivity(ctx context.Context, apiURL, workspace, token string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/logs/?page_size=%d",
		apiURL, url.PathEscape(workspace), flagActivityLimit)
	if flagActivityTool != "" {
		u += "&tool=" + url.QueryEscape(flagActivityTool)
	}

	data, err := fetchActivityJSON(ctx, u, token)
	if err != nil {
		return err
	}

	var resp activityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if len(resp.Results) == 0 {
		fmt.Println("No activity found.")
		return nil
	}

	fmt.Printf("WORKSPACE ACTIVITY: %s\n\n", workspace)
	fmt.Printf("%-35s %-20s %-10s %8s  %-25s  %s\n", "TOOL", "ACTION", "STATUS", "LATENCY", "USER", "TIME")
	for _, e := range resp.Results {
		latency := "-"
		if e.LatencyMs != nil {
			latency = fmt.Sprintf("%dms", *e.LatencyMs)
		}
		ts := formatActivityTime(e.Timestamp)
		user := e.UserEmail
		if user == "" {
			user = e.User
		}
		if user == "" {
			user = "-"
		}
		fmt.Printf("%-35s %-20s %-10s %8s  %-25s  %s\n",
			truncate(e.ToolName, 35), truncate(e.Action, 20),
			e.Status, latency, truncate(user, 25), ts)
	}
	return nil
}

func fetchActivityJSON(ctx context.Context, u, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

func formatActivityTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("Jan 02 15:04:05")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func init() {
	activityCmd.Flags().BoolVar(&flagActivityWorkspace, "workspace", false, "Show workspace-wide activity (admin only)")
	activityCmd.Flags().StringVar(&flagActivityTool, "tool", "", "Filter by tool name")
	activityCmd.Flags().IntVar(&flagActivityLimit, "limit", 25, "Number of results to show")
	rootCmd.AddCommand(activityCmd)
}
