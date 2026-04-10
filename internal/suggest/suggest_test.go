// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package suggest

import (
	"testing"
)

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"weather", "weather", 0},
		{"weather", "wether", 1},
		{"weather", "weater", 1},
		{"github", "githbu", 2},
		{"github", "guthub", 1},
		{"openweathermap", "openweatermap", 1},
		{"slack", "slak", 1},
		{"slack", "slock", 1},
		{"abc", "xyz", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestToolsSingleMatch(t *testing.T) {
	candidates := []string{"weather", "github", "slack", "openweathermap"}
	got := Tools("wether", candidates, 3)
	if len(got) == 0 {
		t.Fatal("expected suggestions, got none")
	}
	if got[0] != "weather" {
		t.Errorf("expected 'weather' as first suggestion, got %q", got[0])
	}
}

func TestToolsMultipleMatches(t *testing.T) {
	candidates := []string{"slack", "slick", "click", "flask"}
	got := Tools("slak", candidates, 3)
	if len(got) == 0 {
		t.Fatal("expected suggestions, got none")
	}
	// "slack" should be first (distance 1)
	if got[0] != "slack" {
		t.Errorf("expected 'slack' first, got %q", got[0])
	}
}

func TestToolsNoMatch(t *testing.T) {
	candidates := []string{"weather", "github", "slack"}
	got := Tools("zzzzzzzzz", candidates, 3)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestToolsCaseInsensitive(t *testing.T) {
	candidates := []string{"GitHub", "GitLab"}
	got := Tools("github", candidates, 3)
	if len(got) == 0 {
		t.Fatal("expected suggestions, got none")
	}
	if got[0] != "GitHub" {
		t.Errorf("expected 'GitHub', got %q", got[0])
	}
}

func TestToolsPrefixMatch(t *testing.T) {
	candidates := []string{"openweathermap", "openai", "openssl"}
	got := Tools("open", candidates, 5)
	if len(got) != 3 {
		t.Errorf("expected 3 prefix matches, got %d: %v", len(got), got)
	}
}

func TestToolsEmptyInput(t *testing.T) {
	got := Tools("", []string{"a", "b"}, 3)
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestToolsEmptyCandidates(t *testing.T) {
	got := Tools("weather", nil, 3)
	if got != nil {
		t.Errorf("expected nil for empty candidates, got %v", got)
	}
}

func TestFormatMessageSingle(t *testing.T) {
	msg := FormatMessage("wether", []string{"weather", "github"})
	if msg == "" {
		t.Fatal("expected message, got empty")
	}
	if !contains(msg, "Did you mean?") {
		t.Errorf("expected 'Did you mean?', got %q", msg)
	}
	if !contains(msg, "weather") {
		t.Errorf("expected 'weather' in message, got %q", msg)
	}
}

func TestFormatMessageNone(t *testing.T) {
	msg := FormatMessage("zzzzz", []string{"weather"})
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestMaxDistance(t *testing.T) {
	if maxDistance(2) != 1 {
		t.Error("short names should allow 1 edit")
	}
	if maxDistance(5) != 2 {
		t.Error("medium names should allow 2 edits")
	}
	if maxDistance(10) != 3 {
		t.Error("long names should allow 3 edits")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
