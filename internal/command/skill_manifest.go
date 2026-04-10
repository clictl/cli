// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clictl/cli/internal/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Skill authoring utilities",
	Long:  "Commands for skill authors to build, validate, and publish skills.",
}

var skillManifestCmd = &cobra.Command{
	Use:   "manifest <dir>",
	Short: "Generate a file manifest with SHA256 hashes",
	Long: `Generate a YAML manifest of all files in a directory (recursively) with their SHA256 hashes.
Used by skill authors to generate the source.files section of their spec YAML.

  clictl skill manifest ./skills/pdf/
  clictl skill manifest .

All files in subdirectories are included with relative paths. Hidden files and directories (starting with .) are skipped.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		return runSkillManifest(dir)
	},
}

// manifestOutput is the top-level YAML structure for the manifest command.
type manifestOutput struct {
	Files []models.SkillSourceFile `yaml:"files"`
}

// runSkillManifest walks a directory recursively and outputs a YAML manifest of all files with SHA256 hashes.
func runSkillManifest(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory path: %w", err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("reading directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	var files []models.SkillSourceFile
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip hidden files and directories
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip directories themselves (we only list files)
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}
		// Normalize to forward slashes for cross-platform consistency
		relPath = filepath.ToSlash(relPath)

		hash, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hashing %s: %w", relPath, err)
		}

		files = append(files, models.SkillSourceFile{
			Path:   relPath,
			SHA256: hash,
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking directory: %w", err)
	}

	// Sort alphabetically by path
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	out := manifestOutput{Files: files}
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	return enc.Close()
}

// hashFile computes the SHA256 hex digest of a file's contents.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func init() {
	skillCmd.AddCommand(skillManifestCmd)
	rootCmd.AddCommand(skillCmd)
}
