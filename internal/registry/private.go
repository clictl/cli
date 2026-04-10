// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
)

// privateRepoDir returns the path where a private repo is cached locally.
func privateRepoDir(sourceID string) string {
	return filepath.Join(config.BaseDir(), "private-repos", sourceID)
}

// FetchPrivateSpec fetches a spec from a private repo using the user's local
// git credentials. Checks the bbolt cache first; if stale or missing, clones/pulls
// the repo and reads the spec from the filesystem. This is used when the backend
// only has metadata (spec_yaml="" for metadata-only synced private repos).
func FetchPrivateSpec(ctx context.Context, name, repoURL, branch string, sourceID string, specCount int, lastSynced string) (*models.ToolSpec, error) {
	// Try bbolt cache first
	cache, cacheErr := OpenPrivateSpecCache()
	if cacheErr == nil {
		defer cache.Close()
		if yamlContent, ok := cache.Get(name, sourceID, specCount, lastSynced); ok {
			spec, err := ParseSpec([]byte(yamlContent))
			if err == nil {
				return spec, nil
			}
		}
	}

	// Cache miss or stale - fetch from git
	repoDir := privateRepoDir(sourceID)

	if err := ensurePrivateRepo(ctx, repoURL, branch, repoDir); err != nil {
		return nil, fmt.Errorf("fetching private repo: %w", err)
	}

	spec, err := findSpecInRepo(repoDir, name)
	if err != nil {
		return nil, fmt.Errorf("finding spec %q in private repo: %w", name, err)
	}

	// Cache the result for next time
	if cache != nil {
		// Read the raw YAML for caching
		if yamlData, readErr := readSpecYAMLFromRepo(repoDir, name); readErr == nil {
			cache.Put(name, string(yamlData), sourceID, specCount, lastSynced)
		}
	}

	return spec, nil
}

// readSpecYAMLFromRepo reads the raw YAML content for a spec from a repo.
func readSpecYAMLFromRepo(repoDir, name string) ([]byte, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("empty spec name")
	}
	prefix := string(name[0])
	candidates := []string{
		filepath.Join(repoDir, "specs", prefix, name, name+".yaml"),
		filepath.Join(repoDir, "specs", prefix, name+".yaml"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("spec YAML not found")
}

// ensurePrivateRepo clones or pulls a private repo using the user's local git credentials.
func ensurePrivateRepo(ctx context.Context, repoURL, branch, destDir string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		// Repo exists, pull latest
		cmd := exec.CommandContext(timeoutCtx, "git", "-C", destDir, "fetch", "--depth", "1", "origin")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
		ref := "origin/main"
		if branch != "" {
			ref = "origin/" + branch
		}
		cmd = exec.CommandContext(timeoutCtx, "git", "-C", destDir, "reset", "--hard", ref)
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Clone fresh
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return err
	}
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, destDir)
	cmd := exec.CommandContext(timeoutCtx, "git", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findSpecInRepo searches for a spec by name in a cloned repo.
// Tries: specs/{prefix}/{name}/{name}.yaml, specs/{prefix}/{name}.yaml,
// then falls back to scanning all YAML files.
func findSpecInRepo(repoDir, name string) (*models.ToolSpec, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("empty spec name")
	}

	prefix := string(name[0])

	// Try new per-tool folder structure: specs/{prefix}/{name}/{name}.yaml
	candidates := []string{
		filepath.Join(repoDir, "specs", prefix, name, name+".yaml"),
		filepath.Join(repoDir, "specs", prefix, name+".yaml"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		spec, err := ParseSpec(data)
		if err != nil {
			continue
		}
		if spec.Name == name {
			return spec, nil
		}
	}

	// Fall back: scan all YAML files
	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		spec, parseErr := ParseSpec(data)
		if parseErr != nil {
			return nil
		}
		if spec.Name == name {
			// Found it - return via error to break the walk
			return &specFoundError{spec: spec}
		}
		return nil
	})

	if found, ok := err.(*specFoundError); ok {
		return found.spec, nil
	}

	return nil, fmt.Errorf("spec %q not found in repo", name)
}

// specFoundError is used to break filepath.Walk when a spec is found.
type specFoundError struct {
	spec *models.ToolSpec
}

// Error returns the error string.
func (e *specFoundError) Error() string {
	return "spec found"
}

// CleanPrivateRepoCache removes the cached clone for a private repo source.
func CleanPrivateRepoCache(sourceID string) error {
	return os.RemoveAll(privateRepoDir(sourceID))
}
