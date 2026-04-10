// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package codemode

import (
	"context"
	"encoding/json"

	"github.com/dop251/goja"

	"github.com/clictl/cli/internal/codegen"
	"github.com/clictl/cli/internal/models"
)

// RegisterBindings adds API client objects to the JS VM for each spec.
// Each spec becomes a namespace object with camelCase method names.
//
// Example: spec "github" with action "list-repos" becomes:
//
//	github.listRepos({username: "octocat"})
//
// Methods are synchronous from the JS perspective (goja doesn't support
// async/await). Each method call dispatches through the Go executor and
// returns the parsed JSON result.
func RegisterBindings(vm *goja.Runtime, ctx context.Context, specs []*models.ToolSpec, dispatch DispatchFunc) {
	for _, spec := range specs {
		spec := spec
		obj := vm.NewObject()

		for i := range spec.Actions {
			action := &spec.Actions[i]
			if action.IsComposite() {
				continue
			}

			funcName := codegen.ToCamelCase(action.Name)
			obj.Set(funcName, makeActionBinding(vm, ctx, spec, action, dispatch))
		}

		// Register under camelCase spec name
		vm.Set(codegen.ToCamelCase(spec.Name), obj)
	}
}

// makeActionBinding creates a goja function that dispatches an action call.
func makeActionBinding(vm *goja.Runtime, ctx context.Context, spec *models.ToolSpec, action *models.Action, dispatch DispatchFunc) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// Extract params from the first argument (object)
		params := make(map[string]any)
		if len(call.Arguments) > 0 {
			arg := call.Arguments[0]
			if !goja.IsUndefined(arg) && !goja.IsNull(arg) {
				exported := arg.Export()
				if m, ok := exported.(map[string]any); ok {
					params = m
				}
			}
		}

		// Dispatch through the Go executor
		result, err := dispatch(ctx, spec, action, params)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		// Parse JSON result and return as JS value
		var parsed any
		if err := json.Unmarshal(result, &parsed); err != nil {
			// Return as string if not valid JSON
			return vm.ToValue(string(result))
		}
		return vm.ToValue(parsed)
	}
}
