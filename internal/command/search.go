// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/permissions"
	"github.com/clictl/cli/internal/registry"
	localsearch "github.com/clictl/cli/internal/search"
	"github.com/clictl/cli/internal/updater"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	searchCategory string
	searchType     string
	searchProtocol string
	searchTag      string
	searchAuth     string
	searchHas      string
	searchReady    bool
	searchRebuild  bool
	searchLocal    bool
)

// isIndexStale checks if the toolbox index was last synced more than the given duration ago.
func isIndexStale(cfg *config.Config, maxAge time.Duration) bool {
	if cfg.Update.LastSyncAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, cfg.Update.LastSyncAt)
	if err != nil {
		return true
	}
	return time.Since(last) > maxAge
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search for tools",
	Long: `Search for tools by keyword. Results are ranked by relevance.

  clictl search weather
  clictl search github --category developer
  clictl search weather --auth none          # only tools with no auth required
  clictl search api --auth api_key           # only tools needing an API key
  clictl search weather --ready              # only tools ready to use (auth keys set)
  clictl search docker --type cli
  clictl search github --type mcp            # only MCP servers
  clictl search commit --type skill          # only skills
  clictl search translation --tag ai
  clictl search github --protocol mcp        # filter by protocol

Use 'clictl categories' to see available categories.
Use 'clictl tags' to see popular tags.

When filters are provided without a query, all matching tools are returned:

  clictl search --category developer
  clictl search --protocol mcp
  clictl search --auth none`,
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		query := ""
		if len(args) > 0 {
			query = args[0]
		}

		hasFilter := searchCategory != "" || searchType != "" || searchProtocol != "" || searchTag != "" || searchAuth != "" || searchReady || searchHas != ""
		if query == "" && !hasFilter {
			return fmt.Errorf("provide a search query or at least one filter flag (e.g., --category, --protocol, --tag)")
		}

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		}

		// Auto-refresh: if toolbox index is stale (>1 hour), refresh before searching.
		if cfg != nil && isIndexStale(cfg, 1*time.Hour) {
			fmt.Fprintf(os.Stderr, "Refreshing toolbox index...\n")
			if syncErr := updater.ForceSyncRegistries(cfg); syncErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not refresh index: %v\n", syncErr)
			} else {
				if reloaded, reloadErr := config.Load(); reloadErr == nil {
					cfg = reloaded
				}
			}
		}

		// Resolve auth token for permissions and API fallback.
		authToken := config.ResolveAuthToken(flagAPIKey, cfg)

		var allResults []models.SearchResult

		// If --local flag is set, only use BM25.
		if searchLocal {
			bm25IndexPath := filepath.Join(config.DefaultCacheDir(), "index.bm25")
			allResults = searchBM25(query, bm25IndexPath, cfg)
			if len(allResults) == 0 {
				fmt.Println("No results found in local index.")
				return nil
			}
		} else if authToken != "" {
			// Authenticated: API first, BM25 fallback.
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			client := registry.NewClient(apiURL, nil, true)
			client.AuthToken = authToken

			workspace := cfg.Auth.ActiveWorkspace
			result, apiErr := client.SearchWithWorkspace(ctx, query, workspace)
			if apiErr == nil && len(result.Results) > 0 {
				allResults = result.Results
			}

			// Fall back to BM25 if API returned no results or errored.
			if len(allResults) == 0 {
				bm25IndexPath := filepath.Join(config.DefaultCacheDir(), "index.bm25")
				allResults = searchBM25(query, bm25IndexPath, cfg)
			}

			// Fall back to local index if BM25 also returned nothing.
			if len(allResults) == 0 {
				bucketsDir := config.ToolboxesDir()
				for _, reg := range cfg.Toolboxes {
					regDir := filepath.Join(bucketsDir, reg.Name)
					li := registry.NewLocalIndex(regDir, reg.Name)
					results, err := li.Search(query)
					if err == nil {
						allResults = append(allResults, results...)
					}
				}
			}
		} else {
			// Unauthenticated: BM25 first, then local index, then API fallback.
			bm25IndexPath := filepath.Join(config.DefaultCacheDir(), "index.bm25")
			allResults = searchBM25(query, bm25IndexPath, cfg)

			if len(allResults) == 0 {
				bucketsDir := config.ToolboxesDir()
				for _, reg := range cfg.Toolboxes {
					regDir := filepath.Join(bucketsDir, reg.Name)
					li := registry.NewLocalIndex(regDir, reg.Name)
					results, err := li.Search(query)
					if err == nil {
						allResults = append(allResults, results...)
					}
				}
			}

			// Fall back to API if no local results.
			if len(allResults) == 0 {
				apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
				client := registry.NewClient(apiURL, nil, true)

				result, err := client.Search(ctx, query)
				if err != nil {
					return fmt.Errorf("search failed: %w", err)
				}

				if len(result.Results) == 0 {
					fmt.Println("No results found.")
					return nil
				}

				allResults = result.Results
			}
		}

		// Apply --has filter (comma-separated: tools, resources, prompts)
		if searchHas != "" {
			wantCaps := strings.Split(strings.ToLower(searchHas), ",")
			var filtered []models.SearchResult
			for _, r := range allResults {
				match := true
				for _, cap := range wantCaps {
					cap = strings.TrimSpace(cap)
					switch cap {
					case "tools":
						// All specs have tools, always matches
					case "resources", "prompts":
						// Only MCP-protocol specs can have resources/prompts
						if !matchesProtocol(r, "mcp", cfg) {
							match = false
						}
					}
				}
				if match {
					filtered = append(filtered, r)
				}
			}
			allResults = filtered
		}

		// Apply category/type/tag/protocol/auth filters
		if searchCategory != "" || searchType != "" || searchProtocol != "" || searchTag != "" || searchAuth != "" || searchReady {
			var filtered []models.SearchResult
			for _, r := range allResults {
				if searchCategory != "" && !strings.EqualFold(r.Category, searchCategory) {
					continue
				}
				// Type, protocol, and tag filters require index entry lookup
				if searchType != "" || searchProtocol != "" || searchTag != "" {
					matched := true
					if searchType != "" {
						matched = matched && matchesType(r, searchType, cfg)
					}
					if searchProtocol != "" {
						matched = matched && matchesProtocol(r, searchProtocol, cfg)
					}
					if searchTag != "" {
						matched = matched && matchesTag(r, searchTag, cfg)
					}
					if !matched {
						continue
					}
				}
				filtered = append(filtered, r)
			}
			allResults = filtered
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
			fmt.Println("No results found.")
			return nil
		}

		// Boost favorites to top of results
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

		response := models.SearchResponse{
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

			// N3.10: Check if any results have qualified names or publishers
			hasQualified := false
			for _, r := range allResults {
				if r.QualifiedName != "" || r.Publisher != "" {
					hasQualified = true
					break
				}
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			multiSource := hasMultipleSources(allResults)
			if hasQualified {
				fmt.Fprintln(w, "NAME\tPUBLISHER\tCATEGORY\tVERSION\tDESCRIPTION")
			} else if multiSource {
				fmt.Fprintln(w, "NAME\tCATEGORY\tVERSION\tSOURCE\tDESCRIPTION")
			} else {
				fmt.Fprintln(w, "NAME\tCATEGORY\tVERSION\tDESCRIPTION")
			}
			for _, r := range allResults {
				name := r.Name
				// Use qualified name as the display name when available
				if r.QualifiedName != "" {
					name = r.QualifiedName
				}
				if badge := trustTierBadge(r.TrustTier); badge != "" {
					name += " " + badge
				}
				if favSet[r.Name] {
					name = "\u2605 " + name // star prefix for favorites
				}
				desc := r.Description
				// Annotate MCP/skill tools that require unavailable runtimes
				if r.Protocol == "mcp" && r.PackageRegistry != "" && !RuntimeAvailable(r.PackageRegistry) {
					desc = "(requires " + r.PackageRegistry + " runtime) " + desc
				}
				if hasQualified {
					publisher := r.Publisher
					if publisher == "" && r.Source != "" {
						publisher = r.Source
					}
					if publisher == "" {
						publisher = "-"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, publisher, r.Category, r.Version, desc)
				} else if multiSource {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, r.Category, r.Version, r.Source, desc)
				} else {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, r.Category, r.Version, desc)
				}
			}
			return w.Flush()
		}
	},
}

