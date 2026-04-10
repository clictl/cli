// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// applyPrefix
// ---------------------------------------------------------------------------

func TestApplyPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		input  any
		check  func(t *testing.T, result any)
	}{
		{
			name:   "string data",
			prefix: "ns_",
			input:  "hello",
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != "ns_hello" {
					t.Errorf("expected 'ns_hello', got %v", result)
				}
			},
		},
		{
			name:   "object with name field",
			prefix: "gh_",
			input:  map[string]any{"name": "list_repos", "desc": "lists repos"},
			check: func(t *testing.T, result any) {
				t.Helper()
				obj := result.(map[string]any)
				if obj["name"] != "gh_list_repos" {
					t.Errorf("expected 'gh_list_repos', got %v", obj["name"])
				}
				if obj["desc"] != "lists repos" {
					t.Errorf("expected desc preserved, got %v", obj["desc"])
				}
			},
		},
		{
			name:   "array of objects with name field",
			prefix: "db_",
			input: []any{
				map[string]any{"name": "query"},
				map[string]any{"name": "insert"},
			},
			check: func(t *testing.T, result any) {
				t.Helper()
				arr := result.([]any)
				if len(arr) != 2 {
					t.Fatalf("expected 2 items, got %d", len(arr))
				}
				first := arr[0].(map[string]any)
				second := arr[1].(map[string]any)
				if first["name"] != "db_query" {
					t.Errorf("expected 'db_query', got %v", first["name"])
				}
				if second["name"] != "db_insert" {
					t.Errorf("expected 'db_insert', got %v", second["name"])
				}
			},
		},
		{
			name:   "object without name field unchanged",
			prefix: "x_",
			input:  map[string]any{"id": "123"},
			check: func(t *testing.T, result any) {
				t.Helper()
				obj := result.(map[string]any)
				if obj["id"] != "123" {
					t.Errorf("expected id preserved, got %v", obj["id"])
				}
				if _, ok := obj["name"]; ok {
					t.Error("name field should not have been added")
				}
			},
		},
		{
			name:   "non-applicable type passthrough",
			prefix: "x_",
			input:  42.0,
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != 42.0 {
					t.Errorf("expected 42.0, got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := applyPrefix(tt.prefix, tt.input)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, result)
		})
	}
}

// ---------------------------------------------------------------------------
// applyOnly
// ---------------------------------------------------------------------------

