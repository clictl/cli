// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.

// MCP tool dispatch. When a spec has Discover=true (all stdio servers), tool calls
// are proxied to the upstream MCP server via the connection pool. The pool manages
// server lifecycle - starting servers on first use and releasing them when done.

package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/transform"
)

// GlobalPool is the shared MCP connection pool for the CLI process.
var GlobalPool = mcp.NewPool()

// DispatchMCP executes an MCP tool call using the connection pool.
// Unlike Dispatch, this takes a tool name (not an Action) and map[string]any args.
func DispatchMCP(ctx context.Context, spec *models.ToolSpec, toolName string, args map[string]any) ([]byte, error) {
	return DispatchMCPWithPool(ctx, spec, toolName, args, GlobalPool)
}

// DispatchMCPWithPool is like DispatchMCP but accepts an explicit pool.
func DispatchMCPWithPool(ctx context.Context, spec *models.ToolSpec, toolName string, args map[string]any, pool *mcp.Pool) ([]byte, error) {
	// Get or create client from pool
	client, err := pool.Get(ctx, spec)
	if err != nil {
		return nil, err
	}
	defer pool.Release(spec.Name)

	// Apply inject transforms (merge default args, user args take precedence)
	args = applyInjectTransforms(spec, toolName, args)

	// Fetch tool schema for validation and error enhancement
	toolSchema := findToolSchema(ctx, client, toolName)

	// Validate required params
	if toolSchema != nil {
		if msg := checkMissingParams(toolSchema, toolName, args); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
	}

	// Call the tool
	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		if fallback := getFallbackValue(spec, toolName); fallback != "" {
			return []byte(fallback), nil
		}
		// If the call failed and no args were provided, the server likely
		// crashed due to missing params. Show available params as a hint.
		if len(args) == 0 && toolSchema != nil {
			return nil, fmt.Errorf("%s\n\n%s", err, formatParamHint(toolSchema, toolName))
		}
		return nil, err
	}

	if result.IsError {
		text := extractResultText(result)
		if fallback := getFallbackValue(spec, toolName); fallback != "" {
			return []byte(fallback), nil
		}
		// Enhance error with param hint when no args were provided
		if len(args) == 0 && toolSchema != nil {
			return nil, fmt.Errorf("%s\n\n%s", text, formatParamHint(toolSchema, toolName))
		}
		return nil, fmt.Errorf("MCP tool %s returned error: %s", toolName, text)
	}

	// Extract text content from result
	text := extractResultText(result)

	// Apply tool-level transforms from the spec
	transformed, err := applyToolTransforms(spec, toolName, text)
	if err != nil {
		return nil, fmt.Errorf("applying transforms for %s: %w", toolName, err)
	}

	return []byte(transformed), nil
}

// DispatchMCPWithClient is like DispatchMCP but uses an existing client
// instead of obtaining one from the connection pool. This is used by the
// interactive shell where a long-lived client connection is already open.
func DispatchMCPWithClient(ctx context.Context, spec *models.ToolSpec, toolName string, args map[string]any, client *mcp.Client) ([]byte, error) {
	// Apply inject transforms (merge default args, user args take precedence)
	args = applyInjectTransforms(spec, toolName, args)

	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		if fallback := getFallbackValue(spec, toolName); fallback != "" {
			return []byte(fallback), nil
		}
		return nil, err
	}

	if result.IsError {
		text := extractResultText(result)
		if fallback := getFallbackValue(spec, toolName); fallback != "" {
			return []byte(fallback), nil
		}
		return nil, fmt.Errorf("MCP tool %s returned error: %s", toolName, text)
	}

	text := extractResultText(result)

	transformed, err := applyToolTransforms(spec, toolName, text)
	if err != nil {
		return nil, fmt.Errorf("applying transforms for %s: %w", toolName, err)
	}

	return []byte(transformed), nil
}

