// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/memory"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var inspectCmd = &cobra.Command{
	Use:     "info <tool>",
	Short:   "Show detailed information about a tool",
	Aliases: []string{"inspect"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		}

		cache := registry.NewCache(cfg.CacheDir)

		// N3.11: Support namespace/name format
		namespace, baseName := parseNamespacedTool(toolName)
		var spec *models.ToolSpec
		if namespace != "" {
			// Try qualified name resolution first
			apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
			client := registry.NewClient(apiURL, cache, flagNoCache)
			token := config.ResolveAuthToken(flagAPIKey, cfg)
			if token != "" {
				client.AuthToken = token
			}
			var fetchErr error
			spec, _, fetchErr = client.GetPackByQualifiedName(ctx, namespace, baseName)
			if fetchErr != nil {
				// Fall back to regular resolution
				spec, fetchErr = registry.ResolveSpec(ctx, baseName, cfg, cache, flagNoCache)
				if fetchErr != nil {
					return fmt.Errorf("tool %q not found: %w", toolName, fetchErr)
				}
				if spec.Namespace != "" && spec.Namespace != namespace {
					return fmt.Errorf("tool %q resolved to namespace %q, not %q", baseName, spec.Namespace, namespace)
				}
			}
		} else {
			var resolveErr error
			spec, resolveErr = registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
			if resolveErr != nil {
				msg := fmt.Sprintf("tool %q not found", toolName)
				if dym := toolSuggestion(toolName, cfg); dym != "" {
					msg += dym
				}
				return fmt.Errorf("%s", msg)
			}
		}

		// Fetch analytics from API (best effort)
		var analytics map[string]any
		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
		analyticsURL := fmt.Sprintf("%s/api/v1/specs/%s/", apiURL, toolName)
		if resp, err := http.Get(analyticsURL); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var detail map[string]any
				if json.NewDecoder(resp.Body).Decode(&detail) == nil {
					if a, ok := detail["analytics"].(map[string]any); ok {
						analytics = a
					}
				}
			}
		}

		// Fetch ownership/badge info from API (best effort)
		var ownership map[string]any
		ownershipURL := fmt.Sprintf("%s/api/v1/specs/%s/ownership/", apiURL, toolName)
		if resp, err := http.Get(ownershipURL); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				json.NewDecoder(resp.Body).Decode(&ownership)
			}
		}

		// Resolve source provenance from workspace cache
		sourceName := ""
		if cfg.Auth.ActiveWorkspace != "" {
			if wsIdx, loadErr := registry.LoadCachedCLIIndex(cfg.Auth.ActiveWorkspace); loadErr == nil {
				for _, src := range wsIdx.Sources {
					// Check if this tool's registry_source_name matches
					if rsName, ok := analytics["registry_source_name"].(string); ok && rsName == src.Name {
						sourceName = src.Name
						break
					}
				}
			}
		}

		// Alias resolution: display note if the spec was resolved from a different name
		if spec.ResolvedFrom != "" {
			displayName := spec.Name
			if spec.Namespace != "" {
				displayName = fmt.Sprintf("%s/%s", spec.Namespace, spec.Name)
			}
			fmt.Fprintf(os.Stderr, "Note: %s resolved from %s -> %s\n", toolName, spec.ResolvedFrom, displayName)
		}

		// MCP runtime discovery: if spec has discover: true, try to
		// connect and merge discovered tools with static actions.
		if spec.Discover {
			discovered, discoverErr := mcp.DiscoverTools(ctx, spec)
			if discoverErr != nil {
				fmt.Fprintf(os.Stderr, "Note: could not discover tools from %s: %v (showing static actions only)\n", spec.Name, discoverErr)
			} else {
				spec.Actions = mcp.MergeActions(spec.Actions, discovered, spec.Allow, spec.Deny)
			}
		}

		switch flagOutput {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(spec)
		case "yaml":
			return yaml.NewEncoder(os.Stdout).Encode(spec)
		default:
			return renderInspectText(spec, analytics, ownership, sourceName)
		}
	},
}

