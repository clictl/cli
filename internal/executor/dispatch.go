// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package executor provides protocol-specific action execution for tool specs.
// Currently supports HTTP/HTTPS protocols with URL building, auth injection,
// content-encoding negotiation, RFC 7234 response caching, and JSONPath response transforms.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/httpcache"
	"github.com/clictl/cli/internal/models"
)

// Executor defines the interface for protocol-specific action executors.
type Executor interface {
	Execute(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string) ([]byte, error)
}

// CoerceArgsToStrings converts map[string]any arguments to map[string]string
// for executors that require string-typed parameters (HTTP, composite).
// This is the type coercion shim at the MCP/executor transform boundary.
func CoerceArgsToStrings(args map[string]any) map[string]string {
	result := make(map[string]string, len(args))
	for k, v := range args {
		switch val := v.(type) {
		case string:
			result[k] = val
		case float64:
			if val == float64(int64(val)) {
				result[k] = fmt.Sprintf("%d", int64(val))
			} else {
				result[k] = fmt.Sprintf("%g", val)
			}
		case bool:
			result[k] = fmt.Sprintf("%t", val)
		case nil:
			result[k] = ""
		default:
			// For complex types, marshal to JSON
			b, err := json.Marshal(val)
			if err != nil {
				result[k] = fmt.Sprintf("%v", val)
			} else {
				result[k] = string(b)
			}
		}
	}
	return result
}

// Dispatch selects the appropriate executor based on the spec's protocol
// and runs the action with the given parameters.
func Dispatch(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any) ([]byte, error) {
	return DispatchWithOptions(ctx, spec, action, params, nil, nil)
}

// DispatchWithCache is like Dispatch but accepts an optional response cache.
func DispatchWithCache(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any, cache *httpcache.Cache) ([]byte, error) {
	return DispatchWithOptions(ctx, spec, action, params, cache, nil)
}

// DispatchOptions holds optional configuration for action dispatch.
type DispatchOptions struct {
	Cache          *httpcache.Cache
	Config         *config.Config
	PaginateAll    bool
	SkipTransforms bool // return raw JSON response without transforms
}

// DispatchWithOptions is like Dispatch but accepts an optional response cache and config.
func DispatchWithOptions(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any, cache *httpcache.Cache, cfg *config.Config) ([]byte, error) {
	return DispatchWithFullOptions(ctx, spec, action, params, &DispatchOptions{Cache: cache, Config: cfg})
}

// DispatchWithFullOptions is like Dispatch but accepts a full options struct.
func DispatchWithFullOptions(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any, opts *DispatchOptions) ([]byte, error) {
	if opts == nil {
		opts = &DispatchOptions{}
	}

	// Coerce map[string]any to map[string]string for HTTP/composite executors
	stringParams := CoerceArgsToStrings(params)

	// Composite actions are dispatched to the composite executor
	if action.IsComposite() {
		if opts.Config == nil {
			return nil, fmt.Errorf("composite actions require a config (for cross-tool resolution)")
		}
		return ExecuteComposite(ctx, spec, action, stringParams, opts.Config)
	}

	// Verify required dependencies are installed
	if err := verifyRequirements(spec); err != nil {
		return nil, err
	}

	var ex Executor

	serverType := spec.ServerType()
	switch serverType {
	case "http":
		ex = &HTTPExecutor{Cache: opts.Cache, Config: opts.Config, PaginateAll: opts.PaginateAll, SkipTransforms: opts.SkipTransforms}
	case "websocket":
		ex = &WebSocketExecutor{Config: opts.Config}
	case "stdio":
		// MCP/stdio specs are dispatched via DispatchMCP, not through the standard Executor interface.
		return DispatchMCP(ctx, spec, action.Name, params)
	case "command":
		return nil, fmt.Errorf("command protocol execution is not yet implemented")
	default:
		return nil, fmt.Errorf("server type %q is not yet implemented", serverType)
	}

	return ex.Execute(ctx, spec, action, stringParams)
}

// verifyRequirements checks that all required binaries in the spec's
// server.requires are available on the system PATH.
// For command-protocol specs without explicit requires, it extracts the
// binary name from the first action's run field and checks that.
func verifyRequirements(spec *models.ToolSpec) error {
	// Check server requirements (covers all server types including stdio/mcp)
	if spec.Server != nil && len(spec.Server.Requires) > 0 {
		for _, req := range spec.Server.Requires {
			if req.Name == "" {
				continue
			}
			if _, err := exec.LookPath(req.Name); err != nil {
				msg := fmt.Sprintf("%q requires %q but it is not installed", spec.Name, req.Name)
				if req.URL != "" {
					msg += fmt.Sprintf("\n  Install it: %s", req.URL)
				}
				if req.Check != "" {
					msg += fmt.Sprintf("\n  Verify with: %s", req.Check)
				}
				return fmt.Errorf("%s", msg)
			}
		}
		return nil
	}

	// For command-protocol specs without requires, infer the binary from the
	// first action's run field (e.g., "git status" -> check for "git")
	if spec.IsCommand() && len(spec.Actions) > 0 {
		for _, action := range spec.Actions {
			run := action.Run
			if run == "" {
				continue
			}
			parts := strings.Fields(run)
			if len(parts) == 0 {
				continue
			}
			bin := parts[0]
			if _, err := exec.LookPath(bin); err != nil {
				return fmt.Errorf("%q requires %q but it is not installed", spec.Name, bin)
			}
			return nil // only need to check the first one
		}
	}

	return nil
}
