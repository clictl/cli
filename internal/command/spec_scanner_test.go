// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"strings"
	"testing"
)

func TestParseSpecFrontmatter_MaxSize(t *testing.T) {
	// Create content larger than maxSpecFileSize (10MB)
	oversized := make([]byte, maxSpecFileSize+1)
	for i := range oversized {
		oversized[i] = 'a'
	}

	_, err := ParseSpecFrontmatter(oversized)
	if err == nil {
		t.Fatal("ParseSpecFrontmatter should reject oversized content")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention 'too large', got: %v", err)
	}
}

func TestParseSpecFrontmatter_ValidSize(t *testing.T) {
	content := []byte("name: test-tool\nversion: \"1.0\"\ndescription: A test tool\n")
	tool, err := ParseSpecFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseSpecFrontmatter should succeed for valid content: %v", err)
	}
	if tool.Name != "test-tool" {
		t.Errorf("Name = %q, want %q", tool.Name, "test-tool")
	}
	if tool.Version != "1.0" {
		t.Errorf("Version = %q, want %q", tool.Version, "1.0")
	}
}

func TestParseSpecFrontmatter_RequiresName(t *testing.T) {
	content := []byte("version: 1.0\n")
	_, err := ParseSpecFrontmatter(content)
	if err == nil {
		t.Fatal("ParseSpecFrontmatter should reject content without name")
	}
}

func TestParseSpecFrontmatter_RequiresVersion(t *testing.T) {
	content := []byte("name: test\n")
	_, err := ParseSpecFrontmatter(content)
	if err == nil {
		t.Fatal("ParseSpecFrontmatter should reject content without version")
	}
}
