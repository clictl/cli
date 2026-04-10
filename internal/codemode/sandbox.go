// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package codemode provides a sandboxed JavaScript execution environment
// for code mode, where LLM agents write code against typed API client
// bindings instead of making individual tool calls.
//
// The sandbox uses goja (Go-native JS VM) with all dangerous globals blocked.
// API access is provided through typed bindings that route calls through
// the Go HTTP executor with full SSRF protection, auth injection, and
// rate limiting.
package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/clictl/cli/internal/models"
)

// DispatchFunc is the callback signature for executing a tool action.
// The sandbox calls this when generated code invokes an API client method.
type DispatchFunc func(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any) ([]byte, error)

// blockedGlobals are names removed from the JS sandbox.
var blockedGlobals = []string{
	"fetch", "XMLHttpRequest", "WebSocket", "EventSource",
	"Request", "Response", "Headers", "URL", "URLSearchParams",
	"eval", "Function",
	"setTimeout", "setInterval", "setImmediate",
	"clearTimeout", "clearInterval", "clearImmediate",
	"require", "import", "module", "exports",
	"process", "Deno", "Bun", "globalThis",
	"window", "document", "navigator", "location",
	"localStorage", "sessionStorage", "indexedDB",
}

// blockedPatterns prevent reconstruction of blocked functions.
var blockedPatterns = []string{
	"new Function(",
	"new Function (",
	"constructor(",
	".constructor(",
}

// ExecuteResult holds the output of a code mode execution.
type ExecuteResult struct {
	Output string // captured console.log output
	Error  string // error message if execution failed
}

// Execute runs JavaScript code in a sandboxed environment with API client bindings.
// Each spec's actions are exposed as methods on a namespace object (e.g., github.listRepos()).
// console.log() output is captured and returned. Execution has a 30-second timeout.
func Execute(ctx context.Context, code string, specs []*models.ToolSpec, dispatch DispatchFunc) (*ExecuteResult, error) {
	// Validate script
	lower := strings.ToLower(code)
	for _, pattern := range blockedPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return nil, fmt.Errorf("blocked pattern in code: %s", pattern)
		}
	}

	vm := goja.New()

	// Block dangerous globals
	for _, name := range blockedGlobals {
		vm.Set(name, goja.Undefined())
	}

	// Capture console.log output
	var output strings.Builder
	consoleObj := vm.NewObject()
	consoleObj.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = formatJSValue(vm, arg)
		}
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(strings.Join(parts, " "))
		return goja.Undefined()
	})
	consoleObj.Set("error", consoleObj.Get("log"))
	consoleObj.Set("warn", consoleObj.Get("log"))
	vm.Set("console", consoleObj)

	// Register API bindings for each spec
	RegisterBindings(vm, ctx, specs, dispatch)

	// Set timeout
	timer := time.AfterFunc(30*time.Second, func() {
		vm.Interrupt("execution timeout: code exceeded 30 second limit")
	})
	defer timer.Stop()

	// Execute the code
	_, err := vm.RunString(code)
	if err != nil {
		// Check if it's a dispatch error thrown as a JS exception
		if jsErr, ok := err.(*goja.Exception); ok {
			return &ExecuteResult{
				Output: output.String(),
				Error:  jsErr.Value().String(),
			}, nil
		}
		return &ExecuteResult{
			Output: output.String(),
			Error:  err.Error(),
		}, nil
	}

	return &ExecuteResult{Output: output.String()}, nil
}

// ExecuteWithManagement runs JavaScript code with clictl management bindings
// (search, inspect, run, list) instead of spec-specific API bindings.
// Used by the clictl_code management tool for dynamic tool discovery workflows.
func ExecuteWithManagement(ctx context.Context, code string, cliFn CLIFunc) (*ExecuteResult, error) {
	lower := strings.ToLower(code)
	for _, pattern := range blockedPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return nil, fmt.Errorf("blocked pattern in code: %s", pattern)
		}
	}

	vm := goja.New()

	for _, name := range blockedGlobals {
		vm.Set(name, goja.Undefined())
	}

	var output strings.Builder
	consoleObj := vm.NewObject()
	consoleObj.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = formatJSValue(vm, arg)
		}
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(strings.Join(parts, " "))
		return goja.Undefined()
	})
	consoleObj.Set("error", consoleObj.Get("log"))
	consoleObj.Set("warn", consoleObj.Get("log"))
	vm.Set("console", consoleObj)

	// Register management bindings (clictl.search, clictl.run, etc.)
	RegisterManagementBindings(vm, ctx, cliFn)

	timer := time.AfterFunc(30*time.Second, func() {
		vm.Interrupt("execution timeout: code exceeded 30 second limit")
	})
	defer timer.Stop()

	_, err := vm.RunString(code)
	if err != nil {
		if jsErr, ok := err.(*goja.Exception); ok {
			return &ExecuteResult{
				Output: output.String(),
				Error:  jsErr.Value().String(),
			}, nil
		}
		return &ExecuteResult{
			Output: output.String(),
			Error:  err.Error(),
		}, nil
	}

	return &ExecuteResult{Output: output.String()}, nil
}

// formatJSValue converts a goja value to a readable string.
func formatJSValue(vm *goja.Runtime, val goja.Value) string {
	if val == nil || goja.IsUndefined(val) {
		return "undefined"
	}
	if goja.IsNull(val) {
		return "null"
	}

	// Try JSON.stringify for objects/arrays
	exported := val.Export()
	switch exported.(type) {
	case map[string]any, []any:
		b, err := json.Marshal(exported)
		if err == nil {
			return string(b)
		}
	}

	return val.String()
}