// matchesType checks if a result matches the requested type by looking up the index entry.
func matchesType(r models.SearchResult, wantType string, cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	bucketsDir := config.ToolboxesDir()
	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := registry.NewLocalIndex(regDir, reg.Name)
		entry, err := li.GetEntry(r.Name)
		if err != nil {
			continue
		}
		return strings.EqualFold(entry.Type, wantType)
	}
	return true
}

// matchesProtocol checks if a result matches the requested protocol by looking up the index entry.
func matchesProtocol(r models.SearchResult, wantProtocol string, cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	bucketsDir := config.ToolboxesDir()
	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := registry.NewLocalIndex(regDir, reg.Name)
		entry, err := li.GetEntry(r.Name)
		if err != nil {
			continue
		}
		return strings.EqualFold(entry.Protocol, wantProtocol)
	}
	return true
}

// matchesTag checks if a result has the requested tag by looking up the index entry.
func matchesTag(r models.SearchResult, wantTag string, cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	bucketsDir := config.ToolboxesDir()
	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := registry.NewLocalIndex(regDir, reg.Name)
		entry, err := li.GetEntry(r.Name)
		if err != nil {
			continue
		}
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, wantTag) {
				return true
			}
		}
		return false
	}
	return true
}

// matchesAuth checks if a result's auth type matches the requested filter.
// Values: "none" (no auth), "api_key", "bearer", "oauth2", or "any" (any auth required).
func matchesAuth(r models.SearchResult, wantAuth string, cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	bucketsDir := config.ToolboxesDir()
	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := registry.NewLocalIndex(regDir, reg.Name)
		entry, err := li.GetEntry(r.Name)
		if err != nil {
			continue
		}
		if wantAuth == "any" {
			return entry.Auth != "" && entry.Auth != "none"
		}
		return strings.EqualFold(entry.Auth, wantAuth)
	}
	return true
}


