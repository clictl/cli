// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clictl/cli/internal/models"
	"gopkg.in/yaml.v3"
)

func TestRunSkillManifest(t *testing.T) {
	dir := t.TempDir()

	// Create test files with known content
	files := map[string]string{
		"SKILL.md":     "# My Skill\nThis is a test skill.",
		"pdf_tools.py": "def convert(): pass\n",
		"helper.sh":    "#!/bin/bash\necho hello\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("writing test file %s: %v", name, err)
		}
	}

	// Create a hidden file that should be skipped
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("junk"), 0644); err != nil {
		t.Fatalf("writing .DS_Store: %v", err)
	}

	// Create a subdirectory that should be skipped
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("creating .git dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillManifest(dir)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runSkillManifest returned error: %v", err)
	}

	// Read captured output
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Parse YAML output
	var result manifestOutput
	if err := yaml.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse YAML output: %v\nOutput was:\n%s", err, output)
	}

	// Verify correct number of files (3 regular files, no hidden, no dirs)
	if len(result.Files) != 3 {
		t.Errorf("expected 3 files, got %d: %+v", len(result.Files), result.Files)
	}

	// Verify alphabetical order
	expectedOrder := []string{"SKILL.md", "helper.sh", "pdf_tools.py"}
	for i, expected := range expectedOrder {
		if i >= len(result.Files) {
			break
		}
		if result.Files[i].Path != expected {
			t.Errorf("file[%d]: expected name %q, got %q", i, expected, result.Files[i].Path)
		}
	}

	// Verify SHA256 hashes are correct
	for _, f := range result.Files {
		content, ok := files[f.Path]
		if !ok {
			t.Errorf("unexpected file in manifest: %s", f.Path)
			continue
		}
		expectedHash := sha256Hex([]byte(content))
		if f.SHA256 != expectedHash {
			t.Errorf("file %s: expected hash %s, got %s", f.Path, expectedHash, f.SHA256)
		}
	}
}

func TestRunSkillManifest_HiddenFilesSkipped(t *testing.T) {
	dir := t.TempDir()

	// Only hidden files
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.tmp"), 0644); err != nil {
		t.Fatal(err)
	}
	// One visible file
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillManifest(dir)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)

	var result manifestOutput
	if err := yaml.Unmarshal(buf[:n], &result); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if len(result.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(result.Files))
	}
	if len(result.Files) > 0 && result.Files[0].Path != "visible.txt" {
		t.Errorf("expected visible.txt, got %s", result.Files[0].Path)
	}
}

func TestRunSkillManifest_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillManifest(dir)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)

	var result manifestOutput
	if err := yaml.Unmarshal(buf[:n], &result); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if result.Files == nil {
		// nil is acceptable for empty
	} else if len(result.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(result.Files))
	}
}

func TestRunSkillManifest_NotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	err := runSkillManifest(f)
	if err == nil {
		t.Fatal("expected error for non-directory argument")
	}
}

func TestRunSkillManifest_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.md"), []byte("# Test"), 0644); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runSkillManifest(dir)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Verify it's valid YAML that can round-trip through models
	var generic map[string][]models.SkillSourceFile
	if err := yaml.Unmarshal([]byte(output), &generic); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, output)
	}

	filesSlice, ok := generic["files"]
	if !ok {
		t.Fatal("YAML output missing 'files' key")
	}
	if len(filesSlice) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(filesSlice))
	}
	if filesSlice[0].Path != "test.md" {
		t.Errorf("expected file name 'test.md', got %q", filesSlice[0].Path)
	}
	if filesSlice[0].SHA256 == "" {
		t.Error("expected non-empty SHA256 hash")
	}
}

// sha256Hex is defined in install_pack.go, reused here.
