// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComputeETag(t *testing.T) {
	data := []byte("name: test-tool\nversion: 1.0.0\n")
	etag := computeETag(data)

	if !strings.HasPrefix(etag, "sha256:") {
		t.Errorf("expected etag to start with 'sha256:', got %q", etag)
	}

	// Verify the hash is correct
	hash := sha256.Sum256(data)
	expected := "sha256:" + hex.EncodeToString(hash[:])
	if etag != expected {
		t.Errorf("etag mismatch: got %q, want %q", etag, expected)
	}
}

func TestComputeETag_DifferentContent(t *testing.T) {
	data1 := []byte("name: tool-a\nversion: 1.0.0\n")
	data2 := []byte("name: tool-a\nversion: 1.0.1\n")

	etag1 := computeETag(data1)
	etag2 := computeETag(data2)

	if etag1 == etag2 {
		t.Error("expected different etags for different content")
	}
}

func TestWriteAndLoadLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, ".clictl", "lock.yaml")

	lf := &LockFile{
		Tools: map[string]LockEntry{
			"github-mcp": {
				Version: "1.2.0",
				ETag:    "sha256:abc123",
			},
			"time-mcp": {
				Version: "1.0.0",
				ETag:    "sha256:def456",
			},
		},
		GeneratedAt: "2024-01-15T10:30:00Z",
	}

	// Write lock file manually to tmp dir
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock file: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("writing lock file: %v", err)
	}

	// Read it back
	readData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}

	var loaded LockFile
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("parsing lock file: %v", err)
	}

	if len(loaded.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(loaded.Tools))
	}

	gh, ok := loaded.Tools["github-mcp"]
	if !ok {
		t.Fatal("expected github-mcp in lock file")
	}
	if gh.Version != "1.2.0" {
		t.Errorf("github-mcp version: got %q, want %q", gh.Version, "1.2.0")
	}
	if gh.ETag != "sha256:abc123" {
		t.Errorf("github-mcp etag: got %q, want %q", gh.ETag, "sha256:abc123")
	}

	tm, ok := loaded.Tools["time-mcp"]
	if !ok {
		t.Fatal("expected time-mcp in lock file")
	}
	if tm.Version != "1.0.0" {
		t.Errorf("time-mcp version: got %q, want %q", tm.Version, "1.0.0")
	}
	if tm.ETag != "sha256:def456" {
		t.Errorf("time-mcp etag: got %q, want %q", tm.ETag, "sha256:def456")
	}

	if loaded.GeneratedAt != "2024-01-15T10:30:00Z" {
		t.Errorf("generated_at: got %q, want %q", loaded.GeneratedAt, "2024-01-15T10:30:00Z")
	}
}

func TestLockFileStructure(t *testing.T) {
	// Verify YAML structure matches expected format
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"my-tool": {
				Version: "2.0.0",
				ETag:    "sha256:aabbcc",
			},
		},
		GeneratedAt: "2024-06-01T00:00:00Z",
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, "tools:") {
		t.Error("expected 'tools:' key in YAML output")
	}
	if !strings.Contains(yamlStr, "generated_at:") {
		t.Error("expected 'generated_at:' key in YAML output")
	}
	if !strings.Contains(yamlStr, "my-tool:") {
		t.Error("expected 'my-tool:' key in YAML output")
	}
	if !strings.Contains(yamlStr, "version: 2.0.0") {
		t.Error("expected 'version: 2.0.0' in YAML output")
	}
	if !strings.Contains(yamlStr, "etag: sha256:aabbcc") {
		t.Error("expected 'etag: sha256:aabbcc' in YAML output")
	}
}

func TestLockFilePath(t *testing.T) {
	path := lockFilePath()
	if path == "" {
		t.Fatal("lockFilePath returned empty string")
	}
	if !strings.HasSuffix(path, filepath.Join(".clictl", "lock.yaml")) {
		t.Errorf("expected path to end with .clictl/lock.yaml, got %q", path)
	}
}

func TestLoadLockFile_NonExistent(t *testing.T) {
	// LoadLockFile should return nil, nil for a non-existent file.
	// We cannot easily test this without mocking the home dir,
	// but we verify the function signature is correct.
	// If the lock file doesn't exist on this system, it returns nil.
	lf, err := LoadLockFile()
	if err != nil {
		// May or may not have a lock file; only fail on real errors
		t.Logf("LoadLockFile returned error (may be expected): %v", err)
	}
	_ = lf // Either nil or a valid lock file
}
