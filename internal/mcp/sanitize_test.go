// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import "testing"

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"color code", "\x1b[31mred text\x1b[0m", "red text"},
		{"bold", "\x1b[1mbold\x1b[22m", "bold"},
		{"cursor move", "\x1b[2Amoved", "moved"},
		{"OSC title", "\x1b]0;window title\x07rest", "rest"},
		{"multiple sequences", "\x1b[1m\x1b[31mhello\x1b[0m", "hello"},
		{"empty string", "", ""},
		{"no escape", "just plain text", "just plain text"},
		{"mixed content", "before\x1b[32mgreen\x1b[0mafter", "beforegreenafter"},
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
