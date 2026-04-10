// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
)

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Manage teams in the current workspace",
}

var teamCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a team in the current workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		slug := cfg.Auth.ActiveWorkspace
		if slug == "" {
			return fmt.Errorf("no active workspace set. Run 'clictl workspace switch' first")
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run 'clictl login' first")
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/teams/", apiURL, url.PathEscape(slug))

		body := fmt.Sprintf(`{"name":%q}`, name)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("creating team: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var result struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Team %q created (slug: %s)\n", result.Name, result.Slug)
		return nil
	},
}

var teamListCmd = &cobra.Command{
	Use:   "list",
	Short: "List teams in the current workspace",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		slug := cfg.Auth.ActiveWorkspace
		if slug == "" {
			return fmt.Errorf("no active workspace set. Run 'clictl workspace switch' first")
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run 'clictl login' first")
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/teams/", apiURL, url.PathEscape(slug))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("listing teams: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var teams []struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			MemberCount int    `json:"member_count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		if len(teams) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No teams found.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSLUG\tMEMBERS")
		for _, t := range teams {
			fmt.Fprintf(w, "%s\t%s\t%d\n", t.Name, t.Slug, t.MemberCount)
		}
		return w.Flush()
	},
}

var teamShowCmd = &cobra.Command{
	Use:   "show <slug>",
	Short: "Show team detail with members",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		teamSlug := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		wsSlug := cfg.Auth.ActiveWorkspace
		if wsSlug == "" {
			return fmt.Errorf("no active workspace set. Run 'clictl workspace switch' first")
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run 'clictl login' first")
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/teams/%s/", apiURL, url.PathEscape(wsSlug), url.PathEscape(teamSlug))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("fetching team: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var team struct {
			Name    string `json:"name"`
			Slug    string `json:"slug"`
			Members []struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			} `json:"members"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&team); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Team: %s (%s)\n", team.Name, team.Slug)
		fmt.Fprintf(cmd.OutOrStdout(), "Members: %d\n\n", len(team.Members))

		if len(team.Members) > 0 {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tROLE")
			for _, m := range team.Members {
				fmt.Fprintf(w, "%s\t%s\n", m.Username, m.Role)
			}
			return w.Flush()
		}

		return nil
	},
}

var teamMembersCmd = &cobra.Command{
	Use:   "members <slug>",
	Short: "List team members",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		teamSlug := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		wsSlug := cfg.Auth.ActiveWorkspace
		if wsSlug == "" {
			return fmt.Errorf("no active workspace set. Run 'clictl workspace switch' first")
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run 'clictl login' first")
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/teams/%s/members/", apiURL, url.PathEscape(wsSlug), url.PathEscape(teamSlug))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("fetching team members: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
		}

		var members []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			Email    string `json:"email"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		if len(members) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No members found.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "USERNAME\tROLE\tEMAIL")
		for _, m := range members {
			fmt.Fprintf(w, "%s\t%s\t%s\n", m.Username, m.Role, m.Email)
		}
		return w.Flush()
	},
}

func init() {
	teamCmd.AddCommand(teamCreateCmd)
	teamCmd.AddCommand(teamListCmd)
	teamCmd.AddCommand(teamShowCmd)
	teamCmd.AddCommand(teamMembersCmd)
	rootCmd.AddCommand(teamCmd)
}