// trustTierBadge returns a display badge for the given trust tier.
// Returns an empty string for community tier or unknown values.
func trustTierBadge(tier string) string {
	switch strings.ToLower(tier) {
	case "official":
		return "(official)"
	case "certified":
		return "(certified)"
	case "verified":
		return "(verified)"
	case "private":
		return "(private)"
	default:
		return ""
	}
}

// isToolReady checks if a tool requires auth and whether auth is likely available.
// Tools with auth "none" are always ready. Others are ready if the tool is installed
// (meaning the user has set up auth).
func isToolReady(r models.SearchResult, cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	bucketsDir := config.ToolboxesDir()
	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := registry.NewLocalIndex(regDir, reg.Name)
		entry, err := li.GetEntry(r.Name)
		if err != nil {
			continue
		}
		// No auth required = always ready
		if entry.Auth == "" || entry.Auth == "none" {
			return true
		}
		// Has auth requirement = not ready unless installed (user took action to set it up)
		return false
	}
	return true
}

var categoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "List available tool categories",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		bucketsDir := config.ToolboxesDir()

		cats := make(map[string]int)
		for _, reg := range cfg.Toolboxes {
			regDir := filepath.Join(bucketsDir, reg.Name)
			li := registry.NewLocalIndex(regDir, reg.Name)
			regCats, err := li.Categories()
			if err != nil {
				continue
			}
			for cat, count := range regCats {
				cats[cat] += count
			}
		}

		if len(cats) == 0 {
			fmt.Println("No categories found. Run 'clictl toolbox update' first.")
			return nil
		}

		// Sort by name
		var names []string
		for name := range cats {
			names = append(names, name)
		}
		sort.Strings(names)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CATEGORY\tTOOLS")
		for _, name := range names {
			fmt.Fprintf(w, "%s\t%d\n", name, cats[name])
		}
		return w.Flush()
	},
}

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "List popular tool tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		bucketsDir := config.ToolboxesDir()

		tags := make(map[string]int)
		for _, reg := range cfg.Toolboxes {
			regDir := filepath.Join(bucketsDir, reg.Name)
			li := registry.NewLocalIndex(regDir, reg.Name)
			idx, err := li.Load()
			if err != nil {
				continue
			}
			for _, entry := range idx.Specs {
				for _, tag := range entry.Tags {
					tags[tag]++
				}
			}
		}

		if len(tags) == 0 {
			fmt.Println("No tags found. Run 'clictl toolbox update' first.")
			return nil
		}

		// Sort by count descending
		type tagCount struct {
			tag   string
			count int
		}
		var sorted []tagCount
		for tag, count := range tags {
			sorted = append(sorted, tagCount{tag, count})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TAG\tTOOLS")
		for _, tc := range sorted {
			fmt.Fprintf(w, "%s\t%d\n", tc.tag, tc.count)
		}
		return w.Flush()
	},
}

