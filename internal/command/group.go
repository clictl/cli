// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

// GroupManifest represents a group of tools that can be installed together.
type GroupManifest struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Version     string        `json:"version"`
	Publisher   string        `json:"publisher,omitempty"`
	Members     []GroupMember `json:"members"`
	Tools       []string      `json:"tools"`
	ToolDetails []GroupMember `json:"tool_details"`
	TrustTier   string        `json:"trust_tier,omitempty"`
	Blocked     bool          `json:"blocked,omitempty"`
}

// ResolvedMembers returns the group's tool list, preferring tool_details
// from the API over the legacy members field.
func (g *GroupManifest) ResolvedMembers() []GroupMember {
	if len(g.Members) > 0 {
		return g.Members
	}
	if len(g.ToolDetails) > 0 {
		return g.ToolDetails
	}
	// Fall back to bare tool names from the tools array
	members := make([]GroupMember, len(g.Tools))
	for i, name := range g.Tools {
		members[i] = GroupMember{Name: name}
	}
	return members
}

// GroupMember represents a single tool within a group.
type GroupMember struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Version  string `json:"version,omitempty"`
	PackURL  string `json:"pack_url,omitempty"`
	SigURL   string `json:"sig_url,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// TrustTier represents the trust level of a tool or group.
type TrustTier string

const (
	TrustTierVerified  TrustTier = "verified"
	TrustTierPartial   TrustTier = "partial"
	TrustTierCommunity TrustTier = "community"
	TrustTierBlocked   TrustTier = "blocked"
)

// trustTierLabel returns a human-readable label with color for a trust tier.
func trustTierLabel(tier string) string {
	switch strings.ToLower(tier) {
	case "verified":
		return "\033[32m[verified]\033[0m"
	case "partial":
		return "\033[33m[partial]\033[0m"
	case "community":
		return "\033[36m[community]\033[0m"
	case "blocked":
		return "\033[31m[blocked]\033[0m"
	default:
		return "[unknown]"
	}
}

// trustTierPrompt returns the install prompt text appropriate for the trust tier.
func trustTierPrompt(tier string) string {
	switch strings.ToLower(tier) {
	case "verified":
		return "This tool is verified by the publisher."
	case "partial":
		return "This tool has partial verification. Some components may not be signed."
	case "community":
		return "This tool is from the community and is not verified. Use --trust to install."
	case "blocked":
		return "This tool is blocked and cannot be installed."
	default:
		return ""
	}
}

// fetchGroupManifest downloads the group manifest from the API.
func fetchGroupManifest(ctx context.Context, apiURL, token, groupName string) (*GroupManifest, error) {
	u := fmt.Sprintf("%s/api/v1/groups/%s/", apiURL, groupName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching group: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("group %q not found", groupName)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("group API returned %d: %s", resp.StatusCode, string(body))
	}

	var manifest GroupManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing group manifest: %w", err)
	}

	return &manifest, nil
}

// installGroup installs all members of a group.
// C2.1: Fetches group manifest, downloads and verifies all member packs, installs each by type.
func installGroup(ctx context.Context, cfg *config.Config, groupName, target string) error {
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	token := config.ResolveAuthToken(flagAPIKey, cfg)

	// Fetch group manifest
	manifest, err := fetchGroupManifest(ctx, apiURL, token, groupName)
	if err != nil {
		return err
	}

	// C2.3: Blocklist checking - refuse blocked packs
	if manifest.Blocked {
		return fmt.Errorf("group %q is blocked and cannot be installed", groupName)
	}

	// C2.2: Trust tier display
	tier := manifest.TrustTier
	if tier == "" {
		tier = "community"
	}

	members := manifest.ResolvedMembers()

	fmt.Printf("Group: %s %s\n", manifest.Name, trustTierLabel(tier))
	if manifest.Description != "" {
		fmt.Printf("  %s\n", manifest.Description)
	}
	fmt.Printf("  %d tools\n\n", len(members))

	// Check trust tier and prompt
	if strings.ToLower(tier) == "blocked" {
		return fmt.Errorf("group %q is blocked by the trust system", groupName)
	}

	// Workspace groups (fetched with auth token) are trusted
	isTrusted := installTrust || token != ""
	if strings.ToLower(tier) == "community" && !isTrusted {
		fmt.Fprintf(os.Stderr, "%s\n", trustTierPrompt(tier))
		return fmt.Errorf("use --trust to install community tools")
	}

	if strings.ToLower(tier) == "partial" {
		fmt.Fprintf(os.Stderr, "%s\n\n", trustTierPrompt(tier))
	}

	// Install each member
	cache := registry.NewCache(cfg.CacheDir)
	client := registry.NewClient(apiURL, cache, flagNoCache)
	if token != "" {
		client.AuthToken = token
	}

	installed := 0
	failed := 0

	fmt.Printf("Installing %s: %d tools...\n", groupName, len(members))

	for _, member := range members {
		// C2.3: Check individual member blocklist status
		if isToolBlocked(cfg, member.Name) {
			fmt.Fprintf(os.Stderr, "  Skipping %s: blocked by policy\n", member.Name)
			failed++
			continue
		}

		memberType := member.Type
		if memberType == "" {
			memberType = member.Protocol
		}
		if member.Version != "" {
			fmt.Printf("  Installing %s (v%s, %s)...\n", member.Name, member.Version, memberType)
		} else {
			fmt.Printf("  Installing %s...\n", member.Name)
		}

		// Try pack-based install first if URLs are available
		if member.PackURL != "" && token != "" {
			packPath, packOK := tryInstallFromRelease(ctx, cfg, member.Name, member.Version, target)
			if packOK {
				fmt.Printf("    Installed from signed pack: %s\n", packPath)
				if err := addToInstalled(member.Name); err != nil {
					fmt.Fprintf(os.Stderr, "    Warning: could not track install: %v\n", err)
				}
				installed++
				continue
			}
		}

		// Fall back to spec-based install
		spec, _, fetchErr := client.GetSpecYAML(ctx, member.Name)
		if fetchErr != nil {
			fmt.Fprintf(os.Stderr, "    Failed: %v\n", fetchErr)
			failed++
			continue
		}

		if spec.IsSkill() {
			path, installErr := installSkillFromSource(ctx, spec, target)
			if installErr != nil {
				fmt.Fprintf(os.Stderr, "    Failed: %v\n", installErr)
				failed++
				continue
			}
			fmt.Printf("    Installed skill: %s\n", path)
		} else {
			path, installErr := generateSkillForTarget(spec, target)
			if installErr != nil {
				fmt.Fprintf(os.Stderr, "    Failed: %v\n", installErr)
				failed++
				continue
			}
			fmt.Printf("    Installed: %s\n", path)
		}

		applyIsolationRules(spec, target)

		if err := addToInstalled(member.Name); err != nil {
			fmt.Fprintf(os.Stderr, "    Warning: could not track install: %v\n", err)
		}

		// Generate shim unless disabled
		if !installNoShims {
			if shimErr := generateShim(member.Name); shimErr != nil {
				fmt.Fprintf(os.Stderr, "    Warning: could not generate shim: %v\n", shimErr)
			}
		}

		installed++
	}

	fmt.Printf("\nGroup %s: %d installed", groupName, installed)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println()

	// Track the group itself as installed
	if err := addToInstalled("group:" + groupName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not track group install: %v\n", err)
	}

	return nil
}

// isToolBlocked checks if a tool is blocked by workspace policy or local blocklist.
func isToolBlocked(cfg *config.Config, toolName string) bool {
	if cfg.Auth.ActiveWorkspace == "" {
		return false
	}
	policy, err := loadWorkspacePolicy(cfg.Auth.ActiveWorkspace)
	if err != nil {
		return false
	}
	// Check if blocked trust tiers include this tool's tier
	for _, blocked := range policy.BlockedTrustTiers {
		if strings.EqualFold(blocked, "blocked") {
			return true
		}
	}
	return false
}

// showGroupInfo displays detailed information about a group.
// C2.4: `clictl info <group> --packages` shows group contents with types and versions.
func showGroupInfo(ctx context.Context, cfg *config.Config, groupName string, showPackages bool) error {
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	token := config.ResolveAuthToken(flagAPIKey, cfg)

	manifest, err := fetchGroupManifest(ctx, apiURL, token, groupName)
	if err != nil {
		return err
	}

	tier := manifest.TrustTier
	if tier == "" {
		tier = "community"
	}

	fmt.Printf("Group: %s %s\n", manifest.Name, trustTierLabel(tier))
	if manifest.Description != "" {
		fmt.Printf("Description: %s\n", manifest.Description)
	}
	fmt.Printf("Version: %s\n", manifest.Version)
	if manifest.Publisher != "" {
		fmt.Printf("Publisher: %s\n", manifest.Publisher)
	}
	fmt.Printf("Members: %d tools\n", len(manifest.Members))

	if showPackages || len(manifest.Members) > 0 {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTYPE\tVERSION")
		for _, m := range manifest.Members {
			fmt.Fprintf(w, "%s\t%s\t%s\n", m.Name, m.Type, m.Version)
		}
		w.Flush()
	}

	return nil
}

// listGroupsForDisplay returns group entries that can be mixed into the `clictl list` output.
// C2.5: Groups appear in `clictl list` output with a [group] tag.
func listGroupsForDisplay(ctx context.Context, cfg *config.Config) []models.SearchResult {
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	token := config.ResolveAuthToken(flagAPIKey, cfg)

	u := fmt.Sprintf("%s/api/v1/groups/", apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var groups []GroupManifest
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return nil
	}

	results := make([]models.SearchResult, 0, len(groups))
	for _, g := range groups {
		desc := g.Description
		if desc == "" {
			desc = fmt.Sprintf("Group with %d tools", len(g.Members))
		}
		results = append(results, models.SearchResult{
			Name:        g.Name,
			Description: "[group] " + desc,
			Version:     g.Version,
			Category:    "group",
			TrustTier:   g.TrustTier,
		})
	}

	return results
}
