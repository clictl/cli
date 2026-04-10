// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"encoding/json"
	"testing"

	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/models"
)

func TestExtractResultText_EmptyContent(t *testing.T) {
	result := &mcp.CallToolResult{}
	got := extractResultText(result)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractResultText_SingleTextBlock(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: "hello world"},
		},
	}
	got := extractResultText(result)
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestExtractResultText_MultipleTextBlocks(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: "line one"},
			{Type: "text", Text: "line two"},
		},
	}
	got := extractResultText(result)
	if got != "line one\nline two" {
		t.Errorf("expected %q, got %q", "line one\nline two", got)
	}
}

func TestExtractResultText_NonTextBlocks(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.ContentBlock{
			{Type: "image", Text: ""},
			{Type: "text", Text: "only text"},
			{Type: "resource", Text: "ignored"},
		},
	}
	got := extractResultText(result)
	if got != "only text" {
		t.Errorf("expected %q, got %q", "only text", got)
	}
}

func TestExtractResultText_EmptyTextBlock(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: ""},
		},
	}
	got := extractResultText(result)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestApplyInjectTransforms_NilTransforms(t *testing.T) {
	spec := &models.ToolSpec{Name: "test"}
	args := map[string]any{"key": "value"}
	got := applyInjectTransforms(spec, "some-tool", args)
	if got["key"] != "value" {
		t.Errorf("expected args to pass through, got %v", got)
	}
}

func TestApplyInjectTransforms_EmptyTransforms(t *testing.T) {
	spec := &models.ToolSpec{
		Name:       "test",
		Transforms: map[string][]models.TransformStep{},
	}
	args := map[string]any{"key": "value"}
	got := applyInjectTransforms(spec, "some-tool", args)
	if got["key"] != "value" {
		t.Errorf("expected args to pass through, got %v", got)
	}
}

func TestApplyInjectTransforms_NoTransformsForTool(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"other-tool": {{
				Type:   "json",
				Inject: map[string]any{"foo": "bar"},
			}},
		},
	}
	args := map[string]any{"key": "value"}
	got := applyInjectTransforms(spec, "my-tool", args)
	if got["key"] != "value" {
		t.Errorf("expected args to pass through, got %v", got)
	}
}

func TestApplyInjectTransforms_MergeDefaults(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {{
				Type: "json",
				Inject: map[string]any{
					"default_arg": "default_val",
					"existing":    "should_not_override",
				},
			}},
		},
	}
	args := map[string]any{"existing": "user_val"}
	got := applyInjectTransforms(spec, "my-tool", args)
	if got["existing"] != "user_val" {
		t.Errorf("inject should not override existing arg, got %v", got["existing"])
	}
	if got["default_arg"] != "default_val" {
		t.Errorf("expected default_arg to be injected, got %v", got["default_arg"])
	}
}

func TestApplyInjectTransforms_SkipsNonInjectSteps(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {
				{Type: "fallback", Value: "some fallback"},
				{Type: "json", Inject: map[string]any{"added": "yes"}},
			},
		},
	}
	args := map[string]any{}
	got := applyInjectTransforms(spec, "my-tool", args)
	if got["added"] != "yes" {
		t.Errorf("expected injected arg, got %v", got)
	}
}

func TestGetFallbackValue_NilConfig(t *testing.T) {
	spec := &models.ToolSpec{Name: "test"}
	got := getFallbackValue(spec, "tool")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGetFallbackValue_NoFallbackDefined(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {{
				Type:   "json",
				Inject: map[string]any{"foo": "bar"},
			}},
		},
	}
	got := getFallbackValue(spec, "my-tool")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGetFallbackValue_FallbackDefined(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {{
				Type:  "fallback",
				Value: "default response",
			}},
		},
	}
	got := getFallbackValue(spec, "my-tool")
	if got != "default response" {
		t.Errorf("expected %q, got %q", "default response", got)
	}
}

func TestGetFallbackValue_NoMatchingTool(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"other-tool": {{
				Type:  "fallback",
				Value: "nope",
			}},
		},
	}
	got := getFallbackValue(spec, "my-tool")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestApplyToolTransforms_NilConfig(t *testing.T) {
	spec := &models.ToolSpec{Name: "test"}
	got, err := applyToolTransforms(spec, "tool", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestApplyToolTransforms_NoTransforms(t *testing.T) {
	spec := &models.ToolSpec{
		Name:       "test",
		Transforms: map[string][]models.TransformStep{},
	}
	got, err := applyToolTransforms(spec, "tool", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestApplyToolTransforms_OnlyInjectAndFallbackSkipped(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {
				{Type: "json", Inject: map[string]any{"foo": "bar"}},
				{Type: "fallback", Value: "default"},
			},
		},
	}
	got, err := applyToolTransforms(spec, "my-tool", "original")
	if err != nil {
		t.Fatal(err)
	}
	if got != "original" {
		t.Errorf("expected %q, got %q", "original", got)
	}
}

func TestApplyToolTransforms_TruncateJSON(t *testing.T) {
	// Build a JSON array with 10 items
	items := make([]map[string]string, 10)
	for i := range items {
		items[i] = map[string]string{"id": "item"}
	}
	data, _ := json.Marshal(items)

	spec := &models.ToolSpec{
		Name: "test",
		Transforms: map[string][]models.TransformStep{
			"my-tool": {
				{Type: "truncate", MaxItems: 3},
			},
		},
	}
	got, err := applyToolTransforms(spec, "my-tool", string(data))
	if err != nil {
		t.Fatal(err)
	}

	// Parse result and verify truncation
	var result []any
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatalf("expected valid JSON array, got %q: %v", got, err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 items after truncation, got %d", len(result))
	}
}

func TestTransformStepsToRaw(t *testing.T) {
	steps := []models.TransformStep{
		{Type: "truncate", MaxItems: 5},
		{Type: "json", Extract: "$.data"},
	}
	got := transformStepsToRaw(steps)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	// Truncate uses nested format: {"truncate": {"max_items": 5}}
	first, ok := got[0].(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any")
	}
	if first["truncate"] == nil {
		t.Error("expected truncate key in nested format")
	}
	// JSON uses flat format: {"extract": "$.data"}
	second, ok := got[1].(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any")
	}
	if second["extract"] != "$.data" {
		t.Errorf("expected extract=$.data, got %v", second["extract"])
	}
}
