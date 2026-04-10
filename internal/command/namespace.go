// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

// parseNamespacedTool parses a tool reference that may include a namespace scope.
// Formats supported:
//   - "tool"         -> namespace="", name="tool"
//   - "scope/tool"   -> namespace="scope", name="tool"
//   - "@scope/tool"  -> namespace="scope", name="tool"
func parseNamespacedTool(ref string) (namespace, name string) {
	ref = strings.TrimPrefix(ref, "@")

	if idx := strings.Index(ref, "/"); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}

	return "", ref
}

// resolveNamespacedSpec resolves a tool spec using namespace-aware lookup.
// If a namespace is specified (scope/name), it filters results to that namespace.
// If no namespace is given and multiple specs match (collision), it prompts
// the user to disambiguate.
func resolveNamespacedSpec(ctx context.Context, toolRef string, cfg *config.Config, cache *registry.Cache) (*models.ToolSpec, error) {
	namespace, name := parseNamespacedTool(toolRef)

	// If namespace is explicit, try direct resolution with the scoped name
	if namespace != "" {
		spec, err := registry.ResolveSpec(ctx, name, cfg, cache, flagNoCache)
		if err != nil {
			return nil, fmt.Errorf("tool %q not found in namespace %q: %w", name, namespace, err)
		}
		// Verify the namespace matches
		if spec.Namespace != "" && spec.Namespace != namespace {
			return nil, fmt.Errorf("tool %q resolved to namespace %q, not %q", name, spec.Namespace, namespace)
		}
		return spec, nil
	}

	// No namespace specified - try direct resolution
	spec, err := registry.ResolveSpec(ctx, name, cfg, cache, flagNoCache)
	if err != nil {
		return nil, err
	}

	return spec, nil
}

// disambiguateTools prompts the user to choose between multiple tools with the same name.
func disambiguateTools(tools []models.SearchResult) (string, error) {
	if len(tools) == 0 {
		return "", fmt.Errorf("no tools to choose from")
	}

	if len(tools) == 1 {
		name := tools[0].Name
		if tools[0].Source != "" {
			return tools[0].Source + "/" + name, nil
		}
		return name, nil
	}

	fmt.Fprintf(os.Stderr, "\nMultiple tools found with this name:\n\n")
	for i, t := range tools {
		source := t.Source
		if source == "" {
			source = "default"
		}
		fmt.Fprintf(os.Stderr, "  [%d] %s/%s - %s (v%s)\n", i+1, source, t.Name, t.Description, t.Version)
	}
	fmt.Fprintf(os.Stderr, "\nEnter number to select: ")

	var input string
	fmt.Scanln(&input)

	var num int
	if _, err := fmt.Sscanf(input, "%d", &num); err != nil || num < 1 || num > len(tools) {
		return "", fmt.Errorf("invalid selection")
	}

	selected := tools[num-1]
	name := selected.Name
	if selected.Source != "" {
		name = selected.Source + "/" + name
	}
	return name, nil
}

// formatNamespacedName formats a tool name with optional namespace prefix.
func formatNamespacedName(namespace, name string) string {
	if namespace != "" {
		return namespace + "/" + name
	}
	return name
}
