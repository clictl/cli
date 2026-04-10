// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/telemetry"
	"github.com/clictl/cli/internal/updater"
)

// Version is set at build time via ldflags:
//   -X github.com/clictl/cli/internal/command.Version=v0.1.0
//   -X github.com/clictl/cli/internal/command.Commit=abc1234
//   -X github.com/clictl/cli/internal/command.BuildDate=2026-04-09
var Version = "dev"

// Commit is the git commit hash, set at build time.
var Commit = ""

// BuildDate is the build timestamp, set at build time.
var BuildDate = ""

// edition is set by enterprise builds via SetEdition().
var edition = ""

// SetEdition sets the CLI edition label. Called by enterprise wrapper.
func SetEdition(e string) {
	edition = e
}

var versionShort bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the clictl version and environment info",
	Run: func(cmd *cobra.Command, args []string) {
		updater.SetVersion(Version)
		telemetry.TrackVersionCheck()

		if versionShort {
			fmt.Println(Version)
			return
		}

		versionLine := fmt.Sprintf("clictl %s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
		if Commit != "" {
			versionLine += fmt.Sprintf(" commit=%s", Commit[:min(len(Commit), 7)])
		}
		if BuildDate != "" {
			versionLine += fmt.Sprintf(" built=%s", BuildDate)
		}
		if edition != "" {
			versionLine += fmt.Sprintf(" [%s]", edition)
		}
		fmt.Println(versionLine)

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
			return
		}

		// Toolboxes
		if len(cfg.Toolboxes) > 0 {
			fmt.Println()
			fmt.Println("Toolboxes:")
			for _, reg := range cfg.Toolboxes {
				regDir := filepath.Join(config.ToolboxesDir(), reg.Name)
				idx := registry.NewLocalIndex(regDir, reg.Name)
				specCount := 0
				if index, err := idx.Load(); err == nil {
					specCount = len(index.Specs)
				}

				syncInfo := ""
				if cfg.Update.LastSyncAt != "" {
					if t, err := time.Parse(time.RFC3339, cfg.Update.LastSyncAt); err == nil {
						syncInfo = fmt.Sprintf(", synced %s", formatTimeAgo(t))
					}
				}
				fmt.Printf("  %-20s %-5s %s (%d specs%s)\n", reg.Name, reg.Type, reg.URL, specCount, syncInfo)
			}
		}

		// Installed tools
		installed := loadInstalled()
		if len(installed) > 0 {
			fmt.Println()
			fmt.Printf("Installed tools: %d\n", len(installed))
			fmt.Printf("  %s\n", strings.Join(installed, ", "))
		}

		// Config/cache/memory paths with sizes
		fmt.Println()
		configPath := filepath.Join(config.BaseDir(), "config.yaml")
		fmt.Printf("Config: %s\n", configPath)

		cacheDir := cfg.CacheDir
		cacheSize, cacheCount := dirSizeAndCount(cacheDir)
		fmt.Printf("Cache:  %s (%d specs, %s)\n", cacheDir, cacheCount, formatSize(cacheSize))

		memoryDir := filepath.Join(config.BaseDir(), "memory")
		_, memCount := dirSizeAndCount(memoryDir)
		fmt.Printf("Memory: %s (%d tools)\n", memoryDir, memCount)

		// Auth status
		fmt.Println()
		token := config.ResolveAuthToken("", cfg)
		if token != "" {
			if cfg.Auth.ActiveWorkspace != "" {
				fmt.Printf("Auth: logged in (workspace: %s)\n", cfg.Auth.ActiveWorkspace)
			} else {
				fmt.Println("Auth: logged in")
			}
		} else {
			fmt.Println("Auth: not logged in")
		}
	},
}

// formatTimeAgo returns a human-readable relative time string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// dirSizeAndCount walks a directory and returns total size in bytes and file count.
func dirSizeAndCount(dir string) (int64, int) {
	var size int64
	var count int
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
			count++
		}
		return nil
	})
	return size, count
}

// formatSize returns a human-readable size string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1fGB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func init() {
	versionCmd.Flags().BoolVar(&versionShort, "short", false, "Print just the version string")
	rootCmd.AddCommand(versionCmd)
}
