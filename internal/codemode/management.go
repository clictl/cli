// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dop251/goja"
)

// CLIFunc executes a clictl CLI command and returns the output.
type CLIFunc func(args ...string) (string, error)

// RegisterManagementBindings adds a `clictl` namespace to the JS VM with
// search, run, and inspect methods. These call through the clictl binary
// and return raw JSON (no transforms applied).
//
// Available in JS as:
//
//	clictl.search("weather")              // search registry
//	clictl.inspect("open-meteo")          // get tool details + actions
//	clictl.run("open-meteo", "current", {q: "London"})  // execute action
//	clictl.list()                         // list all tools
//	clictl.list({category: "developer"})  // list by category
func RegisterManagementBindings(vm *goja.Runtime, ctx context.Context, cliFn CLIFunc) {
	obj := vm.NewObject()

	obj.Set("search", func(call goja.FunctionCall) goja.Value {
		query := call.Arguments[0].String()
		out, err := cliFn("search", query, "-o", "json")
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("clictl.search: %w", err)))
		}
		return parseJSONResult(vm, out)
	})

	obj.Set("inspect", func(call goja.FunctionCall) goja.Value {
		tool := call.Arguments[0].String()
		out, err := cliFn("info", tool, "-o", "json")
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("clictl.inspect: %w", err)))
		}
		return parseJSONResult(vm, out)
	})

	obj.Set("run", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(fmt.Errorf("clictl.run requires at least 2 arguments: tool and action")))
		}
		tool := call.Arguments[0].String()
		action := call.Arguments[1].String()

		args := []string{"run", tool, action, "--json"}

		// Third argument is params object
		if len(call.Arguments) > 2 {
			arg := call.Arguments[2]
			if !goja.IsUndefined(arg) && !goja.IsNull(arg) {
				exported := arg.Export()
				if m, ok := exported.(map[string]any); ok {
					for k, v := range m {
						args = append(args, fmt.Sprintf("--%s", k), fmt.Sprintf("%v", v))
					}
				}
			}
		}

		out, err := cliFn(args...)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("clictl.run %s %s: %w", tool, action, err)))
		}
		return parseJSONResult(vm, out)
	})

	obj.Set("list", func(call goja.FunctionCall) goja.Value {
		args := []string{"list", "-o", "json"}
		if len(call.Arguments) > 0 {
			arg := call.Arguments[0]
			if !goja.IsUndefined(arg) && !goja.IsNull(arg) {
				exported := arg.Export()
				if m, ok := exported.(map[string]any); ok {
					if cat, ok := m["category"].(string); ok {
						args = append(args, "--category", cat)
					}
				}
			}
		}
		out, err := cliFn(args...)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("clictl.list: %w", err)))
		}
		return parseJSONResult(vm, out)
	})

	vm.Set("clictl", obj)
}

// parseJSONResult parses JSON output and returns it as a JS value.
// If the output is not valid JSON, returns it as a string.
func parseJSONResult(vm *goja.Runtime, output string) goja.Value {
	output = strings.TrimSpace(output)
	if output == "" {
		return goja.Null()
	}
	var parsed any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return vm.ToValue(output)
	}
	return vm.ToValue(parsed)
}

// DefaultCLIFunc creates a CLIFunc that executes the clictl binary.
func DefaultCLIFunc(binaryPath string, env []string) CLIFunc {
	return func(args ...string) (string, error) {
		cmd := exec.Command(binaryPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = err.Error()
			}
			return "", fmt.Errorf("%s", strings.TrimSpace(errMsg))
		}
		return stdout.String(), nil
	}
}
