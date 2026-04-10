// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/clictl/cli/internal/archive"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	// packMaxArchiveBytes is the maximum .tar.gz archive size (50 MiB).
	packMaxArchiveBytes = 50 * 1024 * 1024
	// packMaxExtractedBytes is the maximum total extracted size (100 MiB).
	packMaxExtractedBytes = 100 * 1024 * 1024
)

var packOutput string
var packVersion string

var packCmd = &cobra.Command{
	Use:   "pack <directory>",
	Short: "Build a skill pack archive for testing",
	Long: `Build a .tar.gz pack from a skill directory. This is for local testing
before publishing via clictl.dev.

  clictl pack ./my-skill
  clictl pack ./my-skill --output ./dist/
  clictl pack ./my-skill --version 1.2.0`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]

		// Verify directory exists
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}

		// P2.32: enforce pack limits
		var fileCount int
		var totalSize int64
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			fileCount++
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			totalSize += info.Size()
			if totalSize > packMaxExtractedBytes {
				return fmt.Errorf("pack exceeds maximum extracted size of %d bytes (%d MiB)", packMaxExtractedBytes, packMaxExtractedBytes/(1024*1024))
			}
			return nil
		})
		if walkErr != nil {
			return walkErr
		}

		// Auto-detect type
		toolType := detectPackType(dir)

		// Read name from SKILL.md frontmatter or directory name
		name := filepath.Base(dir)
		if skillMD, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err == nil {
			if fm := extractFrontmatter(string(skillMD)); fm["name"] != "" {
				name = fm["name"]
			}
		}

		version := packVersion
		if version == "" {
			version = "0.1.0"
		}

		// Compute content hash
		contentHash, err := archive.HashDirectory(dir)
		if err != nil {
			return fmt.Errorf("computing content hash: %w", err)
		}

		// Build manifest
		manifest := map[string]interface{}{
			"schema_version": "1",
			"name":           name,
			"type":           toolType,
			"version":        version,
			"content_sha256": contentHash,
		}

		// Determine output directory
		outputDir := packOutput
		if outputDir == "" {
			outputDir = "."
		}

		archivePath, err := archive.Pack(dir, manifest, outputDir)
		if err != nil {
			return fmt.Errorf("building pack: %w", err)
		}

		// P2.32: check archive size limit
		archiveInfo, err := os.Stat(archivePath)
		if err != nil {
			return fmt.Errorf("checking archive size: %w", err)
		}
		if archiveInfo.Size() > packMaxArchiveBytes {
			os.Remove(archivePath)
			return fmt.Errorf("archive size %d bytes exceeds maximum of %d bytes (%d MiB)", archiveInfo.Size(), packMaxArchiveBytes, packMaxArchiveBytes/(1024*1024))
		}

		fmt.Printf("Pack built: %s\n", archivePath)
		fmt.Printf("  Name: %s\n", name)
		fmt.Printf("  Type: %s\n", toolType)
		fmt.Printf("  Version: %s\n", version)
		fmt.Printf("  Content SHA256: %s\n", contentHash)
		return nil
	},
}

func detectPackType(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
		return "skill"
	}
	// Check for spec YAML files
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var spec map[string]interface{}
			if yaml.Unmarshal(data, &spec) == nil {
				if p, ok := spec["protocol"].(string); ok {
					return p
				}
				if _, ok := spec["mcp"]; ok {
					return "mcp"
				}
			}
		}
	}
	return "skill" // default
}

func extractFrontmatter(content string) map[string]string {
	result := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return result
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return result
	}
	var fm map[string]interface{}
	if yaml.Unmarshal([]byte(parts[1]), &fm) == nil {
		for k, v := range fm {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
	}
	return result
}

func init() {
	packCmd.Flags().StringVar(&packOutput, "output", "", "Output directory (default: current directory)")
	packCmd.Flags().StringVar(&packVersion, "version", "", "Pack version (default: 0.1.0)")
	rootCmd.AddCommand(packCmd)
}
