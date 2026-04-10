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

var starCmd = &cobra.Command{
	Use:   "star <tool>",
	Short: "Add a tool to your favorites",
	Args:  cobra.ExactArgs(1),
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
		return addFavorite(cmd.Context(), apiURL, ws, token, args[0])
	},
}

var unstarCmd = &cobra.Command{
	Use:   "unstar <tool>",
	Short: "Remove a tool from your favorites",
	Args:  cobra.ExactArgs(1),
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
		return removeFavorite(cmd.Context(), apiURL, ws, token, args[0])
	},
}

var starsCmd = &cobra.Command{
	Use:   "stars",
	Short: "List your favorite tools",
	Args:  cobra.NoArgs,
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
		return listFavorites(cmd.Context(), apiURL, ws, token)
	},
}

func addFavorite(ctx context.Context, apiURL, workspace, token, toolName string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/favorites/", apiURL, url.PathEscape(workspace))

	body, _ := json.Marshal(map[string]string{"tool_name": toolName})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("adding favorite: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		fmt.Printf("Starred %s\n", toolName)
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		fmt.Printf("%s is already starred\n", toolName)
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to star %s: %d %s", toolName, resp.StatusCode, string(respBody))
}

func removeFavorite(ctx context.Context, apiURL, workspace, token, toolName string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/favorites/%s/", apiURL, url.PathEscape(workspace), url.PathEscape(toolName))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("removing favorite: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		fmt.Printf("Unstarred %s\n", toolName)
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to unstar %s: %d %s", toolName, resp.StatusCode, string(respBody))
}

type favoriteItem struct {
	ToolName  string `json:"tool_name"`
	Category  string `json:"category"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

func listFavorites(ctx context.Context, apiURL, workspace, token string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/favorites/", apiURL, url.PathEscape(workspace))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("listing favorites: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("favorites API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var items []favoriteItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return fmt.Errorf("decoding favorites: %w", err)
	}

	if len(items) == 0 {
		fmt.Println("No favorites yet. Use `clictl star <tool>` to add one.")
		return nil
	}

	fmt.Println("FAVORITES")
	for _, item := range items {
		src := item.Source
		if src == "" {
			src = "-"
		}
		fmt.Printf("  %-25s %-15s %s\n", item.ToolName, item.Category, src)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(starCmd)
	rootCmd.AddCommand(unstarCmd)
	rootCmd.AddCommand(starsCmd)
}
