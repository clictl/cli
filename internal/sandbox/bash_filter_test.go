// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"testing"
)

func TestMatchBashCommand_ExactMatch(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match",
			command:  "npm run build",
			patterns: []string{"npm run build"},
			want:     true,
		},
		{
			name:     "no match",
			command:  "npm run dev",
			patterns: []string{"npm run build"},
			want:     false,
		},
		{
			name:     "multiple patterns first matches",
			command:  "go test ./...",
			patterns: []string{"go test ./...", "go build ./..."},
			want:     true,
		},
		{
			name:     "multiple patterns second matches",
			command:  "go build ./...",
			patterns: []string{"go test ./...", "go build ./..."},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchBashCommand(tt.command, tt.patterns)
			if got != tt.want {
				t.Errorf("MatchBashCommand(%q, %v) = %v, want %v", tt.command, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchBashCommand_GlobWildcard(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		patterns []string
		want     bool
	}{
		{
			name:     "trailing wildcard with space",
			command:  "npm run build",
			patterns: []string{"npm run *"},
			want:     true,
		},
		{
			name:     "trailing wildcard no space",
			command:  "npm run build",
			patterns: []string{"npm run*"},
			want:     true,
		},
		{
			name:     "trailing wildcard no match",
			command:  "yarn run build",
			patterns: []string{"npm run *"},
			want:     false,
		},
		{
			name:     "star only matches all",
			command:  "anything goes here",
			patterns: []string{"*"},
			want:     true,
		},
		{
			name:     "prefix with star",
			command:  "git status",
			patterns: []string{"git *"},
			want:     true,
		},
		{
			name:     "prefix mismatch with star",
			command:  "hg status",
			patterns: []string{"git *"},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchBashCommand(tt.command, tt.patterns)
			if got != tt.want {
				t.Errorf("MatchBashCommand(%q, %v) = %v, want %v", tt.command, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchBashCommand_PipeOperatorRejection(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		patterns []string
		want     bool
	}{
		{
			name:     "pipe rejected without exact match",
			command:  "cat file | grep foo",
			patterns: []string{"cat *", "grep *"},
			want:     false,
		},
		{
			name:     "pipe allowed with exact match",
			command:  "cat file | grep foo",
			patterns: []string{"cat file | grep foo"},
			want:     true,
		},
		{
			name:     "semicolon rejected",
			command:  "echo a; echo b",
			patterns: []string{"echo *"},
			want:     false,
		},
		{
			name:     "double ampersand rejected",
			command:  "make && make install",
			patterns: []string{"make *"},
			want:     false,
		},
		{
			name:     "double pipe rejected",
			command:  "true || false",
			patterns: []string{"true *"},
			want:     false,
		},
		{
			name:     "backtick rejected",
			command:  "echo `whoami`",
			patterns: []string{"echo *"},
			want:     false,
		},
		{
			name:     "dollar paren rejected",
			command:  "echo $(whoami)",
			patterns: []string{"echo *"},
			want:     false,
		},
		{
			name:     "semicolon allowed with exact match",
			command:  "echo a; echo b",
			patterns: []string{"echo a; echo b"},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchBashCommand(tt.command, tt.patterns)
			if got != tt.want {
				t.Errorf("MatchBashCommand(%q, %v) = %v, want %v", tt.command, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchBashCommand_EmptyPatterns(t *testing.T) {
	if MatchBashCommand("ls", nil) {
		t.Error("nil patterns should deny all commands")
	}
	if MatchBashCommand("ls", []string{}) {
		t.Error("empty patterns should deny all commands")
	}
}

func TestMatchBashCommand_EmptyCommand(t *testing.T) {
	if MatchBashCommand("", []string{"*"}) {
		t.Error("empty command should be denied")
	}
	if MatchBashCommand("  ", []string{"*"}) {
		t.Error("whitespace-only command should be denied")
	}
}

func TestMatchBashCommand_SpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		patterns []string
		want     bool
	}{
		{
			name:     "path with slashes",
			command:  "go test ./...",
			patterns: []string{"go test ./..."},
			want:     true,
		},
		{
			name:     "equals sign in command",
			command:  "export FOO=bar",
			patterns: []string{"export FOO=bar"},
			want:     true,
		},
		{
			name:     "quoted strings exact match",
			command:  `echo "hello world"`,
			patterns: []string{`echo "hello world"`},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchBashCommand(tt.command, tt.patterns)
			if got != tt.want {
				t.Errorf("MatchBashCommand(%q, %v) = %v, want %v", tt.command, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchBashCommand_WhitespaceHandling(t *testing.T) {
	// Command with leading/trailing spaces should be trimmed
	if !MatchBashCommand("  npm run build  ", []string{"npm run build"}) {
		t.Error("command with surrounding whitespace should match after trim")
	}
}

func TestValidateBashAllowPatterns_Valid(t *testing.T) {
	err := ValidateBashAllowPatterns([]string{"npm run *", "go test ./...", "git status"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateBashAllowPatterns_EmptyPattern(t *testing.T) {
	err := ValidateBashAllowPatterns([]string{"npm run *", ""})
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestValidateBashAllowPatterns_InvalidGlob(t *testing.T) {
	err := ValidateBashAllowPatterns([]string{"[invalid"})
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

func TestValidateBashAllowPatterns_Nil(t *testing.T) {
	err := ValidateBashAllowPatterns(nil)
	if err != nil {
		t.Errorf("nil patterns should be valid, got: %v", err)
	}
}
