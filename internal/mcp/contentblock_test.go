// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"encoding/json"
	"testing"
)

func TestContentBlockAllTypes(t *testing.T) {
	tests := []struct {
		name  string
		block ContentBlock
		// checkField is the primary non-Type field that should be set
		checkField string
		checkValue string
	}{
		{
			name:       "text content",
			block:      ContentBlock{Type: "text", Text: "Hello, world!"},
			checkField: "text",
			checkValue: "Hello, world!",
		},
		{
			name: "image content",
			block: ContentBlock{
				Type:     "image",
				Data:     "iVBORw0KGgo=",
				MimeType: "image/png",
			},
			checkField: "data",
			checkValue: "iVBORw0KGgo=",
		},
		{
			name: "audio content",
			block: ContentBlock{
				Type:     "audio",
				Data:     "UklGRg==",
				MimeType: "audio/wav",
			},
			checkField: "mimeType",
			checkValue: "audio/wav",
		},
		{
			name: "resource_link content",
			block: ContentBlock{
				Type: "resource_link",
				URI:  "file:///project/main.go",
			},
			checkField: "uri",
			checkValue: "file:///project/main.go",
		},
		{
			name: "embedded_resource content",
			block: ContentBlock{
				Type: "embedded_resource",
				Resource: &ResourceContent{
					URI:      "file:///data.json",
					MimeType: "application/json",
					Text:     `{"key":"value"}`,
				},
			},
			checkField: "type",
			checkValue: "embedded_resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal raw: %v", err)
			}

			gotType, ok := raw["type"]
			if !ok {
				t.Fatal("missing 'type' in serialized output")
			}
			if gotType != tt.block.Type {
				t.Errorf("type: got %q, want %q", gotType, tt.block.Type)
			}

			if tt.checkField != "type" {
				val, ok := raw[tt.checkField]
				if !ok {
					t.Errorf("missing field %q in output", tt.checkField)
				} else if strVal, isStr := val.(string); isStr && strVal != tt.checkValue {
					t.Errorf("field %q: got %q, want %q", tt.checkField, strVal, tt.checkValue)
				}
			}

			// Roundtrip
			var roundtrip ContentBlock
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Roundtrip: %v", err)
			}
			if roundtrip.Type != tt.block.Type {
				t.Errorf("roundtrip Type: got %q, want %q", roundtrip.Type, tt.block.Type)
			}
		})
	}
}

func TestContentBlockEmbeddedResourceRoundtrip(t *testing.T) {
	original := ContentBlock{
		Type: "embedded_resource",
		Resource: &ResourceContent{
			URI:      "file:///notes.md",
			MimeType: "text/markdown",
			Text:     "# Notes\n\nSome content here.",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundtrip ContentBlock
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if roundtrip.Resource == nil {
		t.Fatal("Resource is nil after roundtrip")
	}
	if roundtrip.Resource.URI != "file:///notes.md" {
		t.Errorf("Resource.URI: got %q", roundtrip.Resource.URI)
	}
	if roundtrip.Resource.Text != "# Notes\n\nSome content here." {
		t.Errorf("Resource.Text: got %q", roundtrip.Resource.Text)
	}
}

func TestCallToolParamsAnyArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "string arguments",
			args: map[string]any{"query": "hello", "lang": "en"},
		},
		{
			name: "numeric argument",
			args: map[string]any{"count": float64(42)},
		},
		{
			name: "boolean argument",
			args: map[string]any{"verbose": true, "dry_run": false},
		},
		{
			name: "mixed types",
			args: map[string]any{
				"name":    "test",
				"count":   float64(5),
				"enabled": true,
			},
		},
		{
			name: "nil arguments",
			args: nil,
		},
		{
			name: "empty arguments",
			args: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := CallToolParams{
				Name:      "test_tool",
				Arguments: tt.args,
			}

			data, err := json.Marshal(params)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var roundtrip CallToolParams
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if roundtrip.Name != "test_tool" {
				t.Errorf("Name: got %q, want test_tool", roundtrip.Name)
			}

			if tt.args == nil {
				if roundtrip.Arguments != nil {
					t.Errorf("expected nil args, got %v", roundtrip.Arguments)
				}
				return
			}

			if len(roundtrip.Arguments) != len(tt.args) {
				t.Errorf("Arguments length: got %d, want %d", len(roundtrip.Arguments), len(tt.args))
			}

			for k, want := range tt.args {
				got, ok := roundtrip.Arguments[k]
				if !ok {
					t.Errorf("missing argument %q", k)
					continue
				}
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(got)
				if string(wantJSON) != string(gotJSON) {
					t.Errorf("argument %q: got %s, want %s", k, gotJSON, wantJSON)
				}
			}
		})
	}
}

func TestCallToolResultIsError(t *testing.T) {
	tests := []struct {
		name    string
		result  CallToolResult
		wantErr bool
	}{
		{
			name: "success result",
			result: CallToolResult{
				Content: []ContentBlock{{Type: "text", Text: "ok"}},
				IsError: false,
			},
			wantErr: false,
		},
		{
			name: "error result",
			result: CallToolResult{
				Content: []ContentBlock{{Type: "text", Text: "something went wrong"}},
				IsError: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var roundtrip CallToolResult
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if roundtrip.IsError != tt.wantErr {
				t.Errorf("IsError: got %v, want %v", roundtrip.IsError, tt.wantErr)
			}

			// When IsError is false, omitempty should exclude it from JSON
			var raw map[string]any
			json.Unmarshal(data, &raw)
			_, hasIsError := raw["isError"]
			if !tt.wantErr && hasIsError {
				t.Error("IsError=false should be omitted from JSON")
			}
		})
	}
}
