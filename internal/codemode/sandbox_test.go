// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package codemode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func mockDispatch(results map[string]string) DispatchFunc {
	return func(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any) ([]byte, error) {
		key := spec.Name + "." + action.Name
		if result, ok := results[key]; ok {
			return []byte(result), nil
		}
		return json.Marshal(map[string]any{"action": action.Name, "params": params})
	}
}

func testSpecs() []*models.ToolSpec {
	return []*models.ToolSpec{
		{
			Name:    "test-api",
			Version: "1.0",
			Server: &models.Server{
				Type: "http",
				URL:  "https://api.test.com",
			},
			Actions: []models.Action{
				{
					Name:   "get-item",
					Method: "GET",
					Path:   "/items/{id}",
					Params: []models.Param{
						{Name: "id", Type: "string", Required: true},
					},
				},
				{
					Name:   "list-items",
					Method: "GET",
					Path:   "/items",
					Params: []models.Param{
						{Name: "page", Type: "integer"},
					},
				},
			},
		},
	}
}

func TestExecuteBasicConsoleLog(t *testing.T) {
	specs := testSpecs()
	dispatch := mockDispatch(nil)

	result, err := Execute(context.Background(), `console.log("hello world")`, specs, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "hello world" {
		t.Errorf("got output %q, want %q", result.Output, "hello world")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestExecuteAPIBinding(t *testing.T) {
	specs := testSpecs()
	dispatch := mockDispatch(map[string]string{
		"test-api.get-item": `{"id": "123", "name": "Widget"}`,
	})

	code := `
		var item = testApi.getItem({id: "123"})
		console.log(JSON.stringify(item))
	`
	result, err := Execute(context.Background(), code, specs, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Widget") {
		t.Errorf("expected Widget in output, got: %s", result.Output)
	}
}

func TestExecuteChainedCalls(t *testing.T) {
	specs := testSpecs()
	dispatch := mockDispatch(map[string]string{
		"test-api.list-items": `[{"id": "1"}, {"id": "2"}]`,
		"test-api.get-item":   `{"id": "1", "name": "First"}`,
	})

	code := `
		var items = testApi.listItems({page: 1})
		var first = testApi.getItem({id: items[0].id})
		console.log(first.name)
	`
	result, err := Execute(context.Background(), code, specs, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "First" {
		t.Errorf("got output %q, want %q", result.Output, "First")
	}
}

func TestExecuteMultipleConsoleLog(t *testing.T) {
	result, err := Execute(context.Background(), `
		console.log("line 1")
		console.log("line 2")
		console.log("line 3")
	`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(result.Output, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(lines), result.Output)
	}
}

func TestExecuteBlockedPatterns(t *testing.T) {
	_, err := Execute(context.Background(), `new Function("return 1")()`, nil, nil)
	if err == nil {
		t.Error("expected error for blocked pattern")
	}
}

func TestExecuteRuntimeError(t *testing.T) {
	result, err := Execute(context.Background(), `throw new Error("bad things")`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Error("expected error in result")
	}
	if !strings.Contains(result.Error, "bad things") {
		t.Errorf("error should contain 'bad things', got: %s", result.Error)
	}
}

func TestExecuteNoFetchAccess(t *testing.T) {
	result, err := Execute(context.Background(), `
		try {
			fetch("https://evil.com")
			console.log("should not reach")
		} catch(e) {
			console.log("blocked: " + e.message)
		}
	`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "blocked") && !strings.Contains(result.Output, "not a function") {
		t.Errorf("fetch should be blocked, got: %s", result.Output)
	}
}

func TestExecuteObjectOutput(t *testing.T) {
	result, err := Execute(context.Background(), `
		var obj = {name: "test", count: 42}
		console.log(obj)
	`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "test") || !strings.Contains(result.Output, "42") {
		t.Errorf("expected serialized object, got: %s", result.Output)
	}
}
