// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"encoding/json"
	"testing"
)

func TestPromptSerialization(t *testing.T) {
	tests := []struct {
		name   string
		prompt Prompt
		want   map[string]any
	}{
		{
			name: "minimal prompt",
			prompt: Prompt{
				Name: "greet",
			},
			want: map[string]any{"name": "greet"},
		},
		{
			name: "prompt with description and args",
			prompt: Prompt{
				Name:        "code-review",
				Description: "Review code for issues",
				Arguments: []PromptArg{
					{Name: "language", Description: "Programming language", Required: true},
					{Name: "style", Description: "Review style"},
				},
			},
			want: map[string]any{
				"name":        "code-review",
				"description": "Review code for issues",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.prompt)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			for k, wantVal := range tt.want {
				gotVal, ok := got[k]
				if !ok {
					t.Errorf("missing key %q", k)
					continue
				}
				if gotVal != wantVal {
					t.Errorf("key %q: got %v, want %v", k, gotVal, wantVal)
				}
			}

			// Roundtrip
			var roundtrip Prompt
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Roundtrip: %v", err)
			}
			if roundtrip.Name != tt.prompt.Name {
				t.Errorf("Name roundtrip: got %q, want %q", roundtrip.Name, tt.prompt.Name)
			}
		})
	}
}

func TestPromptArgRequired(t *testing.T) {
	tests := []struct {
		name     string
		arg      PromptArg
		wantReq  bool
		wantJSON string
	}{
		{
			name:    "required arg",
			arg:     PromptArg{Name: "code", Description: "Source code", Required: true},
			wantReq: true,
		},
		{
			name:    "optional arg (default false)",
			arg:     PromptArg{Name: "format", Description: "Output format"},
			wantReq: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.arg)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var roundtrip PromptArg
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if roundtrip.Required != tt.wantReq {
				t.Errorf("Required: got %v, want %v", roundtrip.Required, tt.wantReq)
			}
			if roundtrip.Name != tt.arg.Name {
				t.Errorf("Name: got %q, want %q", roundtrip.Name, tt.arg.Name)
			}

			// Verify omitempty: optional args should not have "required" in JSON
			var raw map[string]any
			json.Unmarshal(data, &raw)
			_, hasRequired := raw["required"]
			if !tt.wantReq && hasRequired {
				t.Error("optional arg should omit 'required' field from JSON")
			}
			if tt.wantReq && !hasRequired {
				t.Error("required arg should include 'required' field in JSON")
			}
		})
	}
}

func TestPromptGetResultMessages(t *testing.T) {
	result := PromptGetResult{
		Description: "A multi-turn prompt",
		Messages: []PromptMessage{
			{
				Role:    "user",
				Content: ContentBlock{Type: "text", Text: "Please review this code"},
			},
			{
				Role:    "assistant",
				Content: ContentBlock{Type: "text", Text: "I will review the code."},
			},
			{
				Role: "user",
				Content: ContentBlock{
					Type: "text",
					Text: "Here is the code: func main() {}",
				},
			},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundtrip PromptGetResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if roundtrip.Description != "A multi-turn prompt" {
		t.Errorf("Description: got %q", roundtrip.Description)
	}
	if len(roundtrip.Messages) != 3 {
		t.Fatalf("Messages count: got %d, want 3", len(roundtrip.Messages))
	}
	if roundtrip.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role: got %q, want user", roundtrip.Messages[0].Role)
	}
	if roundtrip.Messages[1].Content.Text != "I will review the code." {
		t.Errorf("Messages[1].Content.Text: got %q", roundtrip.Messages[1].Content.Text)
	}
	if roundtrip.Messages[2].Content.Type != "text" {
		t.Errorf("Messages[2].Content.Type: got %q", roundtrip.Messages[2].Content.Type)
	}
}

func TestPromptsListResultPagination(t *testing.T) {
	result := PromptsListResult{
		Prompts: []Prompt{
			{Name: "greet", Description: "Say hello"},
			{Name: "review", Description: "Code review"},
		},
		NextCursor: "cursor-abc",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundtrip PromptsListResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(roundtrip.Prompts) != 2 {
		t.Fatalf("Prompts count: got %d, want 2", len(roundtrip.Prompts))
	}
	if roundtrip.NextCursor != "cursor-abc" {
		t.Errorf("NextCursor: got %q, want cursor-abc", roundtrip.NextCursor)
	}
}
