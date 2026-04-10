// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CliCtlConfig represents the .clictl.yaml file found in a project root.
// It configures how `clictl toolbox sync` discovers and pushes tool specs.
type CliCtlConfig struct {
	Namespace string   `yaml:"namespace"`
	SpecPaths []string `yaml:"spec_paths"`
	Branches  []string `yaml:"branches"`
}

// LoadCliCtlConfig reads .clictl.yaml from the toolbox/ folder or project root.
// Search order:
//  1. toolbox/.clictl.yaml (in cwd)
//  2. .clictl.yaml (in cwd, legacy)
//  3. toolbox/.clictl.yaml (in git root)
//  4. .clictl.yaml (in git root, legacy)
//
// Missing optional fields are filled with sensible defaults.
func LoadCliCtlConfig() (*CliCtlConfig, error) {
	paths := []string{}

	cwd, err := os.Getwd()
	if err == nil {
		paths = append(paths, filepath.Join(cwd, "toolbox", ".clictl.yaml"))
		paths = append(paths, filepath.Join(cwd, ".clictl.yaml"))
	}

	gitRoot, err := gitRepoRoot()
	if err == nil && gitRoot != cwd {
		paths = append(paths, filepath.Join(gitRoot, "toolbox", ".clictl.yaml"))
		paths = append(paths, filepath.Join(gitRoot, ".clictl.yaml"))
	}

	var data []byte
	var foundPath string
	for _, p := range paths {
		d, err := os.ReadFile(p)
		if err == nil {
			data = d
			foundPath = p
			break
		}
	}

	if foundPath == "" {
		return nil, fmt.Errorf(".clictl.yaml not found; looked in toolbox/ and project root")
	}

	cfg := &CliCtlConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing .clictl.yaml: %w", err)
	}

	// Apply defaults for missing fields.
	if len(cfg.SpecPaths) == 0 {
		configDir := filepath.Dir(foundPath)

		if strings.HasSuffix(configDir, "toolbox") {
			// Config is inside toolbox/ folder - scan siblings
			cfg.SpecPaths = []string{configDir}
		} else {
			// Config is at repo root - check if toolbox/ subfolder exists
			toolboxDir := filepath.Join(configDir, "toolbox")
			if info, err := os.Stat(toolboxDir); err == nil && info.IsDir() {
				cfg.SpecPaths = []string{toolboxDir}
			} else {
				// No toolbox/ subfolder - scan repo root (official toolbox pattern)
				cfg.SpecPaths = []string{configDir}
			}
		}
	}
	if len(cfg.Branches) == 0 {
		cfg.Branches = []string{"main", "master"}
	}

	return cfg, nil
}

// gitRepoRoot returns the top-level directory of the current git repository.
func gitRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