// extractResultText extracts displayable content from an MCP CallToolResult.
// Handles all 5 MCP content block types: text, image, audio, resource_link,
// and embedded_resource.
func extractResultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "image":
			mime := block.MimeType
			if mime == "" {
				mime = "image/png"
			}
			if block.Data != "" {
				parts = append(parts, fmt.Sprintf("[image: %s, %d bytes base64]", mime, len(block.Data)))
			}
		case "audio":
			mime := block.MimeType
			if mime == "" {
				mime = "audio/wav"
			}
			if block.Data != "" {
				parts = append(parts, fmt.Sprintf("[audio: %s, %d bytes base64]", mime, len(block.Data)))
			}
		case "resource_link":
			if block.URI != "" {
				parts = append(parts, fmt.Sprintf("[resource: %s]", block.URI))
			}
		case "embedded_resource":
			if block.Resource != nil {
				if block.Resource.Text != "" {
					parts = append(parts, block.Resource.Text)
				} else if block.Resource.Blob != "" {
					mime := block.Resource.MimeType
					if mime == "" {
						mime = "application/octet-stream"
					}
					parts = append(parts, fmt.Sprintf("[embedded resource: %s, %s, %d bytes]", block.Resource.URI, mime, len(block.Resource.Blob)))
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

// applyInjectTransforms merges inject-defined default args with user-provided args.
// Looks for TransformSteps with Inject fields and merges them as defaults.
func applyInjectTransforms(spec *models.ToolSpec, toolName string, args map[string]any) map[string]any {
	if spec.Transforms == nil {
		return args
	}
	steps, ok := spec.Transforms[toolName]
	if !ok {
		return args
	}

	result := make(map[string]any, len(args))
	for k, v := range args {
		result[k] = v
	}

	for _, step := range steps {
		if step.Inject == nil {
			continue
		}
		for k, v := range step.Inject {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}

	return result
}

// getFallbackValue returns the fallback value for a tool, or empty string if none configured.
func getFallbackValue(spec *models.ToolSpec, toolName string) string {
	if spec.Transforms == nil {
		return ""
	}
	steps, ok := spec.Transforms[toolName]
	if !ok {
		return ""
	}
	for _, step := range steps {
		if step.Value != "" && step.Type == "fallback" {
			return step.Value
		}
	}
	return ""
}

// applyToolTransforms applies the spec's per-action transforms pipeline to the tool output.
func applyToolTransforms(spec *models.ToolSpec, toolName string, text string) (string, error) {
	if spec.Transforms == nil {
		return text, nil
	}

	steps, ok := spec.Transforms[toolName]
	if !ok {
		return text, nil
	}

	// Filter out inject and fallback steps (already handled)
	var transformSteps []models.TransformStep
	for _, step := range steps {
		if step.Inject != nil {
			continue
		}
		if step.Type == "fallback" {
			continue
		}
		transformSteps = append(transformSteps, step)
	}

	if len(transformSteps) == 0 {
		return text, nil
	}

	// Parse the text as JSON if possible for structured transforms
	var data any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		data = text
	}

	// Convert typed TransformSteps to raw []any for ParseSteps
	raw := transformStepsToRaw(transformSteps)
	pipeline, err := transform.ParseSteps(raw)
	if err != nil {
		return text, fmt.Errorf("parsing tool transforms: %w", err)
	}

	result, err := pipeline.Apply(data)
	if err != nil {
		return text, fmt.Errorf("applying tool transforms: %w", err)
	}

	// Convert back to string
	switch v := result.(type) {
	case string:
		return v, nil
	default:
		out, err := json.Marshal(v)
		if err != nil {
			return text, nil
		}
		return string(out), nil
	}
}

// transformStepsToRaw converts typed TransformStep slices to []any maps
// in the format expected by transform.ParseSteps (the legacy nested format).
func transformStepsToRaw(steps []models.TransformStep) []any {
	result := make([]any, len(steps))
	for i, s := range steps {
		m := make(map[string]any)

		switch s.Type {
		case "json":
			if s.Extract != "" {
				m["extract"] = s.Extract
			}
			if len(s.Select) > 0 {
				m["select"] = s.Select
			}
			if len(s.Rename) > 0 {
				m["rename"] = s.Rename
			}
			if len(s.Only) > 0 {
				m["only"] = s.Only
			}
			if s.Flatten {
				m["flatten"] = true
			}
			if s.Unwrap {
				m["unwrap"] = true
			}
			if len(s.Inject) > 0 {
				m["inject"] = s.Inject
			}
			if len(s.DefaultFields) > 0 {
				m["default"] = s.DefaultFields
			}

		case "truncate":
			cfg := map[string]any{}
			if s.MaxItems > 0 {
				cfg["max_items"] = s.MaxItems
			}
			if s.MaxLength > 0 {
				cfg["max_length"] = s.MaxLength
			}
			m["truncate"] = cfg

		case "template", "format":
			if s.Template != "" {
				m["template"] = s.Template
			}

		case "html_to_markdown":
			cfg := map[string]any{}
			if s.RemoveImages {
				cfg["remove_images"] = true
			}
			if s.RemoveLinks {
				cfg["remove_links"] = true
			}
			m["html_to_markdown"] = cfg

		case "prefix":
			if s.Value != "" {
				m["prefix"] = s.Value
			}

		case "filter", "jq":
			if s.Filter != "" {
				m["jq"] = s.Filter
			}

		case "redact":
			if len(s.Patterns) > 0 {
				patterns := make([]map[string]any, len(s.Patterns))
				for j, p := range s.Patterns {
					patterns[j] = map[string]any{"field": p.Field, "replace": p.Replace}
				}
				m["redact"] = patterns
			}

		case "cost":
			cfg := map[string]any{}
			if s.InputTokens != "" {
				cfg["input_tokens"] = s.InputTokens
			}
			if s.OutputTokens != "" {
				cfg["output_tokens"] = s.OutputTokens
			}
			if s.Model != "" {
				cfg["model"] = s.Model
			}
			m["cost"] = cfg

		default:
			// Generic fallback: marshal to JSON then unmarshal to map
			b, err := json.Marshal(s)
			if err != nil {
				m["type"] = s.Type
			} else {
				var flat map[string]any
				if err := json.Unmarshal(b, &flat); err == nil {
					m = flat
				} else {
					m["type"] = s.Type
				}
			}
		}

		result[i] = m
	}
	return result
}

// findToolSchema returns the schema for a specific tool from the server, or nil.
func findToolSchema(ctx context.Context, client *mcp.Client, toolName string) *mcp.Tool {
	tools, err := client.ListTools(ctx)
	if err != nil {
		logger.Debug("tool schema lookup failed", logger.F("error", err.Error()))
		return nil
	}
	for i := range tools {
		if tools[i].Name == toolName {
			return &tools[i]
		}
	}
	return nil
}

// checkMissingParams validates required parameters and returns an error message
// if any are missing. Returns empty string if all required params are present.
func checkMissingParams(tool *mcp.Tool, toolName string, args map[string]any) string {
	if len(tool.InputSchema.Required) == 0 {
		return ""
	}
	var missing []string
	for _, req := range tool.InputSchema.Required {
		if _, ok := args[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("missing required parameter(s) for %s:", toolName))
	for _, name := range missing {
		desc := ""
		if prop, ok := tool.InputSchema.Properties[name]; ok && prop.Description != "" {
			desc = " - " + prop.Description
		}
		lines = append(lines, fmt.Sprintf("  --%s%s", name, desc))
	}
	lines = append(lines, "")
	lines = append(lines, formatParamHint(tool, toolName))
	return strings.Join(lines, "\n")
}

// formatParamHint builds a usage hint showing all parameters for a tool.
func formatParamHint(tool *mcp.Tool, toolName string) string {
	if len(tool.InputSchema.Properties) == 0 {
		return fmt.Sprintf("Hint: %s may require parameters. Use 'clictl mcp list-tools <server>' to see available parameters.", toolName)
	}

	reqSet := make(map[string]bool, len(tool.InputSchema.Required))
	for _, r := range tool.InputSchema.Required {
		reqSet[r] = true
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Parameters for %s:", toolName))
	for name, prop := range tool.InputSchema.Properties {
		marker := ""
		if reqSet[name] {
			marker = " (required)"
		}
		desc := ""
		if prop.Description != "" {
			desc = " - " + prop.Description
		}
		lines = append(lines, fmt.Sprintf("  --%s%s%s", name, marker, desc))
	}

	var usage []string
	for _, req := range tool.InputSchema.Required {
		usage = append(usage, fmt.Sprintf("--%s <value>", req))
	}
	for name := range tool.InputSchema.Properties {
		if !reqSet[name] {
			usage = append(usage, fmt.Sprintf("[--%s <value>]", name))
		}
	}
	if len(usage) > 0 {
		lines = append(lines, fmt.Sprintf("\nUsage: clictl run <tool> %s %s", toolName, strings.Join(usage, " ")))
	}
	return strings.Join(lines, "\n")
}
