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

var flagMetricsDays int

var metricsCmd = &cobra.Command{
	Use:   "metrics [tool]",
	Short: "Show workspace or per-tool usage metrics",
	Long:  "Show workspace usage metrics, or per-tool detail when a tool name is provided.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in - run `clictl login` first")
		}
		ws := cfg.Auth.ActiveWorkspace
		if ws == "" {
			return fmt.Errorf("no active workspace - run `clictl login` first")
		}
		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

		if len(args) == 1 {
			return showToolMetrics(cmd.Context(), apiURL, ws, token, args[0])
		}
		return showOverviewMetrics(cmd.Context(), apiURL, ws, token)
	},
}

type metricsOverview struct {
	TotalInvocations int     `json:"total_invocations"`
	ActiveTools      int     `json:"active_tools"`
	ActiveUsers      int     `json:"active_users"`
	ErrorRate        float64 `json:"error_rate"`
	TopTools         []struct {
		Name        string  `json:"name"`
		Invocations int     `json:"invocations"`
		Errors      int     `json:"errors"`
		AvgMs       float64 `json:"avg_duration_ms"`
	} `json:"top_tools"`
}

func showOverviewMetrics(ctx context.Context, apiURL, workspace, token string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/metrics/overview/?days=%d",
		apiURL, url.PathEscape(workspace), flagMetricsDays)

	data, err := fetchJSON(ctx, u, token)
	if err != nil {
		return err
	}

	var overview metricsOverview
	if err := json.Unmarshal(data, &overview); err != nil {
		return fmt.Errorf("decoding metrics: %w", err)
	}

	fmt.Printf("WORKSPACE METRICS (last %d days)\n", flagMetricsDays)
	fmt.Printf("Invocations: %d    Active tools: %d    Active users: %d    Error rate: %.0f%%\n\n",
		overview.TotalInvocations, overview.ActiveTools, overview.ActiveUsers, overview.ErrorRate*100)

	if len(overview.TopTools) > 0 {
		fmt.Printf("%-30s %12s %8s %8s\n", "TOP TOOLS", "INVOCATIONS", "ERRORS", "AVG MS")
		for _, t := range overview.TopTools {
			fmt.Printf("%-30s %12d %8d %8.0f\n", t.Name, t.Invocations, t.Errors, t.AvgMs)
		}
	}
	return nil
}

type toolMetrics struct {
	Invocations30d int     `json:"invocations_30d"`
	UniqueUsers30d int     `json:"unique_users_30d"`
	ErrorRate30d   float64 `json:"error_rate_30d"`
	AvgDurationMs  float64 `json:"avg_duration_ms"`
	ByAction       []struct {
		Action      string  `json:"action"`
		Invocations int     `json:"invocations"`
		AvgMs       float64 `json:"avg_duration_ms"`
	} `json:"by_action"`
}

func showToolMetrics(ctx context.Context, apiURL, workspace, token, toolName string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/metrics/tools/%s/?days=%d",
		apiURL, url.PathEscape(workspace), url.PathEscape(toolName), flagMetricsDays)

	data, err := fetchJSON(ctx, u, token)
	if err != nil {
		return err
	}

	var metrics toolMetrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return fmt.Errorf("decoding tool metrics: %w", err)
	}

	fmt.Printf("METRICS FOR %s (last %d days)\n", toolName, flagMetricsDays)
	fmt.Printf("Invocations: %d    Unique users: %d    Error rate: %.0f%%    Avg latency: %.0fms\n\n",
		metrics.Invocations30d, metrics.UniqueUsers30d, metrics.ErrorRate30d*100, metrics.AvgDurationMs)

	if len(metrics.ByAction) > 0 {
		fmt.Printf("%-30s %12s %8s\n", "ACTION", "INVOCATIONS", "AVG MS")
		for _, a := range metrics.ByAction {
			fmt.Printf("%-30s %12d %8.0f\n", a.Action, a.Invocations, a.AvgMs)
		}
	}
	return nil
}

func fetchJSON(ctx context.Context, u, token string) ([]byte, error) {
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

func init() {
	metricsCmd.Flags().IntVar(&flagMetricsDays, "days", 30, "Number of days to include")
	rootCmd.AddCommand(metricsCmd)
}
