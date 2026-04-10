// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpecFrontmatter_Valid(t *testing.T) {
	content := []byte(`name: my-tool
version: "2.1"
description: A useful tool
category: developer
protocol: rest
auth: oauth2
tags:
  - api
  - testing
`)

	tool, err := ParseSpecFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tool.Name != "my-tool" {
		t.Errorf("name: got %q, want %q", tool.Name, "my-tool")
	}
	if tool.Version != "2.1" {
		t.Errorf("version: got %q, want %q", tool.Version, "2.1")
	}
	if tool.Description != "A useful tool" {
		t.Errorf("description: got %q, want %q", tool.Description, "A useful tool")
	}
	if tool.Category != "developer" {
		t.Errorf("category: got %q, want %q", tool.Category, "developer")
	}
	if tool.Protocol != "rest" {
		t.Errorf("protocol: got %q, want %q", tool.Protocol, "rest")
	}
	if tool.Auth != "oauth2" {
		t.Errorf("auth: got %q, want %q", tool.Auth, "oauth2")
	}
	if len(tool.Tags) != 2 || tool.Tags[0] != "api" || tool.Tags[1] != "testing" {
		t.Errorf("tags: got %v, want [api testing]", tool.Tags)
	}
}

func TestParseSpecFrontmatter_NumericVersion(t *testing.T) {
	content := []byte(`name: numeric-ver
version: 1.0
`)

	tool, err := ParseSpecFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Version != "1" {
		// YAML parses 1.0 as float64 1, which formats as "1"
		// This is expected behavior.
		t.Errorf("version: got %q, want %q", tool.Version, "1")
	}
}

func TestParseSpecFrontmatter_MissingName(t *testing.T) {
	content := []byte(`version: "1.0"
description: No name here
`)

	_, err := ParseSpecFrontmatter(content)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestParseSpecFrontmatter_MissingVersion(t *testing.T) {
	content := []byte(`name: no-version
description: Missing version field
`)

	_, err := ParseSpecFrontmatter(content)
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

func TestParseSpecFrontmatter_InvalidYAML(t *testing.T) {
	content := []byte(`{{{not valid yaml`)

	_, err := ParseSpecFrontmatter(content)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestScanSpecs_WithTempDir(t *testing.T) {
	dir := t.TempDir()

	// Create a valid spec.
	validSpec := []byte(`name: tool-a
version: "1.0"
description: Tool A
category: developer
protocol: rest
tags:
  - api
`)
	toolDir := filepath.Join(dir, "tool-a")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "tool-a.yaml"), validSpec, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a spec missing required fields (should be skipped).
	invalidSpec := []byte(`description: No name or version
category: misc
`)
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), invalidSpec, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a non-YAML file (should be ignored).
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a hidden file (should be ignored).
	if err := os.WriteFile(filepath.Join(dir, ".meta.yaml"), []byte("name: meta\nversion: 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	tools, err := ScanSpecs([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Name != "tool-a" {
		t.Errorf("name: got %q, want %q", tool.Name, "tool-a")
	}
	if tool.Version != "1.0" {
		t.Errorf("version: got %q, want %q", tool.Version, "1.0")
	}

	// Verify SHA256.
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(validSpec))
	if tool.SHA256 != expectedHash {
		t.Errorf("sha256: got %q, want %q", tool.SHA256, expectedHash)
	}

	// Verify path is set.
	if tool.Path != filepath.Join(toolDir, "tool-a.yaml") {
		t.Errorf("path: got %q, want %q", tool.Path, filepath.Join(toolDir, "tool-a.yaml"))
	}
}

func TestScanSpecs_NonexistentPath(t *testing.T) {
	tools, err := ScanSpecs([]string{"/nonexistent/path/that/should/not/exist"})
	if err != nil {
		t.Fatalf("unexpected error for nonexistent path: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for nonexistent path, got %d", len(tools))
	}
}

func TestScanSpecs_MultiplePaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	spec1 := []byte("name: tool-one\nversion: \"1.0\"\n")
	spec2 := []byte("name: tool-two\nversion: \"2.0\"\n")

	os.WriteFile(filepath.Join(dir1, "one.yaml"), spec1, 0o644)
	os.WriteFile(filepath.Join(dir2, "two.yaml"), spec2, 0o644)

	tools, err := ScanSpecs([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestLoadCliCtlConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`namespace: acme
spec_paths:
  - specs/
  - extras/
branches:
  - main
  - develop
`)
	if err := os.WriteFile(filepath.Join(dir, ".clictl.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Change to the temp dir so LoadCliCtlConfig finds the file.
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	cfg, err := LoadCliCtlConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Namespace != "acme" {
		t.Errorf("namespace: got %q, want %q", cfg.Namespace, "acme")
	}
	if len(cfg.SpecPaths) != 2 || cfg.SpecPaths[0] != "specs/" || cfg.SpecPaths[1] != "extras/" {
		t.Errorf("spec_paths: got %v, want [specs/ extras/]", cfg.SpecPaths)
	}
	if len(cfg.Branches) != 2 || cfg.Branches[0] != "main" || cfg.Branches[1] != "develop" {
		t.Errorf("branches: got %v, want [main develop]", cfg.Branches)
	}
}

func TestLoadCliCtlConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	// Minimal config. Create toolbox/ subfolder so defaults resolve to it.
	content := []byte(`namespace: minimal
`)
	toolboxDir := filepath.Join(dir, "toolbox")
	os.MkdirAll(toolboxDir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, ".clictl.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	cfg, err := LoadCliCtlConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Namespace != "minimal" {
		t.Errorf("namespace: got %q, want %q", cfg.Namespace, "minimal")
	}

	// Defaults should be applied - spec_paths points to toolbox/ subfolder.
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	expectedPath := filepath.Join(resolvedDir, "toolbox")
	if len(cfg.SpecPaths) != 1 || cfg.SpecPaths[0] != expectedPath {
		t.Errorf("spec_paths default: got %v, want [%s]", cfg.SpecPaths, expectedPath)
	}
	if len(cfg.Branches) != 2 || cfg.Branches[0] != "main" || cfg.Branches[1] != "master" {
		t.Errorf("branches default: got %v, want [main master]", cfg.Branches)
	}
}

func TestLoadCliCtlConfig_NotFound(t *testing.T) {
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	_, err := LoadCliCtlConfig()
	if err == nil {
		t.Fatal("expected error when .clictl.yaml not found, got nil")
	}
}

func TestIsBranchAllowed(t *testing.T) {
	allowed := []string{"main", "master", "release"}

	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"master", true},
		{"release", true},
		{"develop", false},
		{"feature/foo", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isBranchAllowed(tt.branch, allowed)
		if got != tt.want {
			t.Errorf("isBranchAllowed(%q, %v): got %v, want %v", tt.branch, allowed, got, tt.want)
		}
	}
}
