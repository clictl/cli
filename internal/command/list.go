// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/permissions"
	"github.com/clictl/cli/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	flagCategory     string
	flagListProtocol string
	flagListDisabled bool
	flagListPinned   bool
	flagListAll      bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		}

		// If --pinned flag, show only pinned tools and return.
		if flagListPinned {
			if cfg == nil || len(cfg.PinnedTools) == 0 {
				fmt.Println("No pinned tools.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PINNED TOOLS")
			for _, name := range cfg.PinnedTools {
				fmt.Fprintln(w, name)
			}
			return w.Flush()
		}

		// If --disabled flag, show only disabled tools and return.
		if flagListDisabled {
			if cfg == nil || len(cfg.DisabledTools) == 0 {
				fmt.Println("No disabled tools.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DISABLED TOOLS")
			for _, name := range cfg.DisabledTools {
				fmt.Fprintln(w, name)
			}
			return w.Flush()
		}

		// Determine list strategy based on login status.
		// If logged in, prefer API (richer results with ranking, permissions, ownership badges),
		// then fall back to local index. If not logged in, try local first, then API fallback.
		authToken := config.ResolveAuthToken(flagAPIKey, cfg)
		loggedIn := authToken != ""

		var allResults []models.SearchResult

		if loggedIn {
			// API first when logged in.
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			client := registry.NewClient(apiURL, nil, true)
			client.AuthToken = authToken

			result, apiErr := client.List(ctx, flagCategory)
			if apiErr == nil && len(result.Results) > 0 {
				allResults = result.Results
			}

			// Fall back to local index if API returned no results.
			if len(allResults) == 0 {
				bucketsDir := config.ToolboxesDir()
				for _, reg := range cfg.Toolboxes {
					regDir := filepath.Join(bucketsDir, reg.Name)
					li := registry.NewLocalIndex(regDir, reg.Name)
					results, err := li.List(flagCategory)
					if err == nil {
						allResults = append(allResults, results...)
					}
				}
			}
		} else {
			// Local index first when not logged in.
			bucketsDir := config.ToolboxesDir()
			for _, reg := range cfg.Toolboxes {
				regDir := filepath.Join(bucketsDir, reg.Name)
				li := registry.NewLocalIndex(regDir, reg.Name)
				results, err := li.List(flagCategory)
				if err == nil {
					allResults = append(allResults, results...)
				}
			}

			// Fall back to API if no local results.
			if len(allResults) == 0 {
				apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
				client := registry.NewClient(apiURL, nil, true)

				result, err := client.List(ctx, flagCategory)
				if err != nil {
					return fmt.Errorf("list failed: %w", err)
				}

				if len(result.Results) == 0 {
					fmt.Println("No tools found.")
					return nil
				}

				allResults = result.Results
			}
		}

		// Filter through permissions if user is logged in with an active workspace.
		if authToken != "" && cfg.Auth.ActiveWorkspace != "" {
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			checker := permissions.NewChecker(apiURL, authToken)
			var filtered []models.SearchResult
			for _, r := range allResults {
				allowed, _, _, permErr := checker.Check(ctx, cfg.Auth.ActiveWorkspace, r.Name, "*")
				if permErr != nil {
					logger.Warn("permission check failed for tool, including in results", logger.F("tool", r.Name), logger.F("error", permErr.Error()))
					filtered = append(filtered, r)
					continue
				}
				if allowed {
					filtered = append(filtered, r)
				}
			}
			allResults = filtered
		}

		if len(allResults) == 0 {
			fmt.Println("No tools found.")
			return nil
		}

		// Boost favorites to top
		var favorites []string
		if cfg.Auth.ActiveWorkspace != "" {
			if wsIdx, loadErr := registry.LoadCachedCLIIndex(cfg.Auth.ActiveWorkspace); loadErr == nil {
				favorites = wsIdx.Favorites
			}
		}
		if len(favorites) > 0 {
			favSet := make(map[string]bool, len(favorites))
			for _, f := range favorites {
				favSet[f] = true
			}
			sort.SliceStable(allResults, func(i, j int) bool {
				iFav := favSet[allResults[i].Name]
				jFav := favSet[allResults[j].Name]
				if iFav != jFav {
					return iFav
				}
				return false
			})
		}

		response := models.ListResponse{
			Results: allResults,
			Count:   len(allResults),
		}

		switch flagOutput {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(response)
		case "yaml":
			return yaml.NewEncoder(os.Stdout).Encode(response)
		default:
			favSet := make(map[string]bool, len(favorites))
			for _, f := range favorites {
				favSet[f] = true
			}

			// N1.12: Check if any results have qualified names to show
			hasQualified := false
			for _, r := range allResults {
				if r.QualifiedName != "" {
					hasQualified = true
					break
				}
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			multiSource := hasMultipleSources(allResults)
			if hasQualified {
				if multiSource {
					fmt.Fprintln(w, "NAME\tQUALIFIED NAME\tCATEGORY\tVERSION\tSOURCE\tDESCRIPTION")
				} else {
					fmt.Fprintln(w, "NAME\tQUALIFIED NAME\tCATEGORY\tVERSION\tDESCRIPTION")
				}
			} else {
				if multiSource {
					fmt.Fprintln(w, "NAME\tCATEGORY\tVERSION\tSOURCE\tDESCRIPTION")
				} else {
					fmt.Fprintln(w, "NAME\tCATEGORY\tVERSION\tDESCRIPTION")
				}
			}
			for _, r := range allResults {
				name := r.Name
				if favSet[r.Name] {
					name = "\u2605 " + r.Name
				}
				if hasQualified {
					qn := r.QualifiedName
					if qn == "" {
						qn = "-"
					}
					if multiSource {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", name, qn, r.Category, r.Version, r.Source, r.Description)
					} else {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, qn, r.Category, r.Version, r.Description)
					}
				} else {
					if multiSource {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, r.Category, r.Version, r.Source, r.Description)
					} else {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, r.Category, r.Version, r.Description)
					}
				}
			}
			return w.Flush()
		}
	},
}

func init() {
	listCmd.Flags().StringVarP(&flagCategory, "category", "c", "", "Filter by category")
	listCmd.Flags().StringVar(&flagListProtocol, "protocol", "", "Filter by protocol (http, command, mcp, skill)")
	listCmd.Flags().BoolVar(&flagListDisabled, "disabled", false, "Show only disabled tools")
	listCmd.Flags().BoolVar(&flagListPinned, "pinned", false, "Show only pinned tools")
	listCmd.Flags().BoolVar(&flagListAll, "all", false, "Show tools across all projects")
	rootCmd.AddCommand(listCmd)
}
