// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := `# Comment
API_KEY=test123
DB_URL=postgres://localhost/test

# Quoted values
QUOTED_DOUBLE="hello world"
QUOTED_SINGLE='single quotes'

# Empty
EMPTY=
`
	os.WriteFile(envFile, []byte(content), 0600)

	// Clear any existing vars
	os.Unsetenv("API_KEY")
	os.Unsetenv("DB_URL")
	os.Unsetenv("QUOTED_DOUBLE")
	os.Unsetenv("QUOTED_SINGLE")
	os.Unsetenv("EMPTY")

	n, err := loadEnvFile(envFile)
	if err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 vars loaded, got %d", n)
	}

	if v := os.Getenv("API_KEY"); v != "test123" {
		t.Errorf("API_KEY: expected 'test123', got %q", v)
	}
	if v := os.Getenv("DB_URL"); v != "postgres://localhost/test" {
		t.Errorf("DB_URL: expected 'postgres://localhost/test', got %q", v)
	}
	if v := os.Getenv("QUOTED_DOUBLE"); v != "hello world" {
		t.Errorf("QUOTED_DOUBLE: expected 'hello world', got %q", v)
	}
	if v := os.Getenv("QUOTED_SINGLE"); v != "single quotes" {
		t.Errorf("QUOTED_SINGLE: expected 'single quotes', got %q", v)
	}

	// Cleanup
	os.Unsetenv("API_KEY")
	os.Unsetenv("DB_URL")
	os.Unsetenv("QUOTED_DOUBLE")
	os.Unsetenv("QUOTED_SINGLE")
	os.Unsetenv("EMPTY")
}

func TestExistingEnvNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	os.WriteFile(envFile, []byte("MY_VAR=from-file\n"), 0600)

	t.Setenv("MY_VAR", "from-shell")

	loadEnvFile(envFile)

	if v := os.Getenv("MY_VAR"); v != "from-shell" {
		t.Errorf("existing env should not be overwritten, got %q", v)
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	_, err := loadEnvFile("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDotenvPaths(t *testing.T) {
	paths := dotenvPaths()
	if len(paths) == 0 {
		t.Error("expected at least one dotenv path")
	}
	// First should be cwd/.env
	cwd, _ := os.Getwd()
	expected := filepath.Join(cwd, ".env")
	if paths[0] != expected {
		t.Errorf("first path should be %q, got %q", expected, paths[0])
	}
}

func TestSkipCommentsAndEmpty(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := `# This is a comment

# Another comment
VALID=yes
  # Indented comment
`
	os.WriteFile(envFile, []byte(content), 0600)
	os.Unsetenv("VALID")

	n, _ := loadEnvFile(envFile)
	if n != 1 {
		t.Errorf("expected 1 var loaded, got %d", n)
	}
	if v := os.Getenv("VALID"); v != "yes" {
		t.Errorf("VALID: expected 'yes', got %q", v)
	}

	os.Unsetenv("VALID")
}
