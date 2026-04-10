// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/memory"
	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory [tool]",
	Short: "Show memories for a tool",
	Long: `Display saved notes for a tool.

Output formats:
  clictl memory openweathermap                   # text (default)
  clictl memory openweathermap --output json     # JSON for piping
  clictl memory openweathermap --output markdown # markdown for docs`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		showAll, _ := cmd.Flags().GetBool("all")
		output, _ := cmd.Flags().GetString("output")

		if showAll || len(args) == 0 {
			return listAllMemories(cmd, output)
		}
		return showToolMemories(cmd, args[0], output)
	},
}

func init() {
	memoryCmd.Flags().Bool("all", false, "List all tools with memories")
	memoryCmd.Flags().StringP("output", "o", "text", "Output format: text, json, markdown")
	rootCmd.AddCommand(memoryCmd)
}

func showToolMemories(cmd *cobra.Command, tool string, output string) error {
	// Load local memories
	entries, err := memory.Load(tool)
	if err != nil {
		return fmt.Errorf("loading memories: %w", err)
	}

	// Load shared workspace memories (best effort)
	shared := fetchSharedMemories(tool)

	switch output {
	case "json":
		s, err := memory.FormatJSON(tool, entries)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), s)
	case "markdown", "md":
		if len(entries) == 0 && len(shared) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No memories for %s\n", tool)
		} else {
			if len(entries) > 0 {
				fmt.Fprint(cmd.OutOrStdout(), memory.FormatMarkdown(tool, entries))
			}
			if len(shared) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n## Shared (workspace)\n\n")
				for _, m := range shared {
					fmt.Fprintf(cmd.OutOrStdout(), "- **[%s]** %s (%s)\n", m.MemoryType, m.Note, m.CreatedBy)
				}
			}
		}
	default:
		fmt.Fprint(cmd.OutOrStdout(), memory.FormatText(tool, entries))
		if len(shared) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nShared (workspace):\n")
			for _, m := range shared {
				pinned := ""
				if m.IsPinned {
					pinned = " [pinned]"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s (%s)%s\n", m.MemoryType, m.Note, m.CreatedBy, pinned)
			}
		}
	}
	return nil
}

// sharedMemory represents a memory from the workspace API.
type sharedMemory struct {
	ToolName   string `json:"tool_name"`
	Note       string `json:"note"`
	MemoryType string `json:"memory_type"`
	IsPinned   bool   `json:"is_pinned"`
	CreatedBy  string `json:"created_by_email"`
}

// fetchSharedMemories loads shared memories from the workspace API.
func fetchSharedMemories(tool string) []sharedMemory {
	cfg, err := config.Load()
	if err != nil || cfg.Auth.ActiveWorkspace == "" {
		return nil
	}
	token := config.ResolveAuthToken("", cfg)
	if token == "" {
		return nil
	}
	apiURL := config.ResolveAPIURL("", cfg)
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/memories/?tool=%s",
		apiURL, url.PathEscape(cfg.Auth.ActiveWorkspace), url.QueryEscape(tool))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var memories []sharedMemory
	json.Unmarshal(data, &memories)
	return memories
}

func listAllMemories(cmd *cobra.Command, output string) error {
	tools, err := memory.ListTools()
	if err != nil {
		return fmt.Errorf("listing memories: %w", err)
	}
	if len(tools) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No memories saved.")
		return nil
	}

	if output == "json" {
		var all []map[string]any
		for _, tool := range tools {
			entries, _ := memory.Load(tool)
			all = append(all, map[string]any{
				"tool":    tool,
				"count":   len(entries),
				"entries": entries,
			})
		}
		data, _ := json.MarshalIndent(all, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Tools with memories:")
	for _, tool := range tools {
		entries, err := memory.Load(tool)
		if err != nil {
			continue
		}
		types := make(map[memory.Type]int)
		for _, e := range entries {
			types[e.Type]++
		}
		var parts []string
		for t, c := range types {
			parts = append(parts, fmt.Sprintf("%d %s", c, t))
		}
		detail := fmt.Sprintf("%d", len(entries))
		if len(parts) > 0 {
			detail = ""
			for i, p := range parts {
				if i > 0 {
					detail += ", "
				}
				detail += p
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s (%s)\n", tool, detail)
	}
	return nil
}
