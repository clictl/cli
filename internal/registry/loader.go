// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.

// ParseSpec is the entry point for all spec loading. Every spec goes through this function
// whether it comes from a local toolbox, the API, or a YAML file. It validates required fields,
// synthesizes server config from package blocks, and enables runtime discovery for stdio servers.

package registry

import (
	"fmt"

	"github.com/clictl/cli/internal/models"
	"gopkg.in/yaml.v3"
)

// ParseSpec parses raw YAML bytes into a ToolSpec.
func ParseSpec(data []byte) (*models.ToolSpec, error) {
	var spec models.ToolSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing spec YAML: %w", err)
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("spec is missing required field: name")
	}

	// Synthesize server config from package metadata (npm/pypi MCP servers)
	spec.EnsureServer()

	// Stdio MCP servers always use runtime discovery. Static actions in the
	// spec are metadata for search and documentation, not for execution.
	if spec.IsStdio() {
		spec.Discover = true
	}

	// Validate protocol matches structure
	switch spec.Protocol {
	case "http", "website":
		if spec.Server == nil || spec.Server.URL == "" {
			return nil, fmt.Errorf("spec %q: protocol %q requires server.url", spec.Name, spec.Protocol)
		}
	case "mcp":
		if spec.Server == nil && spec.Package == nil {
			return nil, fmt.Errorf("spec %q: protocol mcp requires server.command or package block", spec.Name)
		}
	case "skill":
		if spec.Source == nil {
			return nil, fmt.Errorf("spec %q: protocol skill requires source block", spec.Name)
		}
	case "command":
		if len(spec.Actions) == 0 {
			return nil, fmt.Errorf("spec %q: protocol command requires at least one action with run", spec.Name)
		}
	case "":
		// Protocol not set - infer from structure for backward compat
	default:
		return nil, fmt.Errorf("spec %q: unknown protocol %q (valid: http, mcp, skill, website, command)", spec.Name, spec.Protocol)
	}
	return &spec, nil
}

// FindAction looks up an action by name within a spec.
// For MCP specs, this returns nil since tools come from the MCP server, not from spec actions.
func FindAction(spec *models.ToolSpec, actionName string) (*models.Action, error) {
	for i := range spec.Actions {
		if spec.Actions[i].Name == actionName {
			return &spec.Actions[i], nil
		}
	}
	available := make([]string, len(spec.Actions))
	for i, a := range spec.Actions {
		available[i] = a.Name
	}
	return nil, fmt.Errorf("action %q not found in tool %q (available: %v)", actionName, spec.Name, available)
}

// IsMCPToolAllowed checks if an MCP tool name is permitted by the spec's allow/deny config.
// Returns true if the tool is allowed (passes allow/deny filters).
func IsMCPToolAllowed(spec *models.ToolSpec, toolName string) bool {
	// Deny list always wins
	for _, pattern := range spec.Deny {
		if matchToolPattern(pattern, toolName) {
			return false
		}
	}

	// If allow list is set, tool must match at least one pattern
	if len(spec.Allow) > 0 {
		for _, pattern := range spec.Allow {
			if matchToolPattern(pattern, toolName) {
				return true
			}
		}
		return false
	}

	// If discover is set with no allow list, all tools are allowed
	if spec.Discover {
		return true
	}

	// Check if tool name matches a defined action
	for _, action := range spec.Actions {
		if action.Name == toolName {
			return true
		}
	}

	// No restrictions: allow by default
	return true
}

// matchToolPattern checks if a tool name matches a pattern.
// Supports simple glob patterns with * wildcards.
func matchToolPattern(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == name {
		return true
	}
	// Simple prefix glob: "foo_*" matches "foo_bar"
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(name) >= len(prefix) && name[:len(prefix)] == prefix
	}
	return false
}
