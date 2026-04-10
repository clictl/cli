// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
)

var cleanupDryRun bool
var cleanupAll bool

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove old cached data and unused files",
	Long: `Clean up cached specs, stale lock entries, and orphaned files.

  clictl cleanup            # remove stale cache and orphaned files
  clictl cleanup --dry-run  # show what would be removed
  clictl cleanup --all      # also clear the entire spec cache`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		cliDir := config.BaseDir()

		var totalFiles int
		var totalBytes int64

		// 1. Clean stale cached specs (older than 7 days)
		specCacheDir := filepath.Join(cfg.CacheDir, "specs")
		staleCount, staleBytes := cleanStaleCache(specCacheDir, 7*24*time.Hour, cleanupAll, cleanupDryRun)
		totalFiles += staleCount
		totalBytes += staleBytes

		// 2. Clean orphaned lock entries
		lockOrphans := cleanOrphanedLockEntries(cliDir, cleanupDryRun)
		totalFiles += lockOrphans

		// 3. Clean orphaned memory files
		memOrphans, memBytes := cleanOrphanedMemory(cliDir, cleanupDryRun)
		totalFiles += memOrphans
		totalBytes += memBytes

		// 4. Clean orphaned SKILL.md files
		skillOrphans, skillBytes := cleanOrphanedSkills(cleanupDryRun)
		totalFiles += skillOrphans
		totalBytes += skillBytes

		// 5. Clean stale etags
		etagsPath := filepath.Join(cfg.CacheDir, "etags.json")
		if cleanupAll && !cleanupDryRun {
			if info, err := os.Stat(etagsPath); err == nil {
				totalBytes += info.Size()
				totalFiles++
				os.Remove(etagsPath)
				fmt.Println("Cleared etags cache.")
			}
		}

		// Summary
		if totalFiles == 0 {
			fmt.Println("Nothing to clean up.")
		} else if cleanupDryRun {
			fmt.Printf("\nWould remove %d files (%s).\n", totalFiles, formatBytes(totalBytes))
			fmt.Println("Run without --dry-run to apply.")
		} else {
			fmt.Printf("\nRemoved %d files. Freed %s.\n", totalFiles, formatBytes(totalBytes))
		}

		// Record cleanup time
		if !cleanupDryRun && totalFiles > 0 {
			cleanupTimePath := filepath.Join(cliDir, ".last-cleanup")
			os.WriteFile(cleanupTimePath, []byte(time.Now().Format(time.RFC3339)), 0o600)
		}

		return nil
	},
}

func cleanStaleCache(dir string, maxAge time.Duration, removeAll bool, dryRun bool) (int, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}

	cutoff := time.Now().Add(-maxAge)
	var count int
	var bytes int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if removeAll || info.ModTime().Before(cutoff) {
			size := info.Size()
			path := filepath.Join(dir, entry.Name())

			if dryRun {
				fmt.Printf("  Would remove: %s (%s)\n", entry.Name(), formatBytes(size))
			} else {
				os.Remove(path)
				fmt.Printf("  Removed: %s (%s)\n", entry.Name(), formatBytes(size))
			}
			count++
			bytes += size
		}
	}

	if count > 0 {
		action := "Would remove"
		if !dryRun {
			action = "Removed"
		}
		fmt.Printf("%s %d cached specs.\n", action, count)
	}

	return count, bytes
}

func cleanOrphanedLockEntries(cliDir string, dryRun bool) int {
	lockFile, err := LoadLockFile()
	if err != nil || lockFile == nil || len(lockFile.Tools) == 0 {
		return 0
	}

	installed := loadInstalled()
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}

	var orphans []string
	for name := range lockFile.Tools {
		if !installedSet[name] {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) == 0 {
		return 0
	}

	for _, name := range orphans {
		if dryRun {
			fmt.Printf("  Would remove lock entry: %s (not installed)\n", name)
		} else {
			delete(lockFile.Tools, name)
			fmt.Printf("  Removed lock entry: %s (not installed)\n", name)
		}
	}

	if !dryRun {
		saveLockFile(lockFile, cliDir)
	}

	return len(orphans)
}

func cleanOrphanedMemory(cliDir string, dryRun bool) (int, int64) {
	memDir := filepath.Join(cliDir, "memory")
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return 0, 0
	}

	installed := loadInstalled()
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}

	var count int
	var bytes int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !installedSet[name] {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			size := info.Size()
			path := filepath.Join(memDir, entry.Name())

			if dryRun {
				fmt.Printf("  Would remove memory: %s (%s)\n", name, formatBytes(size))
			} else {
				os.Remove(path)
				fmt.Printf("  Removed memory: %s (%s)\n", name, formatBytes(size))
			}
			count++
			bytes += size
		}
	}

	return count, bytes
}

func saveLockFile(lf *LockFile, cliDir string) {
	path := filepath.Join(cliDir, "lock.yaml")
	data := "# clictl lock file - generated by clictl lock\n"
	data += fmt.Sprintf("generated_at: %s\n", lf.GeneratedAt)
	data += "tools:\n"
	for name, entry := range lf.Tools {
		data += fmt.Sprintf("  %s:\n    version: %q\n    etag: %q\n", name, entry.Version, entry.ETag)
	}
	os.WriteFile(path, []byte(data), 0o600)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func cleanOrphanedSkills(dryRun bool) (int, int64) {
	installed := loadInstalled()
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}

	var totalCount int
	var totalBytes int64

	// Check each skill target directory for orphans
	skillDirs := []string{
		filepath.Join(".claude", "skills"),
		filepath.Join(".cursor", "skills"),
	}

	for _, dir := range skillDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if installedSet[entry.Name()] {
				continue
			}
			dirPath := filepath.Join(dir, entry.Name())
			size := dirSize(dirPath)
			if dryRun {
				fmt.Printf("  Would remove orphaned skill: %s/%s (%s)\n", dir, entry.Name(), formatBytes(size))
			} else {
				os.RemoveAll(dirPath)
				fmt.Printf("  Removed orphaned skill: %s/%s (%s)\n", dir, entry.Name(), formatBytes(size))
			}
			totalCount++
			totalBytes += size
		}
	}

	return totalCount, totalBytes
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// cleanupInterval returns the auto-cleanup interval from CLICTL_CLEANUP_INTERVAL
// env var (in days) or defaults to 30 days.
func cleanupInterval() time.Duration {
	if s := os.Getenv("CLICTL_CLEANUP_INTERVAL"); s != "" {
		if days, err := strconv.Atoi(s); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	return 30 * 24 * time.Hour
}

// ShouldAutoCleanup returns true if cleanup hasn't been run within the
// configured interval (default 30 days, configurable via CLICTL_CLEANUP_INTERVAL).
func ShouldAutoCleanup() bool {
	path := filepath.Join(config.BaseDir(), ".last-cleanup")
	data, err := os.ReadFile(path)
	if err != nil {
		return true // never run
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return time.Since(t) > cleanupInterval()
}

func init() {
	cleanupCmd.Flags().BoolVar(&cleanupDryRun, "dry-run", false, "Show what would be removed without removing")
	cleanupCmd.Flags().BoolVar(&cleanupAll, "all", false, "Also clear the entire spec cache")
	rootCmd.AddCommand(cleanupCmd)
}
