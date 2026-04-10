// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/clictl/cli/internal/models"
)

// DiscoveredTool represents a tool discovered at runtime from an MCP server.
type DiscoveredTool struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema for params
}

// DiscoverTools connects to the MCP server defined in spec and returns its
// tool list. The caller is responsible for closing the client when done (or
// use DiscoverToolsWithPool for pooled connections).
func DiscoverTools(ctx context.Context, spec *models.ToolSpec) ([]DiscoveredTool, error) {
	client, err := NewClient(spec)
	if err != nil {
		return nil, fmt.Errorf("creating MCP client for %s: %w", spec.Name, err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("initializing MCP client for %s: %w", spec.Name, err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tools from %s: %w", spec.Name, err)
	}

	return convertTools(tools), nil
}

// DiscoverToolsWithPool is like DiscoverTools but uses the provided
// connection pool for client management.
func DiscoverToolsWithPool(ctx context.Context, spec *models.ToolSpec, pool *Pool) ([]DiscoveredTool, error) {
	client, err := pool.Get(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("getting MCP client for %s: %w", spec.Name, err)
	}
	defer pool.Release(spec.Name)

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tools from %s: %w", spec.Name, err)
	}

	return convertTools(tools), nil
}

// convertTools converts MCP Tool structs to DiscoveredTool structs.
func convertTools(tools []Tool) []DiscoveredTool {
	result := make([]DiscoveredTool, len(tools))
	for i, t := range tools {
		schema := make(map[string]any)
		if t.InputSchema.Type != "" {
			schema["type"] = t.InputSchema.Type
		}
		if len(t.InputSchema.Properties) > 0 {
			props := make(map[string]any, len(t.InputSchema.Properties))
			for k, v := range t.InputSchema.Properties {
				prop := map[string]any{"type": v.Type}
				if v.Description != "" {
					prop["description"] = v.Description
				}
				if v.Default != "" {
					prop["default"] = v.Default
				}
				props[k] = prop
			}
			schema["properties"] = props
		}
		if len(t.InputSchema.Required) > 0 {
			schema["required"] = t.InputSchema.Required
		}

		result[i] = DiscoveredTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		}
	}
	return result
}

// MergeActions merges static spec actions with dynamically discovered tools.
// Static actions take precedence for matching names (they have better descriptions,
// examples, and instructions). Discovered tools are filtered by the allow/deny
// patterns before merging.
func MergeActions(static []models.Action, discovered []DiscoveredTool, allow, deny []string) []models.Action {
	// Build a set of static action names for fast lookup
	staticNames := make(map[string]bool, len(static))
	for _, a := range static {
		staticNames[a.Name] = true
	}

	// Start with a copy of all static actions
	merged := make([]models.Action, len(static))
	copy(merged, static)

	// Add discovered tools that pass filtering and are not already static
	for _, dt := range discovered {
		// Check deny patterns first (deny always wins)
		if matchesAny(dt.Name, deny) {
			continue
		}

		// If allow list is set, the tool must match at least one pattern
		if len(allow) > 0 && !matchesAny(dt.Name, allow) {
			continue
		}

		// Static actions win for name collisions
		if staticNames[dt.Name] {
			continue
		}

		// Convert DiscoveredTool to Action
		action := discoveredToAction(dt)
		merged = append(merged, action)
	}

	return merged
}

// discoveredToAction converts a DiscoveredTool into a models.Action.
func discoveredToAction(dt DiscoveredTool) models.Action {
	action := models.Action{
		Name:        dt.Name,
		Description: dt.Description,
	}

	// Extract params from input schema
	props, _ := dt.InputSchema["properties"].(map[string]any)
	requiredSlice, _ := dt.InputSchema["required"].([]string)
	requiredSet := make(map[string]bool, len(requiredSlice))
	for _, r := range requiredSlice {
		requiredSet[r] = true
	}

	for name, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		p := models.Param{
			Name:     name,
			Required: requiredSet[name],
		}
		if t, ok := prop["type"].(string); ok {
			p.Type = t
		}
		if d, ok := prop["description"].(string); ok {
			p.Description = d
		}
		if def, ok := prop["default"].(string); ok {
			p.Default = def
		}
		action.Params = append(action.Params, p)
	}

	return action
}

// matchPattern checks if a tool name matches an allow/deny pattern.
// Supports exact match and glob patterns with * (e.g., "drop_*", "query_*").
func matchPattern(name, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	return name == pattern
}

// matchesAny returns true if name matches any of the given patterns.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchPattern(name, p) {
			return true
		}
	}
	return false
}
