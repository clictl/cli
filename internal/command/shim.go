// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/clictl/cli/internal/config"
)

// shimBinDir returns the directory where shims are installed.
// On Unix: ~/.clictl/bin/
// On Windows: ~/.clictl/bin/
func shimBinDir() string {
	return filepath.Join(config.BaseDir(), "bin")
}

// generateShim creates a shim script that delegates to `clictl run <tool> <action>`.
// The shim replaces direct executable scripts with a thin wrapper that routes
// through clictl's run command, enabling sandbox enforcement and version management.
func generateShim(toolName string) error {
	dir := shimBinDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating shim directory: %w", err)
	}

	shimPath := filepath.Join(dir, toolName)
	if runtime.GOOS == "windows" {
		shimPath += ".cmd"
	}

	content := generateShimContent(toolName)
	if err := os.WriteFile(shimPath, []byte(content), 0o755); err != nil {
		return fmt.Errorf("writing shim: %w", err)
	}

	return nil
}

// generateShimContent creates the shim script content for a tool.
func generateShimContent(toolName string) string {
	if runtime.GOOS == "windows" {
		return generateWindowsShim(toolName)
	}
	return generateUnixShim(toolName)
}

// generateUnixShim creates a POSIX shell shim script.
func generateUnixShim(toolName string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# clictl shim - auto-generated, do not edit\n")
	sb.WriteString(fmt.Sprintf("# Tool: %s\n", toolName))
	sb.WriteString("set -e\n\n")

	// Resolve the clictl binary path
	sb.WriteString("CLICTL=\"")
	if exe, err := os.Executable(); err == nil {
		sb.WriteString(exe)
	} else {
		sb.WriteString("clictl")
	}
	sb.WriteString("\"\n\n")

	// The shim name may encode tool/action as "tool-action" or just "tool"
	sb.WriteString("# Determine action from arguments or shim name\n")
	sb.WriteString("if [ $# -ge 1 ]; then\n")
	sb.WriteString(fmt.Sprintf("  exec \"$CLICTL\" run %s \"$@\"\n", toolName))
	sb.WriteString("else\n")
	sb.WriteString(fmt.Sprintf("  echo \"Usage: %s <action> [--param value ...]\"\n", toolName))
	sb.WriteString("  exit 1\n")
	sb.WriteString("fi\n")

	return sb.String()
}

// generateWindowsShim creates a Windows batch shim.
func generateWindowsShim(toolName string) string {
	var sb strings.Builder
	sb.WriteString("@echo off\n")
	sb.WriteString("REM clictl shim - auto-generated, do not edit\n")
	sb.WriteString(fmt.Sprintf("REM Tool: %s\n", toolName))

	clictl := "clictl"
	if exe, err := os.Executable(); err == nil {
		clictl = exe
	}

	sb.WriteString(fmt.Sprintf("\"%s\" run %s %%*\n", clictl, toolName))
	return sb.String()
}

// removeShim deletes the shim for a tool.
func removeShim(toolName string) error {
	dir := shimBinDir()
	shimPath := filepath.Join(dir, toolName)
	if runtime.GOOS == "windows" {
		shimPath += ".cmd"
	}

	err := os.Remove(shimPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing shim: %w", err)
	}
	return nil
}

// shimExists checks if a shim already exists for a tool.
func shimExists(toolName string) bool {
	dir := shimBinDir()
	shimPath := filepath.Join(dir, toolName)
	if runtime.GOOS == "windows" {
		shimPath += ".cmd"
	}
	_, err := os.Stat(shimPath)
	return err == nil
}
