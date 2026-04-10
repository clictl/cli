// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStripANSIExtended extends the existing sanitize_test.go with additional
// edge cases for ANSI stripping security.
func TestStripANSIExtended(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "hyperlink OSC sequence",
			input: "\x1b]8;;https://evil.com\x07click here\x1b]8;;\x07",
			want:  "click here",
		},
		{
			name:  "256-color foreground",
			input: "\x1b[38;5;196mred text\x1b[0m",
			want:  "red text",
		},
		{
			name:  "true-color RGB",
			input: "\x1b[38;2;255;0;0mtrue red\x1b[0m",
			want:  "true red",
		},
		{
			name:  "erase display",
			input: "\x1b[2Jhidden content",
			want:  "hidden content",
		},
		{
			name:  "cursor save and restore",
			input: "\x1b[ssaved\x1b[urestored",
			want:  "savedrestored",
		},
		{
			name:  "newlines preserved",
			input: "line1\nline2\n\x1b[31mred\x1b[0m\nline4",
			want:  "line1\nline2\nred\nline4",
		},
		{
			name:  "tabs preserved",
			input: "col1\tcol2\t\x1b[1mbold\x1b[0m",
			want:  "col1\tcol2\tbold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestContentBoundaryMarkers verifies that tool results with special content
// boundaries or delimiters are properly serialized and contained.
func TestContentBoundaryMarkers(t *testing.T) {
	tests := []struct {
		name    string
		content []ContentBlock
	}{
		{
			name: "content with JSON-like text",
			content: []ContentBlock{
				{Type: "text", Text: `{"nested": "json", "key": "value"}`},
			},
		},
		{
			name: "content with newlines and special chars",
			content: []ContentBlock{
				{Type: "text", Text: "line1\nline2\ttab\r\nwindows"},
			},
		},
		{
			name: "content with unicode",
			content: []ContentBlock{
				{Type: "text", Text: "emoji: \U0001F600 chinese: \u4e16\u754c japanese: \u3053\u3093\u306b\u3061\u306f"},
			},
		},
		{
			name: "content with null bytes",
			content: []ContentBlock{
				{Type: "text", Text: "before\x00after"},
			},
		},
		{
			name: "multiple content blocks stay separated",
			content: []ContentBlock{
				{Type: "text", Text: "block 1"},
				{Type: "text", Text: "block 2"},
				{Type: "image", Data: "iVBORw0=", MimeType: "image/png"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CallToolResult{Content: tt.content}

			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var roundtrip CallToolResult
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if len(roundtrip.Content) != len(tt.content) {
				t.Fatalf("Content count: got %d, want %d", len(roundtrip.Content), len(tt.content))
			}

			for i, want := range tt.content {
				got := roundtrip.Content[i]
				if got.Type != want.Type {
					t.Errorf("Content[%d].Type: got %q, want %q", i, got.Type, want.Type)
				}
				if got.Text != want.Text {
					t.Errorf("Content[%d].Text: got %q, want %q", i, got.Text, want.Text)
				}
			}
		})
	}
}

// TestHTTPResponseSizeLimit verifies that the Response struct can represent
// large payloads and that error codes are properly serialized for size-related
// rejections.
func TestHTTPResponseSizeLimit(t *testing.T) {
	// Test that error responses for oversized content use correct JSON-RPC codes
	tests := []struct {
		name     string
		errCode  int
		errMsg   string
		wantCode int
	}{
		{
			name:     "internal error for size overflow",
			errCode:  -32603,
			errMsg:   "Response exceeds maximum allowed size",
			wantCode: -32603,
		},
		{
			name:     "invalid params",
			errCode:  -32602,
			errMsg:   "Invalid parameters",
			wantCode: -32602,
		},
		{
			name:     "method not found",
			errCode:  -32601,
			errMsg:   "Method not found",
			wantCode: -32601,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := Response{
				JSONRPC: "2.0",
				ID:      1,
				Error: &Error{
					Code:    tt.errCode,
					Message: tt.errMsg,
				},
			}

			data, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var roundtrip Response
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if roundtrip.Error == nil {
				t.Fatal("Error should not be nil")
			}
			if roundtrip.Error.Code != tt.wantCode {
				t.Errorf("Error.Code: got %d, want %d", roundtrip.Error.Code, tt.wantCode)
			}
			if roundtrip.Error.Message != tt.errMsg {
				t.Errorf("Error.Message: got %q, want %q", roundtrip.Error.Message, tt.errMsg)
			}
		})
	}

	// Test that a large text response can be serialized
	t.Run("large text response serializes", func(t *testing.T) {
		largeText := strings.Repeat("x", 1024*1024) // 1MB
		result := CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: largeText}},
		}

		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Marshal large result: %v", err)
		}

		var roundtrip CallToolResult
		if err := json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatalf("Unmarshal large result: %v", err)
		}

		if len(roundtrip.Content[0].Text) != 1024*1024 {
			t.Errorf("Large text length: got %d, want %d", len(roundtrip.Content[0].Text), 1024*1024)
		}
	})
}
