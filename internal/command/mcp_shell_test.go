// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/mcp"
)

func TestShellSplitLine(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"tools", []string{"tools"}},
		{"call my_tool", []string{"call", "my_tool"}},
		{`call my_tool {"key": "value"}`, []string{"call", "my_tool", `{"key": "value"}`}},
		{`call tool {"a": 1, "b": [1, 2]}`, []string{"call", "tool", `{"a": 1, "b": [1, 2]}`}},
		{"format json", []string{"format", "json"}},
		{"", nil},
		{"  ", nil},
		{"exit", []string{"exit"}},
		{`read file:///path/to/file`, []string{"read", "file:///path/to/file"}},
		{`prompt review {"lang": "go"}`, []string{"prompt", "review", `{"lang": "go"}`}},
	}

	for _, tt := range tests {
		got := shellSplitLine(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("shellSplitLine(%q) = %v (len %d), want %v (len %d)",
				tt.input, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("shellSplitLine(%q)[%d] = %q, want %q",
					tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestShellPrintHelp(t *testing.T) {
	var buf bytes.Buffer
	shellPrintHelp(&buf)
	output := buf.String()

	expected := []string{"tools", "resources", "prompts", "call", "read", "prompt", "format", "help", "exit"}
	for _, cmd := range expected {
		if !strings.Contains(output, cmd) {
			t.Errorf("help output missing command %q", cmd)
		}
	}
}

func TestShellCompleterCommands(t *testing.T) {
	state := &shellState{
		tools: []mcp.Tool{
			{Name: "search", Description: "Search things"},
			{Name: "status", Description: "Check status"},
		},
		toolNames: map[string]bool{"search": true, "status": true},
	}
	c := newShellCompleter(state)

	// Complete all commands
	all := c.Complete("")
	if len(all) == 0 {
		t.Fatal("expected non-empty completion for empty input")
	}

	// Should include built-in commands and tool names
	found := map[string]bool{}
	for _, s := range all {
		found[s] = true
	}
	for _, want := range []string{"tools", "call", "help", "exit", "search", "status"} {
		if !found[want] {
			t.Errorf("completion missing %q", want)
		}
	}

	// Prefix filter
	matches := c.Complete("s")
	for _, m := range matches {
		if !strings.HasPrefix(m, "s") {
			t.Errorf("completion %q does not have prefix 's'", m)
		}
	}
}

func TestShellCompleterToolNames(t *testing.T) {
	state := &shellState{
		tools: []mcp.Tool{
			{Name: "list_files"},
			{Name: "list_repos"},
			{Name: "create_issue"},
		},
	}
	c := newShellCompleter(state)

	matches := c.Complete("call list")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
	for _, m := range matches {
		if !strings.HasPrefix(m, "list") {
			t.Errorf("completion %q does not have prefix 'list'", m)
		}
	}
}

func TestShellCompleterResourceURIs(t *testing.T) {
	state := &shellState{
		resources: []mcp.Resource{
			{URI: "file:///home/config.yaml"},
			{URI: "file:///home/data.json"},
			{URI: "db://users"},
		},
	}
	c := newShellCompleter(state)

	matches := c.Complete("read file")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestShellCompleterPromptNames(t *testing.T) {
	state := &shellState{
		prompts: []mcp.Prompt{
			{Name: "summarize"},
			{Name: "review"},
		},
	}
	c := newShellCompleter(state)

	matches := c.Complete("prompt s")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), matches)
	}
	if matches[0] != "summarize" {
		t.Errorf("expected 'summarize', got %q", matches[0])
	}
}

func TestShellCompleterFormats(t *testing.T) {
	state := &shellState{}
	c := newShellCompleter(state)

	matches := c.Complete("format j")
	if len(matches) != 1 || matches[0] != "json" {
		t.Errorf("expected [json], got %v", matches)
	}

	all := c.Complete("format ")
	if len(all) != 3 {
		t.Errorf("expected 3 format options, got %d", len(all))
	}
}

func TestShellSetFormat(t *testing.T) {
	state := &shellState{format: shellFormatPretty}

	shellSetFormat(state, "json")
	if state.format != shellFormatJSON {
		t.Errorf("expected json format, got %s", state.format)
	}

	shellSetFormat(state, "text")
	if state.format != shellFormatText {
		t.Errorf("expected text format, got %s", state.format)
	}

	shellSetFormat(state, "pretty")
	if state.format != shellFormatPretty {
		t.Errorf("expected pretty format, got %s", state.format)
	}

	// Invalid format should not change current
	shellSetFormat(state, "xml")
	if state.format != shellFormatPretty {
		t.Errorf("expected pretty format after invalid, got %s", state.format)
	}
}

func TestShellHistory(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, "test_history")

	state := &shellState{
		history: []string{"tools", "call search {}", "exit"},
	}

	state.saveHistory(histPath)

	// Check file permissions
	info, err := os.Stat(histPath)
	if err != nil {
		t.Fatalf("stat history file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	// Load into a new state
	state2 := &shellState{}
	state2.loadHistory(histPath)
	if len(state2.history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(state2.history))
	}
	if state2.history[0] != "tools" {
		t.Errorf("history[0] = %q, want 'tools'", state2.history[0])
	}
	if state2.history[2] != "exit" {
		t.Errorf("history[2] = %q, want 'exit'", state2.history[2])
	}
}

func TestShellHistoryMaxSize(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, "test_history_max")

	state := &shellState{}
	for i := 0; i < shellMaxHistory+100; i++ {
		state.history = append(state.history, "command")
	}

	state.saveHistory(histPath)

	state2 := &shellState{}
	state2.loadHistory(histPath)
	if len(state2.history) != shellMaxHistory {
		t.Errorf("expected %d history entries after trim, got %d", shellMaxHistory, len(state2.history))
	}
}

func TestShellHistoryLoadMissing(t *testing.T) {
	state := &shellState{}
	state.loadHistory("/nonexistent/path/history")
	if len(state.history) != 0 {
		t.Error("expected empty history when loading from nonexistent file")
	}
}

func TestShellFormatModeString(t *testing.T) {
	tests := []struct {
		mode shellFormatMode
		want string
	}{
		{shellFormatPretty, "pretty"},
		{shellFormatJSON, "json"},
		{shellFormatText, "text"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("shellFormatMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestExtractCallToolText(t *testing.T) {
	tests := []struct {
		name   string
		result *mcp.CallToolResult
		want   string
	}{
		{
			name: "text content",
			result: &mcp.CallToolResult{
				Content: []mcp.ContentBlock{
					{Type: "text", Text: "hello world"},
				},
			},
			want: "hello world",
		},
		{
			name: "multiple text blocks",
			result: &mcp.CallToolResult{
				Content: []mcp.ContentBlock{
					{Type: "text", Text: "line 1"},
					{Type: "text", Text: "line 2"},
				},
			},
			want: "line 1\nline 2",
		},
		{
			name: "image content",
			result: &mcp.CallToolResult{
				Content: []mcp.ContentBlock{
					{Type: "image", Data: "base64data", MimeType: "image/jpeg"},
				},
			},
			want: "[image: image/jpeg, 10 bytes base64]",
		},
		{
			name: "resource link",
			result: &mcp.CallToolResult{
				Content: []mcp.ContentBlock{
					{Type: "resource_link", URI: "file:///test.txt"},
				},
			},
			want: "[resource: file:///test.txt]",
		},
		{
			name: "embedded resource text",
			result: &mcp.CallToolResult{
				Content: []mcp.ContentBlock{
					{Type: "embedded_resource", Resource: &mcp.ResourceContent{
						URI:  "file:///test.txt",
						Text: "embedded text",
					}},
				},
			},
			want: "embedded text",
		},
		{
			name:   "empty result",
			result: &mcp.CallToolResult{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCallToolText(tt.result)
			if got != tt.want {
				t.Errorf("extractCallToolText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShellSplitLineEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		// Nested JSON with arrays
		{`call tool {"items": [{"a": 1}, {"b": 2}]}`, []string{"call", "tool", `{"items": [{"a": 1}, {"b": 2}]}`}},
		// Quoted strings
		{`call tool {"key": "hello world"}`, []string{"call", "tool", `{"key": "hello world"}`}},
		// Multiple spaces
		{"call   tool   arg", []string{"call", "tool", "arg"}},
	}

	for _, tt := range tests {
		got := shellSplitLine(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("shellSplitLine(%q) = %v (len %d), want %v (len %d)",
				tt.input, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("shellSplitLine(%q)[%d] = %q, want %q",
					tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 5: Additional Shell Tests
// ---------------------------------------------------------------------------

func TestShellCommandParsingShorthand(t *testing.T) {
	// When a tool name is in the toolNames map, typing "tool {json}" should
	// be equivalent to "call tool {json}". This tests that shellSplitLine
	// correctly parses both forms.
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "explicit call with JSON",
			input: `call search {"query": "test"}`,
			want:  []string{"call", "search", `{"query": "test"}`},
		},
		{
			name:  "shorthand tool name with JSON",
			input: `search {"query": "test"}`,
			want:  []string{"search", `{"query": "test"}`},
		},
		{
			name:  "shorthand with nested object",
			input: `deploy {"config": {"region": "us-east-1", "count": 3}}`,
			want:  []string{"deploy", `{"config": {"region": "us-east-1", "count": 3}}`},
		},
		{
			name:  "shorthand with array arg",
			input: `batch [1, 2, 3]`,
			want:  []string{"batch", `[1, 2, 3]`},
		},
		{
			name:  "tool name with no args",
			input: `status`,
			want:  []string{"status"},
		},
		{
			name:  "call with empty JSON",
			input: `call ping {}`,
			want:  []string{"call", "ping", `{}`},
		},
		{
			name:  "tool name with single-quoted string in JSON",
			input: `search {"query": 'hello world'}`,
			want:  []string{"search", `{"query": 'hello world'}`},
		},
		{
			name:  "deeply nested braces",
			input: `call transform {"a": {"b": {"c": {"d": 1}}}}`,
			want:  []string{"call", "transform", `{"a": {"b": {"c": {"d": 1}}}}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellSplitLine(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("shellSplitLine(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("shellSplitLine(%q)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestShellCompleterMixedContext(t *testing.T) {
	// Completer should return appropriate suggestions based on command context
	state := &shellState{
		tools: []mcp.Tool{
			{Name: "search", Description: "Search things"},
			{Name: "deploy", Description: "Deploy things"},
			{Name: "status", Description: "Check status"},
		},
		toolNames: map[string]bool{
			"search": true,
			"deploy": true,
			"status": true,
		},
		resources: []mcp.Resource{
			{URI: "file:///config.yaml"},
		},
		prompts: []mcp.Prompt{
			{Name: "summarize"},
		},
	}
	c := newShellCompleter(state)

	// "call d" should complete to tool names starting with "d"
	matches := c.Complete("call d")
	if len(matches) != 1 || matches[0] != "deploy" {
		t.Errorf("Complete('call d') = %v, want [deploy]", matches)
	}

	// "read f" should complete to resource URIs starting with "f"
	matches = c.Complete("read f")
	if len(matches) != 1 || matches[0] != "file:///config.yaml" {
		t.Errorf("Complete('read f') = %v, want [file:///config.yaml]", matches)
	}

	// "prompt s" should complete to prompt names starting with "s"
	matches = c.Complete("prompt s")
	if len(matches) != 1 || matches[0] != "summarize" {
		t.Errorf("Complete('prompt s') = %v, want [summarize]", matches)
	}

	// Empty completion should include all commands plus all tool names
	all := c.Complete("")
	if len(all) < 6 {
		t.Errorf("Complete('') returned %d items, expected at least 6 (commands + tools)", len(all))
	}
}

func TestShellHistoryPersistenceRoundTrip(t *testing.T) {
	// Verify that history is correctly saved and loaded through multiple cycles
	dir := t.TempDir()
	histPath := filepath.Join(dir, "roundtrip_history")

	// Write session 1
	s1 := &shellState{
		history: []string{"tools", `call search {"q": "test"}`},
	}
	s1.saveHistory(histPath)

	// Load into session 2, add more, save
	s2 := &shellState{}
	s2.loadHistory(histPath)
	if len(s2.history) != 2 {
		t.Fatalf("session 2: expected 2 entries, got %d", len(s2.history))
	}
	s2.history = append(s2.history, "format json", "exit")
	s2.saveHistory(histPath)

	// Load into session 3 and verify all entries
	s3 := &shellState{}
	s3.loadHistory(histPath)
	if len(s3.history) != 4 {
		t.Fatalf("session 3: expected 4 entries, got %d", len(s3.history))
	}
	expected := []string{"tools", `call search {"q": "test"}`, "format json", "exit"}
	for i, want := range expected {
		if s3.history[i] != want {
			t.Errorf("session 3: history[%d] = %q, want %q", i, s3.history[i], want)
		}
	}
}

func TestShellFormatToggleCycle(t *testing.T) {
	// Verify cycling through all format modes
	state := &shellState{format: shellFormatPretty}

	// pretty -> json
	shellSetFormat(state, "json")
	if state.format != shellFormatJSON {
		t.Errorf("after 'json': got %s, want json", state.format)
	}

	// json -> text
	shellSetFormat(state, "text")
	if state.format != shellFormatText {
		t.Errorf("after 'text': got %s, want text", state.format)
	}

	// text -> pretty
	shellSetFormat(state, "pretty")
	if state.format != shellFormatPretty {
		t.Errorf("after 'pretty': got %s, want pretty", state.format)
	}

	// invalid should preserve current
	shellSetFormat(state, "yaml")
	if state.format != shellFormatPretty {
		t.Errorf("after invalid 'yaml': got %s, want pretty", state.format)
	}

	shellSetFormat(state, "")
	if state.format != shellFormatPretty {
		t.Errorf("after empty string: got %s, want pretty", state.format)
	}
}

func TestShellHistoryEmptyFile(t *testing.T) {
	// Loading from an empty file should result in empty history
	dir := t.TempDir()
	histPath := filepath.Join(dir, "empty_history")

	// Create empty file
	if err := os.WriteFile(histPath, []byte(""), 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	state := &shellState{}
	state.loadHistory(histPath)
	if len(state.history) != 0 {
		t.Errorf("expected 0 history entries from empty file, got %d", len(state.history))
	}
}

func TestShellSplitLineUnclosedBrace(t *testing.T) {
	// Unclosed braces should still produce output (no hang or panic).
	// The brace/bracket opens after "tool", so "call" and "tool" are separate
	// tokens, and everything from the opening brace onward is a third token.
	tests := []struct {
		input string
		want  int // expected number of parts
	}{
		{`call tool {"key": "val"`, 3}, // call, tool, {"key": "val"
		{`call tool [1, 2`, 3},         // call, tool, [1, 2
	}

	for _, tt := range tests {
		got := shellSplitLine(tt.input)
		if len(got) != tt.want {
			t.Errorf("shellSplitLine(%q): got %d parts %v, want %d parts",
				tt.input, len(got), got, tt.want)
		}
	}
}

func TestExtractCallToolTextMixed(t *testing.T) {
	// Mixed content types should be concatenated with newlines
	result := &mcp.CallToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: "Header"},
			{Type: "image", Data: "abc", MimeType: "image/png"},
			{Type: "text", Text: "Footer"},
		},
	}

	got := extractCallToolText(result)
	if !strings.Contains(got, "Header") {
		t.Error("missing 'Header' in output")
	}
	if !strings.Contains(got, "Footer") {
		t.Error("missing 'Footer' in output")
	}
	if !strings.Contains(got, "[image: image/png") {
		t.Error("missing image placeholder in output")
	}
}

func TestShellCompleterEmpty(t *testing.T) {
	// Completer with no tools/resources/prompts should still return built-in commands
	state := &shellState{}
	c := newShellCompleter(state)

	all := c.Complete("")
	if len(all) == 0 {
		t.Fatal("expected at least built-in commands for empty state")
	}

	// Built-in commands should always be present
	found := map[string]bool{}
	for _, s := range all {
		found[s] = true
	}
	for _, want := range []string{"tools", "help", "exit", "format"} {
		if !found[want] {
			t.Errorf("missing built-in command %q in completions", want)
		}
	}
}
