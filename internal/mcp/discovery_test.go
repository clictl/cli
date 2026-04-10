// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"testing"

	"github.com/clictl/cli/internal/models"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"exact match", "query_users", true},
		{"exact mismatch", "query_posts", false},
		{"wildcard all", "*", true},
		{"prefix glob match", "query_*", true},
		{"prefix glob mismatch", "drop_*", false},
		{"prefix glob partial", "query_u*", true},
		{"prefix glob no match", "query_z*", false},
		{"empty pattern", "", false},
		{"empty name against wildcard", "*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "query_users"
			if tt.name == "empty name against wildcard" {
				name = ""
			}
			got := matchPattern(name, tt.pattern)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", name, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		patterns []string
		want     bool
	}{
		{"empty patterns", "query_users", nil, false},
		{"exact match in list", "query_users", []string{"query_users", "drop_table"}, true},
		{"glob match in list", "query_users", []string{"query_*"}, true},
		{"no match in list", "query_users", []string{"drop_*", "create_*"}, false},
		{"wildcard in list", "anything", []string{"*"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAny(tt.toolName, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAny(%q, %v) = %v, want %v", tt.toolName, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMergeActions_StaticWinsOnCollision(t *testing.T) {
	static := []models.Action{
		{
			Name:        "query_users",
			Description: "Static description with more detail",
			Params: []models.Param{
				{Name: "filter", Type: "string", Required: true, Description: "Filter expression"},
			},
		},
	}

	discovered := []DiscoveredTool{
		{
			Name:        "query_users",
			Description: "Discovered description",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filter": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "query_posts",
			Description: "Query blog posts",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "number", "description": "Max results"},
				},
				"required": []string{"limit"},
			},
		},
	}

	merged := MergeActions(static, discovered, nil, nil)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged actions, got %d", len(merged))
	}

	// First action should be the static one (static wins)
	if merged[0].Description != "Static description with more detail" {
		t.Errorf("expected static description to win, got %q", merged[0].Description)
	}

	// Second action should be the discovered one
	if merged[1].Name != "query_posts" {
		t.Errorf("expected query_posts as second action, got %q", merged[1].Name)
	}
	if merged[1].Description != "Query blog posts" {
		t.Errorf("expected discovered description, got %q", merged[1].Description)
	}

	// Check that params were converted from discovered tool
	if len(merged[1].Params) != 1 {
		t.Fatalf("expected 1 param on query_posts, got %d", len(merged[1].Params))
	}
	if merged[1].Params[0].Name != "limit" {
		t.Errorf("expected param name 'limit', got %q", merged[1].Params[0].Name)
	}
	if !merged[1].Params[0].Required {
		t.Error("expected 'limit' param to be required")
	}
}

func TestMergeActions_AllowFilter(t *testing.T) {
	static := []models.Action{}
	discovered := []DiscoveredTool{
		{Name: "query_users", Description: "Query users"},
		{Name: "query_posts", Description: "Query posts"},
		{Name: "drop_table", Description: "Drop a table"},
	}

	merged := MergeActions(static, discovered, []string{"query_*"}, nil)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged actions (allow query_*), got %d", len(merged))
	}
	for _, a := range merged {
		if a.Name == "drop_table" {
			t.Error("drop_table should have been filtered out by allow list")
		}
	}
}

func TestMergeActions_DenyFilter(t *testing.T) {
	static := []models.Action{}
	discovered := []DiscoveredTool{
		{Name: "query_users", Description: "Query users"},
		{Name: "drop_table", Description: "Drop a table"},
		{Name: "drop_database", Description: "Drop database"},
	}

	merged := MergeActions(static, discovered, nil, []string{"drop_*"})

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged action (deny drop_*), got %d", len(merged))
	}
	if merged[0].Name != "query_users" {
		t.Errorf("expected query_users, got %q", merged[0].Name)
	}
}

func TestMergeActions_AllowAndDeny(t *testing.T) {
	static := []models.Action{}
	discovered := []DiscoveredTool{
		{Name: "query_users", Description: "Query users"},
		{Name: "query_sensitive", Description: "Query sensitive data"},
		{Name: "drop_table", Description: "Drop a table"},
	}

	// Allow query_* but deny query_sensitive
	merged := MergeActions(static, discovered, []string{"query_*"}, []string{"query_sensitive"})

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged action, got %d", len(merged))
	}
	if merged[0].Name != "query_users" {
		t.Errorf("expected query_users, got %q", merged[0].Name)
	}
}

