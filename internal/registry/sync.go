// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/clictl/cli/internal/models"
)

// SyncAPI fetches the index from an API registry.
// GET {baseURL}/api/v1/index/ -> returns Index JSON
func SyncAPI(ctx context.Context, baseURL string, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating registry dir: %w", err)
	}

	u := fmt.Sprintf("%s/api/v1/index/", baseURL)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("creating index request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching index from %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("index API returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading index response: %w", err)
	}

	var idx models.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return fmt.Errorf("parsing index response: %w", err)
	}

	formatted, err := json.MarshalIndent(&idx, "", "  ")
	if err != nil {
		return fmt.Errorf("formatting index: %w", err)
	}

	indexPath := filepath.Join(destDir, "index.json")
	if err := os.WriteFile(indexPath, formatted, 0o644); err != nil {
		return fmt.Errorf("writing index file: %w", err)
	}

	return nil
}

// SyncGit clones or pulls a git registry.
// If destDir doesn't exist: git clone --depth 1 <url> <destDir>
// If destDir exists: git -C <destDir> pull --ff-only
func SyncGit(ctx context.Context, url string, branch string, destDir string) error {
	_, err := os.Stat(destDir)
	if os.IsNotExist(err) {
		args := []string{"clone", "--depth", "1"}
		if branch != "" {
			args = append(args, "--branch", branch)
		}
		args = append(args, url, destDir)

		cmd := exec.CommandContext(ctx, "git", args...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking registry dir: %w", err)
	}

	// Fetch and reset instead of pull to avoid multi-branch issues
	targetBranch := branch
	if targetBranch == "" {
		targetBranch = "main"
	}
	fetchCmd := exec.CommandContext(ctx, "git", "-C", destDir, "fetch", "--depth", "1", "origin", targetBranch)
	fetchCmd.Stdout = nil
	fetchCmd.Stderr = nil
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	resetCmd := exec.CommandContext(ctx, "git", "-C", destDir, "reset", "--hard", "FETCH_HEAD")
	resetCmd.Stdout = nil
	resetCmd.Stderr = nil
	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("git reset failed: %w", err)
	}

	return nil
}
