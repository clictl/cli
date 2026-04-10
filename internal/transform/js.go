// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// blockedGlobals are names that must not be available in the JS sandbox.
// These prevent network access, dynamic code execution, and runtime escape.
var blockedGlobals = []string{
	// Network (could exfiltrate data or credentials)
	"fetch", "XMLHttpRequest", "WebSocket", "EventSource",
	"Request", "Response", "Headers", "URL", "URLSearchParams",

	// Dynamic code execution (sandbox escape)
	"eval", "Function",

	// Timers (we enforce our own timeout)
	"setTimeout", "setInterval", "setImmediate",
	"clearTimeout", "clearInterval", "clearImmediate",

	// Module loading
	"require", "import", "module", "exports",

	// Runtime access (shouldn't exist in goja, but block the names)
	"process", "Deno", "Bun", "globalThis",
	"window", "document", "navigator", "location",
	"localStorage", "sessionStorage", "indexedDB",

	// I/O (not in goja, but prevent confusion)
	"console",
}

// blockedPatterns are substrings that must not appear in user scripts.
// These catch attempts to reconstruct blocked functions.
var blockedPatterns = []string{
	"new Function(",
	"new Function (",
	"constructor(",
	".constructor(",
}

// validateScript checks a JS script for blocked patterns before execution.
func validateScript(script string) error {
	lower := strings.ToLower(script)
	for _, pattern := range blockedPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return fmt.Errorf("blocked pattern in script: %s", pattern)
		}
	}
	return nil
}

// sandboxVM configures a goja VM with blocked globals removed.
func sandboxVM() *goja.Runtime {
	vm := goja.New()

	// Remove dangerous globals by setting them to undefined
	for _, name := range blockedGlobals {
		vm.Set(name, goja.Undefined())
	}

	return vm
}

// applyJS runs a JavaScript transform function on the data.
//
// The script must define a function called `transform(data)` that returns
// the transformed result. The JS runtime is sandboxed:
//   - No network access (fetch, XMLHttpRequest, WebSocket blocked)
//   - No dynamic code execution (eval, Function constructor blocked)
//   - No timers (setTimeout, setInterval blocked)
//   - No module loading (require, import blocked)
//   - No I/O or runtime access
//   - 5-second execution timeout
func applyJS(script string, data any) (any, error) {
	// Pre-execution validation
	if err := validateScript(script); err != nil {
		return nil, err
	}

	vm := sandboxVM()

	// Set a 5-second timeout
	timer := time.AfterFunc(5*time.Second, func() {
		vm.Interrupt("execution timeout: script exceeded 5 second limit")
	})
	defer timer.Stop()

	// Convert data to JSON and parse in JS
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshaling data for JS: %w", err)
	}

	// Set up the data as a read-only input
	vm.Set("__raw_data__", string(jsonBytes))

	// Run the user script + call transform()
	fullScript := fmt.Sprintf(`
		var __data__ = JSON.parse(__raw_data__);
		delete __raw_data__;
		%s
		if (typeof transform !== 'function') {
			throw new Error('Script must define a transform(data) function');
		}
		var __result__ = transform(__data__);
		JSON.stringify(__result__);
	`, script)

	val, err := vm.RunString(fullScript)
	if err != nil {
		return nil, fmt.Errorf("JS execution failed: %w", err)
	}

	// Parse the result back from JSON
	resultStr := val.String()
	var result any
	if err := json.Unmarshal([]byte(resultStr), &result); err != nil {
		// If it's not valid JSON, return as string
		return resultStr, nil
	}
	return result, nil
}
