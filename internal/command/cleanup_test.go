// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{10485760, "10.0 MB"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.input)
		if got != tc.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestShouldAutoCleanup(t *testing.T) {
	// With no file, should return true
	home := t.TempDir()
	t.Setenv("HOME", home)

	if !ShouldAutoCleanup() {
		t.Error("expected ShouldAutoCleanup() = true when no file exists")
	}

	// Write recent timestamp
	cliDir := filepath.Join(home, ".clictl")
	os.MkdirAll(cliDir, 0o700)
	os.WriteFile(
		filepath.Join(cliDir, ".last-cleanup"),
		[]byte(time.Now().Format(time.RFC3339)),
		0o600,
	)

	if ShouldAutoCleanup() {
		t.Error("expected ShouldAutoCleanup() = false after recent cleanup")
	}

	// Write old timestamp
	os.WriteFile(
		filepath.Join(cliDir, ".last-cleanup"),
		[]byte(time.Now().Add(-31*24*time.Hour).Format(time.RFC3339)),
		0o600,
	)

	if !ShouldAutoCleanup() {
		t.Error("expected ShouldAutoCleanup() = true after 31 days")
	}
}

func TestCleanStaleCache(t *testing.T) {
	dir := t.TempDir()

	// Create a fresh file
	os.WriteFile(filepath.Join(dir, "fresh.yaml"), []byte("fresh"), 0o644)

	// Create an old file (set mtime to 10 days ago)
	oldPath := filepath.Join(dir, "old.yaml")
	os.WriteFile(oldPath, []byte("old content"), 0o644)
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(oldPath, oldTime, oldTime)

	// Dry run should not remove
	count, _ := cleanStaleCache(dir, 7*24*time.Hour, false, true)
	if count != 1 {
		t.Errorf("dry run: expected 1 stale file, got %d", count)
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Error("dry run removed the file")
	}

	// Real run should remove
	count, _ = cleanStaleCache(dir, 7*24*time.Hour, false, false)
	if count != 1 {
		t.Errorf("expected 1 removed, got %d", count)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old file was not removed")
	}

	// Fresh file should still exist
	if _, err := os.Stat(filepath.Join(dir, "fresh.yaml")); os.IsNotExist(err) {
		t.Error("fresh file was incorrectly removed")
	}
}
