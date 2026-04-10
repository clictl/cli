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
	"os"
	"text/tabwriter"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// requestAPIClient is a lightweight HTTP helper for the requests endpoints.
type requestAPIClient struct {
	baseURL   string
	authToken string
	client    *http.Client
}

func newRequestAPIClient(baseURL, authToken string) *requestAPIClient {
	return &requestAPIClient{
		baseURL:   baseURL,
		authToken: authToken,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *requestAPIClient) newReq(ctx context.Context, method, u string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// toolAccessRequest matches the API response for a tool access request.
type toolAccessRequest struct {
	ID          int    `json:"id"`
	Tool        string `json:"tool"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	ReviewNote  string `json:"review_note"`
	RequestedBy string `json:"requested_by"`
	ReviewedBy  string `json:"reviewed_by"`
	CreatedAt   string `json:"created_at"`
	ReviewedAt  string `json:"reviewed_at"`
}

var flagRequestReason string

var requestCmd = &cobra.Command{
	Use:   "request <tool>",
	Short: "Request access to a tool",
	Long:  `Submit a tool access request for the active workspace. An admin can approve or deny it.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

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
		ac := newRequestAPIClient(apiURL, token)

		payload := map[string]string{
			"tool":      toolName,
			"action":    "*",
			"workspace": ws,
		}
		if flagRequestReason != "" {
			payload["reason"] = flagRequestReason
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}

		u := fmt.Sprintf("%s/api/v1/workspaces/%s/tool-requests/", ac.baseURL, url.QueryEscape(ws))
		req, err := ac.newReq(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		resp, err := ac.client.Do(req)
		if err != nil {
			return fmt.Errorf("submitting request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("request API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result toolAccessRequest
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		fmt.Printf("Access request #%d created for tool %q (status: %s)\n", result.ID, result.Tool, result.Status)
		return nil
	},
}

var flagRequestsStatus string

var requestsCmd = &cobra.Command{
	Use:   "requests",
	Short: "List tool access requests",
	Long:  `List tool access requests for the active workspace.`,
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
		ac := newRequestAPIClient(apiURL, token)

		u := fmt.Sprintf("%s/api/v1/workspaces/%s/tool-requests/", ac.baseURL, url.QueryEscape(ws))
		if flagRequestsStatus != "" {
			u += "&status=" + url.QueryEscape(flagRequestsStatus)
		}

		req, err := ac.newReq(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		resp, err := ac.client.Do(req)
		if err != nil {
			return fmt.Errorf("fetching requests: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("requests API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var results []toolAccessRequest
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		if len(results) == 0 {
			fmt.Println("No requests found.")
			return nil
		}

		switch flagOutput {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		case "yaml":
			return yaml.NewEncoder(os.Stdout).Encode(results)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTOOL\tSTATUS\tREASON\tCREATED")
			for _, r := range results {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", r.ID, r.Tool, r.Status, r.Reason, r.CreatedAt)
			}
			return w.Flush()
		}
	},
}

var flagApproveNote string

var requestsApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a tool access request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return reviewRequest(cmd, args[0], "approved", flagApproveNote)
	},
}

var flagDenyNote string

var requestsDenyCmd = &cobra.Command{
	Use:   "deny <id>",
	Short: "Deny a tool access request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return reviewRequest(cmd, args[0], "denied", flagDenyNote)
	},
}

func reviewRequest(cmd *cobra.Command, id, action, note string) error {
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
		return fmt.Errorf("no active workspace. Run \"clictl workspace switch\" first")
	}

	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	ac := newRequestAPIClient(apiURL, token)

	payload := map[string]string{
		"action": action,
	}
	if note != "" {
		payload["review_note"] = note
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling review: %w", err)
	}

	reviewEndpoint := "approve"
	if action == "denied" {
		reviewEndpoint = "deny"
	}
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/tool-requests/%s/%s/", ac.baseURL, url.QueryEscape(ws), id, reviewEndpoint)
	req, err := ac.newReq(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating review request: %w", err)
	}

	resp, err := ac.client.Do(req)
	if err != nil {
		return fmt.Errorf("submitting review: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("review API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result toolAccessRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	fmt.Printf("Request #%s %s.\n", id, action)
	return nil
}

func init() {
	requestCmd.Flags().StringVar(&flagRequestReason, "reason", "", "Reason for requesting access")
	requestsApproveCmd.Flags().StringVar(&flagApproveNote, "note", "", "Note to include with the approval")
	requestsDenyCmd.Flags().StringVar(&flagDenyNote, "note", "", "Note to include with the denial")
	requestsCmd.AddCommand(requestsApproveCmd)
	requestsCmd.AddCommand(requestsDenyCmd)
	rootCmd.AddCommand(requestCmd)
	rootCmd.AddCommand(requestsCmd)
}