// searchBM25 attempts to search using the local BM25 index. It rebuilds
// the index if --rebuild is set, the index file is missing, or any YAML
// spec file is newer than the index. Returns converted models.SearchResult
// slice, or nil if no results.
func searchBM25(query, indexPath string, cfg *config.Config) []models.SearchResult {
	// Collect toolbox directories to scan.
	var scanPaths []string
	var canonicalPath string
	if cfg != nil {
		bucketsDir := config.ToolboxesDir()
		for _, reg := range cfg.Toolboxes {
			regPath := filepath.Join(bucketsDir, reg.Name)
			scanPaths = append(scanPaths, regPath)
			if reg.Default {
				canonicalPath = regPath
			}
		}
	}

	needsRebuild := searchRebuild

	if !needsRebuild {
		info, err := os.Stat(indexPath)
		if err != nil {
			needsRebuild = true
		} else {
			indexTime := info.ModTime()
			for _, dir := range scanPaths {
				_ = filepath.Walk(dir, func(path string, fi os.FileInfo, walkErr error) error {
					if walkErr != nil || fi.IsDir() {
						return nil
					}
					if strings.HasSuffix(fi.Name(), ".yaml") || strings.HasSuffix(fi.Name(), ".yml") {
						if fi.ModTime().After(indexTime) {
							needsRebuild = true
						}
					}
					return nil
				})
				if needsRebuild {
					break
				}
			}
		}
	}

	var idx *localsearch.Index

	if needsRebuild {
		docs, err := localsearch.ScanToolboxDirs(scanPaths)
		if err != nil {
			logger.Warn("BM25 index scan failed", logger.F("error", err.Error()))
			return nil
		}
		if len(docs) == 0 {
			return nil
		}
		idx = localsearch.BuildIndex(docs)
		if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0755); mkErr == nil {
			if saveErr := idx.Save(indexPath); saveErr != nil {
				logger.Warn("could not save BM25 index", logger.F("error", saveErr.Error()))
			}
		}
	} else {
		var err error
		idx, err = localsearch.LoadIndex(indexPath)
		if err != nil {
			logger.Warn("could not load BM25 index, rebuilding", logger.F("error", err.Error()))
			docs, scanErr := localsearch.ScanToolboxDirs(scanPaths)
			if scanErr != nil || len(docs) == 0 {
				return nil
			}
			idx = localsearch.BuildIndex(docs)
			if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0755); mkErr == nil {
				_ = idx.Save(indexPath)
			}
		}
	}

	boosts := localsearch.DefaultSearchBoosts()
	results := idx.SearchWithBoosts(query, 25, boosts, canonicalPath)
	if len(results) == 0 {
		return nil
	}

	out := make([]models.SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, models.SearchResult{
			Name:        r.Document.Name,
			Description: r.Document.Description,
			Category:    r.Document.Category,
			Version:     r.Document.Version,
			Protocol:    r.Document.Protocol,
			TrustTier:   r.Document.TrustTier,
		})
	}
	return out
}

func init() {
	searchCmd.Flags().StringVar(&searchCategory, "category", "", "Filter by category (e.g., ai, developer, devops)")
	searchCmd.Flags().StringVar(&searchType, "type", "", "Filter by type (api, cli, website, mcp, skill)")
	searchCmd.Flags().StringVar(&searchProtocol, "protocol", "", "Filter by protocol (http, command, mcp, skill, graphql)")
	searchCmd.Flags().StringVar(&searchTag, "tag", "", "Filter by tag (e.g., weather, docker, llm)")
	searchCmd.Flags().StringVar(&searchAuth, "auth", "", "Filter by auth type: none, api_key, bearer, oauth2, any")
	searchCmd.Flags().BoolVar(&searchReady, "ready", false, "Show only tools ready to use (auth keys are set)")
	searchCmd.Flags().BoolVar(&searchRebuild, "rebuild", false, "Force rebuild of the local BM25 search index")
	searchCmd.Flags().BoolVar(&searchLocal, "local", false, "Search local index only, no API fallback")
	searchCmd.Flags().StringVar(&searchHas, "has", "", "Filter by capabilities (comma-separated: tools, resources, prompts)")
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(categoriesCmd)
	rootCmd.AddCommand(tagsCmd)
}