func TestApplyOnly(t *testing.T) {
	tools := []any{
		map[string]any{"name": "read_file", "desc": "read"},
		map[string]any{"name": "write_file", "desc": "write"},
		map[string]any{"name": "delete_file", "desc": "delete"},
	}

	t.Run("filters array by name", func(t *testing.T) {
		result, err := applyOnly([]string{"read_file", "write_file"}, tools)
		if err != nil {
			t.Fatal(err)
		}
		arr := result.([]any)
		if len(arr) != 2 {
			t.Fatalf("expected 2 items, got %d", len(arr))
		}
		names := make(map[string]bool)
		for _, item := range arr {
			obj := item.(map[string]any)
			names[obj["name"].(string)] = true
		}
		if !names["read_file"] || !names["write_file"] {
			t.Errorf("expected read_file and write_file, got %v", names)
		}
		if names["delete_file"] {
			t.Error("delete_file should have been filtered out")
		}
	})

	t.Run("non-array passthrough", func(t *testing.T) {
		input := "just a string"
		result, err := applyOnly([]string{"anything"}, input)
		if err != nil {
			t.Fatal(err)
		}
		if result != input {
			t.Errorf("expected passthrough, got %v", result)
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		result, err := applyOnly([]string{"nonexistent"}, tools)
		if err != nil {
			t.Fatal(err)
		}
		arr := result.([]any)
		if len(arr) != 0 {
			t.Errorf("expected empty result, got %d items", len(arr))
		}
	})
}

// ---------------------------------------------------------------------------
// applyInject
// ---------------------------------------------------------------------------

func TestApplyInject(t *testing.T) {
	defaults := map[string]any{"format": "json", "verbose": true}

	t.Run("object merge defaults not overriding existing", func(t *testing.T) {
		input := map[string]any{"format": "xml", "query": "test"}
		result, err := applyInject(defaults, input)
		if err != nil {
			t.Fatal(err)
		}
		obj := result.(map[string]any)
		// Existing key must not be overwritten
		if obj["format"] != "xml" {
			t.Errorf("expected 'xml' (existing), got %v", obj["format"])
		}
		// Default injected
		if obj["verbose"] != true {
			t.Errorf("expected verbose=true injected, got %v", obj["verbose"])
		}
		// Original key preserved
		if obj["query"] != "test" {
			t.Errorf("expected query='test', got %v", obj["query"])
		}
	})

	t.Run("array of objects", func(t *testing.T) {
		input := []any{
			map[string]any{"name": "a"},
			map[string]any{"name": "b", "format": "csv"},
		}
		result, err := applyInject(defaults, input)
		if err != nil {
			t.Fatal(err)
		}
		arr := result.([]any)
		if len(arr) != 2 {
			t.Fatalf("expected 2 items, got %d", len(arr))
		}
		first := arr[0].(map[string]any)
		if first["format"] != "json" {
			t.Errorf("expected default format='json', got %v", first["format"])
		}
		if first["verbose"] != true {
			t.Errorf("expected verbose=true injected, got %v", first["verbose"])
		}
		second := arr[1].(map[string]any)
		if second["format"] != "csv" {
			t.Errorf("expected existing format='csv', got %v", second["format"])
		}
	})

	t.Run("non-applicable type passthrough", func(t *testing.T) {
		result, err := applyInject(defaults, "a string")
		if err != nil {
			t.Fatal(err)
		}
		if result != "a string" {
			t.Errorf("expected passthrough, got %v", result)
		}
	})
}

// ---------------------------------------------------------------------------
// applyRedact
// ---------------------------------------------------------------------------

func TestApplyRedact(t *testing.T) {
	t.Run("literal pattern", func(t *testing.T) {
		patterns := []RedactPattern{
			{Match: "secret-key-123", Replace: "[REDACTED]"},
		}
		result, err := applyRedact(patterns, "token is secret-key-123 here")
		if err != nil {
			t.Fatal(err)
		}
		s := result.(string)
		if s != "token is [REDACTED] here" {
			t.Errorf("unexpected result: %s", s)
		}
	})

	t.Run("regex pattern", func(t *testing.T) {
		patterns := []RedactPattern{
			{Match: `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, Replace: "[EMAIL]", Type: "regex"},
		}
		result, err := applyRedact(patterns, "contact user@example.com for info")
		if err != nil {
			t.Fatal(err)
		}
		s := result.(string)
		if s != "contact [EMAIL] for info" {
			t.Errorf("unexpected result: %s", s)
		}
	})

	t.Run("invalid regex returns error", func(t *testing.T) {
		patterns := []RedactPattern{
			{Match: `[invalid`, Replace: "", Type: "regex"},
		}
		_, err := applyRedact(patterns, "test")
		if err == nil {
			t.Fatal("expected error for invalid regex")
		}
		if !strings.Contains(err.Error(), "invalid redact regex") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("non-string data JSON serialized", func(t *testing.T) {
		patterns := []RedactPattern{
			{Match: "secret", Replace: "***"},
		}
		input := map[string]any{"key": "secret", "other": "public"}
		result, err := applyRedact(patterns, input)
		if err != nil {
			t.Fatal(err)
		}
		// Result should be parsed back to a map since input was non-string
		obj, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T: %v", result, result)
		}
		if obj["key"] != "***" {
			t.Errorf("expected '***', got %v", obj["key"])
		}
		if obj["other"] != "public" {
			t.Errorf("expected 'public', got %v", obj["other"])
		}
	})
}

// ---------------------------------------------------------------------------
// applyCost
// ---------------------------------------------------------------------------

func TestApplyCost(t *testing.T) {
	t.Run("within budget no truncation", func(t *testing.T) {
		cfg := &CostConfig{MaxTokens: 100} // 400 chars budget
		input := "short string"
		result, err := applyCost(cfg, input)
		if err != nil {
			t.Fatal(err)
		}
		if result != input {
			t.Errorf("expected no truncation, got %v", result)
		}
	})

	t.Run("exceeds budget truncated", func(t *testing.T) {
		cfg := &CostConfig{MaxTokens: 2} // 8 chars budget
		input := "this is a long string that exceeds the budget"
		result, err := applyCost(cfg, input)
		if err != nil {
			t.Fatal(err)
		}
		s := result.(string)
		if !strings.HasPrefix(s, "this is ") {
			t.Errorf("expected truncated prefix, got %q", s)
		}
		if !strings.Contains(s, "truncated") {
			t.Errorf("expected truncation notice, got %q", s)
		}
		if !strings.Contains(s, "tokens removed") {
			t.Errorf("expected token count in notice, got %q", s)
		}
	})

	t.Run("non-string data serialized", func(t *testing.T) {
		cfg := &CostConfig{MaxTokens: 1000}
		input := map[string]any{"key": "value"}
		result, err := applyCost(cfg, input)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := result.(string)
		if !ok {
			t.Fatalf("expected string, got %T", result)
		}
		if !strings.Contains(s, "key") || !strings.Contains(s, "value") {
			t.Errorf("expected serialized content, got %q", s)
		}
	})

	t.Run("zero max_tokens passthrough", func(t *testing.T) {
		cfg := &CostConfig{MaxTokens: 0}
		input := "anything"
		result, err := applyCost(cfg, input)
		if err != nil {
			t.Fatal(err)
		}
		if result != input {
			t.Errorf("expected passthrough for zero budget, got %v", result)
		}
	})
}

// ---------------------------------------------------------------------------
// ParseSteps - new step types via mapToStep
// ---------------------------------------------------------------------------

func TestParseStepsNewTypes(t *testing.T) {
	t.Run("prefix", func(t *testing.T) {
		raw := map[string]any{"prefix": "gh_"}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if pipeline[0].Prefix != "gh_" {
			t.Errorf("expected prefix 'gh_', got %q", pipeline[0].Prefix)
		}
	})

	t.Run("only", func(t *testing.T) {
		raw := map[string]any{
			"only": []any{"read_file", "write_file"},
		}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if len(pipeline[0].Only) != 2 {
			t.Fatalf("expected 2 only entries, got %d", len(pipeline[0].Only))
		}
		if pipeline[0].Only[0] != "read_file" || pipeline[0].Only[1] != "write_file" {
			t.Errorf("unexpected only values: %v", pipeline[0].Only)
		}
	})

	t.Run("inject", func(t *testing.T) {
		raw := map[string]any{
			"inject": map[string]any{"format": "json"},
		}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if pipeline[0].Inject["format"] != "json" {
			t.Errorf("expected inject format='json', got %v", pipeline[0].Inject["format"])
		}
	})

	t.Run("redact array format", func(t *testing.T) {
		raw := map[string]any{
			"redact": []any{
				map[string]any{"match": "secret", "replace": "***", "type": "literal"},
				map[string]any{"match": `\d+`, "replace": "[NUM]", "type": "regex"},
			},
		}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if len(pipeline[0].Redact) != 2 {
			t.Fatalf("expected 2 redact patterns, got %d", len(pipeline[0].Redact))
		}
		if pipeline[0].Redact[0].Match != "secret" {
			t.Errorf("expected match='secret', got %q", pipeline[0].Redact[0].Match)
		}
		if pipeline[0].Redact[1].Type != "regex" {
			t.Errorf("expected type='regex', got %q", pipeline[0].Redact[1].Type)
		}
	})

	t.Run("redact simple key-value format", func(t *testing.T) {
		raw := map[string]any{
			"redact": map[string]any{
				"password123": "[REDACTED]",
			},
		}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if len(pipeline[0].Redact) != 1 {
			t.Fatalf("expected 1 redact pattern, got %d", len(pipeline[0].Redact))
		}
		if pipeline[0].Redact[0].Match != "password123" {
			t.Errorf("expected match='password123', got %q", pipeline[0].Redact[0].Match)
		}
		if pipeline[0].Redact[0].Replace != "[REDACTED]" {
			t.Errorf("expected replace='[REDACTED]', got %q", pipeline[0].Redact[0].Replace)
		}
	})

	t.Run("cost", func(t *testing.T) {
		raw := map[string]any{
			"cost": map[string]any{"max_tokens": float64(500)},
		}
		pipeline, err := ParseSteps(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pipeline) != 1 {
			t.Fatalf("expected 1 step, got %d", len(pipeline))
		}
		if pipeline[0].Cost == nil {
			t.Fatal("expected cost config")
		}
		if pipeline[0].Cost.MaxTokens != 500 {
			t.Errorf("expected max_tokens=500, got %d", pipeline[0].Cost.MaxTokens)
		}
	})
}
