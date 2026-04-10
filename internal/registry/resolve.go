// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/models"
	"gopkg.in/yaml.v3"
)

// IsCuratedToolbox returns true if the toolbox is the official curated toolbox.
// It checks if the URL contains "clictl/toolbox" which is the canonical source.
func IsCuratedToolbox(reg config.ToolboxConfig) bool {
	return strings.Contains(reg.URL, "clictl/toolbox") || reg.Name == "clictl-toolbox"
}

// prioritizeCuratedToolbox reorders registries so curated toolboxes come first.
// This ensures that unscoped installs prefer the official curated toolbox.
func prioritizeCuratedToolbox(regs []config.ToolboxConfig) []config.ToolboxConfig {
	var curated, rest []config.ToolboxConfig
	for _, r := range regs {
		if IsCuratedToolbox(r) {
			curated = append(curated, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(curated, rest...)
}

// findSpecByConvention locates a spec YAML file in a git toolbox directory
// using the standard convention: {letter}/{name}/{name}.yaml or
// toolbox/{letter}/{name}/{name}.yaml.
func findSpecByConvention(regDir, name string) string {
	if name == "" {
		return ""
	}
	letter := strings.ToLower(name[:1])

	// Try: toolbox/{letter}/{name}/{name}.yaml
	p := filepath.Join(regDir, "toolbox", letter, name, name+".yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	// Try: {letter}/{name}/{name}.yaml
	p = filepath.Join(regDir, letter, name, name+".yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	// Try: toolbox/{name}/{name}.yaml (flat toolbox)
	p = filepath.Join(regDir, "toolbox", name, name+".yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	// Try: {name}/{name}.yaml (flat)
	p = filepath.Join(regDir, name, name+".yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	// Try: {name}.yaml (root level)
	p = filepath.Join(regDir, name+".yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// ParseToolVersion splits a "tool@version" string into its name and version parts.
// If no "@" is present, version is returned as empty.
func ParseToolVersion(input string) (name, version string) {
	if idx := strings.LastIndex(input, "@"); idx > 0 {
		return input[:idx], input[idx+1:]
	}
	return input, ""
}

// AllToolNames returns the names of all tools across all configured registries.
// Used for "did you mean?" suggestions when a tool is not found.
// If a workspace CLI index is available, it uses merged registries.
func AllToolNames(cfg *config.Config) []string {
	bucketsDir := config.ToolboxesDir()
	seen := map[string]bool{}
	var names []string

	regs := cfg.Toolboxes
	if cfg.Auth.ActiveWorkspace != "" {
		if wsIdx, err := LoadCachedCLIIndex(cfg.Auth.ActiveWorkspace); err == nil {
			regs = MergedRegistries(cfg, wsIdx)
		}
	}

	for _, reg := range regs {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := NewLocalIndex(regDir, reg.Name)
		idx, err := li.Load()
		if err != nil {
			continue
		}
		for name := range idx.Specs {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// ResolveSpec finds and loads a spec by name across all configured registries.
// For API registries: fetches from API with ETag caching.
// For Git registries: reads from local filesystem.
// Falls back to the default API client if no local index has the spec.
// The name may include a version suffix (e.g. "tool@1.2.0").
func ResolveSpec(ctx context.Context, name string, cfg *config.Config, cache *Cache, noCache bool) (*models.ToolSpec, error) {
	toolName, version := ParseToolVersion(name)
	return ResolveSpecVersion(ctx, toolName, version, cfg, cache, noCache)
}

// ResolveSpecVersion finds and loads a spec by name and optional version.
// When version is empty, the latest version is resolved.
// When version is set, it looks for versioned files in git registries
// and versioned API endpoints for API registries.
// Uses merged registries (workspace + local) when a workspace is active.
//
// Resolution order: project toolboxes > workspace sources > personal toolboxes > curated > API fallback.
// This order ensures local specs take priority over remote ones, and workspace policies
// can override personal preferences.
func ResolveSpecVersion(ctx context.Context, name, version string, cfg *config.Config, cache *Cache, noCache bool) (*models.ToolSpec, error) {
	bucketsDir := config.ToolboxesDir()
	logger.Debug("resolving spec", logger.F("name", name), logger.F("version", version))

	// P11.12: Resolution order is project > workspace > personal > curated.
	// Check project-level toolboxes first (highest priority).
	if spec := resolveFromProjectToolboxes(name, version); spec != nil {
		logger.Info("spec resolved from project toolbox", logger.F("name", name))
		return spec, nil
	}

	// Build merged toolbox list from workspace + local config
	regs := cfg.Toolboxes
	var wsDisabled []string
	if cfg.Auth.ActiveWorkspace != "" {
		if wsIdx, err := LoadCachedCLIIndex(cfg.Auth.ActiveWorkspace); err == nil {
			regs = MergedRegistries(cfg, wsIdx)
			wsDisabled = wsIdx.DisabledTools
		}
	}

	// Sort by scope priority: workspace > personal > curated/local
	regs = sortByScope(regs)

	// N1.10: Prioritize curated toolbox (clictl/toolbox) for unscoped names
	regs = prioritizeCuratedToolbox(regs)

	// Check if tool is disabled at workspace level
	if IsDisabledByWorkspace(wsDisabled, name) {
		return nil, fmt.Errorf("tool %q is disabled in this workspace", name)
	}

	for _, reg := range regs {
		regDir := filepath.Join(bucketsDir, reg.Name)
		li := NewLocalIndex(regDir, reg.Name)

		entry, _ := li.GetEntry(name)
		logger.Debug("checking registry", logger.F("registry", reg.Name), logger.F("type", reg.Type), logger.F("found", entry != nil))

		// For private workspace sources, fetch spec locally via git
		if reg.FromWorkspace && reg.IsPrivate && entry != nil {
			spec, err := FetchPrivateSpec(ctx, name, reg.URL, reg.Branch, reg.SourceID, reg.SpecCount, reg.LastSyncedAt)
			if err != nil {
				continue // try next registry
			}
			if version != "" && spec.Version != version {
				continue
			}
			spec.IsVerified = true // workspace tools are trusted
			return spec, nil
		}

		// Determine if this source is trusted (curated toolbox or workspace)
		trusted := IsCuratedToolbox(reg) || reg.FromWorkspace

		switch reg.Type {
		case "git":
			// If no index.json entry, try finding the spec by filesystem convention
			if entry == nil {
				specPath := findSpecByConvention(regDir, name)
				logger.Debug("convention lookup", logger.F("registry", reg.Name), logger.F("name", name), logger.F("regDir", regDir), logger.F("specPath", specPath))
				if specPath == "" {
					continue
				}
				data, readErr := os.ReadFile(specPath)
				if readErr != nil {
					logger.Debug("convention read failed", logger.F("path", specPath), logger.F("error", readErr.Error()))
					continue
				}
				spec, parseErr := ParseSpec(data)
				if parseErr != nil {
					logger.Debug("convention parse failed", logger.F("path", specPath), logger.F("error", parseErr.Error()))
					continue
				}
				if version != "" && spec.Version != version {
					continue
				}
				spec.IsVerified = trusted
				return spec, nil
			}
			var specPath string
			if version != "" {
				entryDir := filepath.Join(regDir, filepath.Dir(entry.Path))
				// Try {version}.yaml in the tool directory (toolbox convention)
				toolboxVersionPath := filepath.Join(entryDir, version+".yaml")
				// Try {name}@{version}.yaml (legacy convention)
				legacyVersionPath := filepath.Join(entryDir, name+"@"+version+".yaml")
				if _, statErr := os.Stat(toolboxVersionPath); statErr == nil {
					specPath = toolboxVersionPath
				} else if _, statErr := os.Stat(legacyVersionPath); statErr == nil {
					specPath = legacyVersionPath
				} else {
					// Fall back to the default path and check version after parsing
					specPath = filepath.Join(regDir, entry.Path)
				}
			} else {
				specPath = filepath.Join(regDir, entry.Path)
			}

			data, err := os.ReadFile(specPath)
			if err != nil {
				return nil, fmt.Errorf("reading spec file from git registry %q: %w", reg.Name, err)
			}
			spec, err := ParseSpec(data)
			if err != nil {
				return nil, fmt.Errorf("parsing spec from git registry %q: %w", reg.Name, err)
			}
			if version != "" && spec.Version != version {
				return nil, fmt.Errorf("spec %q version %q not found in git registry %q (found %q)", name, version, reg.Name, spec.Version)
			}
			spec.IsVerified = trusted
			return spec, nil

		case "api":
			apiURL := reg.URL
			if apiURL == "" || apiURL == config.DefaultAPIURL {
				apiURL = cfg.APIURL
			}
			client := NewClient(apiURL, cache, noCache)
			if version != "" {
				spec, _, err := client.GetSpecVersionYAML(ctx, name, version)
				if err != nil {
					continue // try next registry
				}
				spec.IsVerified = trusted
				return spec, nil
			}
			spec, _, err := client.GetSpecYAML(ctx, name)
			if err != nil {
				continue // try next registry
			}
			spec.IsVerified = trusted
			return spec, nil
		}
	}

	// No local index had the spec. Fall back to the default API.
	// This is the lowest priority in the resolution chain.
	logger.Debug("falling back to default API", logger.F("name", name))
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	client := NewClient(apiURL, cache, noCache)
	if version != "" {
		spec, _, err := client.GetSpecVersionYAML(ctx, name, version)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch spec %q version %q: %w", name, version, err)
		}
		return spec, nil
	}
	spec, _, err := client.GetSpecYAML(ctx, name)
	if err != nil {
		// If unqualified lookup fails and user has an active workspace,
		// try workspace-scoped lookup (workspace-slug/tool-name)
		if cfg.Auth.ActiveWorkspace != "" && !strings.Contains(name, "/") {
			qualifiedName := cfg.Auth.ActiveWorkspace + "/" + name
			spec, _, err2 := client.GetSpecYAML(ctx, qualifiedName)
			if err2 == nil {
				return spec, nil
			}
		}
		return nil, fmt.Errorf("failed to fetch spec for %q: %w", name, err)
	}
	return spec, nil
}

// resolveFromProjectToolboxes checks project-level .clictl/toolboxes.yaml sources.
func resolveFromProjectToolboxes(name, version string) *models.ToolSpec {
	dir := findProjectRoot()
	if dir == "" {
		return nil
	}
	toolboxesFile := filepath.Join(dir, ".clictl", "toolboxes.yaml")
	data, err := os.ReadFile(toolboxesFile)
	if err != nil {
		return nil
	}

	type projectToolboxes struct {
		Sources []struct {
			Name string `yaml:"name"`
			URL  string `yaml:"url"`
		} `yaml:"sources"`
	}
	var pt projectToolboxes
	if err := parseYAML(data, &pt); err != nil {
		return nil
	}

	bucketsDir := config.ToolboxesDir()
	for _, src := range pt.Sources {
		regDir := filepath.Join(bucketsDir, src.Name)
		li := NewLocalIndex(regDir, src.Name)
		entry, _ := li.GetEntry(name)
		if entry == nil {
			specPath := findSpecByConvention(regDir, name)
			if specPath == "" {
				continue
			}
			specData, err := os.ReadFile(specPath)
			if err != nil {
				continue
			}
			spec, err := ParseSpec(specData)
			if err != nil {
				continue
			}
			if version != "" && spec.Version != version {
				continue
			}
			return spec
		}
		specPath := filepath.Join(regDir, entry.Path)
		specData, err := os.ReadFile(specPath)
		if err != nil {
			continue
		}
		spec, err := ParseSpec(specData)
		if err != nil {
			continue
		}
		if version != "" && spec.Version != version {
			continue
		}
		return spec
	}
	return nil
}

// findProjectRoot walks up from CWD to find .clictl/ or .git root.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".clictl")); err == nil && info.IsDir() {
			return dir
		}
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// sortByScope orders registries by scope priority: workspace > personal > curated/local.
func sortByScope(regs []config.ToolboxConfig) []config.ToolboxConfig {
	scopePriority := func(r config.ToolboxConfig) int {
		switch r.Scope {
		case "workspace":
			return 0
		case "personal":
			return 1
		default:
			return 2
		}
	}
	sorted := make([]config.ToolboxConfig, len(regs))
	copy(sorted, regs)
	// Stable sort to preserve existing order within same scope
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && scopePriority(sorted[j]) < scopePriority(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
}

// parseYAML unmarshals YAML data into the target.
func parseYAML(data []byte, target interface{}) error {
	return yaml.Unmarshal(data, target)
}
