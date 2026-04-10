// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSensitiveDirs_NonEmpty(t *testing.T) {
	dirs := SensitiveDirs()
	if len(dirs) == 0 {
		t.Error("SensitiveDirs should return at least one directory")
	}
}

func TestSensitiveDirs_AllAbsolute(t *testing.T) {
	for _, dir := range SensitiveDirs() {
		if !filepath.IsAbs(dir) {
			t.Errorf("expected absolute path, got %q", dir)
		}
	}
}

func TestSensitiveDirs_ContainsSSH(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	sshDir := filepath.Join(home, ".ssh")
	for _, dir := range SensitiveDirs() {
		if dir == sshDir {
			return
		}
	}
	t.Errorf("SensitiveDirs should contain %s", sshDir)
}

func TestSensitiveDirs_ContainsAWS(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	awsDir := filepath.Join(home, ".aws")
	for _, dir := range SensitiveDirs() {
		if dir == awsDir {
			return
		}
	}
	t.Errorf("SensitiveDirs should contain %s", awsDir)
}

func TestSystemReadOnlyPaths_NonEmpty(t *testing.T) {
	paths := SystemReadOnlyPaths()
	// Windows returns nil, which is expected
	if paths == nil {
		t.Skip("no system read-only paths on this platform")
	}
	if len(paths) == 0 {
		t.Error("SystemReadOnlyPaths should return at least one path")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~/.config/bar", filepath.Join(home, ".config/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
