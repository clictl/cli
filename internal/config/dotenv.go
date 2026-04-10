// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv loads environment variables from .env files.
// Files are loaded in order (first found wins per variable):
//  1. .env in the current directory
//  2. .env in the project root (nearest parent with .git)
//  3. ~/.clictl/.env (global defaults)
//
// Existing environment variables take precedence over .env values.
// Returns the number of variables loaded.
func LoadDotEnv() int {
	loaded := 0

	paths := dotenvPaths()
	for _, path := range paths {
		n, err := loadEnvFile(path)
		if err != nil {
			continue
		}
		loaded += n
	}

	return loaded
}

// dotenvPaths returns .env file paths to check, in priority order.
func dotenvPaths() []string {
	var paths []string

	// 1. Current directory
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".env"))
	}

	// 2. Project root (nearest parent with .git)
	if root := findProjectRoot(); root != "" {
		cwd, _ := os.Getwd()
		rootEnv := filepath.Join(root, ".env")
		// Only add if different from cwd
		if cwd == "" || filepath.Join(cwd, ".env") != rootEnv {
			paths = append(paths, rootEnv)
		}
	}

	// 3. Global defaults
	paths = append(paths, filepath.Join(BaseDir(), ".env"))

	return paths
}

// findProjectRoot walks up from cwd looking for a .git directory.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// loadEnvFile reads a single .env file and sets environment variables.
// Variables already set in the environment are not overwritten.
func loadEnvFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Warn if .env is tracked by git
	checkGitTracked(path)

	loaded := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Strip surrounding quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Don't overwrite existing env vars
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		os.Setenv(key, value)
		loaded++
	}

	return loaded, scanner.Err()
}

// EnforceDotEnvSafety re-checks loaded .env files and returns an error if any
// are potentially tracked by git. Intended for strict/enterprise mode where
// tracked .env files are not allowed.
func EnforceDotEnvSafety() error {
	for _, path := range dotenvPaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if isGitTracked(path) {
			return fmt.Errorf("%s appears to be tracked by git; refusing to use it in strict mode (add '.env' to .gitignore)", path)
		}
	}
	return nil
}

// isGitTracked returns true if the .env file at path appears to be in a git
// repo without a .gitignore entry for .env.
func isGitTracked(path string) bool {
	dir := filepath.Dir(path)

	// Walk up to find .git
	found := false
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			found = true
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if !found {
		return false
	}

	// Check .gitignore for .env
	gitignore := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		return true // No .gitignore means .env might be tracked
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == ".env" || line == "*.env" || line == ".env*" {
			return false // .env is in .gitignore
		}
	}
	return true
}

// checkGitTracked warns if the .env file is tracked by git.
func checkGitTracked(path string) {
	name := filepath.Base(path)
	if isGitTracked(path) {
		fmt.Fprintf(os.Stderr, "Warning: %s may be tracked by git. Add '.env' to .gitignore.\n", name)
	}
}
