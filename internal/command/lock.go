// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

// LockFile represents the structure of ~/.clictl/lock.yaml.
type LockFile struct {
	Tools       map[string]LockEntry `yaml:"tools"`
	GeneratedAt string               `yaml:"generated_at"`
}

// LockEntry represents a single locked tool with its pinned version and etag.
type LockEntry struct {
	Version       string `yaml:"version"`
	ETag          string `yaml:"etag"`
	ContentSHA256 string `yaml:"content_sha256,omitempty"`
	PinnedVersion string `yaml:"pinned_version,omitempty"`
	QualifiedName string `yaml:"qualified_name,omitempty"`
	Alias         string `yaml:"alias,omitempty"`
	Source        string `yaml:"source,omitempty"`
}

// lockFilePath returns the path to the lock file (~/.clictl/lock.yaml).
func lockFilePath() string {
	return filepath.Join(config.BaseDir(), "lock.yaml")
}

// projectLockFilePath returns the path to the project-level lock file (.clictl/installed.yaml).
func projectLockFilePath() string {
	return filepath.Join(".clictl", "installed.yaml")
}

// LoadProjectLockFile reads and parses the project-level lock file at .clictl/installed.yaml.
// Returns nil if the file does not exist.
func LoadProjectLockFile() (*LockFile, error) {
	path := projectLockFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading project lock file: %w", err)
	}

	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing project lock file: %w", err)
	}
	return &lf, nil
}

// writeProjectLockFile writes a LockFile to .clictl/installed.yaml.
func writeProjectLockFile(lf *LockFile) error {
	path := projectLockFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating project lock file dir: %w", err)
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling project lock file: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

// ResolveToolFromLockFiles looks up a tool name in the resolution order:
// 1. Project skill (.claude/skills/)
// 2. Project install (.clictl/installed.yaml)
// 3. Global install (~/.clictl/lock.yaml)
// Returns the lock entry and source level if found.
func ResolveToolFromLockFiles(name string) (*LockEntry, string) {
	// 1. Project skill
	skillDir := filepath.Join(".claude", "skills", name)
	skillMD := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMD); err == nil {
		return &LockEntry{}, "project-skill"
	}

	// 2. Project lock file
	projectLock, err := LoadProjectLockFile()
	if err == nil && projectLock != nil {
		if entry, ok := projectLock.Tools[name]; ok {
			return &entry, "project-install"
		}
	}

	// 3. Global lock file
	globalLock, err := LoadLockFile()
	if err == nil && globalLock != nil {
		if entry, ok := globalLock.Tools[name]; ok {
			return &entry, "global-install"
		}
	}

	return nil, ""
}

// writeLockFile writes a LockFile to ~/.clictl/lock.yaml with 0600 permissions.
func writeLockFile(lf *LockFile) error {
	path := lockFilePath()
	if path == "" {
		return fmt.Errorf("could not determine lock file path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating lock file dir: %w", err)
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

// LoadLockFile reads and parses the lock file from ~/.clictl/lock.yaml.
// Returns nil if the file does not exist.
func LoadLockFile() (*LockFile, error) {
	path := lockFilePath()
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading lock file: %w", err)
	}

	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}
	return &lf, nil
}

// computeETag generates a sha256 etag from raw spec bytes.
func computeETag(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// computeContentSHA256 returns the raw hex SHA256 of spec content for lock file verification.
func computeContentSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// GenerateLockFile creates or updates the lock file for all installed tools.
// This is called automatically after install, uninstall, and upgrade operations.
func GenerateLockFile(ctx context.Context, cfg *config.Config) error {
	installed := loadInstalled()
	if len(installed) == 0 {
		return nil
	}

	cache := registry.NewCache(cfg.CacheDir)
	client := registry.NewClient(cfg.APIURL, cache, false)

	token := config.ResolveAuthToken("", cfg)
	if token != "" {
		client.AuthToken = token
	}

	lockFile := LockFile{
		Tools:       make(map[string]LockEntry),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, name := range installed {
		spec, rawYAML, err := client.GetSpecYAML(ctx, name)
		if err != nil {
			// Skip tools that cannot be fetched
			continue
		}

		qualifiedName := ""
		if spec.Namespace != "" {
			qualifiedName = spec.Namespace + "/" + spec.Name
		}

		contentHash := computeContentSHA256(rawYAML)
		entry := LockEntry{
			Version:       spec.Version,
			ETag:          computeETag(rawYAML),
			ContentSHA256: contentHash,
			QualifiedName: qualifiedName,
		}
		// Preserve pinned version from install args
		if pv, ok := pinnedVersions[name]; ok {
			entry.PinnedVersion = pv
		}
		lockFile.Tools[name] = entry
	}

	return writeLockFile(&lockFile)
}
