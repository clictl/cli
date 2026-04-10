// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/telemetry"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check your clictl installation for common issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		issues := 0

		cfg, cfgErr := config.Load()
		if cfgErr == nil {
			cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)
		}

		fmt.Println("Checking clictl...")
		fmt.Println()

		// 1. CLI version and update check
		if cfg != nil && cfg.Update.LatestVersion != "" && cfg.Update.LatestVersion != Version {
			printWarning("Version: %s (update available: %s)", Version, cfg.Update.LatestVersion)
			fmt.Printf("    Run: clictl update\n")
			issues++
		} else {
			printOK("Version: %s (up to date)", Version)
		}

		// 2. Config file
		configPath := filepath.Join(config.BaseDir(), "config.yaml")
		if cfgErr != nil {
			printFail("Config: %s (error: %v)", configPath, cfgErr)
			issues++
		} else if _, err := os.Stat(configPath); os.IsNotExist(err) {
			printWarning("Config: %s (not found, using defaults)", configPath)
		} else {
			printOK("Config: %s (valid)", configPath)
		}

		// 3. Cache directory
		if cfg != nil {
			cacheSize, cacheCount := dirSizeAndCount(cfg.CacheDir)
			if _, err := os.Stat(cfg.CacheDir); os.IsNotExist(err) {
				printWarning("Cache: %s (directory not found)", cfg.CacheDir)
			} else {
				printOK("Cache: %s (%d specs cached, %s)", cfg.CacheDir, cacheCount, formatSize(cacheSize))
			}
		}

		// 4. Memory directory
		memoryDir := filepath.Join(config.BaseDir(), "memory")
		_, memCount := dirSizeAndCount(memoryDir)
		if _, err := os.Stat(memoryDir); os.IsNotExist(err) {
			printOK("Memory: %s (no memories yet)", memoryDir)
		} else {
			printOK("Memory: %s (%d tools with memories)", memoryDir, memCount)
		}

		// 5. Toolboxes
		if cfg != nil && len(cfg.Toolboxes) > 0 {
			totalSpecs := 0
			for _, reg := range cfg.Toolboxes {
				regDir := filepath.Join(config.ToolboxesDir(), reg.Name)
				idx := registry.NewLocalIndex(regDir, reg.Name)
				if index, err := idx.Load(); err == nil {
					totalSpecs += len(index.Specs)
				}
			}
			syncInfo := "never synced"
			if cfg.Update.LastSyncAt != "" {
				if t, err := time.Parse(time.RFC3339, cfg.Update.LastSyncAt); err == nil {
					syncInfo = fmt.Sprintf("last synced %s", formatTimeAgo(t))
					if time.Since(t) > 7*24*time.Hour {
						printWarning("Toolboxes: %d configured, %d specs (%s - consider running 'clictl update')", len(cfg.Toolboxes), totalSpecs, syncInfo)
						issues++
					} else {
						printOK("Toolboxes: %d configured, %d specs (%s)", len(cfg.Toolboxes), totalSpecs, syncInfo)
					}
				} else {
					printOK("Toolboxes: %d configured, %d specs", len(cfg.Toolboxes), totalSpecs)
				}
			} else {
				printWarning("Toolboxes: %d configured, %d specs (%s)", len(cfg.Toolboxes), totalSpecs, syncInfo)
			}
		}

		// 6. Installed tools
		installed := loadInstalled()
		printOK("Installed tools: %d", len(installed))

		// 6b. Telemetry status
		if telemetry.Enabled() {
			printOK("Telemetry: enabled")
		} else {
			printOK("Telemetry: disabled (set telemetry: true in config to enable)")
		}

		// 7. Stale cache files (older than 30 days)
		if cfg != nil {
			specCacheDir := filepath.Join(cfg.CacheDir, "specs")
			staleCount := countStaleFiles(specCacheDir, 30*24*time.Hour)
			if staleCount > 0 {
				printWarning("Stale cache: %d spec files older than 30 days", staleCount)
				fmt.Printf("    Run: clictl cleanup\n")
				issues++
			} else {
				printOK("Cache freshness: all specs within 30 days")
			}
		}

		// 8. Lock file integrity
		lockFile, lockErr := LoadLockFile()
		if lockErr != nil {
			printOK("Lock file: not found (optional)")
		} else if lockFile != nil {
			lockIssues := 0
			for name, entry := range lockFile.Tools {
				if entry.Version == "" {
					printWarning("Lock file: %s has no version", name)
					lockIssues++
				}
				if entry.ETag == "" {
					printWarning("Lock file: %s has no etag", name)
					lockIssues++
				}
			}
			if lockIssues == 0 {
				printOK("Lock file: %d entries, all valid", len(lockFile.Tools))
			} else {
				fmt.Printf("    Run: clictl lock\n")
				issues += lockIssues
			}

			// 9. Installed tools match lock file versions
			for _, name := range installed {
				if entry, ok := lockFile.Tools[name]; ok {
					if cfg != nil {
						cache := registry.NewCache(cfg.CacheDir)
						spec, err := registry.ResolveSpec(cmd.Context(), name, cfg, cache, flagNoCache)
						if err == nil && spec.Version != entry.Version {
							printWarning("Version mismatch: %s installed=%s lock=%s", name, spec.Version, entry.Version)
							issues++
						}
					}
				}
			}
		}

		// 10. Orphaned SKILL.md files
		orphanedSkills := countOrphanedSkills(installed)
		if orphanedSkills > 0 {
			printWarning("Orphaned skills: %d skill directories for uninstalled tools", orphanedSkills)
			fmt.Printf("    Run: clictl cleanup\n")
			issues++
		} else {
			printOK("Skills: no orphaned skill files")
		}

		fmt.Println()
		fmt.Println("Checking tools...")

		// 7. Common tool binaries
		commonTools := []struct {
			name string
			bin  string
		}{
			{"docker", "docker"},
			{"git", "git"},
			{"kubectl", "kubectl"},
			{"terraform", "terraform"},
		}
		for _, tool := range commonTools {
			path, err := exec.LookPath(tool.bin)
			if err != nil {
				printFail("%s: NOT FOUND", tool.name)
			} else {
				// Try to get version
				ver := getToolVersion(tool.bin)
				if ver != "" {
					printOK("%s: installed (%s)", tool.name, ver)
				} else {
					printOK("%s: installed (%s)", tool.name, path)
				}
			}
		}

		// 8. Package runtimes
		fmt.Println()
		fmt.Println("Runtimes:")

		clearRuntimeCache()
		for _, reg := range []struct {
			registry string
			label    string
		}{
			{"npm", "node"},
			{"pypi", "python"},
		} {
			rt, rtErr := DetectRuntime(reg.registry)
			if rtErr != nil {
				printFail("%-8s NOT FOUND", reg.label)
			} else {
				printOK("%-8s %-12s %s", reg.label, rt.Version, rt.Command)
			}
		}

		// Also show uvx, npx, docker via cached detection
		resetRuntimeInfoCache()
		runtimes := DetectRuntimes()
		for _, name := range []string{"uvx", "npx", "docker"} {
			ri := runtimes[name]
			if ri.Available {
				printOK("%-8s installed (%s) at %s", name, ri.Version, ri.Path)
			} else {
				printFail("%-8s not installed", name)
			}
		}

		// 9. Auth status
		fmt.Println()
		fmt.Println("Checking auth...")

		if cfg != nil {
			token := config.ResolveAuthToken("", cfg)
			if token == "" {
				printOK("Not logged in (login is optional)")
			} else {
				// Check token expiry
				if cfg.Auth.ExpiresAt != "" {
					if t, err := time.Parse(time.RFC3339, cfg.Auth.ExpiresAt); err == nil {
						if time.Now().After(t) {
							printFail("Auth token expired at %s", t.Format("2006-01-02 15:04"))
							fmt.Printf("    Run: clictl login\n")
							issues++
						} else {
							printOK("Logged in (token valid until %s)", t.Format("2006-01-02 15:04"))
						}
					} else {
						printOK("Logged in")
					}
				} else {
					printOK("Logged in")
				}

				if cfg.Auth.ActiveWorkspace != "" {
					printOK("Workspace: %s", cfg.Auth.ActiveWorkspace)
				}
			}
		}

		fmt.Println()
		if issues == 0 {
			fmt.Println("No issues found.")
		} else {
			fmt.Printf("%d issue(s) found.\n", issues)
		}

		return nil
	},
}

func printOK(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("  \033[32m✓\033[0m %s\n", msg)
}

func printFail(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("  \033[31m✗\033[0m %s\n", msg)
}

func printWarning(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("  \033[33m!\033[0m %s\n", msg)
}

// getToolVersion runs "<tool> --version" and returns a trimmed version string.
func getToolVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		// Some tools use "version" instead of "--version"
		out, err = exec.Command(bin, "version").Output()
		if err != nil {
			return ""
		}
	}
	s := strings.TrimSpace(string(out))
	// Take only the first line
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		s = s[:idx]
	}
	// Trim common prefixes
	s = strings.TrimPrefix(s, "git version ")
	s = strings.TrimPrefix(s, "Docker version ")
	s = strings.TrimSuffix(s, ",")
	return s
}

func countStaleFiles(dir string, maxAge time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			count++
		}
	}
	return count
}

func countOrphanedSkills(installed []string) int {
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}

	count := 0
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
			if entry.IsDir() && !installedSet[entry.Name()] {
				count++
			}
		}
	}
	return count
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