func renderInspectText(spec *models.ToolSpec, analytics map[string]any, ownership map[string]any, sourceName string) error {
	// Header: name + installed check + version
	installed := isInstalled(spec.Name)
	checkMark := ""
	if installed {
		checkMark = " \u2714" // checkmark
	}
	fmt.Printf("==> %s%s: %s\n", spec.Name, checkMark, spec.Version)

	// URL
	if spec.Canonical != "" {
		fmt.Println(spec.Canonical)
	} else if spec.Server != nil && spec.Server.URL != "" {
		fmt.Println(spec.Server.URL)
	}

	// Installed status
	if installed {
		fmt.Println("Installed")
	} else {
		fmt.Println("Not installed")
	}

	// Owner
	if spec.Namespace != "" {
		fmt.Printf("From: %s\n", spec.Namespace)
	}

	// Source provenance
	if sourceName != "" {
		fmt.Printf("Source: %s\n", sourceName)
	}

	// Description
	fmt.Printf("==> Description\n")
	fmt.Printf("%s\n", spec.Description)

	// Auth (with status)
	if spec.Auth != nil && len(spec.Auth.Env) > 0 {
		fmt.Printf("==> Auth\n")
		authLabel := inferAuthType(spec.Auth)
		if len(spec.Auth.Env) > 0 {
			for _, envKey := range spec.Auth.Env {
				status := "\033[31mnot set\033[0m"
				if os.Getenv(envKey) != "" {
					status = "\033[32mset\033[0m"
				}
				fmt.Printf("  %s (%s): %s\n", envKey, authLabel, status)
			}
		} else {
			fmt.Printf("  %s\n", authLabel)
		}
	} else {
		fmt.Println("==> Auth")
		fmt.Println("  none (no key required)")
	}

	// Category / tags
	fmt.Printf("==> Category\n")
	fmt.Printf("  %s\n", spec.Category)
	if len(spec.Tags) > 0 {
		fmt.Printf("  Tags: %s\n", strings.Join(spec.Tags, ", "))
	}

	// Actions
	fmt.Printf("==> Actions (%d)\n", len(spec.Actions))
	for _, action := range spec.Actions {
		safe := ""
		if !action.Mutable {
			safe = " (safe)"
		}
		if action.Method != "" {
			fmt.Printf("  %s %s%s\n", strings.ToUpper(action.Method), action.Name, safe)
		} else if action.Run != "" {
			fmt.Printf("  %s%s\n", action.Name, safe)
		} else {
			fmt.Printf("  %s%s\n", action.Name, safe)
		}
		fmt.Printf("    %s\n", action.Description)
		if action.Output != "" {
			fmt.Printf("    Output: %s\n", action.Output)
		}

		if len(action.Params) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, p := range action.Params {
				req := ""
				if p.Required {
					req = "*"
				}
				def := ""
				if p.Default != "" {
					def = fmt.Sprintf("=%s", p.Default)
				}
				desc := p.Description
			if p.Example != "" {
				desc += fmt.Sprintf(" (e.g. %s)", p.Example)
			}
			fmt.Fprintf(w, "    --%s\t%s%s\t%s\t%s\n", p.Name, p.Type, def, req, desc)
			}
			w.Flush()
		}
	}

	// Resources (MCP servers)
	if spec.Resources != nil {
		fmt.Printf("==> Resources\n")
		fmt.Printf("  Configured (use 'clictl mcp list-resources %s' for live listing)\n", spec.Name)
	}

	// Prompts (MCP servers)
	if len(spec.Prompts.Items) > 0 {
		fmt.Printf("==> Prompts (%d)\n", len(spec.Prompts.Items))
		for _, p := range spec.Prompts.Items {
			fmt.Printf("  %s\n", p.Name)
			if p.Description != "" {
				fmt.Printf("    %s\n", p.Description)
			}
		}
	}

	// Quick start
	if len(spec.Actions) > 0 {
		a := spec.Actions[0]
		fmt.Printf("==> Quick Start\n")
		if !installed {
			fmt.Printf("  clictl install %s\n", spec.Name)
		}
		example := fmt.Sprintf("  clictl run %s %s", spec.Name, a.Name)
		for _, p := range a.Params {
			if p.Required {
				example += fmt.Sprintf(" --%s <value>", p.Name)
			}
		}
		fmt.Println(example)
	}

	// Analytics
	if analytics != nil {
		fmt.Printf("==> Analytics\n")
		inv30 := formatCount(analytics["invocations_30d"])
		inv90 := formatCount(analytics["invocations_90d"])
		inv365 := formatCount(analytics["invocations_365d"])
		fmt.Printf("  install: %s (30 days), %s (90 days), %s (365 days)\n", inv30, inv90, inv365)
	}

	// Memories
	mem, _ := memory.Load(spec.Name)
	if len(mem) > 0 {
		fmt.Printf("==> Memories (%d)\n", len(mem))
		for _, m := range mem {
			fmt.Printf("  - [%s] %s (%s)\n", m.Type, m.Note, m.CreatedAt.Format("2006-01-02"))
		}
	}

	// Dependencies (merged from deps command)
	hasServerReqs := spec.Server != nil && len(spec.Server.Requires) > 0
	hasAuth := spec.Auth != nil && len(spec.Auth.Env) > 0
	hasDeps := hasServerReqs ||
		hasAuth ||
		len(spec.Depends) > 0 ||
		spec.Runtime != nil
	if hasDeps {
		fmt.Printf("==> Dependencies\n")

		// Server requirements (binaries)
		if hasServerReqs {
			for _, req := range spec.Server.Requires {
				fmt.Printf("  %s", req.Name)
				if req.URL != "" {
					fmt.Printf(" (%s)", req.URL)
				}
				fmt.Println()
			}
		}

		// Registry dependencies (depends)
		if len(spec.Depends) > 0 {
			for _, dep := range spec.Depends {
				status := "not installed"
				if isInstalled(dep) {
					status = "installed"
				}
				fmt.Printf("  %-14s %s\n", dep, status)
			}
		}

		// Runtime info
		if spec.Runtime != nil {
			label := spec.Runtime.Manager
			fmt.Printf("  Runtime: %s\n", label)
			for _, dep := range spec.Runtime.Dependencies {
				fmt.Printf("    %s\n", dep)
			}
		}
	}

	// Package info
	if spec.Package != nil {
		pkg := spec.Package
		if pkg.Registry != "" || pkg.Name != "" || pkg.Version != "" || pkg.SHA256 != "" {
			fmt.Printf("==> Package\n")
			if pkg.Registry != "" {
				fmt.Printf("  Registry: %s\n", pkg.Registry)
			}
			if pkg.Name != "" {
				fmt.Printf("  Package:  %s\n", pkg.Name)
			}
			if pkg.Version != "" {
				fmt.Printf("  Version:  %s\n", pkg.Version)
			}
			if pkg.SHA256 != "" {
				fmt.Printf("  SHA256:   %s\n", pkg.SHA256)
			}
		}
	}

	// Publisher info
	if ownership != nil {
		tier, _ := ownership["tier"].(string)
		namespace, _ := ownership["namespace"].(string)
		verified, _ := ownership["verified"].(bool)

		if tier != "" {
			badge := map[string]string{
				"community": "Community",
				"verified":  "Verified",
				"partner":   "Partner",
				"premier":   "Premier Partner",
			}
			label, ok := badge[tier]
			if !ok {
				label = tier
			}
			fmt.Printf("==> Publisher\n")
			fmt.Printf("  Tier: %s\n", label)
			if namespace != "" {
				fmt.Printf("  Namespace: %s\n", namespace)
			}
			if verified {
				fmt.Printf("  Verified: yes\n")
			} else if tier == "verified" || tier == "partner" || tier == "premier" {
				fmt.Printf("  Verified: yes\n")
			}
			if ws, ok := ownership["workspace_name"].(string); ok && ws != "" {
				fmt.Printf("  Maintained by %s\n", ws)
			}
		}
	}

	return nil
}

func isInstalled(name string) bool {
	for _, n := range loadInstalled() {
		if n == name {
			return true
		}
	}
	return false
}

func formatCount(v any) string {
	switch n := v.(type) {
	case float64:
		if n >= 1000000 {
			return fmt.Sprintf("%.1fM", n/1000000)
		}
		if n >= 1000 {
			return fmt.Sprintf("%.1fK", n/1000)
		}
		return fmt.Sprintf("%.0f", n)
	case int:
		return fmt.Sprintf("%d", n)
	default:
		return "0"
	}
}

// inferAuthType returns a human-readable auth type label from the 1.0 Auth fields.
func inferAuthType(auth *models.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Header != "" {
		if strings.Contains(strings.ToLower(auth.Header), "bearer") {
			return "bearer"
		}
		return "key"
	}
	if auth.Param != "" {
		return "api_key"
	}
	if len(auth.Env) > 0 {
		return "env"
	}
	return ""
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}