func TestMergeActions_EmptyDiscovered(t *testing.T) {
	static := []models.Action{
		{Name: "action1", Description: "Static action"},
	}

	merged := MergeActions(static, nil, nil, nil)

	if len(merged) != 1 {
		t.Fatalf("expected 1 action, got %d", len(merged))
	}
	if merged[0].Name != "action1" {
		t.Errorf("expected action1, got %q", merged[0].Name)
	}
}

func TestMergeActions_EmptyStatic(t *testing.T) {
	discovered := []DiscoveredTool{
		{Name: "tool_a", Description: "Tool A"},
		{Name: "tool_b", Description: "Tool B"},
	}

	merged := MergeActions(nil, discovered, nil, nil)

	if len(merged) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(merged))
	}
}

func TestDiscoveredToAction(t *testing.T) {
	dt := DiscoveredTool{
		Name:        "search",
		Description: "Search for items",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query",
				},
				"limit": map[string]any{
					"type":    "number",
					"default": "10",
				},
			},
			"required": []string{"query"},
		},
	}

	action := discoveredToAction(dt)

	if action.Name != "search" {
		t.Errorf("expected name 'search', got %q", action.Name)
	}
	if action.Description != "Search for items" {
		t.Errorf("expected description 'Search for items', got %q", action.Description)
	}
	if len(action.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(action.Params))
	}

	// Find the query param
	var queryParam, limitParam *models.Param
	for i := range action.Params {
		switch action.Params[i].Name {
		case "query":
			queryParam = &action.Params[i]
		case "limit":
			limitParam = &action.Params[i]
		}
	}

	if queryParam == nil {
		t.Fatal("missing query param")
	}
	if !queryParam.Required {
		t.Error("query param should be required")
	}
	if queryParam.Type != "string" {
		t.Errorf("query type: got %q, want string", queryParam.Type)
	}

	if limitParam == nil {
		t.Fatal("missing limit param")
	}
	if limitParam.Required {
		t.Error("limit param should not be required")
	}
	if limitParam.Default != "10" {
		t.Errorf("limit default: got %q, want '10'", limitParam.Default)
	}
}

func TestConvertTools(t *testing.T) {
	tools := []Tool{
		{
			Name:        "greet",
			Description: "Say hello",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {Type: "string", Description: "Who to greet"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "count",
			Description: "Count items",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
			},
		},
	}

	discovered := convertTools(tools)

	if len(discovered) != 2 {
		t.Fatalf("expected 2 discovered tools, got %d", len(discovered))
	}

	if discovered[0].Name != "greet" {
		t.Errorf("expected greet, got %q", discovered[0].Name)
	}
	if discovered[0].Description != "Say hello" {
		t.Errorf("expected 'Say hello', got %q", discovered[0].Description)
	}

	props, ok := discovered[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("expected name property")
	}
	if nameProp["type"] != "string" {
		t.Errorf("expected name type string, got %v", nameProp["type"])
	}

	required, ok := discovered[0].InputSchema["required"].([]string)
	if !ok {
		t.Fatal("expected required in schema")
	}
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("expected required [name], got %v", required)
	}
}

func TestIsDeniedWithGlob(t *testing.T) {
	// After updating isDenied to use matchesAny, verify glob support
	if !isDenied([]string{"drop_*"}, "drop_table") {
		t.Error("expected drop_table to be denied by drop_* pattern")
	}
	if isDenied([]string{"drop_*"}, "query_users") {
		t.Error("expected query_users to not be denied by drop_* pattern")
	}
	if !isDenied([]string{"*"}, "anything") {
		t.Error("expected * to deny everything")
	}
}

func TestIsAllowedWithGlob(t *testing.T) {
	if !isAllowed([]string{"query_*"}, "query_users") {
		t.Error("expected query_users to be allowed by query_* pattern")
	}
	if isAllowed([]string{"query_*"}, "drop_table") {
		t.Error("expected drop_table to not be allowed by query_* pattern")
	}
}
